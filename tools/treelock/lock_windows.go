//go:build windows

package main

import (
	"fmt"
	"strconv"
	"syscall"
)

// ErrWindowsUnsupported is returned when acquireFlock is called on Windows,
// as kernel exclusivity locking is not yet implemented or certified on Windows.
var ErrWindowsUnsupported = fmt.Errorf("treelock: kernel exclusivity locking is unsupported on windows; refusing execution without verified lock")

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
	// Obligation retained: Windows platform locking (e.g. LockFileEx) and certification
	// are not yet implemented. Refuse explicitly before launching any command or claiming
	// unearned lock ownership.
	return ErrWindowsUnsupported
}

func isLockBlocked(err error) bool {
	return false
}
