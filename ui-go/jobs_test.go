package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func useTempJobs(t *testing.T) {
	t.Helper()
	old := jobsDir
	jobsDir = t.TempDir()
	t.Cleanup(func() { jobsDir = old })
}

func TestRunJobFinalizesEveryFailure(t *testing.T) {
	for _, failure := range []string{"log", "command", "empty"} {
		t.Run(failure, func(t *testing.T) {
			useTempJobs(t)
			id := "0123456789abcdef"
			cmd := []string{filepath.Join(t.TempDir(), "missing-command")}
			if failure == "log" {
				if err := os.Mkdir(jobLogPath(id), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "empty" {
				cmd = nil
			}
			called := 0
			runJob(id, cmd, jobsDir, nil, func(status string) {
				called++
				if status != "error" {
					t.Errorf("status = %q", status)
				}
			})
			status, err := os.ReadFile(jobStatusPath(id))
			if err != nil || string(status) != "error" || called != 1 {
				t.Fatalf("finalization: status=%q callback=%d err=%v", status, called, err)
			}
		})
	}
}

func TestReconcileInterruptedJobs(t *testing.T) {
	useTempState(t)
	useTempJobs(t)
	start := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	id, orphan, complete := "0123456789abcdef", "1123456789abcdef", "2123456789abcdef"
	state := map[string]*Environment{
		"deploy": {Platform: "docker", Status: "deploy_in_progress", LastJobID: id, UpdatedAt: start,
			LastAppliedConfig: &Config{Prefix: "keep"}},
		"destroy": {Platform: "docker", Status: "destroy_in_progress", LastJobID: complete, UpdatedAt: start},
		"missing": {Platform: "docker", Status: "stop_in_progress", LastJobID: "3123456789abcdef", UpdatedAt: start},
		"running": {Platform: "docker", Status: "running"},
	}
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		id + ".status": "running", orphan + ".status": "pending", complete + ".status": "success",
		id + ".parts": "rs\n", id + ".rs.status": "running", "notes.status": "running",
	} {
		if err := os.WriteFile(filepath.Join(jobsDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Recovery must never delete Terraform state, even after an apparently
	// successful destroy whose completion callback was lost.
	recovery := tfstatePath("destroy", "docker")
	if err := os.MkdirAll(filepath.Dir(recovery), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovery, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := reconcileInterruptedJobs(); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	for env, action := range map[string]string{"deploy": "deploy", "destroy": "destroy", "missing": "stop"} {
		e := state[env]
		if e.Status != action+"_failed" || len(e.History) != 1 || e.History[0].Status != "interrupted" {
			t.Fatalf("%s: %+v", env, e)
		}
		status, err := os.ReadFile(jobStatusPath(e.LastJobID))
		if err != nil || string(status) != "interrupted" {
			t.Fatalf("job status %q: %v", status, err)
		}
	}
	if state["running"].Status != "running" || state["deploy"].LastAppliedConfig.Prefix != "keep" {
		t.Fatal("recovery changed unaffected state")
	}
	if _, err := os.Stat(recovery); err != nil {
		t.Fatal("recovery removed Terraform state")
	}
	parts := readJobParts(id, time.Now())
	if len(parts) != 1 || parts[0].Status != "interrupted" {
		t.Fatalf("stale parts: %+v", parts)
	}
	for name, want := range map[string]string{orphan + ".status": "interrupted", "notes.status": "running"} {
		got, _ := os.ReadFile(filepath.Join(jobsDir, name))
		if string(got) != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestStopJobsWaitsForCompletionCallbacks(t *testing.T) {
	useTempJobs(t)
	t.Cleanup(func() {
		jobLifecycleMu.Lock()
		jobsStopping = false
		jobLifecycleMu.Unlock()
	})
	entered, release := make(chan struct{}), make(chan struct{})
	id := startJob([]string{filepath.Join(t.TempDir(), "missing")}, jobsDir, nil, func(string) {
		close(entered)
		<-release
	})
	if id == "" {
		t.Fatal("job rejected")
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := stopJobs(ctx); err != context.DeadlineExceeded {
		t.Fatalf("shutdown did not wait for callback: %v", err)
	}
	if startJob(nil, jobsDir, nil, nil) != "" {
		t.Fatal("shutdown accepted a new job")
	}
	close(release)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := stopJobs(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile(jobStatusPath(id))
	if err != nil || string(status) != "error" {
		t.Fatalf("callback completed before final status was persisted: %q %v", status, err)
	}
}

func TestShutdownPreventsQueuedProcessStartup(t *testing.T) {
	useTempJobs(t)
	jobLifecycleMu.Lock()
	jobsStopping = true
	jobLifecycleMu.Unlock()
	t.Cleanup(func() {
		jobLifecycleMu.Lock()
		jobsStopping = false
		jobLifecycleMu.Unlock()
	})
	// Represents an admitted goroutine that had not reached proc.Start when
	// shutdown began. Even a nonexistent executable must report interrupted,
	// rather than attempting startup and reporting an executable error.
	called := false
	runJob("0123456789abcdef", []string{filepath.Join(t.TempDir(), "must-not-start")}, jobsDir, nil, func(status string) {
		called = true
		if status != "interrupted" {
			t.Errorf("queued status = %q", status)
		}
	})
	if !called {
		t.Fatal("queued job was not finalized")
	}
}
