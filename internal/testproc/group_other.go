//go:build !unix

package testproc

import "os/exec"

func setGroup(*exec.Cmd) {}

// killGroup kills the child alone: there is no process group to kill.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
