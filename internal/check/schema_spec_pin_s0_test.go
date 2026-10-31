// The s0 pins' checker half (docs/SPEC-TABLES.md, "The three wires", T3W-5):
// the VARIABLE table is the wire for anything LARGE, SPARSE or FREE-FORM —
// everything the other two refuse to carry. The fixed class's refusals are
// TestFixedTableClosureRefusals; this is the other end of the same ruling: a
// variable table carries those constructs and stays on the variable wire.
package check

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVariableTableCarriesWhatTheOtherTwoRefuse declares in one variable table
// every construct the fixed class refuses — a map, an unbounded array, a
// pointer, an unbounded byte buffer and an unbounded string — and requires
// them to be accepted, resolved, and on the VARIABLE wire. A fixed table with
// the same constructs is refused by name (the loop at the end), which is what
// makes this the variable class's reach rather than a parser accident.
func TestVariableTableCarriesWhatTheOtherTwoRefuse(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, `package t
table Node { x int32 }
table Big
{
    by_id  map[uint32]int32
    items  []int32
    next   *Node
    blob   *bytes
    label  *string
}
`)
	if !ir.VariableTables(u)["Big"] {
		t.Fatal("a variable table declaring a map, a list, a pointer and unbounded buffers is the VARIABLE wire")
	}
	big := u.Tables["Big"]
	if big == nil {
		t.Fatal("table Big did not survive checking")
	}
	want := map[string]bool{"by_id": true, "items": true, "next": true, "blob": true, "label": true}
	for _, f := range big.Fields {
		delete(want, f.Name)
	}
	for name := range want {
		t.Errorf("the variable table lost the field %s — everything LARGE, SPARSE or FREE-FORM is its reach", name)
	}
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"a map", "package t\nfixed table F { m map[uint32]int32 }\n"},
		{"an unbounded array", "package t\nfixed table F { xs []int32 }\n"},
		{"a pointer", "package t\ntable Node { x int32 }\nfixed table F { next *Node }\n"},
		{"an unbounded byte buffer", "package t\nfixed table F { blob *bytes }\n"},
		{"an unbounded string", "package t\nfixed table F { label *string }\n"},
	} {
		if got := refuse(t, tc.src); got == "" {
			t.Errorf("%s: a fixed table accepted a construct that makes a body variable size", tc.name)
		}
	}
}
