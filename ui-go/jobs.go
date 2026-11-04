package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jobPIDs maps jobID → *os.Process for cancellation support.
var jobPIDs sync.Map

// Serialize admission and process startup with shutdown. The wait group covers
// completion callbacks as well as subprocesses so state is saved before exit.
var jobLifecycleMu sync.Mutex
var jobsStopping bool
var jobsWG sync.WaitGroup
var jobIDRe = regexp.MustCompile(`^[a-f0-9]{16}$`)

func jobLogPath(jobID string) string    { return filepath.Join(jobsDir, jobID+".log") }
func jobStatusPath(jobID string) string { return filepath.Join(jobsDir, jobID+".status") }

// Parallel playbook runs ("parts") write their files next to the job log.
// The shell block from parallelConfigureShell creates them.
func jobPartsPath(jobID string) string { return filepath.Join(jobsDir, jobID+".parts") }
func jobPartFile(jobID, part, ext string) string {
	return filepath.Join(jobsDir, jobID+"."+part+"."+ext)
}

// JobPart describes one parallel playbook run within a job.
type JobPart struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Start   int64  `json:"start,omitempty"`
	End     int64  `json:"end,omitempty"`
	Elapsed int64  `json:"elapsed"`
}

func readEpochFile(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return n
}

func partStatusFinal(status string) bool {
	return status != "" && status != "running" && status != "pending"
}

// readJobParts returns the job's parallel parts in launch order, or nil when
// the job did not run playbooks in parallel.
func readJobParts(jobID string, now time.Time) []JobPart {
	data, err := os.ReadFile(jobPartsPath(jobID))
	if err != nil {
		return nil
	}
	jobStatusBytes, _ := os.ReadFile(jobStatusPath(jobID))
	jobStatus := strings.TrimSpace(string(jobStatusBytes))
	var parts []JobPart
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(line)
		if !validJobPartName(name) || seen[name] {
			continue
		}
		seen[name] = true
		statusBytes, _ := os.ReadFile(jobPartFile(jobID, name, "status"))
		p := JobPart{
			Name:   name,
			Status: strings.TrimSpace(string(statusBytes)),
			Start:  readEpochFile(jobPartFile(jobID, name, "start")),
			End:    readEpochFile(jobPartFile(jobID, name, "end")),
		}
		if p.Status == "" {
			p.Status = "pending"
		}
		// A cancelled or crashed job leaves parts that will never finish.
		if !partStatusFinal(p.Status) && partStatusFinal(jobStatus) {
			p.Status = "interrupted"
		}
		switch {
		case p.Start > 0 && p.End >= p.Start:
			p.Elapsed = p.End - p.Start
		case p.Start > 0 && !partStatusFinal(p.Status):
			p.Elapsed = max(now.Unix()-p.Start, 0)
		}
		parts = append(parts, p)
	}
	return parts
}

func jobHasPart(jobID, part string) bool {
	if !validJobPartName(part) {
		return false
	}
	for _, p := range readJobParts(jobID, time.Now()) {
		if p.Name == part {
			return true
		}
	}
	return false
}

// secureID returns a cryptographically random hex string of length 2*n bytes.
func secureID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to timestamp-based ID.
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}

// startJob runs cmd in a goroutine, writing output to a log file.
// onComplete is called with the final status string when the job finishes.
func startJob(cmd []string, cwd string, extraEnv map[string]string, onComplete func(string)) string {
	jobID := secureID(8) // 16 hex chars of crypto random – not guessable
	if !launchJob(jobID, cmd, cwd, extraEnv, onComplete) {
		return ""
	}
	return jobID
}

func launchJob(jobID string, cmd []string, cwd string, extraEnv map[string]string, onComplete func(string)) bool {
	jobLifecycleMu.Lock()
	defer jobLifecycleMu.Unlock()
	if jobsStopping {
		return false
	}
	jobsWG.Add(1)
	go func() {
		defer jobsWG.Done()
		runJob(jobID, cmd, cwd, extraEnv, onComplete)
	}()
	return true
}

func runJob(jobID string, cmd []string, cwd string, extraEnv map[string]string, onComplete func(string)) {
	finalStatus := "error"
	statusPath := jobStatusPath(jobID)
	defer func() {
		if err := atomicWriteFile(statusPath, []byte(finalStatus), 0600); err != nil {
			slog.Error("job status write failed", "job", jobID, "err", err)
			finalStatus = "error"
		}
		if onComplete != nil {
			onComplete(finalStatus)
		}
		slog.Info("job finished", "job", jobID, "status", finalStatus)
	}()
	startTime := time.Now()
	logPath := jobLogPath(jobID)

	if err := atomicWriteFile(statusPath, []byte("running"), 0600); err != nil {
		slog.Error("job status write failed", "job", jobID, "err", err)
		return
	}
	if len(cmd) == 0 {
		slog.Error("job command is empty", "job", jobID)
		return
	}

	// Build command string for display
	var cmdDisplay string
	if len(cmd) == 3 && cmd[0] == "bash" && cmd[1] == "-c" {
		cmdDisplay = cmd[2]
	} else {
		cmdDisplay = strings.Join(cmd, " ")
	}
	slog.Info("job started", "job", jobID, "cwd", cwd, "cmd", cmdDisplay)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		slog.Error("job log create failed", "job", jobID, "err", err)
		return
	}
	defer logFile.Close()

	// Write the command at the top so it's visible in the terminal output.
	fmt.Fprintf(logFile, "$ %s\n# cwd: %s\n\n", cmdDisplay, cwd)
	logFile.Sync()

	// Build exec.Cmd
	execCmd := &struct {
		name string
		args []string
		env  []string
	}{
		name: cmd[0],
		args: cmd[1:],
	}
	// Build environment
	for _, e := range os.Environ() {
		execCmd.env = append(execCmd.env, e)
	}
	for k, v := range extraEnv {
		execCmd.env = append(execCmd.env, k+"="+v)
	}
	// Parallel playbook blocks write per-part logs next to the job log.
	execCmd.env = append(execCmd.env, "PSMDB_JOB_ID="+jobID, "PSMDB_JOBS_DIR="+jobsDir)

	finalStatus = runProcess(jobID, execCmd.name, execCmd.args, execCmd.env, cwd, logFile)

	// Append elapsed time to the log so it is visible in the terminal output.
	elapsed := time.Since(startTime)
	totalSecs := int(elapsed.Seconds())
	mins := totalSecs / 60
	secs := totalSecs % 60
	fmt.Fprintf(logFile, "\n# Elapsed: %dm %ds\n", mins, secs)
	logFile.Sync()
}

