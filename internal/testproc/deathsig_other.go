//go:build unix && !linux

package testproc

import "syscall"

// setDeathSignal: only Linux has a parent-death signal; elsewhere the test's
// end and its deadline bound the child.
func setDeathSignal(*syscall.SysProcAttr) {}
