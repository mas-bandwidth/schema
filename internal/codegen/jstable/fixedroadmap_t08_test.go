package jstable

// fixedroadmap_t08 — the js leg's conformance-composites rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the
// plan-execution, interoperability, hostile-input and value-capability clauses
// after t01 (framing) and t02 (compiled plans and the definition hash). One
// subtest per task id in workflow order, each against the page sentence it
// implements — docs/FIXED-FORM-ALGORITHM.md §4.1 (the plan is PARTITIONED),
// §4.4 (the split is the count of unguarded entries), §5.2 (identity ==
// compiled), §5.3's read loop and refusal table, and docs/SPEC-TABLES.md §3.4
// (the fixed form's value domain).
//
// THE CAPABILITY CLAUSE IS A RUN: each value-capability subtest generates the
// module, hands it to the leg's own node (jsNode, $NODE) and writes, reads back
// and compares the construct at non-default and boundary values, which is what
// a target `make tables-js-fixed-form` does. A substring match on the emitted
// source is never the clause. The one capability the compiler refuses —
// utf16-values, wstring(N), compiler/target_javascript.go's refuseWideText — has
// no subtest here: it is named UNPORTED and skipped by name (see
// fixedversioning_test.go's `unported` rows), never folded into a green.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapJsT08ConformanceComposites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/W6", t08W6},
		{"js/P1", t08P1},
		{"js/W13", t08W13},
		{"js/P2", t08P2},
		{"js/R27", t08R27},
		{"js/P3", t08P3},
		{"js/P4", t08P4},
		{"signed-integers/js/valid-data", t08Signed},
		{"unsigned-integers/js/valid-data", t08Unsigned},
		{"flags-values/js/valid-data", t08Flags},
		{"keyed-arrays/js/valid-data", t08Keyed},
		{"nested-keyed-arrays/js/valid-data", t08NestedKeyed},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08Emit is Generate's source for a one-file fixture's runtime home, which is
// where the fixed table's class, writer and decoder land for a single-file unit.
func t08Emit(t *testing.T, src string) (*ir.Unit, string) {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return u, t02Source(t, u, files)
}

// t08LayoutKind answers the layout kind byte of the walk entry whose note is
// `note`, so a capability clause can name the wire kind §3.4 gives the construct.
func t08LayoutKind(t *testing.T, u *ir.Unit, rootName, note string) int {
	t.Helper()
	for _, st := range jsFixedUnitRoots(u) {
		if st.Name != rootName {
			continue
		}
		for _, e := range ir.TableFixedWalkRoot(st) {
			if e.Note == note {
				return e.Kind
			}
		}
		t.Fatalf("the walk of %s has no entry noted %q", rootName, note)
	}
	t.Fatalf("the unit declares no fixed root %s", rootName)
	return 0
}

// js/W6 [owed] "plan partition / split". §4.1 partitions a plan: every
// unguarded entry first, then every guarded one, and §4.4 says `split` is the
// NUMBER OF UNGUARDED ENTRIES — the index the second half starts at — never a
// count of guarded ones. The leg's compiler is the module's own
// TableFixedCompile, so the run compiles the module's layout against itself and
// reads `split` and the guard lane off the built plan (the same walk a lineage
// entry takes; test/js-tables/fixedform.mjs's planPartition is the leg's twin).
func t08W6(t *testing.T) {
	const src = `package probe

enum Kind { A, B }

union Pick
{
    a int32
    b int64
}

fixed table T
{
    head int32
    pick Pick
    tail int32
}
`
	u, _ := t08Emit(t, src)
	st := t02Root(t, u)
	// §4.1's partition as the compiler IR states it: every UNGUARDED entry comes
	// first and `split` is exactly the index the guarded half starts at.
	plan, split := ir.TableFixedBuildPlan(u, st)
	if split <= 0 || split >= len(plan) {
		t.Fatalf("W6: split = %d of %d entries, want a non-empty unguarded half and a non-empty guarded half", split, len(plan))
	}
	for i, e := range plan {
		unguarded := e.Guard == ir.TableFixedNoGuard
		if (i < split) != unguarded {
			t.Fatalf("W6: entry %d has guard=%d but split=%d: the partition rule is broken", i, e.Guard, split)
		}
	}
	// The RUN: the leg's own TableFixedCompile builds the plan and the guard lane
	// and `split` are read off it. TableFixedNoGuard is -1 and a plan entry is
	// TableFixedLanes (11) int32 lanes (fixedruntime.go); the guard is lane 5.
	t08Play(t, src, `const theirs = new TableFixedLayoutView();
if (!TableFixedParseLayout(TFixedLayout, 0, TFixedLayout.length, theirs)) { fail("the layout did not parse"); }
const dst = new Int32Array(theirs.count * 5);
const plan = new TableFixedPlan(256, TFixedBodyBytes, 4096);
const made = TableFixedCompile(theirs, TFixedLayout, dst, plan, new TableFixedReport());
if (made < 0 || made !== plan.count) { fail("compile answered " + made + " count " + plan.count); }
if (!(plan.split > 0 && plan.split < plan.count)) { fail("split " + plan.split + " of " + plan.count + ": want both halves"); }
let unguarded = 0;
for (let i = 0; i < plan.count; i++) {
  const guarded = plan.entries[i * TableFixedLanes + 5] !== -1;
  if (!guarded) { unguarded++; }
  if ((i < plan.split) === guarded) { fail("entry " + i + " guard " + guarded + " at split " + plan.split); }
}
if (unguarded !== plan.split) { fail("split " + plan.split + " is not the unguarded count " + unguarded); }
`)
}

