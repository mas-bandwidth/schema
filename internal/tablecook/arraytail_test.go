package tablecook_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCheckRefusesCountedArrayTail: a counted array's slots at or past its
// used count are dead bytes, and in a cook they are zero (docs/SPEC-TABLES.md
// §7.2, §7.4). The clean cook passes; a non-zero byte in a dead slot of either
// a counted scalar array or a counted array of nested tables is refused, and
// the refusal names the field.
func TestCheckRefusesCountedArrayTail(t *testing.T) {
	t.Parallel()
	u := unit(t, "../../tables/examples")
	m := tabletext.NewModel(u)
	root := m.Lookup("LoadoutConfig")
	if root == nil {
		t.Fatal("Tables.schema defines no LoadoutConfig table")
	}

	inst := m.New(root)
	grades := field(inst, "grades")
	grades.Elems = []tabletext.Cell{{I: 1, U: 1}}
	grades.Count = 1
	attachments := field(inst, "attachments")
	attachment := m.New(m.Lookup("Attachment"))
	setInt(attachment, "slot", 3)
	attachments.Elems = []tabletext.Cell{{Tab: attachment}}
	attachments.Count = 1

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

	ml := ir.RecordLayout(u, root)
	for _, c := range []struct {
		name  string
		field string
	}{
		{"grades", "grades"},
		{"attachments", "attachments"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fl := ml.FieldByName(c.field)
			if fl == nil {
				t.Fatalf("LoadoutConfig has no field %s", c.field)
			}
			pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
			if len(pieces) != 2 {
				t.Fatalf("%s has %d storage pieces, not an array and its count", c.field, len(pieces))
			}
			step := pieces[0].Size / fl.Field.ArrayBound
			at := h.DataOffset() + pieces[0].Offset + 2*step

			bad := append([]byte(nil), cooked...)
			bad[at] = 1
			if _, err := tablecook.Check(m, bad); err == nil {
				t.Fatalf("Check accepted a non-zero byte in %s's slot 2, past its used count", c.field)
			} else if !strings.Contains(err.Error(), c.field) {
				t.Fatalf("the refusal does not name %s: %v", c.field, err)
			}
		})
	}
}
