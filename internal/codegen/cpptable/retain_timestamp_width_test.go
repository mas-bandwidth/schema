package cpptable

import (
	"strings"
	"testing"
)

// TestRetainCarriesTheTimestampKind pins kind 36 in the retain-unknown
// runtime's fixed-width switches (docs/SPEC-TABLES.md §6.6). A timestamp under
// an id this build cannot name is a scalar of the i64's eight bytes (§2.11,
// §3), and the Go oracle (internal/tablewire skipPayload, ir.TableKindWidth)
// keeps it. A capture or emit switch that stops at 28 drops it as retain_lost:
// the release gate's red on streamdemo.Feed at mutant 61319 of seed
// 24845619678, "the leg says retained=0 retain_lost=4, the oracle says
// retained=1 retain_lost=3".
func TestRetainCarriesTheTimestampKind(t *testing.T) {
	t.Parallel()
	src := cppRetainRuntime
	if got := strings.Count(src, "case 5: case 9: case 11: case 23: case 28: case 36:\n"); got != 2 {
		t.Errorf("kind 36 is a retainable fixed-width kind in %d of the capture and emit switches, want 2", got)
	}
	if got := strings.Count(src, "case 5: case 9: case 11: case 23: case 28: case 36: width = 8;"); got != 2 {
		t.Errorf("kind 36 takes eight bytes in %d of the capture and emit width switches, want 2", got)
	}
}
