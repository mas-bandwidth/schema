package jstable

// fixedroadmap_t03 — the js leg's fixed-closure, retirement and retired-runtime
// rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed
// Tables"): the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (PLAN and the writer's declared record),
// §5.3 (LOAD and the floor), §5.5 (the closure rule), §5.6 (what is retired),
// §5.9 #23 (the retirement print), docs/FIXED-FORM-VERSIONING-TESTS.md ("The
// floor and the hash"), docs/SPEC-TABLES.md §3.4. One subtest per task id,
// table-driven, t.Parallel() first.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE OR RUNS NODE: every clause
// is asserted off the JavaScript this leg emits (Generate / GenerateLineage) or
// off the ir walk and the checker it renders, exactly as
// fixedroadmap_t02_test.go does, so the suite RUNS on a tree that never built
// the corpus and carries no node toolchain. A clause an existing test of this
// leg already holds is named in a comment and is not re-decided here.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Flat is the smallest declared fixed table with a lineage: two adjacent
// scalars, so a handed entry gives the emitted build both lanes.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t03CheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal can be read by name rather than caught as a fixture bug.
func t03CheckErrs(t *testing.T, src string) []error {
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

func TestFixedRoadmapJsT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/R6", t03R6},
		{"js/R14", t03R14},
		{"js/R22", t03R22},
		{"js/R3", t03R3},
		{"js/R32", t03R32},
		{"js/R10", t03R10},
		{"js/R15", t03R15},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R6: R6 "a table past §3.4's 65536 ceiling is not a fixed-form root: the
// refusal names the table, no form is emitted, and no lineage entry is parsed
// for it even when the lock carries one". The compiler's warning
// (ir.TableFixedRecordBounds) names the table and the ceiling; the form's own
// root filter (ir.TableFixedFormRoots and this leg's fixedRoots through
// jsFixedUnitRoots) drops it; and a lock entry handed in for it is never
// reached by fixedLineageFor, so no emitted file carries its wire hash.
func t03R6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Big
{
    blob bytes(70000)
}
`)
	st := findTable(t, u, "Big")
	if ir.TableFixedTypeBytes(st) <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past %d", ir.TableFixedTypeBytes(st), ir.TableFixedRecordMaxBytes)
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: ir.TableFixedFormRoots keeps a table past the ceiling as a form root")
		}
	}
	for _, r := range jsFixedUnitRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: this leg's root filter keeps a table past the ceiling as a form root")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning, not a refusal of the unit: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the warning does not name the table and the ceiling: %s", joined)
	}
	// THE REFUSAL IS THE FILTER (fixedRoots): the leg's own refusal sentence
	// names the ceiling it refuses by.
	if reason := fixedRefusal(st); !strings.Contains(reason, "65536") || !strings.Contains(reason, "ceiling") {
		t.Errorf("R6: the leg's refusal does not name the ceiling: %q", reason)
	}
	// A LOCK ENTRY FOR A TABLE WITH NO FORM IS NOT THIS FORM'S BUSINESS
	// (§5.9 #8, fixedlineage.go's refuseFixedLineage): it is not parsed and it
	// reaches no emitted file.
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Big": {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: a lineage entry for a table with no form must not fail the build: %v", err)
	}
	for _, body := range files {
		if strings.Contains(string(body), "0x1122334455667788") {
			t.Error("R6: an emitted file parsed the lock's lineage entry for a table past the ceiling")
		}
	}
}

// t03R14: R14 "layout_record_too_large for an entry reaching past the writer's
// declared record, not only for the 65536 bound and a zero root". §5.2's PLAN
// bounds a single entry and a subtree to the 65536 a reader caps a record at
// and the zero root to the same name (both in TableFixedParseLayout and
// TableFixedCheckEntry); THIS subtest pins the clause the task adds: every
// COMPILED entry's source extent is held to the writer's own declared root
// size, and one entry past it refuses the plan WHOLE under
// layout_record_too_large rather than landing a short read.
func t03R14(t *testing.T) {
	u, src := t02Table(t, t03Flat)
	_ = u
	// The zero root and the 65536 bound, the two clauses that already held.
	t02Has(t, src, "if (rootSize === 0 || rootSize > TableFixedRecordMaxBytes) {", "R14 the zero root and the 65536 bound")
	t02Has(t, src, "if (size > TableFixedRecordMaxBytes) {", "R14 a single entry past the 65536 bound")
	t02Has(t, src, "if (sum > TableFixedRecordMaxBytes) {", "R14 a subtree's sum past the 65536 bound")
	// THE BOUND THE TASK ADDS: the writer's own declared record, taken from the
	// layout the plan is compiled against, holds every compiled entry's source
	// extent; one past it is recorded and refuses the plan whole.
	t02Has(t, src, "plan.record = TableFixedSize(theirs, 0);", "R14 the writer's own declared record is the bound")
	t02Has(t, src, "if (plan.record !== 0 && end > plan.record) { plan.tooLarge = true; return; }",
		"R14 an entry reaching past the writer's declared record")
	t02Has(t, src, "if (plan.tooLarge) { plan.why = TableFixedRefusal.LayoutRecordTooLarge; return -1; }",
		"R14 the plan is refused WHOLE and named layout_record_too_large")
	t02Has(t, src, "if (plan.why === TableFixedRefusal.LayoutRecordTooLarge) { lane = plan; break; }",
		"R14 the refusal is the lineage lane's own name")
	// The reach arithmetic is the run loop's own, op by op, so a grow cannot
	// hide a source read and a const cannot reach past its tag.
	t02Has(t, src, "function TableFixedReach(op, size, aux, meta) {", "R14 the source reach has one spelling")
	t02Has(t, src, "case TableFixedOpCount: return 4;", "R14 a count reaches only its live word")
	t02Has(t, src, "case TableFixedOpText: return 4 + size;", "R14 a text entry reaches its length word and its units")
}

// t03R22: R22 "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure". §5.5: a nested FIXED table reached
// by value is a closure member and not a break; the rejected constructs and a
// plain `table` reached by value are the checker's refusals (ir/table.go's
// FixedClosureBreaks, surfaced by check.Unit); and a fixed table that reaches
// itself by value is refused, so a table is never its own closure member.
func t03R22(t *testing.T) {
	fixed := unitFrom(t, `package probe

fixed table Inner { a int32 }
fixed table T { inner Inner }
`)
	if breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct { return fixed.Tables[name] }, "T"); len(breaks) != 0 {
		t.Errorf("R22: a fixed table nested by value is a closure break: %v", breaks)
	}
	for _, tc := range []struct {
		name, src, want string
	}{
		{"plain table", "package probe\ntable Node { x int32 }\nfixed table T { node Node }\n", "holds the plain table Node by value"},
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		errs := t03CheckErrs(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and the page", tc.name, got, tc.want)
		}
	}
	// T IS NEVER IN ITS OWN CLOSURE: a fixed table that reaches itself by value
	// has no finite record and is refused at the declaration.
	if errs := t03CheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}

// t03R3: R3 "the floor is 1 + the highest retired index (0 when none); below
// the floor is layout_unsupported, reporting the file's hash" (§5.2, §5.7's
// three floor rows). The behavioural column is TestJSFixedVersioningFloor's
// (floor_at, floor_below with the file's hash, floor_raise_live); this subtest
// pins the FLOOR VALUE off the emitted constant, so a floor that disagreed with
// the entries it was derived from is red by name, and the enforcement sentence
// off the emitted load path.
func t03R3(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R3: FixedLineageOf has no entry for T")
	}
	for _, tc := range []struct {
		retire int
		want   int
	}{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		handed := make([]FixedLineageEntry, 0, tc.retire)
		for i := 0; i < tc.retire; i++ {
			handed = append(handed, FixedLineageEntry{
				Wire: own.Wire ^ (0x1111 * uint64(i+1)), Layout: own.Layout, Record: own.Record, Retired: true,
			})
		}
		files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": handed})
		if err != nil {
			t.Fatalf("R3: retire=%d: GenerateLineage: %v", tc.retire, err)
		}
		src := t02Source(t, u, files)
		t02Has(t, src, fmt.Sprintf("export const TFixedFloor = %d;", tc.want),
			"R3 the floor is 1 + the highest retired index, 0 when none is")
	}
	// §5.3 steps 5 and 6: below the floor is layout_unsupported and the report
	// carries THE FILE'S hash, distinct from layout_newer outside the lineage.
	src := t02Emit(t, u)
	t02Has(t, t02Norm(src), "if (pick < TFixedFloor) { return TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported, hashLo, hashHi); }",
		"R3 below the floor is layout_unsupported reporting the file's hash")
	t02Has(t, t02Norm(src), "if (pick < 0) { return TableFixedRefuseHash(report, TableFixedRefusal.LayoutNewer, hashLo, hashHi); }",
		"R3 outside the lineage stays layout_newer")
}

// t03R32: R32 "retire for real: a retired version is refused by name, once,
// idempotently". The by-name and the file's-hash halves are
// TestJSFixedVersioningFloor's floor_below; this subtest is the ONCE and the
// IDEMPOTENTLY, read off the emitted load path (node is not on this tree):
// every call RESETS the report first, recomputes the selection from the file's
// own header bytes, and answers the floor through TableFixedRefuseHash, which
// writes the report and returns -1 and touches no destination byte — so the
// same retired file read twice answers the same name and the same hash with
// every counter zero both times.
func t03R32(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R32: FixedLineageOf has no entry for T")
	}
	handed := []FixedLineageEntry{{
		Wire: own.Wire ^ 0x3333, Layout: own.Layout, Record: own.Record, Retired: true,
	}}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": handed})
	if err != nil {
		t.Fatalf("R32: GenerateLineage: %v", err)
	}
	src := t02Source(t, u, files)
	load := jsFn(src, "TFixedLoad")
	if load == "" {
		t.Fatal("R32: the emitted load function is missing")
	}
	// ONCE: the reset is the first statement, so a second call starts from the
	// same empty report the first did.
	open := strings.Index(load, "{")
	if open < 0 || !strings.HasPrefix(strings.TrimSpace(load[open+1:]), "TableFixedResetReport(report);") {
		t.Error("R32: the load path does not reset the report first, so a second read is not the first")
	}
	// IDEMPOTENTLY: the selection is recomputed from this call's header bytes
	// and the floor is a module constant; nothing on the path remembers the
	// previous read.
	t02Has(t, t02Norm(src), "const pick = TableFixedSelect(TFixedKnown, hashLo, hashHi);",
		"R32 the selection is recomputed from this call's header bytes")
	t02Has(t, t02Norm(src), "if (pick < TFixedFloor) { return TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported, hashLo, hashHi); }",
		"R32 a retired version is refused by name")
	// REFUSE IS TOTAL AND IT IS THE FILE'S HASH: the refusal writes the report
	// and returns, and no destination write stands before it.
	floorAt := strings.Index(load, "TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported")
	if floorAt < 0 {
		t.Fatal("R32: the emitted load has no layout_unsupported refusal")
	}
	prefix := load[:floorAt]
	if strings.Contains(prefix, "image[") || strings.Contains(prefix, "values[") {
		t.Error("R32: a refusal wrote the destination before the floor check")
	}
	refuse := jsFn(src, "TableFixedRefuseHash")
	if refuse == "" {
		t.Fatal("R32: TableFixedRefuseHash is not emitted")
	}
	for _, want := range []string{"report.refused = refusal;", "report.layoutHash =", "return -1;"} {
		if !strings.Contains(refuse, want) {
			t.Errorf("R32: TableFixedRefuseHash does not carry %q", want)
		}
	}
	if strings.Contains(refuse, "image") || strings.Contains(refuse, "values") {
		t.Error("R32: TableFixedRefuseHash touches a destination: a refusal writes report and hash and nothing else")
	}
}

// t03R10: R10 "the run-time walk of a stranger's layout and the recompute of
// the header's hash are retired" (§5.3 steps 4-7, §5.6). The load path's own
// half is js/R7's (t01) and the hash-as-given half is js/R4's (t02); this
// subtest widens the ban to the WHOLE emitted build — no emitted module CALLS
// TableFixedHashOf, and the load path never calls TableFixedParseLayout on a
// file's bytes, because a file's layout is COMPARED with the lock's and never
// walked — and holds §5.9 #23's retirement print, which names at the call site
// where the stranger's-walk coverage is owed.
func t03R10(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R10: FixedLineageOf has no entry for T")
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {{
		Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record,
	}}})
	if err != nil {
		t.Fatalf("R10: GenerateLineage: %v", err)
	}
	for name, body := range files {
		text := string(body)
		// NO RECOMPUTE OF THE HEADER'S HASH: the only mention of the hash
		// function is its definition; the writer spells the compile-time
		// constant and the load takes the header's hash as given.
		if all, defs := strings.Count(text, "TableFixedHashOf("), strings.Count(text, "export function TableFixedHashOf("); all != defs {
			t.Errorf("R10: %s calls TableFixedHashOf: the recompute of the header's hash is retired, on every path", name)
		}
		// NO RUN-TIME WALK OF A STRANGER'S LAYOUT: the load path compares the
		// file's layout bytes with the lock's and never parses them.
		if load := jsFn(text, "TFixedLoad"); load != "" && strings.Contains(load, "TableFixedParseLayout") {
			t.Errorf("R10: %s's load parses a layout at run time; a file's layout is compared, never walked", name)
		}
	}
	// §5.9 #23: a leg whose suite has no skip prints one instead. The js leg's
	// retirement print is test/js-tables/fixedform.mjs's `retired(name, why)`,
	// and it names the stranger's-layout families whose coverage is owed.
	main, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "js-tables", "fixedform.mjs"))
	if err != nil {
		t.Fatalf("R10: read fixedform.mjs: %v", err)
	}
	if !strings.Contains(string(main), "function retired(name, why)") {
		t.Error("R10: fixedform.mjs has no `retired(name, why)` print, so no retired case is named at its call site")
	}
	for _, name := range []string{"versioning", "negativeControls", "layoutValidation"} {
		if !strings.Contains(string(main), "retired(\""+name+"\"") {
			t.Errorf("R10: fixedform.mjs names no retirement for %q: §5.9 #23 wants the retired case named with where its coverage is owed", name)
		}
	}
}

// t03R15: R15 "the four forward-read clamps are retired — count clamp across
// bounds, range clamp across versions, remap of an unknown variant to None,
// drop-and-count of an unknown field: each is layout_newer now" (§5.6). The
// four clauses' rows are field_append (drop-and-count of an unknown field),
// array_bounded_grow (the count clamp across bounds), range_widen (the range
// clamp across versions) and enum_append (the remap of an unknown variant to
// None); each is in jsVersionRows and none is a sameHash row, so its
// OLD-REFUSES-NEW column is a refusal and not a read. The replacement on the
// emitted load path is §5.3 step 5: a hash outside the lineage is layout_newer
// before any record, and no forward clamp survives.
func t03R15(t *testing.T) {
	for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
		found := false
		for _, r := range jsVersionRows {
			if r.row != row {
				continue
			}
			found = true
			if r.sameHash {
				t.Errorf("R15: %s is a same-hash row: it reads in both directions and cannot carry the refusal", row)
			}
		}
		if !found {
			t.Errorf("R15: %s is not in jsVersionRows: its OLD-REFUSES-NEW column is where the retired clamp is layout_newer by name", row)
		}
	}
	u := unitFrom(t, t03Flat)
	src := t02Emit(t, u)
	load := jsFn(src, "TFixedLoad")
	if load == "" {
		t.Fatal("R15: the emitted load function is missing")
	}
	t02Has(t, t02Norm(load), "if (pick < 0) { return TableFixedRefuseHash(report, TableFixedRefusal.LayoutNewer, hashLo, hashHi); }",
		"R15 the retired forward read is layout_newer, on the file's hash alone")
	for _, gone := range []string{"dropAndCount", "remapUnknown", "clampAcrossVersions"} {
		if strings.Contains(load, gone) {
			t.Errorf("R15: the load path still carries the retired forward read %q", gone)
		}
	}
}
