package tabletext_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A ZERO IN TEXT TAKES ONE RULE IN EVERY FORM (docs/SPEC-TABLES.md §16.3):
// the packer's two element entry points name the field they refused, as the
// whole-text read already does, and a field file with no zero reads.
func TestFieldAndElementFileZeroNamesTheField(t *testing.T) {
	t.Parallel()
	m := textZeroModel(t)
	inst := m.New(m.Lookup("Root"))

	var r tabletext.Report
	if m.ReadValue(field(t, inst, "t"), []byte(`"a\u0000b"`), &r) {
		t.Fatal("ReadValue accepted a zero in text")
	}
	if !r.Malformed || r.Reason != "text_holds_zero: Root.t" {
		t.Fatalf("ReadValue report %+v, want Malformed with Reason text_holds_zero: Root.t", r)
	}

	var er tabletext.Report
	if m.ReadElement(field(t, inst, "list"), 0, []byte(`"a\u0000b"`), &er) {
		t.Fatal("ReadElement accepted a zero in text")
	}
	if !er.Malformed || er.Reason != "text_holds_zero: Root.list" {
		t.Fatalf("ReadElement report %+v, want Malformed with Reason text_holds_zero: Root.list", er)
	}

	var clean tabletext.Report
	if !m.ReadValue(field(t, inst, "t"), []byte(`"ab"`), &clean) || !clean.Silent() {
		t.Fatalf("a field file with no zero did not read: %+v", clean)
	}
}
