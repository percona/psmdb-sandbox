//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopJobsKillsRunningProcessesAndFinalizes(t *testing.T) {
	useTempJobs(t)
	t.Cleanup(func() {
		jobLifecycleMu.Lock()
		jobsStopping = false
		jobLifecycleMu.Unlock()
	})
	pidFile := filepath.Join(jobsDir, "started")
	completed := false
	id := startJob([]string{"bash", "-c", "echo ready > " + shellQuote(pidFile) + "; sleep 60"}, jobsDir, nil, func(status string) {
		if status != "interrupted" {
			t.Errorf("shutdown status = %q", status)
		}
		completed = true
	})
	waitForOperationTest(t, func() bool { _, err := os.Stat(pidFile); return err == nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := stopJobs(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile(jobStatusPath(id))
	if !completed || err != nil || string(status) != "interrupted" {
		t.Fatalf("shutdown did not finalize: completed=%v status=%q err=%v", completed, status, err)
	}
}

func TestCancelJobKillsBackgroundChildren(t *testing.T) {
	old := jobsDir
	jobsDir = t.TempDir()
	t.Cleanup(func() { jobsDir = old })
	pidFile := filepath.Join(jobsDir, "child.pid")

	done := make(chan string, 1)
	jobID := startJob([]string{"bash", "-c", "sleep 60 & echo $! > " + shellQuote(pidFile) + "; wait"}, jobsDir, nil, func(s string) { done <- s })

	var childPID int
	for i := 0; i < 50 && childPID == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		data, _ := os.ReadFile(pidFile)
		childPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if childPID == 0 {
		t.Fatal("background child did not start")
	}
	if !cancelJob(jobID) {
		t.Fatal("cancelJob did not find the job")
	}
	select {
	case s := <-done:
		if s == "success" {
			t.Errorf("cancelled job status = %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job did not stop after cancel")
	}
	for i := 0; i < 20; i++ {
		if syscall.Kill(childPID, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	syscall.Kill(childPID, syscall.SIGKILL)
	t.Error("background child survived job cancellation")
}