// js/P1 [weak] "identity == compiled". §5.2: a type whose storage image is the
// wire's coalesces to exactly ONE unguarded COPY over the whole body from
// offset zero, which is what TableFixedIdentityFlat and TableFixedBuildPlan
// answer; the module's baked identity plan carries that same one entry, and its
// lanes are read here so a size or guard lane that drifted from the compiled
// copy goes red (§5.9's identity == compiled).
func t08P1(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Root(t, u)
	if !ir.TableFixedIdentityFlat(u, st) {
		t.Fatal("P1: two adjacent int32 fields are flat, else identity is no longer one copy")
	}
	plan, guarded := ir.TableFixedBuildPlan(u, st)
	if len(plan) != 1 {
		t.Fatalf("P1: the coalescer produced %d entries, want one COPY", len(plan))
	}
	e := plan[0]
	if e.Op != ir.TableFixedOpCopy || e.Guard != ir.TableFixedNoGuard || e.Src != 0 || e.Dst != 0 || e.Size != ir.TableFixedTypeBytes(st) {
		t.Errorf("P1: the one entry is op=%d guard=%d src=%d dst=%d size=%d, want one unguarded COPY of the whole body",
			e.Op, e.Guard, e.Src, e.Dst, e.Size)
	}
	if guarded != len(plan) {
		t.Errorf("P1: every entry of a flat plan is unguarded; split = %d of %d", guarded, len(plan))
	}
	_, src := t08Emit(t, t02Flat)
	// The emitted plan IS the compiled one: eleven int32 lanes (TableFixedLanes),
	// and the lanes carry the same op, src, dst, size and guard the IR states.
	lanes := strings.Split(t02Block(t, src, "const TFixedIdentity = new Int32Array([", "]);"), ",")
	if len(lanes) != 11 {
		t.Fatalf("P1: the identity plan has %d lanes, want 11: %q", len(lanes), lanes)
	}
	nums := make([]int, 11)
	for i, l := range lanes {
		n, err := strconv.Atoi(strings.TrimSpace(l))
		if err != nil {
			t.Fatalf("P1: the identity plan's lane %d %q does not parse: %v", i, l, err)
		}
		nums[i] = n
	}
	if nums[0] != int(ir.TableFixedOpCopy) || nums[1] != 0 || nums[2] != 0 ||
		nums[3] != int(ir.TableFixedTypeBytes(st)) || nums[5] != int(ir.TableFixedNoGuard) {
		t.Errorf("P1: the emitted identity plan is %v, want one unguarded COPY of the whole body [0 0 0 %d 0 -1 ...]",
			nums, ir.TableFixedTypeBytes(st))
	}
}

