package testproc

import "syscall"

// setDeathSignal has the kernel kill the child when the test binary dies.
// The signal is tied to the OS thread that started the child; the Go runtime
// ends a thread only when a goroutine locked to it exits locked, which no
// test helper here does.
func setDeathSignal(a *syscall.SysProcAttr) {
	a.Pdeathsig = syscall.SIGKILL
}
