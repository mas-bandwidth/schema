package rusttable

import (
	"strings"
	"testing"
)

// TestRustFixedRuntimeReservesLayoutNewer holds the Rust fixed runtime to
// SPEC-TABLES §21.3: a hash in no lineage entry is layout_unknown, while the
// retired layout_newer member keeps its ordinal as an alias so locked callers
// still compile. No read answers the retired name.
func TestRustFixedRuntimeReservesLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"LayoutUnknown",
		`"layout_unknown"`,
		"pub const LayoutNewer: TableFixedReason = TableFixedReason::LayoutUnknown;",
	} {
		if !strings.Contains(fixedRuntimeBody, want) {
			t.Errorf("the Rust fixed runtime is missing %q", want)
		}
	}
}
