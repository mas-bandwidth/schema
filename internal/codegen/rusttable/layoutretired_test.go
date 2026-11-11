package rusttable

import (
	"strings"
	"testing"
)

// TestRustFixedRuntimeRetiresLayoutNewer holds the Rust fixed runtime to
// docs/VERSIONING.md and SPEC-TABLES §21.3: a hash in no lineage entry is
// layout_unknown, and the retired layout_newer name is gone rather than kept
// as a second spelling.
func TestRustFixedRuntimeRetiresLayoutNewer(t *testing.T) {
	t.Parallel()
	for _, retired := range []string{"LayoutNewer", "layout_newer"} {
		if strings.Contains(fixedRuntimeBody, retired) {
			t.Errorf("the Rust fixed runtime still carries the retired %s; a hash in no lineage entry is LayoutUnknown", retired)
		}
	}
	for _, want := range []string{"LayoutUnknown", `"layout_unknown"`} {
		if !strings.Contains(fixedRuntimeBody, want) {
			t.Errorf("the Rust fixed runtime has no %s refusal", want)
		}
	}
}
