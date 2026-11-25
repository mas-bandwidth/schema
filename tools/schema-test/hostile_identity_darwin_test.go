//go:build darwin

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
	return fmt.Sprintf("%d/%d.%d", st.Ino, st.Ctimespec.Sec, st.Ctimespec.Nsec), nil
}
