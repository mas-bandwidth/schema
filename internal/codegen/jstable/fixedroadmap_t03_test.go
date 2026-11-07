package jstable

// fixedroadmap_t03 — the js leg's fixed-closure, retirement and retired-runtime
// rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed
// Tables"), the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §1.1 (the seven rules), §5.2 (PLAN's record
// bound and THE FLOOR), §5.5 (the closure), §5.6 (what is retired),
// docs/SPEC-TABLES.md §3.4, and the floor/hash rows of
// docs/FIXED-FORM-VERSIONING-TESTS.md. One subtest per task id, table-driven,
// t.Parallel() first.
//
// A task already held by an existing test is named by that test in the card's
// verdicts and is not repeated here (js/R6 is TestJSFixedNoFormMeansNoLineageToParse);
// this file carries the direct assertion of every clause that had none.
//
// js/R14 [owed]: layout_record_too_large for an entry reaching past the writer's
//                declared record, not only for the 65536 bound and a zero root.
//                It is a RUN: a hand-built hostile layout — the C++ reference's
//                record_bound_case (test/tables/fixedform_main.cpp), a record
//                body of FOUR bytes and a bare array of size zero the reader
//                takes as a counted one — is compiled by the emitted runtime and
//                refused WHOLE, by name, with no accepted entry. Source-text
//                presence is not the clause; the guarded site is.
// js/R22 [owed]: the closure rule: every table or type reached by value is
//                itself fixed; a pointer, map or unbounded array in the closure
//                is a compile refusal; T is never in its own closure.
// js/R3  [verify]: the floor is 1 + the highest retired index (0 when none);
//                below the floor is layout_unsupported, reporting the file's
//                hash.
// js/R32 [weak]: retire for real: a retired version is refused by name, once,
//                idempotently.
// js/R10 [weak]: the run-time walk of a stranger's layout and the recompute of
//                the header's hash are retired.
// js/R15 [verify]: the four forward-read clamps are retired — count clamp across
//                bounds, range clamp across versions, remap of an unknown
//                variant to None, drop-and-count of an unknown field: each is
//                layout_newer now.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapJsT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/R14", t03JsR14},
		{"js/R22", t03JsR22},
		{"js/R3", t03JsR3},
		{"js/R32", t03JsR32},
		{"js/R10", t03JsR10},
		{"js/R15", t03JsR15},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03JsCheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal is read by name rather than caught as a fixture bug.
