//go:build unix

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// pidAlive reports whether a process runs: signal 0 reaches it, or it
// exists and is someone else's.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ownGroup puts a unit's command in a process group of its own, so a
// signal sent to the shard's group (nova-reserve passes one on) reaches the
// shard alone: the shard cancels the unit and records nothing for it,
// instead of reading a verdict from a child the same signal killed. A
// cancel kills the unit's whole group.
func ownGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
}

// reapShard kills the process group of every process whose environment
// carries shardMarker=marker: the units a dead shard left running, which
// would otherwise run beside the shard started again in the same clone. It
// waits, polling with sleep, until each is gone, and returns how many it
// found.
func reapShard(marker string, sleep func(time.Duration)) (int, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("look for the processes a dead shard left running: %w", err)
	}
	want := []byte("\x00" + shardMarker + "=" + marker + "\x00")
	own, self := syscall.Getpgrp(), os.Getpid()
	var pids []int
	groups := map[int]bool{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		env, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil || !bytes.Contains(append(append([]byte{0}, env...), 0), want) {
			continue // gone, someone else's, or not this shard's
		}
		pids = append(pids, pid)
		if g, err := syscall.Getpgid(pid); err == nil && g != own && g > 1 {
			groups[g] = true
		} else {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	for g := range groups {
		_ = syscall.Kill(-g, syscall.SIGKILL)
	}
	for i := 0; ; i++ {
		var left []int
		for _, p := range pids {
			if procLive(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 {
			return len(pids), nil
		}
		if i == 300 {
			return len(pids), fmt.Errorf("processes %v a dead shard left running outlive SIGKILL", left)
		}
		sleep(100 * time.Millisecond)
	}
}

// procLive reports whether a process exists and is not a zombie.
func procLive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(b, ')')
	return i < 0 || i+2 >= len(b) || b[i+2] != 'Z'
}

// lockOut holds the shard's out directory for this process's life: a
// second shard in it refuses to run. The lock goes with the process, so a
// shard that died leaves it free. Letting go unlocks before it closes: a
// process forked in that moment holds a copy of the descriptor until it
// execs, and a close alone would leave the lock held through that copy.
func lockOut(dir string) (func(), error) {
	f, err := os.OpenFile(dir+"/shard.lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another shard runs in %s (%w): one shard at a time in an out directory", dir, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// launcherAlive reports whether pid is still the shard launcher for out:
// where /proc shows a process's command line, a pid a new process took
// over is not the launcher; where it shows none (a process in the middle
// of exec) or there is no /proc, it asks pidAlive, which errs on alive.
func launcherAlive(pid int, out string) bool {
	if _, err := os.Stat("/proc/self/cmdline"); err != nil {
		return pidAlive(pid)
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || !procLive(pid) {
		return false
	}
	if len(b) == 0 {
		return pidAlive(pid)
	}
	return bytes.Contains(append([]byte{0}, b...), []byte("\x00"+out+"\x00"))
}
