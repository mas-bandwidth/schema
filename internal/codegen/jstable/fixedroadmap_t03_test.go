package jstable

// THE ROADMAP'S FIXED-CLOSURE, RETIREMENT AND RETIRED-RUNTIME TASKS ON THE
// JAVASCRIPT LEG (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed
// Tables"). Law: docs/FIXED-FORM-ALGORITHM.md §1, §5.2, §5.3, §5.5, §5.6,
// docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", "The old
// contract's tests, retired by name"), docs/SPEC-TABLES.md §2.2 and §3.4. One
// subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE JAVASCRIPT THIS LEG EMITS or the shared fixed
// runtime it emits with it, so the harness runs under a bare
// `go test ./internal/codegen/jstable/` with no corpus, no sibling runtime and
// no foreign toolchain — the shape fixedroadmap_t02_test.go states for the same
// reason: a slowtest-gated harness skips every subtest on a default run, and a
// verdict over an assertion that never executed is the failure this package
// already paid for.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03JsRetireReason is the operator mark the retirement subtests hand in.
const t03JsRetireReason = "the operator retired this layout"

// t03JsTable resolves one declared table of a checked unit by name.
func t03JsTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no table %s", name)
	}
	return st
}

// t03JsSource is the emitted runtime home (the unit's shared runtime and the
// fixed surface land together for a single-file unit).
func t03JsSource(t *testing.T, u *ir.Unit, files map[string][]byte) string {
	t.Helper()
	name := runtimeHome(u) + "Table.js"
	body, ok := files[name]
	if !ok {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		t.Fatalf("Generate emitted no %s; files are: %v", name, names)
	}
	return string(body)
}

// t03JsEmit generates a unit with no lineage and answers its runtime home.
func t03JsEmit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t03JsSource(t, u, files)
}

// t03JsLineage generates a unit with the handed lineage and answers its runtime
// home.
func t03JsLineage(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t03JsSource(t, u, files)
}

// t03JsRetire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the entries it was handed.
func t03JsRetire(t *testing.T, retired int) (string, []FixedLineageEntry) {
	t.Helper()
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	middle := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	e1, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the first generation")
	}
	e2, ok := FixedLineageOf(middle, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the second generation")
	}
	if retired > 0 {
		e1.Retired = true
		e1.Reason = t03JsRetireReason
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = t03JsRetireReason
	}
	return t03JsLineage(t, own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}}),
		[]FixedLineageEntry{e1, e2}
}

// t03JsLoad is the emitted body of one `<T>FixedLoad(` up to its closing brace.
func t03JsLoad(t *testing.T, src, name string) string {
	t.Helper()
	body := jsFn(src, name+"FixedLoad")
	if body == "" {
		t.Fatalf("the emitted module has no %sFixedLoad", name)
	}
	return body
}

