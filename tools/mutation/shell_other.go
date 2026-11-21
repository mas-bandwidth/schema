//go:build !unix

package main

import "time"

// shell refuses where there is no process group to bound a stage with: the
// sweep runs on the Linux and macOS benches and in CI.
func shell(string, string, time.Duration) ([]byte, bool, bool, time.Duration) {
	return []byte("tools/mutation runs its stages on unix only"), false, false, 0
}
