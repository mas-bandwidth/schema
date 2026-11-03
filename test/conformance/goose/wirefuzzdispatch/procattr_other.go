//go:build !linux

package main

import "syscall"

// childProcAttr is the platform default where no parent-death signal exists;
// the dispatcher's stderr copy still keeps a leftover leg off the harness.
func childProcAttr() *syscall.SysProcAttr { return nil }
