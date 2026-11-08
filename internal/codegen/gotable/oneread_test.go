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

type record struct { typ uint64; body []byte }

// file is a form-1 file: the root's fields, the node table when records are
// given, the terminator and the id table.
func file(ids *TableIds, root func(w *TableWriter), records ...record) []byte {
	w := TableWriter{Buffer: make([]byte, 8192), Ids: ids}
	w.Put8(1)
	root(&w)
	if len(records) > 0 {
		payload := body(ids, func(p *TableWriter) {
			p.PutLeb(uint64(len(records)))
			for _, r := range records { p.ID(r.typ); p.PutLeb(uint64(len(r.body))); p.Raw(r.body) }
		})
		framed(&w, 0xffffffffffffffff, 12, payload)
	}
	w.Put8(0)
	w.Trailer()
	if w.Overflow || ids.Overflow { panic("overflow") }
	return w.Buffer[:w.Offset]
}

func leaf(ids *TableIds, v int32) record {
	return record{LeafTableType().ID, body(ids, func(w *TableWriter) { i32(w, LeafTableFields[0].ID, v); w.Put8(0) })}
}

func node(ids *TableIds, f func(w *TableWriter)) record {
	return record{NodeTableType().ID, body(ids, func(w *TableWriter) { f(w); w.Put8(0) })}
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
	var ids TableIds
	// a repeated field id in one body is damage
	accepts(t, "one n", file(&ids, func(w *TableWriter) { i32(w, field(fN), 5) }))
	refuses(t, "n twice", file(&ids, func(w *TableWriter) { i32(w, field(fN), 5); i32(w, field(fN), 6) }))

	// an unknown field is skipped and counted, and damage inside it refuses
	unknown := uint64(0x1234567890abcdef)
	good := body(&ids, func(w *TableWriter) { w.ID(field(fN)); w.Put8(1); w.Put8(1); w.Put8(0) })
	bad := body(&ids, func(w *TableWriter) { w.ID(field(fN)); w.Put8(1); w.Put8(2); w.Put8(0) })
	accepts(t, "unknown body", file(&ids, func(w *TableWriter) { framed(w, unknown, 13, good) }))
	refuses(t, "a bool byte of 2 inside an unknown body", file(&ids, func(w *TableWriter) { framed(w, unknown, 13, bad) }))
	refuses(t, "a repeated id inside an unknown body", file(&ids, func(w *TableWriter) {
		framed(w, unknown, 13, body(&ids, func(b *TableWriter) { i32(b, field(fN), 1); i32(b, field(fN), 2); b.Put8(0) }))
	}))

	// an array's header inside its L, every byte of the L an element's
	xs := func(count byte, extra ...byte) []byte { return append([]byte{3, count, 1, 0}, extra...) }
	accepts(t, "xs", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, xs(1)) }))
	refuses(t, "a byte left in the array's L", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, xs(1, 0)) }))
	refuses(t, "an array body too short for its header", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, []byte{3}) }))
	refuses(t, "a count past the reader's bound", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, []byte{3, 5, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0}) }))
	refuses(t, "an element kind that is not a widening", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, []byte{4, 1, 1, 0, 0, 0}) }))
	accepts(t, "an element kind that widens", file(&ids, func(w *TableWriter) { framed(w, field(fXs), 14, []byte{2, 1, 0xff}) }))

	// a value is held to the reader's range; nothing clamps
	accepts(t, "n at its max", file(&ids, func(w *TableWriter) { i32(w, field(fN), 100) }))
	refuses(t, "n past its max", file(&ids, func(w *TableWriter) { i32(w, field(fN), 101) }))
	refuses(t, "a kind mismatch", file(&ids, func(w *TableWriter) { w.ID(field(fN)); w.Put8(10); w.Put32(0) }))
	accepts(t, "name at its bound", file(&ids, func(w *TableWriter) { framed(w, field(fName), 12, []byte("12345678")) }))
	refuses(t, "name past its bound", file(&ids, func(w *TableWriter) { framed(w, field(fName), 12, []byte("123456789")) }))
	refuses(t, "a bool byte of 2", file(&ids, func(w *TableWriter) { w.ID(field(fOn)); w.Put8(1); w.Put8(2) }))

	// THE GRAPH: node 2 is the first record
	accepts(t, "a leaf", file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, leaf(&ids, 3)))
	refuses(t, "a record no pointer reaches", file(&ids, func(w *TableWriter) {}, leaf(&ids, 3)))
	refuses(t, "a record reached only from an unreachable record", file(&ids, func(w *TableWriter) {},
		node(&ids, func(w *TableWriter) { ptr(w, field(fNext), 3) }), node(&ids, func(w *TableWriter) {})))
	refuses(t, "a cycle", file(&ids, func(w *TableWriter) { ptr(w, field(fNext), 2) },
		node(&ids, func(w *TableWriter) { ptr(w, field(fNext), 2) })))
	refuses(t, "a pointer to the root", file(&ids, func(w *TableWriter) { ptr(w, field(fNext), 1) }))
	accepts(t, "a pointer from an unknown slot reaches a record", file(&ids, func(w *TableWriter) { ptr(w, unknown, 2) }, leaf(&ids, 3)))
	refuses(t, "a record reached through an unknown slot is held to its declaration",
		file(&ids, func(w *TableWriter) { ptr(w, unknown, 2) }, node(&ids, func(w *TableWriter) { i32(w, field(fN), 101) })))
	refuses(t, "a pointer to a named record of another type", file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) },
		node(&ids, func(w *TableWriter) {})))
	stranger := record{0x0bad0bad0bad0bad, body(&ids, func(w *TableWriter) { w.Put8(0) })}
	if !verdict(t, "a pointer to a type this reader cannot name", file(&ids, func(w *TableWriter) { ptr(w, field(fLeaf), 2) }, stranger)) {
		t.Fatal("a pointer to an unnameable type must read null, not refuse")
	}
}

// TestPathsAgree is the Go side of OneVerdict over random inputs: every
// mutation of a file with a node table gets one verdict on every path, a
// measure refusal implies a load refusal, and an accepted value writes back
// bytes that read.
func TestPathsAgree(t *testing.T) {
	var ids TableIds
	seed := file(&ids, func(w *TableWriter) {
		i32(w, field(fN), 7); ptr(w, field(fNext), 2); ptr(w, field(fLeaf), 3)
		framed(w, field(fXs), 14, []byte{3, 2, 1, 0, 2, 0}); framed(w, field(fName), 12, []byte("ab")); w.ID(field(fOn)); w.Put8(1); w.Put8(1)
	}, node(&ids, func(w *TableWriter) { i32(w, field(fN), 9); ptr(w, field(fLeaf), 3) }), leaf(&ids, 4))
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
