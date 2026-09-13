//go:build !windows

package main

import (
	"errors"
	"strconv"
	"syscall"
)

func isAlive(pidStr string) bool {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func acquireFlock(fd uintptr) error {
	return syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
}

func isLockBlocked(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
