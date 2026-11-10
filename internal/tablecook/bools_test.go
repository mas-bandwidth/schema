package tablecook_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COOKED BOOL IS 0 OR 1 (docs/SPEC-TABLES.md §7): a bool's storage byte and
// an optional's present byte are read by the mapped readers as a bool, and a
// byte that is neither 0 nor 1 is undefined behaviour in them (§19).
// `schema cook-check` refuses one by name, at the field it is in — a bool
// inside an array or a nested record included — and the uncook does too.
func TestACookRefusesABoolByteOverOne(t *testing.T) {
	t.Parallel()
	u := unit(t, "../../test/tables/VNEW_hostile_bool.schema")
	m := tabletext.NewModel(u)
	root := m.Lookup("Hbb")
	if root == nil {
		t.Fatal("VNEW_hostile_bool defines no Hbb table")
	}

	cooked, err := tablecook.Cook(m, m.New(root), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tablecook.Check(m, cooked); err != nil {
		t.Fatalf("the clean cook does not check: %v", err)
	}
	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}

	ml := ir.RecordLayout(u, root)
	boolSlot := ml.FieldByName("flag")
	presentSlot := ml.FieldByName("link")
	arySlot := ml.FieldByName("flags")
	if boolSlot == nil || presentSlot == nil || arySlot == nil {
		t.Fatal("Hbb lacks a field the test pins")
	}
	flagPiece := ir.FieldPieces(u, boolSlot.Field, boolSlot.Offset)[0].Offset
	presentPiece := ir.FieldPieces(u, presentSlot.Field, presentSlot.Offset)
	presentOffset := presentPiece[len(presentPiece)-1].Offset
	arrayOffset := arySlot.Offset

	for _, c := range []struct {
		name string
		at   int64
	}{
		{"bool field", flagPiece},
		{"optional present byte", presentOffset},
		{"bool array element", arrayOffset + 3},
	} {
		for _, big := range []bool{false, true} {
			name := c.name + "/little"
			if big {
				name = c.name + "/big"
			}
			t.Run(name, func(t *testing.T) {
				bad := append([]byte(nil), cooked...)
				h.Data(bad)[c.at] = 2
				if _, err := tablecook.Check(m, bad); err == nil {
					t.Error("Check accepted a byte 2 in a " + c.name)
				}
				if _, err := tablecook.Uncook(m, root, bad); err == nil {
					t.Error("Uncook accepted a byte 2 in a " + c.name)
				}
			})
		}
	}
}
