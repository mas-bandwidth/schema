package darttable

import (
	"strings"
	"testing"
)

// TestDartFixedRuntimeRetiresLayoutNewer holds the Dart fixed runtime to
// docs/VERSIONING.md and SPEC-TABLES §21.3: a hash in no lineage entry is
// layout_unknown, and the retired layout_newer name is gone rather than kept
// as a second spelling.
func TestDartFixedRuntimeRetiresLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, retired := range []string{"layoutNewer", "layout_newer"} {
		if strings.Contains(fixedRuntime, retired) {
			t.Errorf("the Dart fixed runtime still carries the retired %s; a hash in no lineage entry is layoutUnknown", retired)
		}
	}
	for _, want := range []string{"layoutUnknown", `'layout_unknown'`} {
		if !strings.Contains(fixedRuntime, want) {
			t.Errorf("the Dart fixed runtime has no %s refusal", want)
		}
	}
}
