//go:build !linux && !darwin

package main

import (
	"fmt"
	"os"
	"runtime"
)

// fileIdentity has no inode and change time to read here, so the dry-run
// test fails naming the platform rather than missing a delete-and-restore.
func fileIdentity(os.FileInfo) (string, error) {
	return "", fmt.Errorf("no inode and change time on %s", runtime.GOOS)
}
