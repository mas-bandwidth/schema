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

// AN ABSENT OPTIONAL'S PAYLOAD IS ZERO (docs/SPEC-TABLES.md §7.2, §7.6;
// docs/rulings.json fixed-dead-bytes-are-zero). An absent optional reads as
// its declared default, and in the cooked form its presence byte is zero and
// the bytes its payload occupies are zero too, in both byte orders, whatever
// the payload type's declared defaults are and whatever the value still holds:
// the Lua cook writer writes the same file the tool does. A present optional
// is written as before.
const absentOptionalSchema = `package probe

table Inner
{
    a float64 = 21.0
    b int32 = 500
}

table Root
{
    level ?int32
    inner ?Inner
    kept  ?Inner
    tail  int32 = 3
}
`

func TestLuaCookWriteAbsentOptionalIsZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	u, files := declaredFormsUnit(t, absentOptionalSchema)

	fresh := tabletext.NewModel(u).New(u.Tables["Inner"])
	if fresh.Fields[0].Cell.F != 21.0 || fresh.Fields[1].Cell.I != 500 {
		t.Fatalf("the record's declared defaults are %v %v, want 21 500",
			fresh.Fields[0].Cell.F, fresh.Fields[1].Cell.I)
	}

	// the tool's file: level and inner absent, kept present at its defaults
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	root.Fields[2].Present = true

	var cakes [][]byte
	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: cook: %v", big, err)
		}
		cakes = append(cakes, cooked)
	}

	// each absent optional's whole storage, payload and presence byte
	layout := ir.RecordLayout(u, u.Tables["Root"])
	type span struct{ from, to int64 }
	var absent []span
	for i := range 2 {
		fl := layout.Fields[i]
		pieces := ir.FieldPieces(u, fl.Field, 64+fl.Offset)
		last := pieces[len(pieces)-1]
		absent = append(absent, span{64 + fl.Offset, last.Offset + 1})
	}
	for order, cooked := range cakes {
		for _, s := range absent {
			if got := cooked[s.from:s.to]; len(got) == 0 || !bytes.Equal(got, make([]byte, len(got))) {
				t.Fatalf("order %d: the tool's absent optional at %d is not zero: %x", order+1, s.from, got)
			}
		}
	}

	probe := fmt.Sprintf(`local T = require(%q)
local C = require("cookwrite")
local inputs = require("inputs")
local v = T.NewRoot()
-- absent, but holding values: the payload is still zero
v.level, v.level_present = 77, false
v.inner, v.inner_present = T.NewInner(), false
v.inner.a, v.inner.b = 1.5, 9
v.kept, v.kept_present = T.NewInner(), true
for order = 1, 2 do
  local bytes = assert(C.Cook(T, "Root", v, nil, order))
  assert(bytes:sub(%d, %d) == string.rep("\0", %d), "order " .. order .. ": an absent scalar optional's storage is not zero")
  assert(bytes:sub(%d, %d) == string.rep("\0", %d), "order " .. order .. ": an absent record optional's storage is not zero")
  assert(bytes == inputs["cook" .. order], "order " .. order .. ": the cook differs from the tool's file")
end
print("absent optionals zero")
`, ModuleName(u),
		absent[0].from+1, absent[0].to, absent[0].to-absent[0].from,
		absent[1].from+1, absent[1].to, absent[1].to-absent[1].from)
	out := declaredFormsRun(t, files, probe, map[string][]byte{"cook1": cakes[0], "cook2": cakes[1]})
	if !bytes.Contains(out, []byte("absent optionals zero")) {
		t.Fatalf("%s", out)
	}
}
