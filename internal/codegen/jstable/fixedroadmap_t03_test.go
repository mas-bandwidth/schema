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
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE OR RUNS THE LEG'S NODE:
// every clause is read off the JavaScript this leg emits or off the shared
// walk it renders, so the suite RUNS on a tree that never built the corpus and
// never installed a node — the failure mode a reader found in the previous
// harness, whose SKIP reported done verdicts over tests that never executed.
//
//	A task already held by an existing test is named by that test in the
//	card's verdicts and is not repeated here (js/R6 is
//	TestJSFixedNoFormMeansNoLineageToParse); this file carries the direct
//	assertion of every clause that had none.
//
//	js/R14 [owed]: layout_record_too_large for an entry reaching past the
//	               writer's declared record, not only for the 65536 bound and a
//	               zero root.
//	js/R22 [owed]: the closure rule: every table or type reached by value is
//	               itself fixed; a pointer, map or unbounded array in the closure
//	               is a compile refusal; T is never in its own closure.
//	js/R3  [verify]: the floor is 1 + the highest retired index (0 when none);
//	               below the floor is layout_unsupported, reporting the file's
//	               hash.
//	js/R32 [weak]: retire for real: a retired version is refused by name, once,
//	               idempotently.
//	js/R10 [weak]: the run-time walk of a stranger's layout and the recompute of
//	               the header's hash are retired.
//	js/R15 [verify]: the four forward-read clamps are retired — count clamp across
//	               bounds, range clamp across versions, remap of an unknown
//	               variant to None, drop-and-count of an unknown field: each is
//	               layout_newer now.

import (
	"fmt"
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

// t03JsR14: R14 "layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root".
// docs/FIXED-FORM-ALGORITHM.md §5.2's PLAN: "record := x.root.size — EVERY
// entry is bounded by this ... if an entry reached past `record`: REFUSE
// layout_record_too_large", and §5.8 row 13 (fix 3): the name is not wired
// only to the 65536 bound and a zero root. The emitted runtime body is the
// whole of the read side's defence.
func t03JsR14(t *testing.T) {
	runtime := fixedRuntime
	for _, want := range []struct{ text, what string }{
		{"rootSize === 0 || rootSize > TableFixedRecordMaxBytes", "R14 the zero root and the 65536 bound"},
		{"size > TableFixedRecordMaxBytes", "R14 a single entry past the 65536 bound"},
		{"sum > TableFixedRecordMaxBytes", "R14 a subtree's sum past the 65536 bound"},
		{"plan.record = TableFixedSize(theirs, 0);", "R14 the writer's own declared record is the bound"},
		{"const reach = src + size + (op === TableFixedOpText ? 4 : 0);", "R14 an entry's source reach past the writer's record"},
		{"if (reach > plan.record) { plan.hostile = true; return; }", "R14 the over-reach is recorded"},
		{"guard >= plan.record || guard + w > plan.record", "R14 a guard offset out of the record"},
		{"if (plan.hostile) { return -2; }", "R14 the plan is refused WHOLE"},
		{"lane.why = TableFixedRefusal.LayoutRecordTooLarge;", "R14 the refusal is named layout_record_too_large"},
	} {
		if !strings.Contains(runtime, want.text) {
			t.Errorf("%s: the emitted runtime does not carry %q", want.what, want.text)
		}
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
		{"a plain table by value", "plain table Node",
			"package probe\n\ntable Node { x int32 }\n\nfixed table T { node Node }\n"},
		{"a plain table in an array", "plain table Ship",
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
