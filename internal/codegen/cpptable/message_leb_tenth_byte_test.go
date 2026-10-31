package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageLebTenthByteRefusalEmitted pins the tenth-byte refusal in the
// emitted TableMessageLeb: a tenth byte with a bit above the 64th value bit is
// malformed (docs/SPEC-TABLES.md §3.3), so the reader must refuse it before the
// OR.
func TestMessageLebTenthByteRefusalEmitted(t *testing.T) {
	t.Parallel()
	u := &ir.Unit{Package: "bench"}
	src := tableMessageForm(u)
	want := "if ( shift == 63 && by > 1 ) { return false; }"
	if !strings.Contains(src, want) {
		t.Errorf("the emitted TableMessageLeb does not carry the tenth-byte refusal %q", want)
	}
}
