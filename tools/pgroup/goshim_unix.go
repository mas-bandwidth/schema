//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// execGo replaces the shim with the real go, so a caller that signals or
// kills the process it started (exec.CommandContext) reaches go itself.
func execGo(real string, args, env []string) int {
	err := syscall.Exec(real, append([]string{"go"}, args...), env)
	fmt.Fprintf(os.Stderr, "pgroup go shim: exec %s: %v\n", real, err)
	return 127
}

// holdSignals keeps the recording shim alive through the stop signals the
// whole process group receives, so it drains go test's last events before
// it exits; go test gets the same signal from the group. A caught signal
// (not an ignored one) resets to the default in the child.
func holdSignals() func() {
	c := make(chan os.Signal, 4)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return func() { signal.Stop(c) }
}

func shimStatus(cmd *exec.Cmd, err error) int { return status(cmd, err) }
