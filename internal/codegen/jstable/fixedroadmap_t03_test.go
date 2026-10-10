package jstable

// fixedroadmap_t03 — the js leg's fixed-closure, retirement and retired-runtime
// rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed
// Tables"), in roadmap order: 7 tasks (2 owed, 2 verify, 3 weak). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T) and PLAN's record bound),
// §5.3 (LOAD's step order and the two layout refusals), §5.5 (the closure),
// §5.6 (what is retired), docs/SPEC-TABLES.md §2.2, §3.4, and
// docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", "The old
// contract's tests, retired by name").
//
// Every assertion reads the JavaScript this leg emits, the ir the compiler
// builds, or the checker's own refusal, so NOTHING here needs the node
// toolchain: the suite RUNS on a bench that never built dist/ and never
// skipped, which is the failure mode this package's own t01/t02 opening notes
// name. One subtest per task this card proves; a clause already held by another
// test of this leg is named there and asserted here too, so a regression is red
// by name in one place.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Flat is the smallest declared fixed table, the fixture the emission
// clauses are read off.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t03Has is a required substring of the emitted source, named by rule.
func t03Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t03Norm collapses a run of whitespace to one space, so a pin reads a
// statement across the emitter's line breaks.
func t03Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t03Files is [Generate]'s files for a unit.
func t03Files(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

// t03LineageFiles is [GenerateLineage]'s files for a unit and a handed lineage.
func t03LineageFiles(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) map[string][]byte {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return files
}

// t03Runtime is the emitted module that carries the unit's shared fixed-form
// runtime, where the layout reader, the plan compiler and the one read loop
// live for a single-file unit.
func t03Runtime(t *testing.T, u *ir.Unit, files map[string][]byte) string {
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

// t03CheckErrs parses and checks one source and answers the checker's errors.
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
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R6: R6 [weak] "a table past §3.4's 65536 ceiling is not a fixed-form root:
// the refusal names the table, no form is emitted, and no lineage entry is
// parsed for it even when the lock carries one" (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #8).
func t03R6(t *testing.T) {
	const src = `package probe
fixed table Small
{
    keep uint32 = 0
}
fixed table Huge
{
    payload bytes(70000)
}
`
	u := unitFrom(t, src)
	huge := findTable(t, u, "Huge")

	// (a) THE CEILING IS A REFUSAL HERE, so the table is not a root and the
	// module has a line naming it.
	n := fixedTypeBytes(huge)
	if n <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past the %d-byte ceiling", n, ir.TableFixedRecordMaxBytes)
	}
	roots := jsFixedUnitRoots(u)
	if len(roots) != 1 || roots[0].Name != "Small" {
		var got []string
		for _, st := range roots {
			got = append(got, st.Name)
		}
		t.Fatalf("R6: a table past the 65536-byte ceiling is NOT a fixed-form root, got %v", got)
	}
	if reason := fixedRefusal(huge); !strings.Contains(reason, "65536-byte ceiling") {
		t.Errorf("R6: the refusal names the ceiling, got %q", reason)
	}

	// (b) NO FORM IS EMITTED, and (c) THE LOCK'S ENTRY FOR IT IS NOT PARSED
	// even though a lock records every fixed table. The walk over the whole
	// body is handed in exactly as lockfile.Lineage hands it.
	w := fixedWalkRoot(huge)
	layout := fixedLayoutBytes(w.entries)
	files := t03LineageFiles(t, u, map[string][]FixedLineageEntry{
		"Small": {mustOwn(t, u, "Small")},
		"Huge":  {{Wire: ir.TableFixedLayoutHash(layout, huge), Layout: layout, Record: fixedHashBytes + n}},
	})
	base64Layout := fixedLayoutBase64(layout)
	sawRefusalLine := false
	for name, body := range files {
		text := string(body)
		for _, bad := range []string{
			"HugeFixedBodyBytes", "HugeFixedRecordBytes", "HugeFixedLayout",
			"HugeFixedKnownLayout", "HugeFixedWriteBody", "HugeFixedDecode", base64Layout,
		} {
			if strings.Contains(text, bad) {
				t.Errorf("R6: %s names %q — a table with no fixed form has no fixed surface and no parsed lineage entry", name, bad)
			}
		}
		sawRefusalLine = sawRefusalLine || strings.Contains(text, "table Huge has NO FIXED FORM in JavaScript")
	}
	if !sawRefusalLine {
		t.Error("R6: NAMED, NEVER SILENT: the generated module says which table has no fixed form and why")
	}
}

// mustOwn is FixedLineageOf with a named failure, so the fixture states which
// table it meant.
func mustOwn(t *testing.T, u *ir.Unit, name string) FixedLineageEntry {
	t.Helper()
	e, ok := FixedLineageOf(u, name)
	if !ok {
		t.Fatalf("no lineage entry for %s", name)
	}
	return e
}

// t03R14: R14 [owed] "layout_record_too_large for an entry reaching past the
// writer's declared record, not only for the 65536 bound and a zero root"
// (docs/FIXED-FORM-ALGORITHM.md §5.2's PLAN: "record := x.root.size — EVERY
// entry is bounded by this ... if an entry reached past `record`: REFUSE
// layout_record_too_large").
//
// The runtime the plan compiler lives in is the assertion's subject and not a
// Go twin: on this leg PLAN is emitted JavaScript, so the bound is read off the
// emitted module (the same shape every sibling leg's t02/t03 R14 takes).
func t03R14(t *testing.T) {
	u := unitFrom(t, t03Flat)
	src := t03Runtime(t, u, t03Files(t, u))

	// (i) THE TWO 65536 SITES AND THE ZERO ROOT keep their own name.
	t03Has(t, src, "TableFixedRecordMaxBytes = 65536", "R14 the ceiling constant")
	t03Has(t, src, "if (size > TableFixedRecordMaxBytes) { TableFixedFail(b, TableFixedRefusal.LayoutRecordTooLarge); return 0; }",
		"R14 a single entry past the 65536 bound")
	t03Has(t, src, "if (sum > TableFixedRecordMaxBytes) { TableFixedFail(b, TableFixedRefusal.LayoutRecordTooLarge); return 0; }",
		"R14 a subtree's sum past the 65536 bound")
	t03Has(t, src, "if (rootSize === 0 || rootSize > TableFixedRecordMaxBytes) {",
		"R14 the zero root and the 65536 bound")

	// (ii) THE CLAUSE THE TASK ADDS: every compiled entry is bounded by the
	// WRITER's own declared root size, and an over-reach refuses the plan WHOLE
	// under layout_record_too_large, never partly compiled and never grown into.
	t03Has(t, src, "function TableFixedPlanPastRecord(plan, record) {",
		"R14 the compiled plan is held to the writer's record")
	t03Has(t, src, "if (TableFixedPlanPastRecord(plan, TableFixedSize(theirs, 0))) { plan.recordTooLarge = true; return -1; }",
		"R14 the writer's own declared record is the bound")
	t03Has(t, src, "TableFixedPlanPastRecord(plan, TableFixedSize(theirs, 0))",
		"R14 every compiled entry is held to the writer's record")
	t03Has(t, src, "this.recordTooLarge = false;",
		"R14 the over-reach is recorded on the plan")
	t03Has(t, src, "if (plan.recordTooLarge) {",
		"R14 an entry past the writer's declared record is refused WHOLE")
	t03Has(t, src, "bad.why = TableFixedRefusal.LayoutRecordTooLarge;",
		"R14 the refusal is named layout_record_too_large")
	// A bigger room cannot put a byte inside the writer's record, so the
	// refusal BREAKS out of the growth loop rather than taking its continue.
	plans := jsFn(src, "TableFixedLineagePlans")
	i := strings.Index(plans, "if (made < 0) {")
	j := strings.Index(plans, "if (plan.recordTooLarge) {")
	if i < 0 || j < i {
		t.Errorf("R14: the record bound is not tested inside the retry's made<0 branch (made at %d, bound at %d)", i, j)
	} else if k := strings.Index(plans[j:], "continue;"); k < 0 {
		t.Error("R14: the retry loop has no continue after the record bound")
	} else if seg := plans[j : j+k]; !strings.Contains(seg, "lane = bad;") || !strings.Contains(seg, "break;") {
		t.Errorf("R14: the record bound falls through to the retry's continue instead of refusing WHOLE:\n%s", seg)
	}
	// The source reaches that feed the bound: a count word, a text length and
	// its units, an ordinal at its own width, an f32, and a plain run.
	for op, want := range map[string]string{
		"count":   "case TableFixedOpCount:\n      return at + 4;",
		"text":    "case TableFixedOpText:\n      return at + 4 + size;",
		"ordinal": "case TableFixedOpOrdinal:",
		"float":   "case TableFixedOpWidenF:\n      return at + 4;",
	} {
		t03Has(t, src, want, "R14 the "+op+" entry's source reach")
	}
}

// t03R22: R22 [owed] "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure" (docs/FIXED-FORM-ALGORITHM.md §5.5,
// docs/SPEC-TABLES.md §2.2).
func t03R22(t *testing.T) {
	fixed := unitFrom(t, `package probe

fixed table Inner { a int32 }
fixed table T { inner Inner }
`)
	// A fixed table reached by value is a closure MEMBER, not a break.
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
			t.Errorf("R22 %s: refusal = %q, want it to name %q and §2.2", tc.name, got, tc.want)
		}
	}

	// T is never in its own closure: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	if errs := t03CheckErrs(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}

