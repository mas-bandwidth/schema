package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S ELISION TEST JUDGES TEXT BY ITS BYTES (docs/SPEC-TABLES.md §3,
// §3.3: a field is elided only where it holds its declared default). A text
// cell holds its value in Str, so a text element or field at its default is
// default and one holding any other bytes is not. No schema reaches
// cellIsDefault with a text cell today (a text list takes no lower bound and
// no fixed extent, §2.13.1, and a scalar text field elides by messageElides
// and elides, not here); the test holds the function to the rule directly so
// a future caller inherits the right answer.
func TestCellIsDefaultJudgesTextByItsBytes(t *testing.T) {
	t.Parallel()
	m := covModel(t, "package celldefault\ntable Root {\n    names [..4]string\n    title string(8) = \"ab\"\n    blob bytes(4)\n}\n")
	st := m.Unit.Tables["Root"]
	field := func(name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	e := &encoder{m: m}
	for _, tc := range []struct {
		field, value string
		want         bool
	}{
		{"names", "", true},
		{"names", "ab", false},
		{"title", "ab", true},
		{"title", "", false},
		{"title", "cd", false},
		{"blob", "", true},
		{"blob", "\x01", false},
	} {
		cell := tabletext.Cell{Str: []byte(tc.value)}
		if got := cellIsDefault(e, field(tc.field), &cell); got != tc.want {
			t.Errorf("%s holding %q: cellIsDefault = %v, want %v", tc.field, tc.value, got, tc.want)
		}
	}
}
