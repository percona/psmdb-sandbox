//go:build windows

package main

import (
	"os"
	"os/exec"
)

func configureJobProcess(cmd *exec.Cmd) {}

func killJobProcess(proc *os.Process) error {
	return proc.Kill()
}
