//go:build linux

package main

import "syscall"

// childProcAttr kills a leg when the dispatcher dies, so a leg spinning on a
// mutant never outlives the run that sent it.
func childProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
