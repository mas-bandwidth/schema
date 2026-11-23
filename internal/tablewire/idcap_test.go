package tablewire

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE REFERENCE WRITER REFUSES AN ID TABLE ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3, ruling R8): the values needing 4095 and 4096 ids
// encode, and the one needing 4097 is refused, so Encode never produces a file
// every reader answers wire_damaged. IdCapSchema is the schema every port's
// writer test shares.
func TestEncodeRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	m := covModel(t, IdCapSchema())
	set := func(x, y uint64) []byte {
		t.Helper()
		inst := m.New(m.Lookup("Root"))
		for i := range inst.Fields {
			switch inst.Fields[i].Def.Name {
			case "keys":
				for e := range inst.Fields[i].Elems {
					inst.Fields[i].Elems[e].U = 1
				}
			case "x":
				inst.Fields[i].Cell.U = x
			case "y":
				inst.Fields[i].Cell.U = y
			}
		}
		out, err := Encode(m, inst)
		if err != nil {
			if !strings.Contains(err.Error(), "id table needs") {
				t.Fatalf("x=%d y=%d: wrong refusal: %v", x, y, err)
			}
			return nil
		}
		return out
	}
	for _, row := range []struct {
		x, y uint64
		ids  int
	}{{0, 0, IdCapKeysIds}, {1, 0, ir.TableWireIdTableBound}} {
		out := set(row.x, row.y)
		if out == nil {
			t.Fatalf("%d ids: the writer refused a table at or under the bound", row.ids)
		}
		if _, ids, ok := trailer(out); !ok || len(ids) != row.ids {
			t.Fatalf("%d ids: the file's own reader answered %d entries, ok=%v", row.ids, len(ids), ok)
		}
	}
	if out := set(1, 1); out != nil {
		t.Fatalf("%d ids: the writer wrote a file past the bound (%d bytes)", ir.TableWireIdTableBound+1, len(out))
	}
}
