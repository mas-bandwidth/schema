//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// fileIdentity is a path's inode and change time (TestEveryHostileTargetDryRunsWithoutWriting).
func fileIdentity(info os.FileInfo) (string, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("no stat for %s", info.Name())
	}
	return fmt.Sprintf("%d/%d.%d", st.Ino, st.Ctim.Sec, st.Ctim.Nsec), nil
}
