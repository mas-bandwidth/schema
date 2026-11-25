//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// pidAlive cannot ask outside unix: a shard runs on a Linux bench, and a
// status asked anywhere else reports its launcher gone.
func pidAlive(int) bool { return false }

// ownGroup has no process groups to make outside unix.
func ownGroup(*exec.Cmd) {}

// reapShard cannot look for leftover processes outside unix, so a shard
// started again there refuses to run beside them.
func reapShard(string, func(time.Duration)) (int, error) {
	return 0, errors.New("a shard runs on a unix bench: here it cannot look for the processes a dead shard left running")
}

// lockOut cannot hold a directory outside unix, so a shard refuses to run.
func lockOut(dir string) (func(), error) {
	return nil, fmt.Errorf("a shard runs on a unix bench: here it cannot hold %s for itself", dir)
}

// launcherAlive cannot ask outside unix.
func launcherAlive(int, string) bool { return false }
