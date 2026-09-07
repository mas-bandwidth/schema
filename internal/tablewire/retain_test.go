package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ORACLE'S RETENTION (docs/SPEC-TABLES.md §6.6). `internal/tablewire` is
// the compiler's own engine and the wire fuzzer's divergence oracle (§4.2), a
// third reading of §3 written from the page rather than from a backend. These
// rows read the SAME pinned vectors the C++ reference's retain gate reads —
// testdata/wire/tables/retain_*.bin, written by test/tables/retain_main.cpp —
// and require the same counters, the same retained ids and the same saved
// bytes. Two engines that agree on those agree on the feature.

func retainModel(t *testing.T, schema string) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "test", "tables", schema)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func retainVector(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wire", "tables", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// idsOf is a saved file's trailer, in first-use order (§3).
func idsOf(t *testing.T, wire []byte) []uint64 {
	t.Helper()
	if len(wire) < 9 {
		t.Fatal("a wire is at least a form byte and an entry count")
	}
	count := int(binary.LittleEndian.Uint64(wire[len(wire)-8:]))
	first := len(wire) - count*8 - 8
	if first < 1 {
		t.Fatalf("the trailer claims %d entries, which the wire does not hold", count)
	}
	ids := make([]uint64, count)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(wire[first+i*8:])
	}
	return ids
}