func t03JsCheckErrs(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return []error{perrs[0]}
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// t03PutEntry appends one seventeen-byte layout entry — an id, a kind, a size
// and a child count — the way §1's Entry is laid down (docs/FIXED-FORM-ALGORITHM.md
// §1, test/tables/fixedform_main.cpp's put_entry).
func t03PutEntry(layout []byte, id uint64, kind uint8, size, children uint32) []byte {
	e := make([]byte, 17)
	binary.LittleEndian.PutUint64(e, id)
	e[8] = kind
	binary.LittleEndian.PutUint32(e[9:], size)
	binary.LittleEndian.PutUint32(e[13:], children)
	return append(layout, e...)
}

// t03JsR14: R14 "layout_record_too_large for an entry reaching past the writer's
// declared record, not only for the 65536 bound and a zero root".
// docs/FIXED-FORM-ALGORITHM.md §5.2's PLAN: "record := x.root.size — EVERY entry
// is bounded by this ... if an entry reached past `record`: REFUSE
// layout_record_too_large", and §5.8 row 13 (fix 3): the name is not wired only
// to the 65536 bound and a zero root. The hostile layout is the C++ reference's
// record_bound_case (test/tables/fixedform_main.cpp): a record body of FOUR
// bytes and a bare array of size ZERO — valid under §1.1, but the reader takes
// bytes(N) as a COUNTED array, so the count entry reads at 4..8 of a record
// that ends at 4. The emitted runtime compiles it and refuses the plan WHOLE,
// by name, with no accepted entry, and the run is the whole of the clause —
// not a substring of the source.
func t03JsR14(t *testing.T) {
	const schema = `package probe

fixed table Host
{
    keep  uint32 = 7
    label string(8) = "hi"
    blob  bytes(6)
}
`
	u := unitFrom(t, schema)
	st := findTable(t, u, "Host")
	w := fixedWalkRoot(st)

	// THE IDS ARE READ OUT OF THIS BUILD'S OWN LAYOUT, not spelled as hashes,
	// so the case cannot quietly stop naming the fields it means.
	var rootID, keepID, blobID, blobElemID uint64
	for i, e := range w.entries {
		switch {
		case i == 0:
			rootID = e.id
		case e.kind == 8 && e.size == 4 && keepID == 0:
			keepID = e.id // keep, a uint32
		case e.kind == 14 && i+1 < len(w.entries) && w.entries[i+1].kind == 6 && w.entries[i+1].size == 1:
			blobID, blobElemID = e.id, w.entries[i+1].id // blob, bytes(6): an array of u8
		}
	}
	if rootID == 0 || keepID == 0 || blobID == 0 {
		t.Fatal("R14: the ids this case names are the ids this build's own layout carries")
	}

	// THE HOSTILE LAYOUT: the record body is FOUR bytes (keep fills it), then an
	// array of size ZERO — a valid bare array of no elements, which §1.1 admits
	// (size % elem == 0). This reader's own row takes bytes(N) as a COUNTED
	// array, so the count entry reads at 4..8 of a record that ends at 4: an
	// entry reaching past the writer's declared record.
	hostile := binary.LittleEndian.AppendUint32(nil, 4)
	hostile = t03PutEntry(hostile, rootID, 13, 4, 2)
	hostile = t03PutEntry(hostile, keepID, 8, 4, 0)
	hostile = t03PutEntry(hostile, blobID, 14, 0, 1)
	hostile = t03PutEntry(hostile, blobElemID, 6, 1, 0)
	hostileWire := ir.TableFixedLayoutHash(hostile, st)

	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Host": {
		{Wire: hostileWire, Layout: hostile, Record: fixedHashBytes + 4},
	}})
	if err != nil {
		t.Fatalf("R14: GenerateLineage: %v", err)
	}

	// MY SIDE of the walk, for the direct compile — the reader's own dst rows,
	// five lanes per entry, read off the same walk the emitter writes.
	var dst strings.Builder
	for _, d := range w.dst {
		fmt.Fprintf(&dst, "  %d, %d, %d, %d, %d,\n", d.dst, d.stride, d.aux, d.counted, d.arg)
	}
	var hostileList strings.Builder
	for _, b := range hostile {
		fmt.Fprintf(&hostileList, "  %d,\n", b)
	}

	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u)
	probe := fmt.Sprintf(`// GENERATED BY internal/codegen/jstable/fixedroadmap_t03_test.go — the
// js/R14 probe, one hostile lineage entry handed to the build the way a lock
// would hand it (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #18).
import { Host, InitHost, HostFixedSave, HostFixedMeasure, HostFixedLoad, HostFixedNewPlan,
         HostFixedLayoutCopy, HostFixedLayoutBytes, HostFixedBodyBytes, HostFixedRecordBytes,
         HostFixedHashLo, HostFixedHashHi,
         TableFixedReport, TableFixedRefusal, TableFixedRefusalName, TableFixedLayoutView,
         TableFixedParseLayout, TableFixedPlan, TableFixedCompile, TableFixedKnownLayout,
         TableFixedLineagePlans } from "./%sTable.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

// 1. THE HOSTILE LAYOUT PASSES EVERY RULE OF §1.1, so the refusal below is the
//    record bound and not a malformation.
const hostile = new Uint8Array([
%s]);
const lay = new TableFixedLayoutView();
if (!TableFixedParseLayout(hostile, 0, hostile.length, lay)) {
  fail("R14: the hostile layout passes every rule of §1.1, got " + TableFixedRefusalName(lay.refusal));
}

// 2. THE PLAN IT RESOLVES TO IS REFUSED WHOLE, never partly compiled: the
//    compile answers -2 (hostile) and records the over-reach.
const dst = new Int32Array([
%s]);
const plan = new TableFixedPlan(256, HostFixedBodyBytes, 4096);
const made = TableFixedCompile(lay, HostFixedLayoutCopy(), dst, plan, new TableFixedReport());
if (made !== -2) { fail("R14: the compile answers " + made + ", want -2 (hostile)"); }
if (plan.hostile !== true) { fail("R14: the over-reach is not recorded on the plan"); }

// 3. THE LINEAGE LANE it becomes carries the NAME and NO accepted entry: the
//    build refuses it WHOLE and lays down an empty lane, never a partly-built
//    plan.
const known = [
  new TableFixedKnownLayout(%d, %d, hostile, hostile.length, %d),
  new TableFixedKnownLayout(HostFixedHashLo, HostFixedHashHi, HostFixedLayoutCopy(), HostFixedLayoutBytes, HostFixedRecordBytes),
];
const plans = TableFixedLineagePlans(known, HostFixedHashLo, HostFixedHashHi, HostFixedLayoutCopy(), dst, HostFixedBodyBytes);
if (plans[0].why !== TableFixedRefusal.LayoutRecordTooLarge) {
  fail("R14: the hostile entry owes layout_record_too_large, got " + TableFixedRefusalName(plans[0].why));
}
if (plans[0].count !== 0) {
  fail("R14: the plan is refused WHOLE, never partly compiled (" + plans[0].count + " entries)");
}

// 4. A LOAD OF THE SAME BYTES refuses by the same name, and sets nothing.
const file = new Uint8Array(20 + hostile.length + 12);
file[0] = 3;
const view = new DataView(file.buffer);
view.setUint32(8, %d, true);
view.setUint32(12, %d, true);
view.setUint32(16, hostile.length, true);
file.set(hostile, 20);
view.setUint32(20 + hostile.length, %d, true);
view.setUint32(20 + hostile.length + 4, %d, true);
const values = [new Host()]; InitHost(values[0]);
const report = new TableFixedReport();
const n = HostFixedLoad(values, 1, file, file.length, HostFixedNewPlan(4096, 4096), report);
if (n !== -1 || report.refused !== TableFixedRefusal.LayoutRecordTooLarge) {
  fail("R14: the load owes -1 layout_record_too_large, got " + n + " " + TableFixedRefusalName(report.refused));
}
if (report.unknown !== 0 || report.kindMismatch !== 0 || report.clamped !== 0 || report.widened !== 0 || report.malformed) {
  fail("R14: the refusal sets nothing and counts nothing");
}

// 5. THE POSITIVE CONTROL: this build's own file reads, so the refusal above is
//    the over-reach and not the reader.
const v = new Host(); InitHost(v);
const good = new Uint8Array(HostFixedMeasure(1));
if (HostFixedSave([v], 1, good) !== good.length) { fail("R14: the control write failed"); }
const back = [new Host()]; InitHost(back[0]);
const rr = new TableFixedReport();
const nn = HostFixedLoad(back, 1, good, good.length, HostFixedNewPlan(4096, 4096), rr);
if (nn !== 1 || rr.refused !== 0) {
  fail("R14: this build's own file reads: " + nn + " " + TableFixedRefusalName(rr.refused));
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`, home, hostileList.String(), dst.String(), uint32(hostileWire), uint32(hostileWire>>32), fixedHashBytes+4, uint32(hostileWire), uint32(hostileWire>>32), uint32(hostileWire), uint32(hostileWire>>32))
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	node := jsNode(t)
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("R14: the probe over the generated module: %v\n%s", err, out)
	}
}