// js/W13 [verify] "layout+hash == C++ reference". fixedform.go's shared golden:
// the block and its fnv1a64 hash for the paired bench's own table equal the
// constants the C++ reference emits for the same schema.
func t08W13(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	block := fixedLayoutBytes(fixedWalkRoot(st).entries)
	const (
		refEntries   = 75
		refHash      = uint64(0x5f1320927e9ad910)
		refBodyBytes = int64(1236)
	)
	if len(fixedWalkRoot(st).entries) != refEntries {
		t.Fatalf("W13: block entries = %d, the C++ reference emits %d", len(fixedWalkRoot(st).entries), refEntries)
	}
	if got := ir.TableFixedLayoutHash(block, st); got != refHash {
		t.Fatalf("W13: block hash = 0x%016x, the C++ reference emits 0x%016x", got, refHash)
	}
	if got := fixedTypeBytes(st); got != refBodyBytes {
		t.Fatalf("W13: body bytes = %d, the C++ reference emits %d", got, refBodyBytes)
	}
}

// js/P2 [verify] "write-read-write byte-identical". §3.4: the writer lays only
// the VALUE's used units — a count's live elements, a text field's used length
// — and leaves every slack byte the template's zero, so a second write of the
// same value lands the same bytes.
func t08P2(t *testing.T) {
	// A counted array and a bounded text field, written, read back and written
	// again: the second write must land the same bytes (the slack stays zero).
	t08RoundTrip(t, `package probe

enum Grade { Bronze, Silver, Gold }

fixed table T
{
    grades [1..4]Grade
    label  string(8)
}
`, `v.Grades[0] = 2; v.Grades[1] = 0; v.Grades[2] = 1; v.GradesCount = 3;
v.Label = new Uint8Array([104, 105]); v.LabelLength = 2;`,
		`if (back[0].GradesCount !== 3) { fail("GradesCount " + back[0].GradesCount); }
if (back[0].Grades[0] !== 2 || back[0].Grades[2] !== 1) { fail("grades " + show(back[0].Grades)); }
if (back[0].LabelLength !== 2 || back[0].Label[0] !== 104) { fail("label " + back[0].LabelLength); }`)
}

// js/R27 [verify] "the manifest is the wire". build/fixedform-corpus/manifest.txt
// is the single value oracle every leg reads to (§5.9 #32, #37): one line per
// corpus file, each carrying its record count and every value it holds, and the
// values are NOT derivable from the schema. This subtest READS that file and
// takes a named corpus row through the same parser every leg's oracle is taken
// through (jsManifestRow); the deep test that drives EVERY row through
// jsManifestChecks is TestJSFixedVersioningNewReadsOld, named beside this one.
func t08R27(t *testing.T) {
	corpus := jsFixedCorpus(t)
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("R27: build/fixedform-corpus/manifest.txt is the value oracle the title names and it is unreadable: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatal("R27: the manifest is empty; the oracle carries a row per corpus file")
	}
	// The oracle's own contract: a row per corpus file with a record count and
	// at least one named value (jsManifestRow fatals when a row is missing).
	records, values := jsManifestRow(t, corpus, "old_field_append.bin")
	if records < 1 || len(values) == 0 {
		t.Fatalf("R27: the manifest row for old_field_append.bin carries records=%d values=%d", records, len(values))
	}
}

// t08Play generates the module for a one-table fixture and runs one probe with
// the leg's own node: the run is the target a gate runs (jsNode, $NODE), not a
// reading of the emitted text (docs/SPEC-TABLES.md §3.4).
func t08Play(t *testing.T, src, body string) string {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
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
	probe := fmt.Sprintf(`// one generated probe, run under the leg's own node (docs/FIXED-FORM-ALGORITHM.md §5.9 #18).
import { T, InitT, TFixedSave, TFixedLoad, TFixedNewPlan, TFixedMeasure, TFixedLayout, TFixedBodyBytes,
         TableFixedReport, TableFixedRefusal, TableFixedRefusalName, TableFixedLayoutView,
         TableFixedParseLayout, TableFixedPlan, TableFixedCompile, TableFixedLanes } from "./%sTable.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
function show(v) { return JSON.stringify(v, (k, x) => typeof x === "bigint" ? x.toString() : x); }
%s
if (failures !== 0) { process.exit(1); }
console.log("ok");
`, home, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	node := jsNode(t)
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated module did not run clean under %s: %v\n%s", node, err, out)
	}
	return string(out)
}

