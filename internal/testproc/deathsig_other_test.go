//go:build unix && !linux

package testproc_test

import "syscall"

func hasDeathSignal(*syscall.SysProcAttr) bool { return false }
