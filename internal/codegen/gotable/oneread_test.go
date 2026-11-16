package gotable

import "testing"

// THE ONE READ, CONDITION BY CONDITION (docs/SPEC-TABLES.md §4, §6.5): each
// case below is one input the read refuses, beside its control, the nearest
// input the read accepts. Every path answers the same verdict — the region
// load, the builder load and ReadStrict — and a measure that refuses implies a
// load that refuses (tla/ReadOrder.tla, OneVerdict).
const oneReadSchema = `package probe
fixed table Leaf { v int32 = 0 }
table Node {
 n int32 = 0 | min = 0, max = 100
 next *Node
 leaf *Leaf
 xs [..4]int16
 name string(8)
 on bool
}
`

const oneReadPrelude = `package probe
import ("math/rand"; "testing"; "unsafe")

func region(n int64) []byte {
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func body(ids *TableIds, f func(w *TableWriter)) []byte {
	w := TableWriter{Buffer: make([]byte, 4096), Ids: ids}
	f(&w)
	if w.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

func field(i int) uint64 { return NodeTableFields[i].ID }

const (fN, fNext, fLeaf, fXs, fName, fOn = 0, 1, 2, 3, 4, 5)

func i32(w *TableWriter, id uint64, v int32) { w.ID(id); w.Put8(4); w.Put32(uint32(v)) }
func ptr(w *TableWriter, id uint64, index uint64) { w.ID(id); w.Put8(17); w.PutLeb(index) }
func framed(w *TableWriter, id uint64, kind uint8, payload []byte) { w.ID(id); w.Put8(kind); w.PutLeb(uint64(len(payload))); w.Raw(payload) }

type record struct { typ uint64; build func(ids *TableIds) []byte }

// framedBody writes a framed payload whose body is built after the reference,
// so the id table records the outer id before the ids the body names.
func framedBody(w *TableWriter, id uint64, kind uint8, build func() []byte) {
	w.ID(id); w.Put8(kind)
	payload := build()
	w.PutLeb(uint64(len(payload))); w.Raw(payload)
}

// file is a form-1 file: the root's fields, the node table when records are
// given, the terminator and the id table. Each file has its own TableIds, and
// a record's body is built after the root and the node-table reference that
// first name it, so the table is ordered by first use on the wire.
func file(root func(w *TableWriter), records ...record) []byte {
	var ids TableIds
	w := TableWriter{Buffer: make([]byte, 8192), Ids: &ids}
	w.Put8(1)
	root(&w)
	if len(records) > 0 {
		framedBody(&w, 0xffffffffffffffff, 12, func() []byte {
			return body(&ids, func(p *TableWriter) {
				p.PutLeb(uint64(len(records)))
				for _, r := range records { p.ID(r.typ); b := r.build(&ids); p.PutLeb(uint64(len(b))); p.Raw(b) }
			})
		})
	}
	w.Put8(0)
	w.Trailer()
	if w.Overflow || ids.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

func leaf(v int32) record {
	return record{LeafTableType().ID, func(ids *TableIds) []byte {
		return body(ids, func(w *TableWriter) { i32(w, LeafTableFields[0].ID, v); w.Put8(0) })
	}}
}

func node(f func(w *TableWriter)) record {
	return record{NodeTableType().ID, func(ids *TableIds) []byte {
		return body(ids, func(w *TableWriter) { f(w); w.Put8(0) })
	}}
}

// verdict reads one input on every path and requires one answer: the region
// load, the builder load and ReadStrict agree, and a measure refusal implies
// a load refusal. An accepted input writes back bytes that read again.
func verdict(t *testing.T, what string, wire []byte) bool {
	t.Helper()
	size := NodeLoadMeasure(wire)
	var rr TableReport
	var root *Node
	if size >= 0 {
		root = NodeLoad(region(size), wire, &rr)
	} else if NodeLoad(region(1<<16), wire, &rr) != nil {
		t.Fatalf("%s: the measure refused and the load accepted", what)
	}
	var b NodeBuilder
	if !b.Init() { t.Fatal("init") }
	defer b.Shutdown()
	var br TableReport
	bok := NodeLoadBuilder(&b, wire, &br)
	_, err := NodeReadStrict(wire)
	ok := root != nil
	if ok != bok || ok != (err == nil) || ok != (rr.Err() == nil) || bok != (br.Err() == nil) {
		t.Fatalf("%s: the paths disagree: region %v %+v, builder %v %+v, ReadStrict %v", what, ok, rr, bok, br, err)
	}
	if !ok {
		if rr.Unknown != 0 || rr.Widened != 0 || br.Unknown != 0 || br.Widened != 0 {
			t.Fatalf("%s: a refusal moved a counter: %+v %+v", what, rr, br)
		}
		return false
	}
	out := make([]byte, NodeMeasure(root))
	if NodeSave(root, out) != int64(len(out)) { t.Fatalf("%s: the value read does not write", what) }
	var again TableReport
	if NodeLoad(region(max(NodeLoadMeasure(out), 0)), out, &again) == nil {
		t.Fatalf("%s: the written-back bytes refuse: %+v", what, again)
	}
	return true
}

func refuses(t *testing.T, what string, wire []byte) { t.Helper(); if verdict(t, what, wire) { t.Fatalf("%s: accepted", what) } }
func accepts(t *testing.T, what string, wire []byte) { t.Helper(); if !verdict(t, what, wire) { t.Fatalf("%s: refused", what) } }
`