// t08RoundTrip writes `v` with the module's own writer, reads it into a fresh
// value and writes THAT value again: the read value is checked and the second
// write must land the same bytes (write-read-write byte-identical).
func t08RoundTrip(t *testing.T, src, setup, check string) {
	t.Helper()
	body := fmt.Sprintf(`const v = new T(); InitT(v);
%s
const bytes = new Uint8Array(TFixedMeasure(1));
const written = TFixedSave([v], 1, bytes);
if (written !== bytes.length) { fail("save answered " + written + " of " + bytes.length); }
const back = [new T()]; InitT(back[0]);
const plan = TFixedNewPlan(4096, 4096);
const report = new TableFixedReport();
const read = TFixedLoad(back, 1, bytes, bytes.length, plan, report);
if (read !== 1) { fail("load answered " + read + " " + TableFixedRefusalName(report.refused)); }
if (report.refused !== 0 || report.malformed) { fail("a clean round trip refused: " + TableFixedRefusalName(report.refused)); }
%s
const again = new Uint8Array(TFixedMeasure(1));
TFixedSave(back, 1, again);
for (let i = 0; i < bytes.length; i++) { if (bytes[i] !== again[i]) { fail("write-read-write differs at " + i); break; } }
`, setup, check)
	t08Play(t, src, body)
}

// js/P3 [weak] "hostile bytes, sweep + sanitizer". §5.3: the parser bounds a
// layout by the file and refuses a malformation by name, and the decode pass
// clamps a count or an ordinal the record carries to the reader's own bound and
// counts one clamped — the identity path included. Each clause is a RUN of the
// generated module against tampered bytes.
func t08P3(t *testing.T) {
	t08Play(t, `package probe

enum Grade { Bronze, Silver, Gold }

fixed table T
{
    grades [1..4]Grade
}
`, `const v = new T(); InitT(v);
v.Grades[0] = 1; v.GradesCount = 1;
const good = new Uint8Array(TFixedMeasure(1));
if (TFixedSave([v], 1, good) !== good.length) { fail("save failed"); }

// a non-zero reserved header byte is refused, not ignored (§3.4, §5.3).
const reserved = good.slice(); reserved[3] = 9;
if (TFixedLoad([new T()], 1, reserved, reserved.length, TFixedNewPlan(4096, 4096), new TableFixedReport()) !== -1) {
  fail("a non-zero reserved byte was not refused");
}
// a file shorter than its header is malformed.
const short = good.slice(0, 12);
if (TFixedLoad([new T()], 1, short, short.length, TFixedNewPlan(4096, 4096), new TableFixedReport()) !== -1) {
  fail("a truncated file was not refused");
}
const rec = 20 + TFixedLayout.length;
// a count the record carries past the reader's bound clamps AND counts.
const over = good.slice();
over[rec + 8] = 200;
const counted = [new T()]; InitT(counted[0]);
const rc = new TableFixedReport();
if (TFixedLoad(counted, 1, over, over.length, TFixedNewPlan(4096, 4096), rc) !== 1) { fail("the over-count record did not load"); }
if (counted[0].GradesCount !== 4) { fail("the count was not clamped to the declared bound: " + counted[0].GradesCount); }
if (rc.clamped < 1) { fail("the clamp was not counted"); }
// an ordinal past the enum's top lands the None ordinal AND counts.
const ord = good.slice();
ord[rec + 12] = 9;
const sanitized = [new T()]; InitT(sanitized[0]);
const ro = new TableFixedReport();
if (TFixedLoad(sanitized, 1, ord, ord.length, TFixedNewPlan(4096, 4096), ro) !== 1) { fail("the bad-ordinal record did not load"); }
if (sanitized[0].Grades[0] !== 0) { fail("an ordinal past the enum's top did not land None: " + sanitized[0].Grades[0]); }
if (ro.clamped < 1) { fail("the ordinal clamp was not counted"); }
`)
}

// js/P4 [weak] "wrong plan goes red". §5.3 step 11 and §4.1: a record whose
// hash is no plan's is refused no_layout, and an arm entry runs only under its
// own tag, compared at its own width — so a wrong plan lands nothing. The run
// tampers a record's own hash and reads the named refusal.
func t08P4(t *testing.T) {
	t08Play(t, t02Flat, `const v = new T(); InitT(v); v.X = 7; v.Y = 9;
const good = new Uint8Array(TFixedMeasure(1));
TFixedSave([v], 1, good);
const rec = 20 + TFixedLayout.length;
const wrong = good.slice();
wrong[rec] = (wrong[rec] + 1) & 0xff;
const back = [new T()]; InitT(back[0]);
const r = new TableFixedReport();
const n = TFixedLoad(back, 1, wrong, wrong.length, TFixedNewPlan(4096, 4096), r);
if (n !== -1) { fail("a record whose hash names no layout was not refused"); }
if (r.refused !== TableFixedRefusal.NoLayout) { fail("a wrong plan owed no_layout, got " + TableFixedRefusalName(r.refused)); }
`)
}

