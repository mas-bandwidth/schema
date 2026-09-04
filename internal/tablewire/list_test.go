// The UNBOUNDED ARRAY on the wire and in the text (docs/SPEC-TABLES.md §2.9),
// as the tool's two halves carry it. Every case here is one of the section's
// own claims, and each names the sabotage it goes red for.
package tablewire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// listUnit is §2.9's own example, and listBoundUnit is the SAME CONTENT under
// bounded declarations — the two halves of the `list_migrates` claim.
const listUnit = `package save

table Placement
{
    x     float32
    y     float32
    model uint32
}

table LogEntry { tick uint32 }

table Save
{
    placements []Placement
    log        []*LogEntry
    scores     []int32
}
`

const listBoundUnit = `package save

table Placement
{
    x     float32
    y     float32
    model uint32
}

table LogEntry { tick uint32 }

table Save
{
    placements [..8]Placement
    log        [..8]*LogEntry
    scores     [..8]int32
}
`

// the section's own text, `null` slot and shared `&node` included
const listText = `{
  "placements": [ { "x": 1.0, "y": 2.0, "model": 3 },
                  { "x": 3.0, "y": 4.0, "model": 7 } ],
  "log": [ { "&node": 1, "tick": 7 }, { "&node": 1 }, null ],
  "scores": [ 10, 20, 30 ]
}`

func listModel(t *testing.T, src string) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Save.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// TestListWireIsTheBoundedArrayWire is `list_migrates` as a unit test: ONE
// content, TWO declarations of the holder, and the same bytes from both. It is
// what makes the bound a declaration-side fact and never a wire fact (§2.9).
func TestListWireIsTheBoundedArrayWire(t *testing.T) {
	unbounded, err := tablewire.Encode(listModel(t, listUnit), place(t, listModel(t, listUnit), "Save", listText))
	if err != nil {
		t.Fatal(err)
	}
	bounded, err := tablewire.Encode(listModel(t, listBoundUnit), place(t, listModel(t, listBoundUnit), "Save", listText))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unbounded, bounded) {
		t.Fatalf("[]T and [..N]T are one wire (§2.9): %d bytes against %d, and they differ", len(unbounded), len(bounded))
	}
	// and the bounded declaration READS the unbounded writer's bytes back to
	// equal values, in silence, which is the other half of the claim
	m := listModel(t, listBoundUnit)
	back := m.New(m.Lookup("Save"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, unbounded, &r)
	if err != nil || !ok {
		t.Fatalf("the bounded reader refused the unbounded writer's bytes: ok=%v err=%v", ok, err)
	}
	if !r.Silent() {
		t.Fatalf("the read was not silent: %+v", r)
	}
	again, err := tablewire.Encode(m, back)
	if err != nil || !bytes.Equal(again, unbounded) {
		t.Fatalf("the bounded read did not reproduce the value: %v", err)
	}
}

// TestListRoundTrip is the wire and the text over one another: the elements in
// INDEX order, a `[]*T`'s two slots naming ONE node, and a null slot.
func TestListRoundTrip(t *testing.T) {
	m := listModel(t, listUnit)
	inst := place(t, m, "Save", listText)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("Save"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, wire, &r)
	if err != nil || !ok || !r.Silent() {
		t.Fatalf("decode: ok=%v err=%v report=%+v", ok, err, r)
	}
	again, err := tablewire.Encode(m, back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, again) {
		t.Fatal("the round trip did not reproduce the wire: measure == save over a list is the arithmetic alone (§2.9)")
	}
	// THE COUNTS ARE THE DATA'S: three lists, at 2, 3 and 3
	for _, want := range []struct {
		field string
		count int
	}{{"placements", 2}, {"log", 3}, {"scores", 3}} {
		fv := fieldByName(t, back, want.field)
		if fv.Count != want.count || len(fv.Elems) != want.count {
			t.Fatalf("%s: count %d over %d slots, want %d of each", want.field, fv.Count, len(fv.Elems), want.count)
		}
	}
	// A SHARED NODE IS ONE NODE: the two `&node` slots resolve to one record,
	// and the third slot is null
	log := fieldByName(t, back, "log")
	if log.Elems[0].Node == nil || log.Elems[0].Node != log.Elems[1].Node {
		t.Fatal("two slots naming one node hold one node (§2.9, §3.1)")
	}
	if log.Elems[2].Node != nil {
		t.Fatal("a null element of a []*T is a null slot (§16.2)")
	}
}

