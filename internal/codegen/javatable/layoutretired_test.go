package javatable

import (
	"strings"
	"testing"
)

// TestJavaFixedRuntimeReservesLayoutNewer holds the Java fixed runtime to
// SPEC-TABLES §21.3: a hash in no lineage entry is layout_unknown, while the
// retired layout_newer member keeps its ordinal as an alias so locked callers
// still compile. No read answers the retired name.
func TestJavaFixedRuntimeReservesLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"layoutUnknown",
		"public static final Reason layoutNewer = layoutUnknown;",
	} {
		if !strings.Contains(tableFixedRuntime, want) {
			t.Errorf("the Java fixed runtime is missing %q", want)
		}
	}
}
