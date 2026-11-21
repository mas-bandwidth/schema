package testproc_test

import "syscall"

func hasDeathSignal(a *syscall.SysProcAttr) bool { return a.Pdeathsig == syscall.SIGKILL }
