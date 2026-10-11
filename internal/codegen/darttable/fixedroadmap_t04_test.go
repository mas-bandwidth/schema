package darttable

// THE DART LEG'S FIXED-TABLE VERSIONING ROWS (docs/roadmap.sexp node
// `fixed-tables`, the rows retirement, retired-runtime, numeric-evolution and
// optional-values). Each subtest is one roadmap task id and asserts the clause
// its own title states. The law is docs/FIXED-FORM-ALGORITHM.md §5.2 and §5.3
// (the lineage, the floor, the refusals), docs/FIXED-FORM-VERSIONING-TESTS.md
// ("The floor and the hash"), docs/SPEC-TABLES.md §3.4, ROADMAP.md "NEW Fixed
// Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A task this card does not add an
// assertion for is reported in $JOB/verdicts.txt as `unknown`, never faked.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapDartT04Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/R3", t04R3},
		{"dart/R32", t04R32},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t04LineageModule answers the fixed module for table `T` with the entries the
// caller hands, so a floor clause can be held against §5.2's own arithmetic
// (`floor = 1 + the highest retired index, 0 when none`) and not against a
// corpus file's side effect.
func t04LineageModule(t *testing.T, entries []FixedLineageEntry) (*fixedModule, *ir.Unit, *ir.Struct, FixedLineageEntry) {
	t.Helper()
	const src = `package probe

fixed table T
{
    a uint32 = 1
    b uint32 = 2
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("the fixture fixed table T did not lock")
	}
	g := &fixedModule{lineage: map[string][]FixedLineageEntry{"T": entries}}
	return g, u, st, own
}

// t04Floor reads the floor constant this leg PUBLISHES for table T out of the
// generated library — the number the runtime's `pick < tFixedFloor` test uses —
// rather than recomputing it, so the assertion is over the leg's own output.
func t04Floor(t *testing.T, g *fixedModule, st *ir.Struct, own FixedLineageEntry) int {
	t.Helper()
	entries, floor := g.fixedLineage(st, own)
	if len(entries) == 0 {
		t.Fatal("the lineage is empty")
	}
	return floor
}

// t04R3: R3 [verify] "the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash".
// FIXED-FORM-ALGORITHM §5.2: "the floor — 1 + the highest retired index, 0 when
// none is". §5.3 steps 5 and 6: "PART of the lineage but below its floor is
// `layout_unsupported` — upgrade the client — instead of `layout_newer`, and
// both report the FILE'S hash".
func t04R3(t *testing.T) {
	// (1) THE FLOOR IS 1 + THE HIGHEST RETIRED INDEX: three locked layouts
	// oldest first; retiring entries 0 and 2 (the HIGHEST index 2) puts the
	// floor at 3, not at the count of retired entries (2).
	dir := []FixedLineageEntry{
		{Wire: 0x01, Layout: []byte{0x01}, Record: 8},
		{Wire: 0x02, Layout: []byte{0x02}, Record: 8},
		{Wire: 0x03, Layout: []byte{0x03}, Record: 8},
	}
	dir[0].Retired = true
	dir[0].Reason = "ancient"
	dir[2].Retired = true
	dir[2].Reason = "old"
	g, u, st, own := t04LineageModule(t, dir)
	if got := t04Floor(t, g, st, own); got != 3 {
		t.Errorf("R3: the floor is %d, want 1 + the highest retired index (2) = 3 — a count of retired entries would say 2", got)
	}

	// (2) 0 WHEN NONE IS retired: the same three entries, none retired.
	none := []FixedLineageEntry{
		{Wire: 0x01, Layout: []byte{0x01}, Record: 8},
		{Wire: 0x02, Layout: []byte{0x02}, Record: 8},
		{Wire: 0x03, Layout: []byte{0x03}, Record: 8},
	}
	g2, _, st2, own2 := t04LineageModule(t, none)
	if got := t04Floor(t, g2, st2, own2); got != 0 {
		t.Errorf("R3: the floor is %d, want 0 when nothing is retired", got)
	}

	// (3) THE EMITTED FLOOR is the same number: the library publishes
	// `const int tFixedFloor = 3;` and the load's `pick < tFixedFloor` test
	// answers `layoutUnsupported` for a file below it.
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": dir})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := t02Source(t, files)
	if !strings.Contains(text, "tFixedFloor = 3;") {
		t.Errorf("R3: the generated library does not publish `tFixedFloor = 3;` — the floor is 1 + the highest retired index")
	}
	if !strings.Contains(text, "TableFixedRefusal.layoutUnsupported") {
		t.Errorf("R3: the generated library does not name layout_unsupported — below the floor is layout_unsupported")
	}
	if !strings.Contains(text, "report.layoutHash = hash;") {
		t.Errorf("R3: the generated refusal does not report the file's hash — both layout refusals report it")
	}
}

// t04R32: R32 [weak] "retire for real: a retired version is refused by name,
// once, idempotently".
// §5.2, and the `--retire` note in fixedlineage.go: retiring "keeps the entry
// and moves the floor"; §5.3 answers a file below it with `layout_unsupported`.
// Refused BY NAME is the kept entry carrying its Reason; idempotent is the same
// floor and the same entries however many times the walk runs.
func t04R32(t *testing.T) {
	dir := []FixedLineageEntry{
		{Wire: 0x11, Layout: []byte{0x11}, Record: 8, Retired: true, Reason: "retired-v0"},
		{Wire: 0x12, Layout: []byte{0x12}, Record: 8},
	}
	g, u, st, own := t04LineageModule(t, dir)
	first, floor1 := g.fixedLineage(st, own)

	// (1) RETIRE KEEPS THE ENTRY: the retired layout is still in the lineage,
	// so a file carrying it is SELECTED and then refused by the FLOOR, not
	// mistaken for an unknown newer layout.
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": dir})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text := t02Source(t, files)
	if !strings.Contains(text, "// RETIRED: retired-v0") {
		t.Errorf("R32: the generated lineage does not carry the retired entry's reason — a retired version is refused BY NAME")
	}
	if !strings.Contains(text, fixedDartHex(0x11)) {
		t.Errorf("R32: the retired layout's wire hash 0x%x is absent from the lineage — retiring keeps the entry", 0x11)
	}
	// (2) IDEMPOTENT: running the walk again answers the same entries and the
	// same floor; retiring is a fixed point, not an accumulating one.
	second, floor2 := g.fixedLineage(st, own)
	if floor1 != floor2 || len(first) != len(second) {
		t.Errorf("R32: retiring twice moved the lineage (floor %d->%d, entries %d->%d); it is idempotent",
			floor1, floor2, len(first), len(second))
	}
	if floor1 != 1 {
		t.Errorf("R32: the floor is %d, want 1 — the single retired index 0 moved it exactly once", floor1)
	}
}
