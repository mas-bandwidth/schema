// Input no writer produces is damage (docs/SPEC-TABLES.md §4, the
// maintainer's 2026-10-31 list): a body carrying one id twice, a collection
// header that does not lie inside its own L, a repeated map key or keyed
// slot, bytes left inside a nested union's arm, and a JSON object naming one
// member twice. No writer emits any of these, so every read refuses the
// whole load. These are the engine's NoWriter gates: the cases the old
// PR #2557 listed, checked against main's strict read, which already
// refuses each binary case below through the wire-alone walk; this file
// pins that verdict so a tolerant path cannot return unnoticed.
package tablewire_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const noWriterSchema = `package nowriter

enum Slot { A, B }

table Inner { x int32 }

union InnerU
{
    number int32
}

union OuterU
{
    inner InnerU
    other int32
}

table Root
{
    a       int32
    inner   Inner
    scores  []int32
    tally   [Slot]int32
    entries map[int32]int32
    choice  OuterU
}
`

func noWriterModel(t *testing.T) *tabletext.Model {
	t.Helper()
	return listModel(t, noWriterSchema)
}

// noWriterWire frames a root body under the id table given, in slot order:
// a reference in the body is the 1-based slot of its id.
func noWriterWire(ids []uint64, body []byte) []byte {
	wire := []byte{ir.TableWireForm}
	wire = append(wire, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func nwLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func nwCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func nwI32(v int32) []byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], uint32(v))
	return out[:]
}

// nwField is one field: reference, kind byte, then payload bytes already
// framed for that kind (a length-prefixed kind carries its own L).
func nwField(ref byte, kind int, payload ...byte) []byte {
	return nwCat([]byte{ref, byte(kind)}, payload)
}

func nwLen(payload []byte) []byte { return nwCat(nwLeb(uint64(len(payload))), payload) }

func noWriterRefuse(t *testing.T, m *tabletext.Model, wire []byte) {
	t.Helper()
	for _, read := range []struct {
		name string
		fn   func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
	}{
		{"Decode", tablewire.Decode},
		{"DecodeRegion", tablewire.DecodeRegion},
	} {
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := read.fn(m, inst, wire, &report)
		if ok || err == nil || !report.Refused {
			t.Fatalf("%s accepted input no writer produces: ok=%v err=%v report=%+v wire=%x", read.name, ok, err, report, wire)
		}
		if got := fieldByName(t, inst, "a").Cell.I; got != 0 {
			t.Fatalf("%s kept a partial value after refusal: a=%d", read.name, got)
		}
	}
	for _, read := range []struct {
		name string
		fn   func(*tabletext.Model, *tabletext.Instance, []byte, *tablewire.Retain, *tabletext.Report) (bool, error)
	}{
		{"DecodeRetain", tablewire.DecodeRetain},
		{"DecodeRegionRetain", tablewire.DecodeRegionRetain},
	} {
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := read.fn(m, inst, wire, &tablewire.Retain{Capacity: 8192, IdCapacity: 128}, &report)
		if ok || err == nil || !report.Refused {
			t.Fatalf("%s accepted input no writer produces: ok=%v err=%v report=%+v wire=%x", read.name, ok, err, report, wire)
		}
	}
}

func TestNoWriterValidWireReads(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	inst := place(t, m, "Root", `{"a":7,"inner":{"x":1},"scores":[1,2],"tally":{"A":3},"entries":{"1":10,"2":20},"choice":{"inner":{"number":5}}}`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, back, wire, &report); !ok || err != nil {
		t.Fatalf("a writer's wire refused: ok=%v err=%v %+v", ok, err, report)
	}
}

func TestNoWriterRepeatedIDRefused(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	a := ir.TableWireId("a")
	inner := ir.TableWireId("inner")
	x := ir.TableWireId("x")
	zzz := ir.TableWireId("zzz")
	cases := map[string][]byte{
		// one known id twice in the root body
		"known": noWriterWire([]uint64{a}, nwCat(
			nwField(1, ir.TableKindI32, nwI32(1)...),
			nwField(1, ir.TableKindI32, nwI32(2)...),
			[]byte{0})),
		// one unknown id twice: a reader that cannot name it still refuses
		"unknown": noWriterWire([]uint64{a, zzz}, nwCat(
			nwField(2, ir.TableKindI32, nwI32(1)...),
			nwField(2, ir.TableKindI32, nwI32(2)...),
			[]byte{0})),
		// one id twice inside a nested body
		"nested": noWriterWire([]uint64{inner, x}, nwCat(
			nwField(1, ir.TableKindTable, nwLen(nwCat(
				nwField(2, ir.TableKindI32, nwI32(1)...),
				nwField(2, ir.TableKindI32, nwI32(2)...),
				[]byte{0}))...),
			[]byte{0})),
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) { noWriterRefuse(t, m, wire) })
	}
}

