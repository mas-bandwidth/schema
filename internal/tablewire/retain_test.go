package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
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
	if !(at(future) > at(name) && at(future) < at(extra)) {
		t.Fatalf("future is at %d, name at %d, extra at %d", at(future), at(name), at(extra))
	}
	// a retained id enters AFTER its body's own fields, and `list` is the last
	// field the root declares
	if !(at(list) > at(name) && at(extra) > at(list)) {
		t.Fatalf("extra is at %d and list at %d: the tail did not follow the body", at(extra), at(list))
	}
	// the root's two records intern in the order they were retained
	if at(parcel) != at(extra)+1 {
		t.Fatalf("parcel is at %d and extra at %d", at(parcel), at(extra))
	}
	// THE TAIL IS PINNED BEFORE THE NODE-TABLE FIELD (§3.1, §6.6)
	if !(at(nodes) > at(parcel)) {
		t.Fatalf("the node table's id is at %d and parcel at %d", at(nodes), at(parcel))
	}
	// and the RETAINED IDS the save interned are exactly the two the root's
	// tail and the inner tails carry
	interned := retain.Ids()
	if len(interned) != 3 {
		t.Fatalf("the save interned %d retained ids, want 3", len(interned))
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
	one := tablewire.Retain{Capacity: 1 << 20, IdCapacity: len(whole.Ids())}
	var loadReport tabletext.Report
	if _, err := tablewire.DecodeRetain(m, sparse, wire, &one, &loadReport); err != nil {
		t.Fatal(err)
	}
	// THE LIST FILLS AS THE SAVE WALK INTERNS, so the load's own count stands
	if loadReport.Retained != wholeReport.Retained {
		t.Fatalf("the load dropped a record for an id list it never wrote into: %+v", loadReport)
	}
	one.IdCapacity = len(whole.Ids()) - 1
	var saveReport tabletext.Report
	out, err := tablewire.EncodeRetain(m, sparse, &one, &saveReport)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("the save was refused, and an id past the capacity is never a refusal")
	}
	if saveReport.RetainLost == 0 {
		t.Fatal("an id list one entry short dropped no record")
	}
}
