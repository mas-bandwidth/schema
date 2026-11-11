package tablecook_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COUNTED ARRAY'S SLOTS PAST ITS COUNT ARE DEAD BYTES (docs/SPEC-TABLES.md
// §7.2, §7.4): a conforming writer leaves them zero, so `schema cook-check`
// refuses a cook carrying a non-zero byte there, naming the field. One array of
// scalars and one of nested tables, each cooked at count 1: the clean cook
// checks, and the same cook with one byte set in slot 2 does not.
func TestCheckRefusesCountedArrayTail(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, dir, root, field string
	}{
		{"scalars", "../../tables/scalars", "SimState", "weights"},
		{"nested tables", "../../tables/pointers", "Scene", "layers"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := unit(t, c.dir)
			m := tabletext.NewModel(u)
			root := m.Lookup(c.root)
			if root == nil {
				t.Fatalf("%s defines no %s table", c.dir, c.root)
			}
			inst := m.New(root)
			field(inst, c.field).Count = 1

			cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
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

			fl := ir.RecordLayout(u, root).FieldByName(c.field)
			if fl == nil {
				t.Fatalf("%s has no field %s", c.root, c.field)
			}
			if fl.Field.Array != ir.ArrayCounted || fl.Field.ArrayBound < 3 {
				t.Fatalf("%s.%s is not a counted array with a slot 2", c.root, c.field)
			}
			pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
			step := pieces[0].Size / fl.Field.ArrayBound
			if step <= 0 {
				t.Fatalf("%s.%s is not a counted array with a slot 2", c.root, c.field)
			}

			bad := append([]byte(nil), cooked...)
			h.Data(bad)[pieces[0].Offset+2*step] = 0x5A
			_, err = tablecook.Check(m, bad)
			if err == nil {
				t.Fatalf("Check accepted a non-zero byte in slot 2 of %s.%s, past its count of 1", c.root, c.field)
			}
			if !strings.Contains(err.Error(), c.root+"."+c.field) {
				t.Errorf("the refusal does not name %s.%s: %v", c.root, c.field, err)
			}
		})
	}
}
