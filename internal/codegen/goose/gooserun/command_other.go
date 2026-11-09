//go:build !linux

package gooserun

import "os/exec"

// dieWithParent has no kernel hook off Linux; the wall-clock limit is the
// whole of the guarantee there.
func dieWithParent(*exec.Cmd) {}