// t03JsHas is a required substring of the emitted source, named by rule.
func t03JsHas(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03JsHasNot is a banned substring, named by rule.
func t03JsHasNot(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source carries %q", what, bad)
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

// ---- js/R6 ------------------------------------------------------------------

// t03JsR6 holds R6 to SPEC-TABLES §3.4 and ALGORITHM §5.2: a table whose record
// body is past the 65536-byte ceiling has NO FORM EMITTED, is NAMED by the
// compiler (a warning, not a refusal of the unit — it keeps form 1), and no
// lineage entry handed in for it is parsed even when the lock carries one.
func t03JsR6(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Small
{
    keep uint32
}

fixed table Big
{
    blob bytes(70000)
}
`)
	big := t03JsTable(t, u, "Big")
	if got := ir.TableFixedTypeBytes(big); got <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past the %d ceiling", got, ir.TableFixedRecordMaxBytes)
	}
	if fixedRefusal(big) == "" {
		t.Error("R6: a table past the ceiling is not refused BY NAME")
	}
	roots := jsFixedUnitRoots(u)
	if len(roots) != 1 || roots[0].Name != "Small" {
		var got []string
		for _, st := range roots {
			got = append(got, st.Name)
		}
		t.Errorf("R6: a table past the ceiling is a fixed-form root; roots = %v", got)
	}
	if ir.TableFixedEmitted(u, big) {
		t.Error("R6: ir.TableFixedEmitted keeps a table past the ceiling")
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: TableFixedFormRoots keeps a table past the ceiling")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: past the ceiling is a warning, not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Big") || !strings.Contains(joined, "ceiling") {
		t.Errorf("R6: the refusal does not name the table and the ceiling: %s", joined)
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Big": {{Wire: 0x1122334455667788, Layout: []byte{1, 2, 3, 4}, Record: 70008}},
	})
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	emitted := false
	for name, body := range files {
		text := string(body)
		if strings.Contains(text, "SmallFixedHashLo") {
			emitted = true
		}
		if strings.Contains(text, "BigFixed") {
			t.Errorf("R6: %s emits a form for a table past the ceiling", name)
		}
		if strings.Contains(text, "0x1122334455667788") {
			t.Errorf("R6: %s parsed the lock's lineage entry for a table past the ceiling", name)
		}
	}
	if !emitted {
		t.Fatal("R6: the positive control Small emitted no form, so the assertion is vacuous")
	}
}

// ---- js/R14 -----------------------------------------------------------------

// t03JsR14 holds R14 to ALGORITHM §5.2's PLAN: "record := x.root.size -- EVERY
// entry is bounded by this"; "if an entry reached past `record`: REFUSE
// layout_record_too_large". The runtime names layout_record_too_large for the
// zero root and the 65536 bound, and — the clause the task adds — for a single
// entry whose source extent reaches past the writer's OWN declared record size,
// refusing the plan WHOLE and never landing a short read.
func t03JsR14(t *testing.T) {
	runtime := fixedRuntime
	t03JsHas(t, runtime, "if (rootSize === 0 || rootSize > TableFixedRecordMaxBytes) {", "R14 the zero root and the 65536 bound")
	t03JsHas(t, runtime, "out.refusal = TableFixedRefusal.LayoutRecordTooLarge;", "R14 the reader's own layout names the rule")
	t03JsHas(t, runtime, "if (size > TableFixedRecordMaxBytes) { TableFixedFail(b, TableFixedRefusal.LayoutRecordTooLarge); return 0; }", "R14 a single entry past the 65536 bound")
	t03JsHas(t, runtime, "if (sum > TableFixedRecordMaxBytes) { TableFixedFail(b, TableFixedRefusal.LayoutRecordTooLarge); return 0; }", "R14 a subtree's sum past the 65536 bound")
	t03JsHas(t, runtime, "function TableFixedSrcEnd(", "R14 the entry's source extent is a function of the plan entry")
	t03JsHas(t, runtime, "if (plan.record !== 0 && TableFixedSrcEnd(op, src, size, aux, guard, plan.argw) > plan.record) {", "R14 an entry reaching past the writer's declared record")
	t03JsHas(t, runtime, "plan.tooLarge = true;", "R14 the over-reach is recorded")
	t03JsHas(t, runtime, "if (plan.tooLarge) { return -3; }", "R14 the compile answers layout_record_too_large")
	t03JsHas(t, runtime, "plan.record = TableFixedSize(theirs, 0);", "R14 the writer's record is the bound")
	t03JsHas(t, runtime, "plan.why = TableFixedRefusal.LayoutRecordTooLarge;", "R14 the lane carries the name")
	// The generated module carries the runtime, so the ban is on the emitted
	// build and not on this package's constant alone.
	t03JsHas(t, t03JsEmit(t, unitFrom(t, "package probe\n\nfixed table T\n{\n    x int32\n    y int32\n}\n")), "TableFixedSrcEnd", "R14 the emitted build carries the entry bound")
}

// ---- js/R22 -----------------------------------------------------------------

// t03JsR22 holds R22 to ALGORITHM §5.5 and SPEC-TABLES §2.2: a table or type
// reached by value from a fixed table is itself fixed (a nested FIXED table is a
// closure member, not a break), a pointer, a map or an unbounded array in that
// closure is a compile refusal naming the construct, and a table that reaches
// itself by value — a table in its own closure — is refused.
func t03JsR22(t *testing.T) {
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
		errs := t03JsCheckErrs(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and §2.2", tc.name, got, tc.want)
		}
	}

	// T is never in its own closure: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	if errs := t03JsCheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}

	// AND THE FORM'S OWN CLOSURE TEST agrees with the checker for the constructs
	// a variable table may hold: none is a closure this form lays out, so none
	// gets a form-3 emitter.
	for _, tc := range []struct{ src, table string }{
		{"package probe\ntable Node { x int32 }\ntable Scene { head *Node }\n", "Scene"},
		{"package probe\ntable Fleet { ships map[uint32]int32 }\n", "Fleet"},
		{"package probe\ntable Log { entries []int32 }\n", "Log"},
	} {
		u := unitFrom(t, tc.src)
		if ir.TableFixedSupported(t03JsTable(t, u, tc.table), 0) {
			t.Errorf("R22: TableFixedSupported admits %s, whose closure holds a pointer, a map or an unbounded array", tc.table)
		}
	}
}

// ---- js/R3 ------------------------------------------------------------------

// t03JsR3 holds R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The arithmetic and the emitted site are read here.
func t03JsR3(t *testing.T) {
	for _, tc := range []struct{ retire, want int }{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		text, _ := t03JsRetire(t, tc.retire)
		t03JsHas(t, text, fmt.Sprintf("export const LineageFixedFloor = %d;", tc.want), "R3 1 + the highest retired index")
		load := t03JsLoad(t, text, "Lineage")
		t03JsHas(t, load, "if (pick < LineageFixedFloor) {", "R3 below the floor is a comparison of the index")
		t03JsHas(t, load, "return TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported, hashLo, hashHi);", "R3 below the floor is layout_unsupported")
	}
}

// ---- js/R32 -----------------------------------------------------------------

// t03JsR32 holds R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry (`// RETIRED:`), the floor check is
// placed ONCE, after the hash select and BEFORE the byte comparison or any
// record, and its whole body is the named refusal — a pure read of the picked
// index, so a second load of the same file answers the same name. REFUSE is
// TOTAL: the runtime's reset zeroes every counter.
func t03JsR32(t *testing.T) {
	text, entries := t03JsRetire(t, 1)
	t03JsHas(t, text, "// RETIRED: "+t03JsRetireReason, "R32 the retired entry's mark is laid down")
	t03JsHas(t, text, fmt.Sprintf("0x%08x, 0x%08x", uint32(entries[0].Wire), uint32(entries[0].Wire>>32)), "R32 the retired entry keeps its hash in the lineage")
	load := t03JsLoad(t, text, "Lineage")
	if n := strings.Count(load, "if (pick < LineageFixedFloor) {"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "if (pick < LineageFixedFloor) {")
	known := strings.Index(load, "const known = LineageFixedKnown[pick];")
	bytes := strings.Index(load, "layoutBytes !== known.layoutBytes")
	records := strings.Index(load, "for (let k = 0; k < n; k++)")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if !(at < known && known < bytes && bytes < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads the picked index and returns
	// the named refusal; nothing before it moves a counter or writes a byte.
	line := load[at:]
	if end := strings.Index(line, "\n"); end >= 0 {
		line = line[:end]
	}
	for _, banned := range []string{"report.unknown", "report.widened", "values[", "copy("} {
		if strings.Contains(line, banned) {
			t.Errorf("R32: the floor refusal is not a pure named refusal (%q):\n%s", banned, line)
		}
	}
	t03JsHas(t, line, "TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported, hashLo, hashHi)", "R32 the guarded body is the named refusal")
	for _, zero := range []string{"report.malformed = false;", "report.refused = 0;", "report.unknown = 0;", "report.kindMismatch = 0;", "report.clamped = 0;", "report.widened = 0;", "report.duplicate = 0;", "report.layoutHash = 0n;"} {
		t03JsHas(t, fixedRuntime, zero, "R32 REFUSE is TOTAL")
	}
	t03JsHas(t, fixedRuntime, "report.refused = refusal;", "R32 the refusal names itself")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapJsT03PlansVersions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "js/R6", check: t03JsR6},
		{id: "js/R14", check: t03JsR14},
		{id: "js/R22", check: t03JsR22},
		{id: "js/R3", check: t03JsR3},
		{id: "js/R32", check: t03JsR32},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
