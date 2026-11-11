package ctable

import (
	"strings"
	"testing"
)

// TestCFixedRuntimeReservesLayoutNewer holds the C fixed runtime to
// SPEC-TABLES §21.3: a hash in no lineage entry is layout_unknown, while the
// retired layout_newer member keeps its ordinal as an alias so locked callers
// still compile. No read answers the retired name.
func TestCFixedRuntimeReservesLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"SCHEMA_TABLE_LAYOUT_UNKNOWN = 18",
		"SCHEMA_TABLE_LAYOUT_NEWER = SCHEMA_TABLE_LAYOUT_UNKNOWN",
	} {
		if !strings.Contains(tableFixedRuntime, want) {
			t.Errorf("the C fixed runtime is missing %q", want)
		}
	}
}