// runProcess executes the given command and pipes stdout+stderr to logFile.
// Returns "success", "failed:N", "interrupted", or "error".
func runProcess(jobID string, name string, args []string, envVars []string, cwd string, logFile *os.File) string {
	proc := execCommand(name, args...)
	proc.Dir = cwd
	proc.Env = envVars
	proc.Stdout = logFile
	proc.Stderr = logFile
	configureJobProcess(proc)
	jobLifecycleMu.Lock()
	if jobsStopping {
		jobLifecycleMu.Unlock()
		return "interrupted"
	}
	if err := proc.Start(); err != nil {
		jobLifecycleMu.Unlock()
		fmt.Fprintf(logFile, "\nError starting process: %v\n", err)
		return "error"
	}
	jobPIDs.Store(jobID, proc.Process)
	jobLifecycleMu.Unlock()
	defer jobPIDs.Delete(jobID)
	if err := proc.Wait(); err != nil {
		jobLifecycleMu.Lock()
		stopping := jobsStopping
		jobLifecycleMu.Unlock()
		if stopping {
			return "interrupted"
		}
		return fmt.Sprintf("failed:%d", proc.ProcessState.ExitCode())
	}
	return "success"
}

// cancelJob looks up the running process for jobID and kills it.
// Returns true if the process was found and killed, false otherwise.
func cancelJob(jobID string) bool {
	val, ok := jobPIDs.Load(jobID)
	if !ok {
		return false
	}
	proc := val.(*os.Process)
	if err := killJobProcess(proc); err != nil {
		slog.Warn("cancel job kill failed", "job", jobID, "err", err)
	}
	return true
}

// killAllJobs stops every running job. Jobs run in their own process group,
// so they would otherwise outlive the UI when it is interrupted.
func killAllJobs() {
	jobPIDs.Range(func(key, val any) bool {
		if err := killJobProcess(val.(*os.Process)); err != nil {
			slog.Warn("shutdown job kill failed", "job", key, "err", err)
		}
		return true
	})
}

// stopJobs prevents admission and catches jobs that have not started a process
// yet. A timeout leaves on-disk recovery markers for the next startup.
func stopJobs(ctx context.Context) error {
	jobLifecycleMu.Lock()
	jobsStopping = true
	killAllJobs()
	jobLifecycleMu.Unlock()
	done := make(chan struct{})
	go func() {
		jobsWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reconcileInterruptedJobs runs before accepting requests. A lost completion
// callback is not proof of a successful apply/destroy: keep all recovery files
// and applied snapshots, and require the user to inspect and retry explicitly.
func reconcileInterruptedJobs() error {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".status")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".status") || !jobIDRe.MatchString(id) {
			continue
		}
		status, err := os.ReadFile(jobStatusPath(id))
		if err != nil {
			return err
		}
		if !partStatusFinal(strings.TrimSpace(string(status))) {
			if err := atomicWriteFile(jobStatusPath(id), []byte("interrupted"), 0600); err != nil {
				return err
			}
		}
	}
	return updateState(func(state map[string]*Environment) error {
		now := time.Now().UTC()
		for _, env := range state {
			if !environmentInProgress(env) {
				continue
			}
			action := strings.TrimSuffix(env.Status, "_in_progress")
			if jobIDRe.MatchString(env.LastJobID) {
				if err := atomicWriteFile(jobStatusPath(env.LastJobID), []byte("interrupted"), 0600); err != nil {
					return err
				}
			}
			duration := int64(0)
			if start, err := time.Parse(time.RFC3339, env.UpdatedAt); err == nil {
				duration = max(int64(now.Sub(start).Seconds()), 0)
			}
			env.History = append(env.History, HistoryEvent{
				Action: action, StartedAt: env.UpdatedAt, Status: "interrupted", DurationSecs: duration,
			})
			env.Status = action + "_failed"
			env.UpdatedAt = now.Format(time.RFC3339)
		}
		return nil
	})
}

// POST /api/job/{job_id}/cancel
func jobCancelHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	cancelled := cancelJob(jobID)
	writeJSON(w, 200, map[string]bool{"cancelled": cancelled})
}
