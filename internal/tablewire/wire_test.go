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

// ---- the id-table wire, as a test reaches into it (docs/SPEC-TABLES.md §3) ----

// trailerOf splits a wire into its body and its id table, which is what a
// reader does at open: the final eight bytes are the entry count, the
// `8 x count` before them are the entries, and the body ends where the first
// entry begins.
func trailerOf(t *testing.T, wire []byte) (body []byte, ids []uint64) {
	t.Helper()
	if len(wire) < 9 {
		t.Fatal("a wire is at least a form byte and an entry count")
	}
	count := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	first := len(wire) - count*8 - 8
	ids = make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[first+i*8:])
	}
	return wire[1:first], ids
}

// leb is one canonical unsigned LEB128, which every reference, length, count
// and index on this wire is.
func leb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// lebAt reads one canonical LEB128 at an offset and answers the offset after
// it, which is how a test steps through a header whose numbers are variable.
func lebAt(b []byte, off int) (v uint64, next int) {
	shift := uint(0)
	for {
		x := b[off]
		off++
		v |= uint64(x&0x7F) << shift
		if x&0x80 == 0 {
			return v, off
		}
		shift += 7
	}
}

// headerAt is the offset INSIDE the body of the field header naming `id` under
// `kind`: the id's reference, then the kind byte.
func headerAt(t *testing.T, wire []byte, id uint64, kind byte) int {
	t.Helper()
	body, ids := trailerOf(t, wire)
	ref := uint64(0)
	for i, entry := range ids {
		if entry == id {
			ref = uint64(i) + 1
			break
		}
	}
	if ref == 0 {
		t.Fatalf("the id %016x is not in the wire's table", id)
	}
	want := append(leb(ref), kind)
	for i := 0; i+len(want) <= len(body); i++ {
		if string(body[i:i+len(want)]) == string(want) {
			return i
		}
	}
	t.Fatalf("no field header for %016x under kind %d", id, kind)
	return -1
}

// withField prepends one field to a wire's ROOT body under an id the table
// gains an entry for. Field ORDER within a body is not part of the contract
// (§3), so a reader finds it wherever it sits.
func withField(t *testing.T, wire []byte, id uint64, kind byte, payload []byte) []byte {
	t.Helper()
	body, ids := trailerOf(t, wire)
	ref := uint64(len(ids)) + 1
	for i, entry := range ids {
		if entry == id {
			ref = uint64(i) + 1
		}
	}
	out := []byte{ir.TableWireForm}
	out = append(out, leb(ref)...)
	out = append(out, kind)
	out = append(out, payload...)
	out = append(out, body...)
	if ref > uint64(len(ids)) {
		ids = append(ids, id)
	}
	for _, entry := range ids {
		out = binary.LittleEndian.AppendUint64(out, entry)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
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
	// step over the field header, the body length, the element kind and the
	// count to the first triple's KEY REFERENCE, and write the reference that
	// names no id
	fv, _ := inst.FieldByKey("thresholds")
	body, _ := trailerOf(t, wire)
	at := headerAt(t, wire, ir.TableFieldWireId(fv.Def), ir.TableKindKeyed)
	_, off := lebAt(body, at)
	_, off = lebAt(body, off+1)
	_, off = lebAt(body, off+1)
	body[off] = 0

	back := m.New(m.Lookup("PackConfig"))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, back, wire, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Malformed {
		t.Fatalf("a key reference of 0 should be framing damage, got %+v", r)
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
	// an unknown u32 field ahead of the body's own, under an id the table
	// gains an entry for
	hostile := withField(t, wire, 0xEEEEEEEEEEEEEEEE, ir.TableKindU32, []byte{1, 0, 0, 0})

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

// §3: an entry this reader cannot name is never counted at RESOLVE time — a
// table with an unnameable entry no body references counts nothing at all.
func TestUnnameableEntryCountsNothing(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range append(ids, 0xEEEEEEEEEEEEEEEE) {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids))+1)

	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, out, &r); !ok || err != nil {
		t.Fatalf("an unreferenced entry should not stop the decode: %v %+v", err, r)
	}
	if !r.Silent() {
		t.Fatalf("resolving an unnameable entry counted something: %+v", r)
	}
}

