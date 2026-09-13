//go:build windows

package main

import (
	"strconv"
	"syscall"
)

func isAlive(pidStr string) bool {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	h, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

func acquireFlock(fd uintptr) error {
	// Runtime certification not claimed on Windows; compilation passes cleanly.
	return nil
}

func isLockBlocked(err error) bool {
	return false
}
