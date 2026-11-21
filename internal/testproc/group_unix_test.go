//go:build unix

package testproc_test

import (
	"runtime"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// Every child leads its own process group, so the kill at the test's end
// reaches what the child started too; on Linux it also carries the
// parent-death signal, so it dies with the test binary.
func TestChildLeadsItsOwnGroup(t *testing.T) {
	cmd := testproc.Command(t, "true")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("the child does not lead its own process group")
	}
	if cmd.Cancel == nil || cmd.WaitDelay != testproc.WaitDelay {
		t.Fatal("the child's cancel does not kill its group, or its wait is unbounded")
	}
	if runtime.GOOS == "linux" && !hasDeathSignal(cmd.SysProcAttr) {
		t.Fatal("on Linux the child carries no parent-death signal")
	}
}
