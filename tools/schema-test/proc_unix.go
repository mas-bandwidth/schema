//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// setGroup starts the child in its own process group, so killGroup reaches
// everything it started (a compiler, a VM).
func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
