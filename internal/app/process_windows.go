//go:build windows

package app

import (
	"os"
	"os/exec"
)

func configureManagedProcess(cmd *exec.Cmd) {}

func interruptManagedProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

func killManagedProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