// t03JsR22: R22 "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure". §5.5 and the rows of
// docs/FIXED-FORM-VERSIONING-TESTS.md (closure_plain_table,
// closure_variable_kind, closure_self).
func t03JsR22(t *testing.T) {
	// "every table or type reached by value is itself fixed": a nested fixed
	// table is a closure member, not a break.
	fixed := jsUnitOf(t, `package probe

fixed table Inner { x int32 }
fixed table T { inner Inner }
`)
	if breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct { return fixed.Tables[name] }, "T"); len(breaks) != 0 {
		t.Errorf("R22: a fixed table nested by value is a closure break: %v", breaks)
	}

	rows := []struct {
		name, want, src string
	}{
		{"a variable table by value", "variable table Node",
			"package probe\n\ntable Node { x int32 }\n\nfixed table T { node Node }\n"},
		{"a variable table in an array", "variable table Ship",
			"package probe\n\ntable Ship { hp int32 }\n\nfixed table T { ships [..4]Ship }\n"},
		{"a pointer", "is a pointer",
			"package probe\n\ntable Node { x int32 }\n\nfixed table T { head *Node }\n"},
		{"a map", "is a map",
			"package probe\n\nfixed table T { ships map[uint32]int32 }\n"},
		{"an unbounded array", "is an unbounded array",
			"package probe\n\nfixed table T { entries []int32 }\n"},
		{"itself by value", "T",
			"package probe\n\nfixed table T { seq uint32\n    next T }\n"},
		{"itself through a type", "T",
			"package probe\n\ntype Link { to T }\n\nfixed table T { seq uint32\n    link Link }\n"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := t03JsCheckErrs(t, tc.src)
			if len(errs) == 0 {
				t.Fatalf("R22 %s: the closure break compiled — %q is a compile refusal", tc.name, tc.want)
			}
			var got strings.Builder
			for _, e := range errs {
				fmt.Fprintf(&got, "%s", e)
			}
			if !strings.Contains(got.String(), tc.want) {
				t.Errorf("R22 %s: the refusal must name %q: %s", tc.name, tc.want, got.String())
			}
		})
	}

	// THE POSITIVE CONTROL: a closure of fixed things alone compiles and the
	// leg emits the holder's form.
	legal := jsUnitOf(t, `package probe

fixed table Inner
{
    x int32
}

fixed table T
{
    child Inner
}
`)
	files, err := Generate(legal)
	if err != nil {
		t.Fatalf("R22: a closure of fixed things alone compiles: %v", err)
	}
	saw := false
	for _, body := range files {
		saw = saw || strings.Contains(string(body), "TFixedHashLo")
	}
	if !saw {
		t.Error("R22: the legal closure keeps its form")
	}
}

