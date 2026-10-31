package mootable_test

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// occurrenceUnit is the READER'S build for the occurrence rules of
// docs/SPEC-TABLES.md §6.6: every shape a legal wire can write down twice, a
// nested table, a union, a list and a bounded array of tables, a list of
// unions, and a map. The wires below carry a `future` field the reader cannot
// name inside each of them.
const occurrenceUnit = `package occ

table Inner
{
    value int32
}

union Pick
{
    alpha Inner
    beta  int32
}

table Outer
{
    inner Inner
    pair  [2]Inner
}

table Node
{
    outer Outer
    child Inner
    pick  Pick
    kids  []Inner
    grid  [3]Inner
    items []Pick
    names map[string(8)]Inner
    last  int32
}
`

// wb is a wire writer for the variable form: the form byte, the body, then the
// id table in first-use order and its fixed u64 count.
type wb struct {
	body []byte
	ids  []uint64
}

func (b *wb) ref(name string) uint64 {
	id := ir.TableWireId(name)
	for i, e := range b.ids {
		if e == id {
			return uint64(i) + 1
		}
	}
	b.ids = append(b.ids, id)
	return uint64(len(b.ids))
}

func (b *wb) u8(v uint8) { b.body = append(b.body, v) }
func (b *wb) leb(v uint64) {
	for v >= 0x80 {
		b.u8(uint8(v) | 0x80)
		v >>= 7
	}
	b.u8(uint8(v))
}
func (b *wb) openLen() int { at := len(b.body); b.u8(0); return at }
func (b *wb) closeLen(at int) {
	payload := len(b.body) - at - 1
	width := 1
	for v := uint64(payload); v >= 0x80; v >>= 7 {
		width++
	}
	if width > 1 {
		b.body = append(b.body, make([]byte, width-1)...)
		copy(b.body[at+width:], b.body[at+1:])
	}
	w, v := at, uint64(payload)
	for v >= 0x80 {
		b.body[w] = uint8(v) | 0x80
		w++
		v >>= 7
	}
	b.body[w] = uint8(v)
}
func (b *wb) field(name string, kind uint8) { b.leb(b.ref(name)); b.u8(kind) }
func (b *wb) end()                          { b.u8(0) }
func (b *wb) i32(name string, v uint32) {
	b.field(name, ir.TableKindI32)
	b.body = binary.LittleEndian.AppendUint32(b.body, v)
}
func (b *wb) finish() []byte {
	out := append([]byte{1}, b.body...)
	for _, id := range b.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(b.ids)))
}

// inner writes a whole `Inner` body behind its length: the known `value`
// where value >= 0, then the unknown `future` where future >= 0.
func (b *wb) innerBody(value, future int, damage bool) {
	at := b.openLen()
	if value >= 0 {
		b.i32("value", uint32(value))
	}
	if future >= 0 {
		b.i32("future", uint32(future))
	}
	b.end()
	if damage {
		b.body = append(b.body, 0xAA, 0xBB)
	}
	b.closeLen(at)
}

func (b *wb) child(value, future int, damage bool) {
	b.field("child", ir.TableKindTable)
	b.innerBody(value, future, damage)
}

// outer writes an `Outer` whose children hold unknown fields: `inner`, and
// the second element of the fixed array `pair`. damage appends two bytes after
// the terminator, so the parent's framing fails and it falls back on its
// declared defaults; the Go engine keeps the child tables (Refill).
func (b *wb) outer(damage, _ bool) {
	b.field("outer", ir.TableKindTable)
	at := b.openLen()
	b.field("inner", ir.TableKindTable)
	b.innerBody(1, 7, false)
	b.list("pair", 2, func(i int) { b.innerBody(i, 40+i, false) })
	b.end()
	if damage {
		b.body = append(b.body, 0xAA, 0xBB)
	}
	b.closeLen(at)
}

func (b *wb) list(name string, n int, body func(i int)) {
	b.field(name, ir.TableKindArray)
	at := b.openLen()
	b.u8(uint8(ir.TableKindTable))
	b.leb(uint64(n))
	for i := range n {
		body(i)
	}
	b.closeLen(at)
}

// armBody is a union field's payload: the arm id, its kind and its framed body.
func (b *wb) armInner(arm string, value, future int, damage bool) {
	b.leb(b.ref(arm))
	b.u8(uint8(ir.TableKindTable))
	b.innerBody(value, future, damage)
}

func (b *wb) mapOf(entries [][3]int, names []string, damage bool) {
	b.field("names", ir.TableKindArray)
	at := b.openLen()
	b.u8(uint8(ir.TableKindTable))
	b.leb(uint64(len(entries)))
	for i, e := range entries {
		ea := b.openLen()
		b.field("key", ir.TableKindString)
		b.leb(uint64(len(names[i])))
		b.body = append(b.body, names[i]...)
		b.field("value", ir.TableKindTable)
		b.innerBody(e[0], e[1], damage && e[2] == 1)
		b.end()
		b.closeLen(ea)
	}
	b.closeLen(at)
}