// §3: A TABLE THAT CARRIES ONE ID TWICE is malformed for the WHOLE wire, and
// nothing is decoded.
func TestRepeatedEntryIsMalformed(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range append(ids, ids[0]) {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids))+1)

	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, out, &r)
	if err != nil || ok || !r.Malformed {
		t.Fatalf("one id in two entries is malformed for the whole wire: %v %v %+v", err, ok, r)
	}
	fv, _ := back.FieldByKey("tick_rate")
	if fv.Cell.U == 90 {
		t.Fatal("nothing is decoded under a malformed table")
	}
}

// §3: a FORM BYTE this reader does not know is a named refusal and never
// damage, whatever else is wrong with the file — the form byte is read FIRST.
// Form 2 is the MESSAGE form and rides here too: a message stored on its own
// is not readable, because its table is somewhere else, so a FILE reader
// handed one refuses by name (docs/SPEC-TABLES.md §3.3).
func TestUnknownFormIsARefusal(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range []byte{0, 2, 0xFF} {
		damaged := append([]byte(nil), wire...)
		damaged[0] = form
		damaged = damaged[:len(damaged)-3] // and damaged too, in the trailer
		back := m.New(m.Lookup("GlobalSettings"))
		var r tabletext.Report
		ok, err := tablewire.Decode(m, back, damaged, &r)
		if !tablewire.Refused(err) {
			t.Fatalf("form %d: expected a named refusal, got %v", form, err)
		}
		if ok || !r.Silent() {
			t.Fatalf("form %d: a refusal moves no counter and reports no damage: %+v", form, r)
		}
	}
}

// §3: a reference ABOVE the entry count is framing damage on the body that
// carries it, and the LAST legal slot must resolve.
func TestReferenceBound(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	if body[0] != 1 {
		t.Fatalf("the first field's reference is %d, not the first slot", body[0])
	}
	// the last legal slot RESOLVES
	body[0] = byte(len(ids))
	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, back, wire, &r); err != nil {
		t.Fatal(err)
	}
	if r.Malformed {
		t.Fatalf("the last legal slot must resolve: %+v", r)
	}
	// one past it does not
	body[0] = byte(len(ids) + 1)
	back = m.New(m.Lookup("GlobalSettings"))
	r = tabletext.Report{}
	if _, err := tablewire.Decode(m, back, wire, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Malformed {
		t.Fatalf("a reference past the table is framing damage: %+v", r)
	}
}

// §3: a NON-CANONICAL reference is malformed — one value has one spelling.
func TestNonCanonicalReferenceIsMalformed(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	out := append([]byte{ir.TableWireForm}, 0x81, 0x00)
	out = append(out, body[1:]...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids)))

	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	if _, err := tablewire.Decode(m, back, out, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Malformed {
		t.Fatalf("a non-minimal reference is malformed: %+v", r)
	}
}

// §3: a byte between the root's terminator and the table's first entry is
// malformed for the WHOLE wire, because no field claims it.
func TestStrayByteBeforeTheTableIsMalformed(t *testing.T) {
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	out := append([]byte{ir.TableWireForm}, body...)
	out = append(out, 0x7F) // a byte no field claims
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids)))

	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	ok, err := tablewire.Decode(m, back, out, &r)
	if err != nil || ok || !r.Malformed {
		t.Fatalf("a stray byte is malformed for the whole wire: %v %v %+v", err, ok, r)
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
	body, _ := trailerOf(t, wire)
	at := headerAt(t, wire, ir.TableFieldWireId(fv.Def), ir.TableKindKeyed)
	_, kindAt := lebAt(body, at)
	body[kindAt] = ir.TableKindArray
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
	// the same graph with the array PRESENT and empty: the two-byte array body
	// rides in the root under its own header and nothing else moves
	present := place(t, m, "Holder", `{ "marks": [], "head": { "value": 1, "next": { "value": 2 } } }`)
	presentWire, err := tablewire.Encode(m, present)
	if err != nil {
		t.Fatal(err)
	}
	// the header is the reference and the kind byte, the length is one byte,
	// the body is the element kind and a zero count — and the id table gains
	// the one entry `marks` costs, once
	if len(presentWire) != len(absentWire)+1+1+1+2+8 {
		t.Fatalf("a present empty optional array moved %d bytes, want %d",
			len(presentWire)-len(absentWire), 1+1+1+2+8)
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