const oneReadConditions = oneReadPrelude + `
func TestConditions(t *testing.T) {
	// a repeated field id in one body is damage
	accepts(t, "one n", file(func(w *TableWriter) { i32(w, field(fN), 5) }))
	refuses(t, "n twice", file(func(w *TableWriter) { i32(w, field(fN), 5); i32(w, field(fN), 6) }))

	// an unknown field is skipped and counted, and damage inside it refuses
	unknown := uint64(0x1234567890abcdef)
	good := func(w *TableWriter) []byte { return body(w.Ids, func(b *TableWriter) { b.ID(field(fN)); b.Put8(1); b.Put8(1); b.Put8(0) }) }
	bad := func(w *TableWriter) []byte { return body(w.Ids, func(b *TableWriter) { b.ID(field(fN)); b.Put8(1); b.Put8(2); b.Put8(0) }) }
	accepts(t, "unknown body", file(func(w *TableWriter) { framedBody(w, unknown, 13, func() []byte { return good(w) }) }))
	refuses(t, "a bool byte of 2 inside an unknown body", file(func(w *TableWriter) { framedBody(w, unknown, 13, func() []byte { return bad(w) }) }))
	refuses(t, "a repeated id inside an unknown body", file(func(w *TableWriter) {
		framedBody(w, unknown, 13, func() []byte {
			return body(w.Ids, func(b *TableWriter) { i32(b, field(fN), 1); i32(b, field(fN), 2); b.Put8(0) })
		})
	}))

	// an array's header inside its L, every byte of the L an element's
	xs := func(count byte, extra ...byte) []byte { return append([]byte{3, count, 1, 0}, extra...) }
	accepts(t, "xs", file(func(w *TableWriter) { framed(w, field(fXs), 14, xs(1)) }))
	refuses(t, "a byte left in the array's L", file(func(w *TableWriter) { framed(w, field(fXs), 14, xs(1, 0)) }))
	refuses(t, "an array body too short for its header", file(func(w *TableWriter) { framed(w, field(fXs), 14, []byte{3}) }))
	refuses(t, "a count past the reader's bound", file(func(w *TableWriter) { framed(w, field(fXs), 14, []byte{3, 5, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0}) }))
	refuses(t, "an element kind that is not a widening", file(func(w *TableWriter) { framed(w, field(fXs), 14, []byte{4, 1, 1, 0, 0, 0}) }))
	accepts(t, "an element kind that widens", file(func(w *TableWriter) { framed(w, field(fXs), 14, []byte{2, 1, 0xff}) }))

	// a value is held to the reader's range; nothing clamps
	accepts(t, "n at its max", file(func(w *TableWriter) { i32(w, field(fN), 100) }))
	refuses(t, "n past its max", file(func(w *TableWriter) { i32(w, field(fN), 101) }))
	refuses(t, "a kind mismatch", file(func(w *TableWriter) { w.ID(field(fN)); w.Put8(10); w.Put32(0) }))
	accepts(t, "name at its bound", file(func(w *TableWriter) { framed(w, field(fName), 12, []byte("12345678")) }))
	refuses(t, "name past its bound", file(func(w *TableWriter) { framed(w, field(fName), 12, []byte("123456789")) }))
	refuses(t, "a bool byte of 2", file(func(w *TableWriter) { w.ID(field(fOn)); w.Put8(1); w.Put8(2) }))

	// THE GRAPH: node 2 is the first record
	accepts(t, "a leaf", file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, leaf(3)))
	refuses(t, "a record no pointer reaches", file(func(w *TableWriter) {}, leaf(3)))
	refuses(t, "a record reached only from an unreachable record", file(func(w *TableWriter) {},
		node(func(w *TableWriter) { ptr(w, field(fNext), 3) }), node(func(w *TableWriter) {})))
	refuses(t, "a cycle", file(func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(func(w *TableWriter) { ptr(w, field(fNext), 2) })))
	refuses(t, "a pointer to the root", file(func(w *TableWriter) { ptr(w, field(fNext), 1) }))
	accepts(t, "a pointer from an unknown slot reaches a record", file(func(w *TableWriter) { ptr(w, unknown, 2) }, leaf(3)))
	refuses(t, "a record reached through an unknown slot is held to its declaration",
		file(func(w *TableWriter) { ptr(w, unknown, 2) }, node(func(w *TableWriter) { i32(w, field(fN), 101) })))
	refuses(t, "a pointer to a named record of another type", file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		node(func(w *TableWriter) {})))
	stranger := record{0x0bad0bad0bad0bad, func(ids *TableIds) []byte { return body(ids, func(w *TableWriter) { w.Put8(0) }) }}
	if !verdict(t, "a pointer to a type this reader cannot name", file(func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger)) {
		t.Fatal("a pointer to an unnameable type must read null, not refuse")
	}
}

// TestPathsAgree is the Go side of OneVerdict over random inputs: every
// mutation of a file with a node table gets one verdict on every path, a
// measure refusal implies a load refusal, and an accepted value writes back
// bytes that read.
func TestPathsAgree(t *testing.T) {
	seed := file(func(w *TableWriter) {
		i32(w, field(fN), 7); ptr(w, field(fNext), 2); ptr(w, field(fLeaf), 3)
		framed(w, field(fXs), 14, []byte{3, 2, 1, 0, 2, 0}); framed(w, field(fName), 12, []byte("ab")); w.ID(field(fOn)); w.Put8(1); w.Put8(1)
	}, node(func(w *TableWriter) { i32(w, field(fN), 9); ptr(w, field(fLeaf), 3) }), leaf(4))
	accepts(t, "the seed", seed)
	rng := rand.New(rand.NewSource(20261103))
	accepted, refused := 0, 0
	for i := range 4000 {
		m := append([]byte(nil), seed...)
		switch rng.Intn(3) {
		case 0:
			m[rng.Intn(len(m))] ^= byte(1 << rng.Intn(8))
		case 1:
			m[rng.Intn(len(m))] = byte(rng.Intn(256))
		default:
			at := rng.Intn(len(m))
			m = append(m[:at:at], append([]byte{byte(rng.Intn(256))}, m[at:]...)...)
		}
		if verdict(t, "mutation", m) { accepted++ } else { refused++ }
		_ = i
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("the mutations are vacuous: %d accepted, %d refused", accepted, refused)
	}
}
`

func TestGoOneReadConditionsAndPathsAgree(t *testing.T) {
	runGenerated(t, oneReadSchema, oneReadConditions)
}