// THE ROUND TRIP AT EVERY DEPTH, against the reference's own pinned save.
// RT2 writes eight bodies carrying a field RT1 cannot name, plus `parcel`, a
// whole table whose body is what the RESOLVING WALK is measured on. RT1 loads
// with retention, saves, and the bytes are the reference's byte for byte.
func TestRetainRoundTrip(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	wire := retainVector(t, "retain_rt2")

	inst := m.New(m.Lookup("Node"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var report tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, wire, &retain, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || report.Malformed {
		t.Fatalf("the load reported damage on a sound wire: %+v", report)
	}
	// NINE RECORDS: an unknown field in the root, in `inner`, in two elements
	// of `items`, in a slot of `banks`, in the union's arm body, in a map
	// entry's value and in a list element, and `parcel` beside them in the
	// root. TWO EXCLUDED CLASSES ride the same wire — the enum variant `tier`
	// names and the keyed slot RT2's third variant writes — and each counts
	// one retain_lost.
	if report.Retained != 9 || report.RetainLost != 2 || report.Unknown != 11 {
		t.Fatalf("retained=%d retain_lost=%d unknown=%d, want 9 / 2 / 11", report.Retained, report.RetainLost, report.Unknown)
	}

	var saveReport tabletext.Report
	out, err := tablewire.EncodeRetain(m, inst, &retain, &saveReport)
	if err != nil {
		t.Fatal(err)
	}
	if saveReport.RetainLost != 0 {
		t.Fatalf("the save lost %d records it had room for", saveReport.RetainLost)
	}
	want := retainVector(t, "retain_rt1_save")
	if !bytes.Equal(out, want) {
		t.Fatalf("the save is %d bytes and the reference's pin is %d, and they differ", len(out), len(want))
	}

	// THE PLAIN SAVE IS SMALLER, because the retained fields rode
	plain, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) >= len(out) {
		t.Fatalf("the retaining save wrote %d bytes and the plain one %d", len(out), len(plain))
	}

	// IDEMPOTENCE (§6.6): the same region saved twice is the same bytes, and a
	// second round trip reproduces the first save exactly.
	var second tabletext.Report
	again, err := tablewire.EncodeRetain(m, inst, &retain, &second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, out) {
		t.Fatal("the second save of one region is not the first")
	}
	reloaded := m.New(m.Lookup("Node"))
	retain2 := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var third tabletext.Report
	if _, err := tablewire.DecodeRetain(m, reloaded, out, &retain2, &third); err != nil {
		t.Fatal(err)
	}
	var fourth tabletext.Report
	round, err := tablewire.EncodeRetain(m, reloaded, &retain2, &fourth)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(round, out) {
		t.Fatal("loaded and saved again, the retained fields did not land in the same bytes")
	}
}

// THE MERGED TRAILER (§6.6): an id this build can name takes its entry from
// the generated table and a retained id takes its from the caller's list, and
// BOTH are numbered into one trailer in the order the walk first uses them. A
// retained id enters AFTER its body's own fields, and the root's tail is
// pinned BEFORE the node-table field, so the node table's own id is last.
func TestRetainTrailerIsMergedInFirstUseOrder(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	wire := retainVector(t, "retain_rt2")

	inst := m.New(m.Lookup("Node"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var report tabletext.Report
	if _, err := tablewire.DecodeRetain(m, inst, wire, &retain, &report); err != nil {
		t.Fatal(err)
	}
	// THE RETAINED IDS ARE THE CALLER'S LIST, in the order the SAVE walk
	// interned them: the load writes into neither list (§6.6).
	if got := len(retain.Ids()); got != 0 {
		t.Fatalf("the load interned %d ids, and the load writes into neither list", got)
	}
	var saveReport tabletext.Report
	out, err := tablewire.EncodeRetain(m, inst, &retain, &saveReport)
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(t, out)
	at := func(id uint64) int {
		for i, entry := range ids {
			if entry == id {
				return i
			}
		}
		return -1
	}
	name := ir.TableWireId("name")
	list := ir.TableWireId("list")
	future := ir.TableWireId("future")
	extra := ir.TableWireId("extra")
	parcel := ir.TableWireId("parcel")
	nodes := ir.TableNodeWireId
	if at(name) != 0 {
		t.Fatalf("the root's first declared field names entry %d, not the first", at(name))
	}
	// `future` interns inside `inner`'s own tail, which the walk reaches
	// before the ROOT's tail
	if at(future) <= at(name) || at(future) >= at(extra) {
		t.Fatalf("future is at %d, name at %d, extra at %d", at(future), at(name), at(extra))
	}
	// a retained id enters AFTER its body's own fields, and `list` is the last
	// field the root declares
	if at(list) <= at(name) || at(extra) <= at(list) {
		t.Fatalf("extra is at %d and list at %d: the tail did not follow the body", at(extra), at(list))
	}
	// the root's two records intern in the order they were retained
	if at(parcel) != at(extra)+1 {
		t.Fatalf("parcel is at %d and extra at %d", at(parcel), at(extra))
	}
	// THE TAIL IS PINNED BEFORE THE NODE-TABLE FIELD (§3.1, §6.6)
	if at(nodes) <= at(parcel) {
		t.Fatalf("the node table's id is at %d and parcel at %d", at(nodes), at(parcel))
	}
	// THE SPLIT IS THE WRITER'S STORAGE RATHER THAN THE WIRE'S (§6.6): every
	// id the save interned into the CALLER'S LIST is one this build cannot
	// spell, and every one of them is in the file's own single trailer. The
	// generated table holds none of them and grew no entry.
	build := map[uint64]bool{}
	for _, id := range ir.TableWireIds(m.Unit) {
		build[id] = true
	}
	interned := retain.Ids()
	if len(interned) == 0 {
		t.Fatal("the save interned no retained id, and nine records name several")
	}
	for _, id := range interned {
		if build[id] {
			t.Fatalf("id %#x took an entry from the caller's list, and this build can name it", id)
		}
		if at(id) < 0 {
			t.Fatalf("id %#x is in the caller's list and not in the file's trailer", id)
		}
	}
	// and the two the ROOT's own tail names are among them
	if !slices.Contains(interned, extra) || !slices.Contains(interned, parcel) {
		t.Fatalf("the root's tail names extra and parcel, and the list holds %d ids without them", len(interned))
	}
}

// THE SIX EXCLUDED CLASSES, one row each and one `retain_lost` each (§6.6).
// The table is the law and its rows are the count: nothing is an exclusion
// that is not a row, and each vector below carries exactly one class and
// nothing else this reader cannot keep.
func TestRetainExcludedClasses(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	classes := []struct {
		vector string
		what   string
	}{
		{"retain_excluded_0", "a field of kind 17"},
		{"retain_excluded_1", "an array whose element kind is 17"},
		{"retain_excluded_2", "a table whose payload meets a 17 three bodies down"},
		{"retain_excluded_3", "an unknown enum variant reference"},
		{"retain_excluded_4", "an unknown union arm id"},
		{"retain_excluded_5", "an unknown keyed-array slot"},
	}
	for _, c := range classes {
		t.Run(c.vector, func(t *testing.T) {
			inst := m.New(m.Lookup("Node"))
			retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var report tabletext.Report
			ok, err := tablewire.DecodeRetain(m, inst, retainVector(t, c.vector), &retain, &report)
			if err != nil {
				t.Fatal(err)
			}
			if !ok || report.Malformed {
				t.Fatalf("%s: the load reported damage on a sound wire: %+v", c.what, report)
			}
			if report.RetainLost != 1 || report.Retained != 0 {
				t.Fatalf("%s: retained=%d retain_lost=%d, want 0 / 1", c.what, report.Retained, report.RetainLost)
			}
			// AND THE SAVE CARRIES NOTHING OF IT: the load already counted the
			// class, and the save has nothing to place.
			var saveReport tabletext.Report
			if _, err := tablewire.EncodeRetain(m, inst, &retain, &saveReport); err != nil {
				t.Fatal(err)
			}
			if saveReport.RetainLost != 0 {
				t.Fatalf("%s: the save counted %d lost, and it had nothing to place", c.what, saveReport.RetainLost)
			}
		})
	}
}

// A NODE RECORD whose type id this reader cannot name is the class RT3
// isolates: `head` is a field RT1 already has, at the same id and the same
// kind, pointing at a table RT1 never heard of. A whole node has nothing to
// append it to.
func TestRetainUnknownNodeRecord(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	inst := m.New(m.Lookup("Node"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var report tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, retainVector(t, "retain_rt3"), &retain, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || report.Malformed {
		t.Fatalf("the load reported damage on a sound wire: %+v", report)
	}
	if report.Unknown != 1 || report.RetainLost != 1 || report.Retained != 0 {
		t.Fatalf("unknown=%d retain_lost=%d retained=%d, want 1 / 1 / 0", report.Unknown, report.RetainLost, report.Retained)
	}
}

// RETENTION MOVES NO EXISTING COUNTER, and `Decode` retains nothing: the three
// names are ADDITIVE (§6.6), so a plain load of the same wire answers exactly
// what it answered before retention existed.
func TestRetainIsAdditive(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	wire := retainVector(t, "retain_rt2")

	plainInst := m.New(m.Lookup("Node"))
	var plain tabletext.Report
	if _, err := tablewire.Decode(m, plainInst, wire, &plain); err != nil {
		t.Fatal(err)
	}
	if plain.Retained != 0 || plain.RetainLost != 0 {
		t.Fatalf("a plain load moved a retention counter: %+v", plain)
	}
	retainInst := m.New(m.Lookup("Node"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	var retaining tabletext.Report
	if _, err := tablewire.DecodeRetain(m, retainInst, wire, &retain, &retaining); err != nil {
		t.Fatal(err)
	}
	if plain.Unknown != retaining.Unknown || plain.KindMismatch != retaining.KindMismatch ||
		plain.Clamped != retaining.Clamped || plain.Widened != retaining.Widened ||
		plain.Duplicate != retaining.Duplicate || plain.Malformed != retaining.Malformed {
		t.Fatalf("retention moved a read counter: plain %+v, retaining %+v", plain, retaining)
	}
	// A REGION LOADED WITH RETENTION MAY BE SAVED WITHOUT IT, which drops the
	// retained fields and reports nothing at all (§6.6).
	dropped, err := tablewire.Encode(m, retainInst)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := tablewire.Encode(m, plainInst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dropped, kept) {
		t.Fatal("a plain save of a retaining load is not a plain save")
	}
}

// THE TWO CAPACITIES are the only ceilings, and the wire cannot raise either
// (§6.6). A record past the remaining capacity counts one `retain_lost` and is
// not written at all; a retained id past the id capacity does the same, and
// the save is never refused.
func TestRetainCapacities(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	wire := retainVector(t, "retain_rt2")

	full := m.New(m.Lookup("Node"))
	whole := tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1024}
	var wholeReport tabletext.Report
	if _, err := tablewire.DecodeRetain(m, full, wire, &whole, &wholeReport); err != nil {
		t.Fatal(err)
	}
	if whole.Used() <= 0 {
		t.Fatal("the retention buffer holds nothing after nine records")
	}
	// THE LIST FILLS AS THE SAVE WALK INTERNS, and a load writes into neither
	// store (§6.6), so the retained ids this wire carries are only countable
	// after a SAVE with room for all of them. Sizing the short list off a
	// decode alone reads zero, and one short of zero loses every record.
	var wholeSave tabletext.Report
	if _, err := tablewire.EncodeRetain(m, full, &whole, &wholeSave); err != nil {
		t.Fatal(err)
	}
	if wholeSave.RetainLost != 0 {
		t.Fatalf("a save with room for every id lost %d records", wholeSave.RetainLost)
	}
	carried := len(whole.Ids())
	if carried < 2 {
		t.Fatalf("the wire carries %d distinct retained ids, and one entry short of it is not a row", carried)
	}

	// ONE BYTE SHORT OF THE LAST RECORD: one record fewer is kept, one more
	// retain_lost counts, and the READ's own counters do not move.
	short := m.New(m.Lookup("Node"))
	tight := tablewire.Retain{Capacity: whole.Used() - 1, IdCapacity: 1024}
	var tightReport tabletext.Report
	if _, err := tablewire.DecodeRetain(m, short, wire, &tight, &tightReport); err != nil {
		t.Fatal(err)
	}
	if tightReport.Retained != wholeReport.Retained-1 {
		t.Fatalf("retained=%d, want %d", tightReport.Retained, wholeReport.Retained-1)
	}
	if tightReport.RetainLost != wholeReport.RetainLost+1 {
		t.Fatalf("retain_lost=%d, want %d", tightReport.RetainLost, wholeReport.RetainLost+1)
	}
	if tightReport.Unknown != wholeReport.Unknown || tightReport.Malformed ||
		tightReport.KindMismatch != 0 || tightReport.Clamped != 0 || tightReport.Widened != 0 {
		t.Fatalf("a full buffer moved a read counter: %+v", tightReport)
	}

	// AN ID LIST ONE ENTRY SHORT: the records whose id has no entry are
	// dropped, one retain_lost each, and the save is NEVER REFUSED.
	sparse := m.New(m.Lookup("Node"))
	one := tablewire.Retain{Capacity: 1 << 20, IdCapacity: carried}
	var loadReport tabletext.Report
	if _, err := tablewire.DecodeRetain(m, sparse, wire, &one, &loadReport); err != nil {
		t.Fatal(err)
	}
	// THE LIST FILLS AS THE SAVE WALK INTERNS, so the load's own count stands
	if loadReport.Retained != wholeReport.Retained {
		t.Fatalf("the load dropped a record for an id list it never wrote into: %+v", loadReport)
	}
	// `parcel` is the ONE record naming ids beyond its own, so it is the one a
	// short list cannot seat, and every other record rides whatever the
	// shortfall. Two capacities say it: one entry short of the twelve the wire
	// carries, which is the page's own row, and the two the reference's gate
	// runs, which is short by ten. THE COUNT IS PER RECORD AND NOT PER ENTRY,
	// so both answer one.
	wantShort := retainVector(t, "retain_rt1_save_id_short")
	for _, capacity := range []int{carried - 1, 2} {
		one.IdCapacity = capacity
		var saveReport tabletext.Report
		out, err := tablewire.EncodeRetain(m, sparse, &one, &saveReport)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) == 0 {
			t.Fatalf("capacity %d: the save was refused, and an id past the capacity is never a refusal", capacity)
		}
		if saveReport.RetainLost != 1 {
			t.Fatalf("capacity %d: retain_lost=%d, want 1: one record's ids do not fit and exactly it is dropped",
				capacity, saveReport.RetainLost)
		}
		// AND THE DROPPED RECORD SPENT NO ENTRY: the entries it reached before
		// the overflow are given back, so what is left is what the records that
		// RODE named.
		if got := len(one.Ids()); got > capacity {
			t.Fatalf("capacity %d: the caller's list holds %d entries", capacity, got)
		}
		if slices.Contains(one.Ids(), ir.TableWireId("parcel")) {
			t.Fatalf("capacity %d: the dropped record's own id took an entry in the caller's list", capacity)
		}
		for _, name := range []string{"future", "extra"} {
			if !slices.Contains(one.Ids(), ir.TableWireId(name)) {
				t.Fatalf("capacity %d: %q names a record that rode, and it is not in the caller's list", capacity, name)
			}
		}
		// AND THE BYTES ARE THE REFERENCE'S: a drop for want of an id entry
		// takes a record out of a body and renumbers every reference behind it,
		// which a counter cannot see.
		if !bytes.Equal(out, wantShort) {
			t.Fatalf("capacity %d: the save is %d bytes and the reference's pin is %d, and they differ",
				capacity, len(out), len(wantShort))
		}
	}
}

// ---------------------------------------------------------------------------
// the hand-built wire, and the rows a pinned vector cannot reach
// ---------------------------------------------------------------------------

// wireBuilder is the subset of the reference's WireBuilder
// (test/tables/wirebuilder.h) the rows below need. The framing is built the way
// §3 states it rather than spelled in magic bytes: the body names ids by
// REFERENCE, the builder interns them in first-use order, and `finish` lays out
// the form byte, the body and the id table the body actually named.
type wireBuilder struct {
	body []byte
	ids  []uint64
}

// ref is the reference an id takes, appended on first use: reference k is the
// table's kth entry, counted from 1.
func (b *wireBuilder) ref(name string) uint64 {
	id := ir.TableWireId(name)
	for i, entry := range b.ids {
		if entry == id {
			return uint64(i) + 1
		}
	}
	b.ids = append(b.ids, id)
	return uint64(len(b.ids))
}

func (b *wireBuilder) u8(v uint8) { b.body = append(b.body, v) }

// leb is one canonical unsigned LEB128: every length, count, index and
// reference on this wire.
func (b *wireBuilder) leb(v uint64) {
	for v >= 0x80 {
		b.u8(uint8(v) | 0x80)
		v >>= 7
	}
	b.u8(uint8(v))
}

// openLen reserves ONE placeholder byte for a length that cannot be patched in
// place, because a canonical LEB128 has one spelling and its width follows the
// payload. closeLen moves the payload up when the length needs more room, so
// what comes out is that one legal spelling.
func (b *wireBuilder) openLen() int {
	at := len(b.body)
	b.u8(0)
	return at
}

func (b *wireBuilder) closeLen(at int) {
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

// field is a field header: the id reference and the kind byte, which is the
// whole of it.
func (b *wireBuilder) field(name string, kind uint8) {
	b.leb(b.ref(name))
	b.u8(kind)
}

// end is the body's own ZERO REFERENCE, which is what ends it (§3).
func (b *wireBuilder) end() { b.u8(0) }

// finish is the whole file: the form byte, the body, then the entries and the
// fixed u64 count the reader finds them from.
func (b *wireBuilder) finish() []byte {
	out := []byte{1} // the FORM BYTE is the whole header
	out = append(out, b.body...)
	for _, id := range b.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(b.ids)))
}

// deepWire is the reference's `deep_wire` (test/tables/retain_main.cpp) written
// against the same grammar: one unknown field whose payload is `depth` nested
// table bodies, with a scalar at the bottom.
func deepWire(depth int) []byte {
	b := &wireBuilder{}
	marks := make([]int, depth)
	b.field("future", ir.TableKindTable)
	marks[0] = b.openLen()
	for d := 1; d < depth; d++ {
		b.field("deeper", ir.TableKindTable)
		marks[d] = b.openLen()
	}
	b.field("hits", ir.TableKindI32)
	b.body = binary.LittleEndian.AppendUint32(b.body, 9)
	for d := depth - 1; d >= 0; d-- {
		b.end()
		b.closeLen(marks[d])
	}
	b.end()
	return b.finish()
}

// THE CAP IS A SMALL STATED CONSTANT AND IT COUNTS NESTED BODIES: 64 of them
// (docs/SPEC-TABLES.md §6.6). A record one past the cap is dropped on the same
// rule as any other shape the walk cannot take, and the LAST DEPTH THE CAP
// ADMITS still rides, out as well as in. The reference pins these same two
// depths in the same words (test/tables/retain_main.cpp, `walk_is_linear`), and
// a cap that counts from a different floor is a second wire law: the two
// engines drop different files.
func TestRetainWalkDepthCap(t *testing.T) {
	m := retainModel(t, "RT1.schema")
	rows := []struct {
		depth    int
		retained int
		lost     int
		what     string
	}{
		{depth: 64, retained: 1, lost: 0, what: "the last depth the cap admits"},
		{depth: 65, retained: 0, lost: 1, what: "one past the cap"},
	}
	for _, row := range rows {
		t.Run(row.what, func(t *testing.T) {
			inst := m.New(m.Lookup("Node"))
			retain := tablewire.Retain{Capacity: 1 << 16, IdCapacity: 1024}
			var report tabletext.Report
			ok, err := tablewire.DecodeRetain(m, inst, deepWire(row.depth), &retain, &report)
			if err != nil {
				t.Fatal(err)
			}
			// THE WALK'S VERDICT CHANGES NOTHING ELSE (§6.6): the outer framing
			// was sound and the reader's own data is what retention off would
			// have left, so `malformed` does not move either way.
			if !ok || report.Malformed {
				t.Fatalf("%d nested bodies: the load reported damage on a sound wire: %+v", row.depth, report)
			}
			if report.Retained != row.retained || report.RetainLost != row.lost {
				t.Fatalf("%d nested bodies: retained=%d retain_lost=%d, want %d / %d",
					row.depth, report.Retained, report.RetainLost, row.retained, row.lost)
			}
			// and the record the cap admits rides back out, which is the save
			// side's own pass
			var saveReport tabletext.Report
			out, err := tablewire.EncodeRetain(m, inst, &retain, &saveReport)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) == 0 || saveReport.RetainLost != 0 {
				t.Fatalf("%d nested bodies: the save wrote %d bytes and lost %d records it had room for",
					row.depth, len(out), saveReport.RetainLost)
			}
		})
	}
}

// A DROPPED RECORD SPENDS NO ENTRY IN EITHER STORE (docs/SPEC-TABLES.md §6.6).
// A record whose emit goes bad is not written at all: one `retain_lost` counts,
// NOTHING ELSE ABOUT THE SAVE CHANGES, and the save is never refused. The ids
// that record reached before the overflow are part of "nothing else": they were
// taken for a field that never rode, so a later record must find the list
// exactly as the dropped one found it.
//
// The reference undoes both stores at one mark
// (internal/codegen/cpptable/retain.go, `truncate`), and the expected counts
// here are its rule read off the page: EXACTLY ONE `retain_lost` for each
// dropped record, and the later record's id interned once.
//
// The wire is a record naming four ids under a list that holds two, so it
// overflows PARTWAY, and a second record naming one new id behind it. A leaked
// entry is invisible in the first record's own answer and kills the second.
func TestRetainDroppedRecordSpendsNoId(t *testing.T) {
	m := retainModel(t, "RT1.schema")

	b := &wireBuilder{}
	b.field("future", ir.TableKindTable) // record one: four ids, `future` and its three
	at := b.openLen()
	for _, name := range []string{"one", "two", "three"} {
		b.field(name, ir.TableKindI32)
		b.body = binary.LittleEndian.AppendUint32(b.body, 7)
	}
	b.end()
	b.closeLen(at)
	b.field("later", ir.TableKindI32) // record two: one id, and it is new
	b.body = binary.LittleEndian.AppendUint32(b.body, 11)
	b.end()
	wire := b.finish()

	inst := m.New(m.Lookup("Node"))
	// TWO ENTRIES, which is fewer than the first record's four and more than
	// the second record's one
	retain := tablewire.Retain{Capacity: 1 << 16, IdCapacity: 2}
	var loadReport tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, wire, &retain, &loadReport)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loadReport.Malformed {
		t.Fatalf("the load reported damage on a sound wire: %+v", loadReport)
	}
	// THE LIST FILLS AS THE SAVE WALK INTERNS, so the load keeps both records
	// and neither store has been written into yet (§6.6)
	if loadReport.Retained != 2 || loadReport.RetainLost != 0 {
		t.Fatalf("retained=%d retain_lost=%d, want 2 / 0", loadReport.Retained, loadReport.RetainLost)
	}

	var saveReport tabletext.Report
	out, err := tablewire.EncodeRetain(m, inst, &retain, &saveReport)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("the save was refused, and an id past the capacity is never a refusal")
	}
	// ONE DROPPED RECORD, ONE `retain_lost`: the second record names one id the
	// list has room for, so it rides.
	if saveReport.RetainLost != 1 {
		t.Fatalf("retain_lost=%d, want 1: the first record overflows and the second has room",
			saveReport.RetainLost)
	}
	later := ir.TableWireId("later")
	if got := retain.Ids(); len(got) != 1 || got[0] != later {
		t.Fatalf("the caller's list is %#x, want exactly the surviving record's own id %#x", got, later)
	}
	// and the file's own trailer says the same: the surviving id is in it, and
	// no id the dropped record reached took an entry anywhere
	ids := idsOf(t, out)
	if !slices.Contains(ids, later) {
		t.Fatal("the surviving record rode without its id in the file's trailer")
	}
	for _, name := range []string{"future", "one", "two", "three"} {
		if slices.Contains(ids, ir.TableWireId(name)) {
			t.Fatalf("%q took a trailer entry for a record that was dropped", name)
		}
	}
}