// signed-integers/js/valid-data [capability]: int8..int64 ride at their
// declared storage widths, two's complement; the run writes each width's
// MINIMUM (the negative boundary, which only the signed encoding reads back
// unchanged) and reads it back (docs/SPEC-TABLES.md §3.4).
func t08Signed(t *testing.T) {
	t08RoundTrip(t, `package probe

fixed table T
{
    a int8
    b int16
    c int32
    d int64
}
`, `v.A = -128; v.B = -32768; v.C = -2147483648; v.D = -9223372036854775808n;`,
		`if (back[0].A !== -128) { fail("A " + back[0].A); }
if (back[0].B !== -32768) { fail("B " + back[0].B); }
if (back[0].C !== -2147483648) { fail("C " + back[0].C); }
if (back[0].D !== -9223372036854775808n) { fail("D " + back[0].D); }`)
}

// unsigned-integers/js/valid-data [capability]: uint8..uint64 ride at their
// declared storage widths, unsigned; the run writes each width's boundary
// value, reads it back and compares (docs/SPEC-TABLES.md §3.4).
func t08Unsigned(t *testing.T) {
	t08RoundTrip(t, `package probe

fixed table T
{
    a uint8
    b uint16
    c uint32
    d uint64
}
`, `v.A = 255; v.B = 65535; v.C = 4294967295; v.D = 18446744073709551615n;`,
		`if (back[0].A !== 255) { fail("A " + back[0].A); }
if (back[0].B !== 65535) { fail("B " + back[0].B); }
if (back[0].C !== 4294967295) { fail("C " + back[0].C); }
if (back[0].D !== 18446744073709551615n) { fail("D " + back[0].D); }`)
}

// flags-values/js/valid-data [capability]: a flags mask rides as the raw
// uint64 its storage is; the run writes a non-default mask (both bits) and
// reads it back (docs/SPEC-TABLES.md §3.4).
func t08Flags(t *testing.T) {
	t08RoundTrip(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
}
`, `v.M = 3n;`,
		`if (back[0].M !== 3n) { fail("the raw mask did not round-trip: " + back[0].M); }`)
}

// keyed-arrays/js/valid-data [capability]: an enum-keyed array writes EVERY
// slot of the key enum's declared order, no count riding (§2.4), and the
// layout names it kind 16; the run writes non-default slots and reads them
// back.
func t08Keyed(t *testing.T) {
	src := `package probe

enum Tier { Bronze, Silver, Gold }

fixed table Qty { n int32 }

fixed table T
{
    slots [Tier]Qty
}
`
	t08RoundTrip(t, src, `v.Slots[0].N = 7; v.Slots[1].N = 0; v.Slots[2].N = -3;`,
		`if (back[0].Slots[0].N !== 7 || back[0].Slots[1].N !== 0 || back[0].Slots[2].N !== -3) {
  fail("the keyed slots did not round-trip: " + show(back[0].Slots));
}`)
	if got := t08LayoutKind(t, unitFrom(t, src), "T", "slots"); got != ir.TableKindKeyed {
		t.Errorf("keyed: layout kind = %d, want %d (keyed)", got, ir.TableKindKeyed)
	}
}

// nested-keyed-arrays/js/valid-data [capability]: a keyed array whose element
// type itself carries a keyed array recurses one level; the run writes a
// non-default nested slot through both writers and reads it back.
func t08NestedKeyed(t *testing.T) {
	t08RoundTrip(t, `package probe

enum Tier { Bronze, Silver, Gold }

enum Slot { Low, High }

fixed table Qty { n int32 }

fixed table Inner
{
    slots [Slot]Qty
}

fixed table T
{
    grid [Tier]Inner
}
`, `v.Grid[0].Slots[1].N = 42; v.Grid[2].Slots[0].N = 5;`,
		`if (back[0].Grid[0].Slots[1].N !== 42) { fail("nested slot [0][1] " + show(back[0].Grid)); }
if (back[0].Grid[2].Slots[0].N !== 5) { fail("nested slot [2][0] " + show(back[0].Grid)); }`)
}
