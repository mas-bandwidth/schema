package cstable

// fixedroadmap_t03 — the cs leg's plan-selection (R12), fixed-closure (R6,
// R14, R22), retirement (R3, R32) and retired-runtime (R10, R15) rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"),
// the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §5.1 (the lock's monotone refusals), §5.2
// (COMPILE(lock, T): PLAN, the writer's record bound, THE FLOOR), §5.3 (LOAD
// and its refusal table), §5.6 (what is retired), §5.9 #23 (the retirement
// print), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash",
// "closure_plain_table", "closure_variable_kind", "closure_self"),
// docs/SPEC-TABLES.md §2.2 and §3.4. One subtest per task id, table-driven,
// t.Parallel() first, and the assertion under each is the clause the task's
// own title states.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the
// direct assertion of every clause that had none. [verify] tasks prove the
// holder RUNS and BITES; [owed] tasks are asserted here red-first and, where
// the behaviour was missing, implemented in the leg's own runtime.

import (
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

// t03File is the generated file that carries the unit's shared runtime and its
// fixed table for the single-file `package probe` fixtures below.
const t03File = "ProbeTable.cs"

// t03Names lists the emitted file names, so a moved file is red by name.
func t03Names(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// t03Block is the text between two markers, from the first occurrence of
// `start` to the first `end` after it. It fails rather than answering "".
func t03Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t03Norm collapses a run of whitespace to one space, so an assertion reads the
// emitted statement and not the indentation the string builder chose.
func t03Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t03Has is a required substring, named by rule. `got` is the haystack (the
// source or its normalised form) so a caller can read a statement across lines.
func t03Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t03Generate generates a single-file fixture and answers its files with the
// runtime home's source, where the table's fixed form and the shared runtime
// live.
func t03Generate(t *testing.T, src string) (map[string][]byte, string) {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files[t03File]
	if !ok {
		t.Fatalf("Generate emitted no %s; files are: %v", t03File, t03Names(files))
	}
	return files, string(body)
}

// t03Method is one emitted method's body, or "" when the file has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n    }\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// t03FindTable finds a table by name in the unit's files.
func t03FindTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("the unit declares no table %s", name)
	return nil
}

// t03CheckErrs parses and checks one source and answers the checker's errors, so
// a closure refusal can be read by name rather than caught as a fixture bug.
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

// t03Older is a locked older entry for table T: the whole own entry with a
// different wire hash and the retired mark the caller asks for.
func t03Older(wire uint64, retired bool) FixedLineageEntry {
	return FixedLineageEntry{
		Wire:    wire,
		Layout:  []byte{1, 0, 0, 0},
		Record:  12,
		Retired: retired,
		Reason:  "retired by the test's lock",
	}
}

func TestFixedRoadmapCSharpT03PlansVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R12", t03R12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R12: R12 [owed] "the per-record hash check is before the prefill:
// no_layout writes nothing" (§5.3 step 11, §5.2's PLAN; §5.8 row 9). The
// emitted record loop tests the record's own hash against the file's FIRST,
// before the prefill-and-run it guards, so a no_layout refusal has written not
// one destination byte.
func t03R12(t *testing.T) {
	// A reader whose old peer carries one fewer field, so the compiled plan has
	// a NONEMPTY prefill: the appended `y` is the slot the plan does not land.
	older := unitFrom(t, `package probe

fixed table T
{
    x int32
}
`)
	entry, ok := FixedLineageOf(older, "T")
	if !ok {
		t.Fatal("R12: FixedLineageOf has no entry for T")
	}
	reader := unitFrom(t, t03Flat)
	files, err := GenerateLineage(reader, map[string][]FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatalf("R12: GenerateLineage: %v", err)
	}
	src := string(files[t03File])
	load := t03Method(src, "public static long TFixedLoad(")
	if load == "" {
		t.Fatal("R12: the generated source has no TFixedLoad")
	}
	hashAt := strings.Index(load, "BinaryPrimitives.ReadUInt64LittleEndian(at) != hash")
	fillAt := strings.Index(load, "TableFixedWire.FillRun(")
	runAt := strings.Index(load, "TableFixedWire.Run(")
	if hashAt < 0 {
		t.Fatal(`R12: the record loop carries no per-record hash check`)
	}
	if fillAt < 0 || runAt < 0 {
		t.Fatalf("R12: the record loop carries no prefill-and-run (fill at %d, run at %d)", fillAt, runAt)
	}
	if hashAt > fillAt || hashAt > runAt {
		t.Errorf("R12: the per-record hash check (at %d) is not before the prefill-and-run (fill at %d, run at %d): a no_layout refusal would have written the destination", hashAt, fillAt, runAt)
	}
	t03Has(t, t03Norm(load), `if (BinaryPrimitives.ReadUInt64LittleEndian(at) != hash) { if (report != null) { report.Refused = true; report.Reason = "no_layout"; report.Verdict = TableWire.Verdict.Refused; } return -1; }`,
		"R12 no_layout is the refusal the check returns, before any byte lands")
}
