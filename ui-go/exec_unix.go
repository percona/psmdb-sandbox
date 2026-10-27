//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// configureJobProcess puts the job in its own process group so cancelling it
// also stops background children such as parallel ansible-playbook runs.
func configureJobProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killJobProcess kills the job's whole process group, falling back to the
// leader alone if the group cannot be signalled.
func killJobProcess(proc *os.Process) error {
	if err := syscall.Kill(-proc.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return proc.Kill()
}
