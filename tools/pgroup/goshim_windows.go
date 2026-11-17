//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// execGo runs the real go as a child: Windows has no exec.
func execGo(real string, args, env []string) int {
	cmd := exec.Command(real, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	stop := holdSignals()
	defer stop()
	return shimStatus(cmd, cmd.Run())
}

func holdSignals() func() {
	c := make(chan os.Signal, 4)
	signal.Notify(c, os.Interrupt)
	return func() { signal.Stop(c) }
}

func shimStatus(cmd *exec.Cmd, err error) int {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		return 127
	}
}
