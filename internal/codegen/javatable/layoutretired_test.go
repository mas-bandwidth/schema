package javatable

import (
	"strings"
	"testing"
)

// TestJavaFixedRuntimeRetiresLayoutNewer holds the Java fixed runtime to
// docs/VERSIONING.md and SPEC-TABLES §21.3: a hash in no lineage entry is
// layout_unknown, and the retired layout_newer name is gone rather than kept
// as a second spelling.
func TestJavaFixedRuntimeRetiresLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, retired := range []string{"layoutNewer", "layout_newer"} {
		if strings.Contains(tableFixedRuntime, retired) {
			t.Errorf("the Java fixed runtime still carries the retired %s; a hash in no lineage entry is layoutUnknown", retired)
		}
	}
	if !strings.Contains(tableFixedRuntime, "layoutUnknown") {
		t.Error("the Java fixed runtime has no layoutUnknown refusal")
	}
}
