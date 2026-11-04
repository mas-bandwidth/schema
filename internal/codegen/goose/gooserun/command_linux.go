//go:build linux

package gooserun

import (
	"os/exec"
	"syscall"
)

// dieWithParent has the kernel kill the process when the test binary that
// started it dies, whatever ended the test binary.
func dieWithParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
