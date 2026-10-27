//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
