package cstable

// fixedroadmap_t03 — the cs leg's plan-selection, fixed-closure, retirement and
// retired-runtime rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the assertions no other test of this leg makes.
//
// ONE SUBTEST PER TASK ID, table-driven, t.Parallel() first. Every assertion
// reads the C# this leg emits, the bytes its own walk lays down, or the shared
// compiler's refusal — so the harness is reachable on a tree that never built
// the C++ reference corpus, never carries a sibling serialize.cs and never runs
// dotnet. A clause that needs the emitted code to RUN is asserted at the emitted
// site that runs it, and that site is named in the check; the shape
// fixedroadmap_t02_test.go states for the same reason.
//
// The law per task: docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T), the
// floor, the plan bound) and §5.3 (LOAD, the refusal table), §5.5 (the closure),
// §5.6 (what is retired), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and
// the hash", "The old contract's tests, retired by name"), SPEC-TABLES §3.4.
//
// cs/R12 [owed]: "the per-record hash check is before the prefill: no_layout
// writes nothing" (ALGORITHM §5.3, VERSIONING-TESTS §5.8 row 9).
//
// cs/R6 [owed]: "a table past §3.4's 65536 ceiling is not a fixed-form root: the
// refusal names the table, no form is emitted, and no lineage entry is parsed
// for it even when the lock carries one" (SPEC-TABLES §3.4, ALGORITHM §5.9 #48).
//
// cs/R22 [owed]: "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure" (ALGORITHM §5.5, SPEC-TABLES §2.2).
//
// cs/R3 [verify]: "the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash" (§5.2, §5.3).
//
// cs/R32 [verify]: "retire for real: a retired version is refused by name, once,
// idempotently" (§5.2, §5.3).
//
// cs/R10 [weak]: "the run-time walk of a stranger's layout and the recompute of
// the header's hash are retired" (§5.3 steps 4 and 7, §5.6).
//
// cs/R15 [weak]: "the four forward-read clamps are retired — count clamp across
// bounds, range clamp across versions, remap of an unknown variant to None,
// drop-and-count of an unknown field: each is layout_newer now" (§5.3 step 5,
// §5.6).
//
// cs/R14 is reported `unknown` in the card's verdicts: the emitted runtime
// carries layout_record_too_large for the 65536 bound and a zero root but has no
// writer's-declared-record bound in its plan compiler (§5.2 PLAN), and adding it
// changes the shared runtime text, which moves the pinned generated/ tree
// outside this card's PATHS (docs/roadmap-evidence/fixed-cs.sexp records the
// same conflict for cs/R7). It is a proposed diff in the card's report, not an
// assertion made to pass here.

import (
	"strings"
	"testing"

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

// t03Emit generates a single-file `package probe` fixture and answers its one
// table file, where the fixed form and the shared runtime live.
func t03Emit(t *testing.T, src string) string {
	t.Helper()
	files := generateFiles(t, "Probe", src)
	return t03File(t, files)
}

// t03EmitLineage is t03Emit with the lock's lineage handed in, exactly as
// fixedversioning_test.go's probe harness hands it (ALGORITHM §5.2, §5.9 #1).
func t03EmitLineage(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t03File(t, files)
}

// t03File answers the one table file of a generated map, named so a moved file
// is red by name rather than an empty string.
func t03File(t *testing.T, files map[string][]byte) string {
	t.Helper()
	body, ok := files["ProbeTable.cs"]
	if !ok {
		t.Fatalf("the unit's fixed surface is not in ProbeTable.cs; files are %v", t02Names(files))
	}
	return string(body)
}

// t03Has is a required substring of the emitted source, named by rule.
func t03Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03LineageUnits builds THREE generations of one table — x, x+y, x+y+z — and
// answers the current unit with the first two as its lineage, oldest first.
func t03LineageUnits(t *testing.T) (*ir.Unit, []FixedLineageEntry) {
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
	return own, []FixedLineageEntry{e1, e2}
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

// ---- cs/R12 -----------------------------------------------------------------

// t03R12 holds R12 to §5.3's record loop and to §5.8 row 9: the per-record hash
// check stands BEFORE the prefill, so a no_layout refusal has written not one
// destination byte. The emitted load is read for the order — the hash compare,
// then the fill, then the plan run — and for the refusal's name.
func t03R12(t *testing.T) {
	src := t03Emit(t, t03Flat)
	load := csFn(src, "public static long TFixedLoad(")
	if load == "" {
		t.Fatal("R12: TFixedLoad is not emitted")
	}
	hash := strings.Index(load, "BinaryPrimitives.ReadUInt64LittleEndian(at) != hash")
	fill := strings.Index(load, "TableFixedWire.FillRun(")
	run := strings.Index(load, "TableFixedWire.Run(")
	if hash < 0 || fill < 0 || run < 0 {
		t.Fatalf("R12: the record loop's shape moved (hash=%d fill=%d run=%d)", hash, fill, run)
	}
	if !(hash < fill && fill < run) {
		t.Errorf("R12: the per-record hash check is not before the prefill and the plan run (%d, %d, %d); a no_layout refusal would have written the destination", hash, fill, run)
	}
	t03Has(t, load, `report.Reason = "no_layout"`, "R12 the refusal is no_layout by name")
}
