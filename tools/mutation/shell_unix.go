//go:build unix

package main

import (
	"bytes"
	"os/exec"
	"syscall"
	"time"
)

// shell is the Exec that runs a real command. The command leads its own
// process group, and the whole group is killed at the limit: a mutant that
// turns a loop endless leaves compilers and interpreters behind it, and they
// must not outlive the stage.
func shell(dir, sh string, limit time.Duration) ([]byte, bool, bool, time.Duration) {
	cmd := exec.Command("sh", "-c", sh)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = 10 * time.Second
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return []byte(err.Error()), false, false, 0
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-done:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return out.Bytes(), err == nil, false, time.Since(start)
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return out.Bytes(), false, true, time.Since(start)
	}
}
