package tablewire_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func model(t *testing.T) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// place fills one instance from a text, so a wire case is written as the value
// it encodes rather than as a byte array.
func place(t *testing.T, m *tabletext.Model, table, text string) *tabletext.Instance {
	t.Helper()
	inst := m.New(m.Lookup(table))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) || !r.Silent() {
		t.Fatalf("%s: the text did not place cleanly: %+v", table, r)
	}
	return inst
}

// Encode then Decode is the identity on every table in the corpus, and the
// report is silent: bytes this build wrote are bytes this build reads with
// nothing skipped, nothing renamed and nothing cut down.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := model(t)
	inst := place(t, m, "PackConfig", `{
		"version": 9,
		"global": { "tick_rate": 30, "difficulty": "Easy", "spawn_delays": [1.0, 2.0, 3.0] },
		"ships": { "Bomber": { "name": "B", "health": 3.5, "gunner": {} } },
		"thresholds": { "Hard": 900 },
		"reserves": [ { "name": "R" } ]
	}`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("PackConfig"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, wire, &r)
	if err != nil || !ok {
		t.Fatalf("the decode stopped: %v %+v", err, r)
	}
	if !r.Silent() {
		t.Fatalf("expected silence, got %+v", r)
	}
	again, err := tablewire.Encode(m, back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(wire) {
		t.Fatal("encode -> decode -> encode moved bytes")
	}
}

// §2.3: PRESENCE, not content, decides whether a `?T` rides — a present
// optional holding nothing but defaults is longer on the wire than an absent
// one, and reads back present.
func TestPresentOptionalAlwaysRides(t *testing.T) {
	m := model(t)
	absent, err := tablewire.Encode(m, place(t, m, "ShipEntry", `{ "name": "X" }`))
	if err != nil {
		t.Fatal(err)
	}
	present, err := tablewire.Encode(m, place(t, m, "ShipEntry", `{ "name": "X", "gunner": {} }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(present) <= len(absent) {
		t.Fatal("a present optional at its defaults did not ride")
	}
	back := m.New(m.Lookup("ShipEntry"))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, back, present, &r); err != nil {
		t.Fatal(err)
	}
	fv, _ := back.FieldByKey("gunner")
	if !fv.Present {
		t.Fatal("a field that rode did not read back present")
	}
}

// §3.2: a stored key of 0 is None's, which keys no record — framing damage,
// not an unknown name, and the decode continues past it.
func TestKeyZeroIsMalformed(t *testing.T) {
	m := model(t)
	inst := place(t, m, "PackConfig", `{ "thresholds": { "Hard": 900 } }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// the thresholds body's first pair key sits after: id(2) kind(1) L(4)
	// elemkind(1) N(4) — find the field header and zero the key that follows
	fv, _ := inst.FieldByKey("thresholds")
	id := ir.TableFieldId(fv.Def)
	at := -1
	for i := 0; i+3 <= len(wire); i++ {
		if binary.LittleEndian.Uint16(wire[i:]) == id && wire[i+2] == ir.TableKindKeyed {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("the keyed field is not on the wire")
	}
	key := at + 2 + 1 + 4 + 1 + 4
	binary.LittleEndian.PutUint16(wire[key:], 0)

	back := m.New(m.Lookup("PackConfig"))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, back, wire, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Malformed {
		t.Fatalf("a None key should be framing damage, got %+v", r)
	}
}

// §4: a field id this reader cannot name is skipped by its kind and counted,
// and everything after it still decodes.
func TestUnknownFieldIsSkipped(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// an unknown u32 field ahead of the body's own
	unknown := []byte{0xEE, 0xEE, ir.TableKindU32, 1, 0, 0, 0}
	hostile := append(append([]byte{}, unknown...), wire...)

	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, hostile, &r); !ok || err != nil {
		t.Fatalf("an unknown field should not stop the decode: %v %+v", err, r)
	}
	if r.Unknown != 1 || r.Malformed {
		t.Fatalf("expected one unknown, got %+v", r)
	}
	fv, _ := back.FieldByKey("tick_rate")
	if fv.Cell.U != 90 {
		t.Fatal("the field after the unknown one did not decode")
	}
}

// §3.2, §4: a keyed body sent under the POSITIONAL array kind is an ordinary
// kind mismatch — skipped, counted, never misdecoded, which is the whole point
// of the keyed body carrying its own kind.
func TestKeyedUnderArrayKindIsAMismatch(t *testing.T) {
	m := model(t)
	inst := place(t, m, "PackConfig", `{ "thresholds": { "Hard": 900 } }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	fv, _ := inst.FieldByKey("thresholds")
	id := ir.TableFieldId(fv.Def)
	for i := 0; i+3 <= len(wire); i++ {
		if binary.LittleEndian.Uint16(wire[i:]) == id && wire[i+2] == ir.TableKindKeyed {
			wire[i+2] = ir.TableKindArray
			break
		}
	}
	back := m.New(m.Lookup("PackConfig"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, wire, &r); !ok || err != nil {
		t.Fatalf("a kind mismatch should not stop the decode: %v %+v", err, r)
	}
	if r.KindMismatch != 1 {
		t.Fatalf("expected one kind_mismatch, got %+v", r)
	}
	slots, _ := back.FieldByKey("thresholds")
	hard := tabletext.KeyedValueSlot(slots.Def, tabletext.EnumValue(slots.Def.KeyEnumRef, "Hard"))
	if slots.Elems[hard].U != 0 {
		t.Fatal("a mismatched body was decoded into the slots anyway")
	}
}

// A variable-length root rides the engine.s wire under §3.1.s flat node table:
// an empty pointered root reaches no node, writes none of them, and its bytes
// are the root body alone.
func TestVariableRootRidesTheWire(t *testing.T) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	variable := ir.VariableTables(u)
	for _, name := range m.Roots() {
		if !variable[name] {
			continue
		}
		// an empty pointered root reaches no node, so it writes none of them
		// and its bytes are the root body alone
		wire, err := tablewire.Encode(m, m.New(m.Lookup(name)))
		if err != nil {
			t.Fatalf("%s: the wire engine refused a variable-length root: %v", name, err)
		}
		var r tabletext.Report
		if _, err := tablewire.Decode(m, m.New(m.Lookup(name)), wire, &r); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !r.Silent() {
			t.Fatalf("%s: an empty pointered root did not read back clean: %+v", name, r)
		}
		return
	}
	t.Skip("the pointers corpus declares no variable-length root")
}

// AN OPTIONAL ARRAY DECLARED BEFORE A POINTER, in a table the pointer makes
// VARIABLE (docs/SPEC-TABLES.md §2.3, §3.1). `?[..N]T` of a scalar holds no
// edge, so the declaration-order pointer walk must step over it and reach the
// pointer alone: the node table is the same whether the array is absent or
// present, and only the root body's bytes differ by the array's own framing.
// A walk that counted the array as an edge would number a different graph,
// and #433's law is that the numbering, the pack measure and the pack are one
// walk — so a field it must skip is worth pinning where it is declared FIRST.
func TestOptionalArrayBeforeAPointerMovesNoNode(t *testing.T) {
	dir := t.TempDir()
	src := "package edgecase\n\n" +
		"table Node\n{\n    value int32\n    next  *Node\n}\n\n" +
		"table Holder\n{\n    marks ?[..2]int32\n    head  *Node\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "EdgeCase.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("an optional array beside a pointer field did not compile: %v", err)
	}
	if !ir.VariableTables(u)["Holder"] {
		t.Fatal("Holder holds a pointer and is not derived VARIABLE")
	}
	m := tabletext.NewModel(u)

	// a two-node chain off `head`, and the array absent
	absent := place(t, m, "Holder", `{ "head": { "value": 1, "next": { "value": 2 } } }`)
	absentWire, err := tablewire.Encode(m, absent)
	if err != nil {
		t.Fatal(err)
	}
	// the same graph with the array PRESENT and empty: the five-byte array
	// body rides in the root and nothing else moves
	present := place(t, m, "Holder", `{ "marks": [], "head": { "value": 1, "next": { "value": 2 } } }`)
	presentWire, err := tablewire.Encode(m, present)
	if err != nil {
		t.Fatal(err)
	}
	if len(presentWire) != len(absentWire)+3+4+5 {
		t.Fatalf("a present empty optional array moved %d bytes, want %d",
			len(presentWire)-len(absentWire), 3+4+5)
	}

	// both read back with the graph intact, and the array's state is its own
	for _, arm := range []struct {
		name    string
		wire    []byte
		present bool
	}{{"absent", absentWire, false}, {"present", presentWire, true}} {
		back := m.New(m.Lookup("Holder"))
		var r tabletext.Report
		ok, err := tablewire.Decode(m, back, arm.wire, &r)
		if err != nil || !ok || !r.Silent() {
			t.Fatalf("%s: the decode did not read clean: %v %v %+v", arm.name, err, ok, r)
		}
		if back.Fields[0].Present != arm.present {
			t.Fatalf("%s: marks reads present=%v", arm.name, back.Fields[0].Present)
		}
		head := back.Fields[1].Cell.Node
		if head == nil || head.Fields[1].Cell.Node == nil {
			t.Fatalf("%s: the two-node chain off head did not survive the walk", arm.name)
		}
		if head.Fields[0].Cell.I != 1 || head.Fields[1].Cell.Node.Fields[0].Cell.I != 2 {
			t.Fatalf("%s: the chain's values moved", arm.name)
		}
	}
}
