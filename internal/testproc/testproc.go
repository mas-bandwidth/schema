// Package testproc starts a test's child processes so that none can outlive
// the test or loop forever (review-owed/ci-tools/test-children-bounded:
// TestLuauVariableSaveRefusesOffDeclaration once left a luau process spinning
// for six days on a bench, after the test binary that started it was gone).
//
// A Command is bounded three ways:
//
//   - it is killed, with its whole process group, when the test ends: its
//     context is the test's own (testing.TB.Context), cancelled as the test
//     finishes;
//   - it is killed a margin before the test binary's -timeout deadline, so a
//     child that never answers fails its own test by name instead of taking
//     the whole binary down in a timeout panic that would orphan it;
//   - on Linux the kernel kills it if the test binary dies first
//     (PR_SET_PDEATHSIG), the one case no deadline in the parent can reach.
//
// Every child runs as the leader of its own process group, so what it starts
// in turn (an interpreter under a shell, a compiler driver's jobs) is killed
// with it. internal/ci's TestEveryTestChildIsBounded holds every _test.go file
// to this package. It reads no clock of its own: the deadline is the one the
// testing package hands the test.
package testproc

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Margin is how long before the test binary's deadline a child is killed.
const Margin = 5 * time.Second

// WaitDelay bounds how long Wait waits on a killed child's pipes, which a
// descendant that escaped the group could otherwise hold open.
const WaitDelay = 5 * time.Second

// Command is exec.Command for a child of test t, bounded as the package says.
func Command(t testing.TB, name string, args ...string) *exec.Cmd {
	t.Helper()
	return CommandContext(t, t.Context(), name, args...)
}

// CommandContext is exec.CommandContext for a child of test t: ctx bounds it
// as well, and it is bounded as the package says.
func CommandContext(t testing.TB, ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	ctx = withTestBounds(t, ctx)
	cmd := exec.CommandContext(ctx, name, args...)
	bound(cmd)
	return cmd
}

// withTestBounds joins ctx to the test's end and to the binary's deadline.
func withTestBounds(t testing.TB, ctx context.Context) context.Context {
	if ctx != t.Context() {
		var stop context.CancelFunc
		ctx, stop = context.WithCancel(ctx)
		after := context.AfterFunc(t.Context(), stop)
		t.Cleanup(func() { after(); stop() })
	}
	if dt, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if deadline, ok := dt.Deadline(); ok {
			var stop context.CancelFunc
			ctx, stop = context.WithDeadline(ctx, deadline.Add(-Margin))
			t.Cleanup(stop)
		}
	}
	return ctx
}

// bound puts the child in its own process group, kills the group when the
// context ends, and bounds the wait on its pipes.
func bound(cmd *exec.Cmd) {
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = WaitDelay
}
