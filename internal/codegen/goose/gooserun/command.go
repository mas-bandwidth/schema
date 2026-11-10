package gooserun

import (
	"context"
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"time"
)

// Timeout is the most wall clock one Goose compile, or one program it built,
// is given. A Goose program that never ends (a miscompiled loop spins at full
// CPU) fails its test here, by name, rather than outliving the test binary.
const Timeout = 5 * time.Minute

// Command is exec.Command for every process a Goose test starts: the pinned
// compiler, a binary it built, or an oracle beside them. The process is
// killed when the wall clock runs out ([Timeout], or less when the test's
// own deadline is nearer) and when the test binary dies (on Linux, where the
// pinned compiler runs), so no Goose process outlives the test that started
// it. A killed process answers Run and Wait with an error, and the test
// fails on it as on any other.
func Command(t testing.TB, name string, args ...string) *exec.Cmd {
	t.Helper()
	slowtest.Gate(t, "the Goose toolchain")
	limit := Timeout
	if d, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if at, set := d.Deadline(); set {
			if left := time.Until(at) - 30*time.Second; left > 0 && left < limit {
				limit = left
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 10 * time.Second
	dieWithParent(cmd)
	return cmd
}
