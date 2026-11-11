package ctable

import (
	"strings"
	"testing"
)

// TestCFixedRuntimeRetiresLayoutNewer holds the C fixed runtime to
// docs/VERSIONING.md and SPEC-TABLES §21.3: a hash in no lineage entry is
// layout_unknown, and the retired layout_newer name is gone rather than kept
// as a second spelling.
func TestCFixedRuntimeRetiresLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, retired := range []string{"SCHEMA_TABLE_LAYOUT_NEWER", "layout_newer"} {
		if strings.Contains(tableFixedRuntime, retired) {
			t.Errorf("the C fixed runtime still carries the retired %s; a hash in no lineage entry is layout_unknown", retired)
		}
	}
	if !strings.Contains(tableFixedRuntime, "SCHEMA_TABLE_LAYOUT_UNKNOWN") {
		t.Error("the C fixed runtime has no layout_unknown refusal")
	}
}
