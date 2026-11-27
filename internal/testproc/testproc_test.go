package testproc_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// childEnv, set, makes the test binary a child that never ends on its own: it
// reads a stdin its parent never closes, so only a kill ends it. It reads no
// clock and opens no socket.
const childEnv = "SCHEMA_TESTPROC_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "block":
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	case "hold":
		// a grandchild: hold fd 3 open and block for good on a pipe this
		// process owns both ends of, so only a kill ends it
		r, w, err := os.Pipe()
		if err != nil {
			os.Exit(5)
		}
		_, _ = r.Read(make([]byte, 1))
		_ = w.Close()
		os.Exit(0)
	case "launch":
		// a launcher: start a grandchild that inherits fd 3, then exit 0
		// at once, leaving the grandchild running
		exe, err := os.Executable()
		if err != nil {
			os.Exit(6)
		}
		g := exec.Command(exe, "-test.run=^$")
		g.Env = append(os.Environ(), childEnv+"=hold")
		g.ExtraFiles = []*os.File{os.NewFile(3, "held")}
		if err := g.Start(); err != nil {
			os.Exit(7)
		}
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
			return testproc.Command(t, name, args...).Cmd
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
		return testproc.Command(past, name, args...).Cmd
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
		return testproc.CommandContext(t, ctx, name, args...).Cmd
	})
	defer stdin.Close()
	if err := cmd.Start(); !errors.Is(err, context.Canceled) {
		if err == nil {
			_ = cmd.Wait()
		}
		t.Fatalf("Start under a cancelled context = %v, want %v", err, context.Canceled)
	}
}

// A child that exits normally leaves nothing of its own behind: a launcher
// starts a grandchild and exits 0, and when Run returns the grandchild is
// gone. The grandchild holds the write end of a pipe; the read end sees EOF
// only once every holder has died. The read is bounded by the test's own
// deadline, so a surviving grandchild fails this test rather than hanging it.
func TestGrandchildIsKilledWhenTheChildExitsNormally(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), childEnv+"=launch")
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Run(); err != nil {
		t.Fatalf("the launcher: %v", err)
	}
	w.Close()
	if deadline, ok := t.Deadline(); ok {
		_ = r.SetReadDeadline(deadline.Add(-2 * testproc.Margin))
	}
	if n, err := r.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("a grandchild outlived its launcher's Run: read = %d, %v; want EOF", n, err)
	}
}