// t03JsR3: R3 "the floor is 1 + the highest retired index (0 when none); below
// the floor is layout_unsupported, reporting the file's hash". The behavioural
// column is TestJSFixedVersioningFloor's (floor_at, floor_below with the file's
// hash, floor_raise_live); this subtest pins the FLOOR VALUE itself off the
// emitted constant and the Go-side computation, so a floor that disagreed with
// the entries it was derived from is red by name.
func t03JsR3(t *testing.T) {
	u := jsUnitOf(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R3: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{
		{Wire: own.Wire ^ 0x1111111111111111, Layout: own.Layout, Record: own.Record},
		{Wire: own.Wire ^ 0x2222222222222222, Layout: own.Layout, Record: own.Record},
	}
	for _, tc := range []struct {
		retire int
		want   int
	}{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		handed := append([]FixedLineageEntry(nil), older...)
		for i := 0; i < tc.retire; i++ {
			handed[i].Retired = true
			handed[i].Reason = "retired by the test's lock"
		}
		entries, floor := fixedLineageFor(handed, own)
		if floor != tc.want {
			t.Errorf("R3: retire=%d: the floor is %d, want 1 + the highest retired index = %d (0 when none is)", tc.retire, floor, tc.want)
		}
		if len(entries) != len(handed)+1 {
			t.Errorf("R3: retire=%d: the lineage has %d entries, want the %d handed and the own entry last", tc.retire, len(entries), len(handed))
		}
		// The emitted module carries the floor as one number.
		files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": handed})
		if err != nil {
			t.Fatalf("R3: retire=%d: GenerateLineage: %v", tc.retire, err)
		}
		src := t02Source(t, u, files)
		if want := fmt.Sprintf("export const TFixedFloor = %d;", tc.want); !strings.Contains(src, want) {
			t.Errorf("R3: retire=%d: the emitted floor is not %q", tc.retire, want)
		}
	}
}

// t03JsR32: R32 "retire for real: a retired version is refused by name, once,
// idempotently". The by-name half is TestJSFixedVersioningFloor's floor_below;
// this subtest is the ONCE and the IDEMPOTENTLY, and the stay-forever: the
// retired entry's hash rides once in the known array, the emitted load refuses
// below the floor layout_unsupported by name, and re-deriving the floor over
// the same retired marks changes nothing.
func t03JsR32(t *testing.T) {
	u := jsUnitOf(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R32: FixedLineageOf has no entry for T")
	}
	older := FixedLineageEntry{
		Wire:    own.Wire ^ 0x1111111111111111,
		Layout:  own.Layout,
		Record:  own.Record,
		Retired: true,
		Reason:  "retired by the test's lock",
	}
	handed := []FixedLineageEntry{older}

	// IDEMPOTENTLY: the floor is derived from a bool mark, so re-deriving over
	// the same retired entries answers the same floor and the same lineage.
	entries, floor := fixedLineageFor(handed, own)
	if floor != 1 {
		t.Fatalf("R32: a retired version's floor is %d, want 1", floor)
	}
	again, floor2 := fixedLineageFor(handed, own)
	if floor2 != floor || len(again) != len(entries) {
		t.Errorf("R32: retiring the same version again moved the floor (%d -> %d) or the lineage (%d -> %d entries)", floor, floor2, len(entries), len(again))
	}

	// REFUSED BY NAME, ONCE, and the entry stays forever: the emitted load
	// refuses below the floor layout_unsupported, and the retired entry's hash
	// rides exactly once in the known array.
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": handed})
	if err != nil {
		t.Fatalf("R32: GenerateLineage: %v", err)
	}
	src := t02Source(t, u, files)
	t02Has(t, src, "TableFixedRefusal.LayoutUnsupported", "R32 a retired version is refused by name layout_unsupported")
	wire := fmt.Sprintf("0x%08x, 0x%08x", uint32(older.Wire), uint32(older.Wire>>32))
	if n := strings.Count(src, wire); n != 1 {
		t.Errorf("R32: the retired entry's hash appears %d times in the known array, want once", n)
	}
	if n := strings.Count(src, "// RETIRED: retired by the test's lock"); n != 1 {
		t.Errorf("R32: the retired entry is marked %d times, want once", n)
	}
}

// t03JsR10: R10 "the run-time walk of a stranger's layout and the recompute of
// the header's hash are retired". The load path never reaches the layout parser
// and never derives a hash: the file's hash is taken as given (§5.3 step 4),
// the layout is compared to the lock's bytes and never walked (§5.6), and the
// only TableFixedHashOf in the emitted build is its definition.
func t03JsR10(t *testing.T) {
	_, src := t02Table(t, t03Flat)
	load := jsFn(src, "TFixedLoad")
	if load == "" {
		t.Fatal("R10: the emitted module has no TFixedLoad")
	}
	for _, banned := range []string{"TableFixedParseLayout(", "TableFixedHashOf("} {
		if strings.Contains(load, banned) {
			t.Errorf("R10: the load path reaches %q — a stranger's layout is never walked and the header's hash is never recomputed at run time", banned)
		}
	}
	// The whole emitted build holds one TableFixedHashOf — its definition — and
	// no call: a runtime never derives a hash from layout bytes it holds (§5.9
	// #47).
	if n := strings.Count(src, "TableFixedHashOf("); n != 1 {
		t.Errorf("R10: the emitted build carries %d mentions of TableFixedHashOf(, want its one definition and no call", n)
	}
	// The layout parser is reached only from the module-load lineage walk, which
	// is build time and never a load path (§5.9 #3, §5.8 row 3).
	if n := strings.Count(src, "TableFixedParseLayout("); n != 3 {
		t.Errorf("R10: TableFixedParseLayout( occurs %d times, want its one definition and its two module-load callers", n)
	}
}

// t03JsR15: R15 "the four forward-read clamps are retired — count clamp across
// bounds, range clamp across versions, remap of an unknown variant to None,
// drop-and-count of an unknown field: each is layout_newer now". The four rows'
// OLD-REFUSES-NEW columns are TestJSFixedVersioningOldRefusesNew's; this
// subtest proves the four rows are in the suite and none is a same-hash row,
// so a row quietly dropped from the suite — or a clamp still forwarded — is red
// by name.
func t03JsR15(t *testing.T) {
	// field_append is the drop-and-count of an unknown field,
	// array_bounded_grow the count clamp across bounds, range_widen the range
	// clamp across versions, enum_append the remap of an unknown variant to None.
	for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
		inRows := false
		for _, r := range jsVersionRows {
			if r.row != row {
				continue
			}
			inRows = true
			if r.sameHash {
				t.Errorf("R15: %s is a same-hash row: it reads in both directions and cannot carry the refusal", row)
			}
		}
		if !inRows {
			t.Errorf("R15: %s is not in jsVersionRows: its OLD-REFUSES-NEW column is where the retired clamp is layout_newer by name", row)
		}
	}
	// The refusal each column lands is layout_newer, by name, in the emitted
	// load — the reader that once forwarded now refuses on the hash alone
	// (§5.6: all four forward reads are now layout_newer).
	_, src := t02Table(t, t03Flat)
	t02Has(t, src, "return TableFixedRefuseHash(report, TableFixedRefusal.LayoutNewer, hashLo, hashHi);", "R15 the four retired forward reads are layout_newer by name")
}