// TestMooRetainOccurrencesMatchTheGoEngine is the retention differential on the
// OCCURRENCE RULES (docs/SPEC-TABLES.md §6.6): a record belongs to the body
// occurrence that carried it and dies with it when a later legal occurrence
// replaces that body, moving neither counter; a body damage resets stays the
// body it was; an excluded unknown counts one retain_lost. Each wire is read by
// moo and by the Go reference engine at a full capacity and one byte under what
// that used, and the counters and the saved bytes must be the same.
func TestMooRetainOccurrencesMatchTheGoEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Occ.schema": occurrenceUnit})
	wires := map[string]func(b *wb){
		"child once": func(b *wb) { b.child(1, 7, false); b.end() },
		"child twice, the second known": func(b *wb) {
			b.child(1, 7, false)
			b.child(2, -1, false)
			b.end()
		},
		"child twice, both unknown": func(b *wb) {
			b.child(1, 7, false)
			b.child(-1, 8, false)
			b.end()
		},
		"child damaged": func(b *wb) { b.child(1, 7, true); b.end() },
		"child damaged then replaced": func(b *wb) {
			b.child(1, 7, true)
			b.child(2, -1, false)
			b.end()
		},
		"damaged parent keeps its children": func(b *wb) { b.outer(true, false); b.end() },
		"damaged parent then replaced":      func(b *wb) { b.outer(true, false); b.outer(false, false); b.end() },
		"sound parent":                      func(b *wb) { b.outer(false, false); b.end() },
		"kids twice, fewer": func(b *wb) {
			b.list("kids", 4, func(i int) { b.innerBody(i, 40+i, false) })
			b.list("kids", 2, func(i int) { b.innerBody(i, -1, false) })
			b.end()
		},
		"kids twice, more": func(b *wb) {
			b.list("kids", 2, func(i int) { b.innerBody(i, 40+i, false) })
			b.list("kids", 4, func(i int) { b.innerBody(i, 50+i, false) })
			b.end()
		},
		"kids once, damaged element": func(b *wb) {
			b.list("kids", 3, func(i int) { b.innerBody(i, 40+i, i == 1) })
			b.end()
		},
		"grid twice": func(b *wb) {
			b.list("grid", 3, func(i int) { b.innerBody(i, 60+i, false) })
			b.list("grid", 1, func(i int) { b.innerBody(9, -1, false) })
			b.end()
		},
		"pick twice, another arm": func(b *wb) {
			b.field("pick", ir.TableKindUnion)
			b.armInner("alpha", 1, 7, false)
			b.field("pick", ir.TableKindUnion)
			b.leb(b.ref("beta"))
			b.u8(ir.TableKindI32)
			b.leb(4)
			b.body = binary.LittleEndian.AppendUint32(b.body, 5)
			b.end()
		},
		"pick damaged arm then replaced": func(b *wb) {
			b.field("pick", ir.TableKindUnion)
			b.armInner("alpha", 1, 7, true)
			b.field("pick", ir.TableKindUnion)
			b.leb(0)
			b.end()
		},
		"pick damaged arm": func(b *wb) {
			b.field("pick", ir.TableKindUnion)
			b.armInner("alpha", 1, 7, true)
			b.end()
		},
		"pick unknown arm": func(b *wb) {
			b.field("pick", ir.TableKindUnion)
			b.leb(b.ref("gamma"))
			b.u8(ir.TableKindI32)
			b.leb(4)
			b.body = binary.LittleEndian.AppendUint32(b.body, 5)
			b.end()
		},
		"items twice, arms past the second count": func(b *wb) {
			b.field("items", ir.TableKindArray)
			at := b.openLen()
			b.u8(uint8(ir.TableKindUnion))
			b.leb(6)
			for i := range 6 {
				if i == 4 {
					b.armInner("alpha", 1, 7, true)
				} else {
					b.leb(0)
				}
			}
			b.closeLen(at)
			b.field("items", ir.TableKindArray)
			at = b.openLen()
			b.u8(uint8(ir.TableKindUnion))
			b.leb(2)
			b.leb(0)
			b.leb(0)
			b.closeLen(at)
			b.end()
		},
		"items once, a retained arm": func(b *wb) {
			b.field("items", ir.TableKindArray)
			at := b.openLen()
			b.u8(uint8(ir.TableKindUnion))
			b.leb(3)
			b.leb(0)
			b.armInner("alpha", 1, 7, false)
			b.leb(0)
			b.closeLen(at)
			b.end()
		},
		"map once": func(b *wb) {
			b.mapOf([][3]int{{1, 7, 0}, {2, 8, 0}}, []string{"a", "b"}, false)
			b.end()
		},
		"map twice": func(b *wb) {
			b.mapOf([][3]int{{1, 7, 0}, {2, 8, 0}}, []string{"a", "b"}, false)
			b.mapOf([][3]int{{3, -1, 0}}, []string{"c"}, false)
			b.end()
		},
		"map duplicate key": func(b *wb) {
			b.mapOf([][3]int{{1, 7, 0}, {2, 8, 0}}, []string{"a", "a"}, false)
			b.end()
		},
		"map damaged entry then replaced": func(b *wb) {
			b.mapOf([][3]int{{1, 7, 1}}, []string{"a"}, true)
			b.mapOf([][3]int{{3, -1, 0}}, []string{"c"}, false)
			b.end()
		},
		"map damaged entry": func(b *wb) {
			b.mapOf([][3]int{{1, 7, 1}}, []string{"a"}, true)
			b.end()
		},
		"root unknown among known": func(b *wb) {
			b.i32("future", 3)
			b.i32("last", 4)
			b.i32("future2", 5)
			b.end()
		},
		"unknown enum-less deep table": func(b *wb) {
			b.field("future", ir.TableKindTable)
			at := b.openLen()
			b.field("deeper", ir.TableKindTable)
			at2 := b.openLen()
			b.i32("hits", 9)
			b.end()
			b.closeLen(at2)
			b.end()
			b.closeLen(at)
			b.end()
		},
	}
	names := make([]string, 0, len(wires))
	for n := range wires {
		names = append(names, n)
	}
	sortStrings(names)
	retaining := 0
	for _, name := range names {
		b := &wb{}
		wires[name](b)
		wire := b.finish()
		full := goRetain(t, g.unit, "Node", wire, retainFull, -1)
		probes := [][2]int{{retainFull, -1}}
		if full.retained > 0 {
			retaining++
			probes = append(probes, [2]int{full.used - 1, -1})
		}
		for _, p := range probes {
			want := goRetain(t, g.unit, "Node", wire, p[0], p[1])
			got := mooRetain(t, g, "Node", wire, p[0], p[1])
			if got.used != want.used || got.counters() != want.counters() || got.value != want.value ||
				got.saveLost != want.saveLost || string(got.save) != string(want.save) || (got.saveErr == "") != (want.saveErr == "") {
				t.Errorf("%s at capacity %d: moo and the Go engine disagree:\nmoo: used %d %s value %v lost %d %s %x\ngo:  used %d %s value %v lost %d %s %x",
					name, p[0], got.used, got.counters(), got.value, got.saveLost, got.saveErr, got.save,
					want.used, want.counters(), want.value, want.saveLost, want.saveErr, want.save)
			}
		}
	}
	if retaining < 10 {
		t.Fatalf("only %d of %d occurrence wires retained anything", retaining, len(names))
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestMooRetainDamagedParentKeepsItsChildren pins the vector the divergence
// was found on: a nested body whose framing is damaged falls back on its
// declared defaults and STAYS THE BODY IT WAS, with the child tables it holds,
// so the records retained under those children are still placed at the save
// (internal/tabletext Refill; docs/SPEC-TABLES.md §6.6, damage is not a later
// occurrence). The wire and its expectations are fixed here, and the Go engine
// must give the same answer.
func TestMooRetainDamagedParentKeepsItsChildren(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_retain.moo)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Occ.schema": occurrenceUnit})
	b := &wb{}
	b.outer(true, false)
	b.end()
	wire := b.finish()
	const (
		wantWire     = "01010d34020d0d03040100000004040700000000050e1e0d020d030400000000040428000000000d0304010000000404290000000000aabb00d41b5e8f67ede6308792885f8f064c2dea0ce83094fde47c5630b3ead46180d8319a88eb0d2569030500000000000000"
		wantCounters = "3,0,3"
		wantSave     = "01010d20020d0703040700000000040e120d020703042800000000070304290000000000050e080d0301000100010000d41b5e8f67ede6308792885f8f064c2d5630b3ead46180d8319a88eb0d25690331b8db43729f53fb0500000000000000"
	)
	if got := hex.EncodeToString(wire); got != wantWire {
		t.Fatalf("the vector moved: %s", got)
	}
	got := mooRetain(t, g, "Node", wire, retainFull, -1)
	want := goRetain(t, g.unit, "Node", wire, retainFull, -1)
	if got.counters() != wantCounters || got.saveLost != 0 || hex.EncodeToString(got.save) != wantSave {
		t.Errorf("moo: load %s, save lost %d, saved %x", got.counters(), got.saveLost, got.save)
	}
	if want.counters() != wantCounters || want.saveLost != 0 || hex.EncodeToString(want.save) != wantSave {
		t.Errorf("the Go engine: load %s, save lost %d, saved %x", want.counters(), want.saveLost, want.save)
	}
}
