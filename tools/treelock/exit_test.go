//go:build !windows

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestChildExitStatusAndLockRelease(t *testing.T) {
	bin := buildTreelock(t)
	for _, tc := range []struct {
		name, command string
		want          int
	}{
		{"success", "exit 0", 0},
		{"nonzero", "exit 7", 7},
		{"signal", "kill -TERM $$", 143},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lock := filepath.Join(t.TempDir(), "lock")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "-lock", lock, "sh", "-c", tc.command)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("child command did not terminate: %v", ctx.Err())
			}
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.want {
				t.Fatalf("want exit %d, state %v, error %v, output %s", tc.want, cmd.ProcessState, err, out)
			}
			// Every terminal path must leave the same lock usable by the next command.
			next := exec.CommandContext(ctx, bin, "-lock", lock, "sh", "-c", "exit 0")
			if out, err := next.CombinedOutput(); err != nil {
				t.Fatalf("lock was not reusable after child exit: %v: %s", err, out)
			}
		})
	}
}