func TestNoWriterHeaderInsideBodyRefused(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	scores := ir.TableWireId("scores")
	tally := ir.TableWireId("tally")
	entries := ir.TableWireId("entries")
	// A header is the element kind and the count; L below 2 cannot hold
	// it, and a count whose LEB runs past L is inside no body.
	short := func(refID uint64, kind int) []byte {
		return noWriterWire([]uint64{refID}, nwCat(nwField(1, kind, nwLen(nil)...), []byte{0}))
	}
	oneByte := func(refID uint64, kind int, elem int) []byte {
		return noWriterWire([]uint64{refID}, nwCat(nwField(1, kind, nwLen([]byte{byte(elem)})...), []byte{0}))
	}
	crossing := func(refID uint64, kind int, elem int) []byte {
		// L=2 holds the element kind and one byte of a two-byte count.
		return noWriterWire([]uint64{refID}, nwCat(nwField(1, kind, nwLen([]byte{byte(elem), 0x82})...), []byte{0}))
	}
	cases := map[string][]byte{
		"array L0":        short(scores, ir.TableKindArray),
		"array L1":        oneByte(scores, ir.TableKindArray, ir.TableKindI32),
		"array crosses":   crossing(scores, ir.TableKindArray, ir.TableKindI32),
		"keyed L0":        short(tally, ir.TableKindKeyed),
		"keyed L1":        oneByte(tally, ir.TableKindKeyed, ir.TableKindI32),
		"keyed crosses":   crossing(tally, ir.TableKindKeyed, ir.TableKindI32),
		"map L1":          oneByte(entries, ir.TableKindArray, ir.TableKindTable),
		"map crosses":     crossing(entries, ir.TableKindArray, ir.TableKindTable),
		"foreign crosses": crossing(scores, ir.TableKindArray, ir.TableKindF32),
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) { noWriterRefuse(t, m, wire) })
	}
}

func TestNoWriterRepeatedKeyRefused(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	entries := ir.TableWireId("entries")
	tally := ir.TableWireId("tally")
	slotA := ir.TableWireId("A")
	zzz := ir.TableWireId("zzz")
	entry := func(key, val int32) []byte {
		body := nwCat(
			nwField(2, ir.TableKindI32, nwI32(key)...),
			nwField(3, ir.TableKindI32, nwI32(val)...),
			[]byte{0})
		return nwLen(body)
	}
	mapWire := func(elems ...[]byte) []byte {
		body := nwCat([]byte{byte(ir.TableKindTable)}, nwLeb(uint64(len(elems))), nwCat(elems...))
		return noWriterWire([]uint64{entries, ir.MapKeyWireId, ir.MapValueWireId},
			nwCat(nwField(1, ir.TableKindArray, nwLen(body)...), []byte{0}))
	}
	keyedWire := func(ids []uint64, slots ...[]byte) []byte {
		body := nwCat([]byte{byte(ir.TableKindI32)}, nwLeb(uint64(len(slots))), nwCat(slots...))
		return noWriterWire(ids, nwCat(nwField(1, ir.TableKindKeyed, nwLen(body)...), []byte{0}))
	}
	slot := func(ref byte, v int32) []byte { return nwCat(nwLeb(uint64(ref)), nwLen(nwI32(v))) }
	cases := map[string][]byte{
		"map key twice": mapWire(entry(1, 10), entry(1, 20)),
		"map entry key id twice": mapWire(nwLen(nwCat(
			nwField(2, ir.TableKindI32, nwI32(1)...),
			nwField(2, ir.TableKindI32, nwI32(1)...),
			nwField(3, ir.TableKindI32, nwI32(10)...),
			[]byte{0}))),
		"keyed slot twice":         keyedWire([]uint64{tally, slotA}, slot(2, 1), slot(2, 2)),
		"keyed unknown slot twice": keyedWire([]uint64{tally, zzz}, slot(2, 1), slot(2, 2)),
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) { noWriterRefuse(t, m, wire) })
	}
}

func TestNoWriterNestedUnionTrailingRefused(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	choice := ir.TableWireId("choice")
	inner := ir.TableWireId("inner")
	number := ir.TableWireId("number")
	innerUnion := nwCat(nwLeb(3), []byte{byte(ir.TableKindI32)}, nwLen(nwI32(5)))
	// The outer arm's L holds the whole inner union plus one byte no
	// writer put there.
	trailing := nwCat(nwLeb(2), []byte{byte(ir.TableKindUnion)}, nwLen(nwCat(innerUnion, []byte{0xFF})))
	wire := noWriterWire([]uint64{choice, inner, number},
		nwCat(nwField(1, ir.TableKindUnion, trailing...), []byte{0}))
	noWriterRefuse(t, m, wire)
	// The same arm with no trailing byte reads.
	exact := nwCat(nwLeb(2), []byte{byte(ir.TableKindUnion)}, nwLen(innerUnion))
	good := noWriterWire([]uint64{choice, inner, number},
		nwCat(nwField(1, ir.TableKindUnion, exact...), []byte{0}))
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, inst, good, &report); !ok || err != nil {
		t.Fatalf("a nested union filling its arm exactly refused: ok=%v err=%v %+v", ok, err, report)
	}
}

func TestNoWriterJSONDuplicateRefused(t *testing.T) {
	t.Parallel()
	m := noWriterModel(t)
	for _, text := range []string{
		`{"a":1,"a":2}`,
		`{"entries":{"1":10,"1":20}}`,
		`{"tally":{"A":1,"A":2}}`,
	} {
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if m.Read(inst, []byte(text), &report) || !report.Refused {
			t.Fatalf("JSON naming one member twice accepted: %s report=%+v", text, report)
		}
	}
}
