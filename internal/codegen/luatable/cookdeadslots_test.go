package luatable

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COUNTED ARRAY'S DEAD SLOTS ARE ZERO (docs/SPEC-TABLES.md §7.2;
// docs/rulings.json fixed-dead-bytes-are-zero). A value with fewer live
// elements than the bound must cook the same file the tool does: the slots
// past the live count stay zero in both byte orders, whatever the element
// type's declared defaults are. A fixed or enum-keyed array and every slot
// under the live count keep the element value.
const deadSlotSchema = `package probe

table Slot
{
    a float64 = 21.0
    b int32 = 500
}

table Root
{
    items [..4]Slot
}
`

func TestLuaCookWriteDeadSlotsAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	u, files := declaredFormsUnit(t, deadSlotSchema)

	fresh := tabletext.NewModel(u).New(u.Tables["Slot"])
	if fresh.Fields[0].Cell.F != 21.0 || fresh.Fields[1].Cell.I != 500 {
		t.Fatalf("the element's declared defaults are %v %v, want 21 500",
			fresh.Fields[0].Cell.F, fresh.Fields[1].Cell.I)
	}

	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	items := &root.Fields[0]
	items.Count = 2
	for i, v := range [][2]float64{{1.5, 7}, {2.5, 8}} {
		slot := items.Elems[i].Tab
		slot.Fields[0].Cell.F = v[0]
		slot.Fields[1].Cell = tabletext.Cell{I: int64(v[1]), U: uint64(v[1])}
	}

	var cakes [][]byte
	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: cook: %v", big, err)
		}
		cakes = append(cakes, cooked)
	}

	layout := ir.RecordLayout(u, u.Tables["Root"])
	facts := ir.BlockFieldOf(u, items.Def, layout.Fields[0].Offset, false)
	if !facts.Counted || facts.ArrayBound != 4 {
		t.Fatalf("the unit's counted array is not 4 slots: %+v", facts)
	}
	firstDead := int64(64) + layout.Fields[0].Offset + 2*facts.ElemSize
	end := int64(64) + layout.Fields[0].Offset + 4*facts.ElemSize
	for order, cooked := range cakes {
		if dead := cooked[firstDead:end]; len(dead) == 0 || !bytes.Equal(dead, make([]byte, len(dead))) {
			t.Fatalf("order %d: the tool's dead slots are not zero: %x", order+1, dead)
		}
	}

	probe := fmt.Sprintf(`local T = require(%q)
local C = require("cookwrite")
local inputs = require("inputs")
local v = T.NewRoot()
local a = T.NewSlot()
a.a, a.b = 1.5, 7
local b = T.NewSlot()
b.a, b.b = 2.5, 8
v.items = { a, b }
for order = 1, 2 do
  local bytes = assert(C.Cook(T, "Root", v, nil, order))
  local dead = bytes:sub(%d, %d)
  assert(dead == string.rep("\0", %d), "a counted array's dead slots are not zero")
  assert(bytes == inputs["cook" .. order], "the cook differs from the tool's file")
end
print("dead slots zero")
`, ModuleName(u), firstDead+1, end, end-firstDead)
	out := declaredFormsRun(t, files, probe, map[string][]byte{"cook1": cakes[0], "cook2": cakes[1]})
	if !bytes.Contains(out, []byte("dead slots zero")) {
		t.Fatalf("%s", out)
	}
}
