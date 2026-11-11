package darttable

import (
	"strings"
	"testing"
)

// TestDartFixedRuntimeReservesLayoutNewer holds the Dart fixed runtime to
// SPEC-TABLES §21.3: a hash in no lineage entry is layout_unknown, while the
// retired layout_newer member keeps its ordinal as an alias so locked callers
// still compile. No read answers the retired name.
func TestDartFixedRuntimeReservesLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"layoutUnknown",
		"'layout_unknown'",
		"static const int layoutNewer = layoutUnknown;",
	} {
		if !strings.Contains(fixedRuntime, want) {
			t.Errorf("the Dart fixed runtime is missing %q", want)
		}
	}
}