// TestListEmptyElides is §3's by-value elision rule at this construct: a fresh
// list is empty, an empty list is elided, and its count's zero IS its absence
// — which is why there is no `?[]T`.
func TestListEmptyElides(t *testing.T) {
	m := listModel(t, listUnit)
	empty, err := tablewire.Encode(m, place(t, m, "Save", `{}`))
	if err != nil {
		t.Fatal(err)
	}
	one, err := tablewire.Encode(m, place(t, m, "Save", `{ "scores": [ 1 ] }`))
	if err != nil {
		t.Fatal(err)
	}
	// three empty lists and nothing else: the form byte, the body's own
	// terminator and an id table of zero entries, and not one field header
	if len(empty) != 10 {
		t.Fatalf("an empty list rides NO bytes at all: %d, want the form byte, the terminator and an empty id table: %x", len(empty), empty)
	}
	if len(one) <= len(empty) {
		t.Fatalf("one element rides: %d against %d empty", len(one), len(empty))
	}
	back := m.New(m.Lookup("Save"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, empty, &r); !ok || err != nil || !r.Silent() {
		t.Fatalf("an all-empty save did not read back silently: ok=%v err=%v %+v", ok, err, r)
	}
	if fieldByName(t, back, "scores").Count != 0 {
		t.Fatal("an elided list reads back empty")
	}
}

// TestListTextReadsEveryElement is §16.2's row for the construct: EVERY
// element the text carries is read, because there is no bound to drop a tail
// against, and `clamped` cannot fire on the count.
func TestListTextReadsEveryElement(t *testing.T) {
	m := listModel(t, listUnit)
	var b bytes.Buffer
	b.WriteString(`{ "scores": [`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("1")
	}
	b.WriteString("] }")
	inst := m.New(m.Lookup("Save"))
	var r tabletext.Report
	if !m.Read(inst, b.Bytes(), &r) {
		t.Fatal("the text did not place")
	}
	if r.Clamped != 0 {
		t.Fatalf("clamped cannot fire on a list's count (§2.9): %d", r.Clamped)
	}
	if got := fieldByName(t, inst, "scores").Count; got != 100 {
		t.Fatalf("every element the text carries is read: %d of 100", got)
	}
	// and `null` where an element stands is a kind_mismatch, the array row's
	// own rule, because the element is not a pointer
	var nulls tabletext.Report
	if !m.Read(m.New(m.Lookup("Save")), []byte(`{ "scores": [ 1, null, 3 ] }`), &nulls) {
		t.Fatal("the text did not place")
	}
	if nulls.KindMismatch != 1 {
		t.Fatalf("null is a kind_mismatch at a scalar element (§16.2): %d", nulls.KindMismatch)
	}
}

// TestListCountOverLengthIsFramingDamage is §2.9's first overflow row on the
// BUILDER side: a count the body cannot cover lands the prefix the body
// covers, counts `malformed`, and the parent reads on past the field's L.
func TestListCountOverLength(t *testing.T) {
	m := listModel(t, listUnit)
	wire, err := tablewire.Encode(m, place(t, m, "Save", `{ "scores": [ 10, 20, 30 ] }`))
	if err != nil {
		t.Fatal(err)
	}
	// the scores body is `kind 4 (i32)` then the count 3 then twelve bytes;
	// raising the count to 30 leaves an N the L cannot carry
	damaged := append([]byte(nil), wire...)
	at := bytes.Index(damaged, []byte{byte(ir.TableKindI32), 3, 10, 0, 0, 0})
	if at < 0 {
		t.Fatal("the scores body is not where this test expects it")
	}
	damaged[at+1] = 30
	back := m.New(m.Lookup("Save"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, damaged, &r); !ok || err != nil {
		t.Fatalf("the walk stopped rather than reporting: ok=%v err=%v", ok, err)
	}
	if !r.Malformed {
		t.Fatal("a count the body cannot cover is framing damage (§2.9, §4)")
	}
	if r.Clamped != 0 {
		t.Fatalf("clamped cannot fire on a list's count (§2.9): %d", r.Clamped)
	}
	if got := fieldByName(t, back, "scores").Count; got != 3 {
		t.Fatalf("the prefix the body covers lands: %d of 3", got)
	}
}

// TestListElementKindMismatch is §3's element-kind rule at this construct: the
// field is skipped whole by its L, the list reads empty, and one
// `kind_mismatch` counts.
func TestListElementKindMismatch(t *testing.T) {
	m := listModel(t, listUnit)
	wire, err := tablewire.Encode(m, place(t, m, "Save", `{ "scores": [ 10, 20, 30 ] }`))
	if err != nil {
		t.Fatal(err)
	}
	swapped := append([]byte(nil), wire...)
	at := bytes.Index(swapped, []byte{byte(ir.TableKindI32), 3, 10, 0, 0, 0})
	if at < 0 {
		t.Fatal("the scores body is not where this test expects it")
	}
	swapped[at] = byte(ir.TableKindF32) // []int32 read as []float32
	back := m.New(m.Lookup("Save"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, swapped, &r); !ok || err != nil {
		t.Fatalf("decode: ok=%v err=%v", ok, err)
	}
	if r.KindMismatch != 1 || r.Malformed {
		t.Fatalf("an element kind that disagrees is one kind_mismatch and no damage: %+v", r)
	}
	if got := fieldByName(t, back, "scores").Count; got != 0 {
		t.Fatalf("the list reads empty: %d", got)
	}
}

// TestListWalkOrder is `list_before_pointer`: a `[]*T` DECLARED BEFORE a
// pointer field reaches its shared node FIRST and numbers it first, because
// the numbering is one walk over the fields in declaration order that descends
// each by-value edge WHERE IT IS DECLARED (§2.9, §3.1). It is
// `stream_arm_first`'s shape at this construct.
func TestListWalkOrder(t *testing.T) {
	const src = `package walk

table Mark { tick uint32 }

table Head
{
    marks []*Mark
    solo  *Mark
}
`
	m := listModel(t, src)
	// the list's own node is declared first, so it takes index 2 (record 1)
	// and the pointer field's takes index 3
	inst := place(t, m, "Head", `{ "marks": [ { "&node": 1, "tick": 7 } ], "solo": { "&node": 2, "tick": 9 } }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// the FIRST record body after the root carries tick 7, the list's node:
	// find the two tick payloads in the order they were written
	first := bytes.Index(wire, []byte{byte(ir.TableKindU32), 7, 0, 0, 0})
	second := bytes.Index(wire, []byte{byte(ir.TableKindU32), 9, 0, 0, 0})
	if first < 0 || second < 0 {
		t.Fatalf("both records ride: %d and %d", first, second)
	}
	if first > second {
		t.Fatal("a []*T declared before a pointer field reaches its node first and numbers it first (§2.9, §3.1)")
	}
	back := m.New(m.Lookup("Head"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, wire, &r); !ok || err != nil || !r.Silent() {
		t.Fatalf("decode: ok=%v err=%v %+v", ok, err, r)
	}
	again, err := tablewire.Encode(m, back)
	if err != nil || !bytes.Equal(again, wire) {
		t.Fatal("the round trip did not reproduce the numbering")
	}
}

// TestListOfTablesReachesTheirEdges: a list of TABLES is a by-value edge, so
// each element is descended for the pointer slots inside it before the next
// element is reached (§2.9, §3.1). It is the `list_nested` shape at one depth.
func TestListOfTablesReachesTheirEdges(t *testing.T) {
	const src = `package deep

table Leaf { tick uint32 }

table Row  { leaf *Leaf }

table Sheet { rows []Row }
`
	m := listModel(t, src)
	inst := place(t, m, "Sheet", `{ "rows": [ { "leaf": { "&node": 1, "tick": 7 } }, { "leaf": { "&node": 1 } }, { "leaf": null } ] }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("Sheet"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, wire, &r); !ok || err != nil || !r.Silent() {
		t.Fatalf("decode: ok=%v err=%v %+v", ok, err, r)
	}
	rows := fieldByName(t, back, "rows")
	if rows.Count != 3 {
		t.Fatalf("three rows ride: %d", rows.Count)
	}
	// ONE NODE reached from inside two elements, and a null in the third
	a := rows.Elems[0].Tab.Fields[0].Cell.Node
	b := rows.Elems[1].Tab.Fields[0].Cell.Node
	if a == nil || a != b {
		t.Fatal("a node named from two elements is one node (§2.9, §3.1)")
	}
	if rows.Elems[2].Tab.Fields[0].Cell.Node != nil {
		t.Fatal("a null pointer inside an element stays null")
	}
	again, err := tablewire.Encode(m, back)
	if err != nil || !bytes.Equal(again, wire) {
		t.Fatal("the round trip did not reproduce the wire")
	}
}

func fieldByName(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("%s declares no field %s", inst.Def.Name, name)
	return nil
}
