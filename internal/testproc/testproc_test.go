package testproc_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// childEnv, set, makes the test binary a child that never ends on its own: it
// reads a stdin its parent never closes, so only a kill ends it. It reads no
// clock and opens no socket.
const childEnv = "SCHEMA_TESTPROC_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "block" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// blocking starts the test binary as a child that only a kill can end. The
// returned pipe is the child's stdin, held open by the caller.
func blocking(t testing.TB, start func(name string, args ...string) *exec.Cmd) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := start(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"=block")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	return cmd, stdin
}

// A child that would block forever is killed when the test that started it
// ends: Wait, called after that test returned, comes back with the kill.
func TestChildIsKilledWhenItsTestEnds(t *testing.T) {
	var cmd *exec.Cmd
	var stdin io.WriteCloser
	t.Run("starts a child that never ends", func(t *testing.T) {
		cmd, stdin = blocking(t, func(name string, args ...string) *exec.Cmd {
			return testproc.Command(t, name, args...)
		})
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	})
	defer stdin.Close()
	err := cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.Exited() {
		t.Fatalf("the child outlived its test: Wait = %v, want it killed by a signal", err)
	}
}

// Control: the same child under a bare exec.Command is not ended by its test
// returning; the test above passes because of testproc, not because the child
// ends by itself. The child is ended here by closing its stdin.
func TestBareChildOutlivesItsTest(t *testing.T) {
	var cmd *exec.Cmd
	var stdin io.WriteCloser
	t.Run("starts a bare child", func(t *testing.T) {
		cmd, stdin = blocking(t, exec.Command)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	})
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the bare child was ended by something other than its stdin closing (%v); the control proves nothing", err)
	}
}

// deadlineT is a test whose binary deadline is the one given.
type deadlineT struct {
	*testing.T
	at time.Time
}

func (d deadlineT) Deadline() (time.Time, bool) { return d.at, true }

// A child is bounded by the test binary's deadline less the margin: with that
// deadline already past, the child never starts.
func TestChildIsBoundedByTheBinaryDeadline(t *testing.T) {
	past := deadlineT{T: t, at: time.Unix(0, 0)}
	cmd, stdin := blocking(t, func(name string, args ...string) *exec.Cmd {
		return testproc.Command(past, name, args...)
	})
	defer stdin.Close()
	if err := cmd.Start(); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			_ = cmd.Wait()
		}
		t.Fatalf("Start under a past deadline = %v, want %v", err, context.DeadlineExceeded)
	}
}

// The caller's context bounds the child as well as the test does.
func TestChildIsBoundedByItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd, stdin := blocking(t, func(name string, args ...string) *exec.Cmd {
		return testproc.CommandContext(t, ctx, name, args...)
	})
	defer stdin.Close()
	if err := cmd.Start(); !errors.Is(err, context.Canceled) {
		if err == nil {
			_ = cmd.Wait()
		}
		t.Fatalf("Start under a cancelled context = %v, want %v", err, context.Canceled)
	}
}
