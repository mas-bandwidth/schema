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
		{"cs/R6", t03R6},
		{"cs/R22", t03R22},
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

// ---- cs/R6 ------------------------------------------------------------------

// t03R6 holds R6 to SPEC-TABLES §3.4 and ALGORITHM §5.9 #48: "65536 BYTES OF
// RECORD BODY: THE FORM IS NOT EMITTED, AND THE TABLE IS NAMED", and "Such a
// table is NOT a fixed-form root, so COMPILE consults no lineage for it and
// parses no entry of it, even when the LOCK carries one". The leg's own root
// selection and ir's agree, the ceiling names the table, and a lineage entry for
// a table with no form is ignored rather than failing the build.
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
	small := u.Tables["Small"]
	huge := u.Tables["Huge"]
	if small == nil || huge == nil {
		t.Fatal("R6: the fixture declares Small and Huge")
	}
	if n := ir.TableFixedTypeBytes(huge); n <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture's body is %d bytes, not past the %d-byte ceiling", n, ir.TableFixedRecordMaxBytes)
	}
	// (1) NOT A FIXED-FORM ROOT: this leg's own answer and ir's agree.
	if ir.TableFixedEmitted(u, huge) {
		t.Error("R6: a table past §3.4's ceiling is not a fixed-form root; the leg still emits the form for Huge")
	}
	if !ir.TableFixedEmitted(u, small) {
		t.Error("R6: the ceiling refuses Huge alone; Small keeps its form")
	}
	var roots []string
	for _, st := range ir.TableFixedFormRoots(u) {
		roots = append(roots, st.Name)
	}
	if len(roots) != 1 || roots[0] != "Small" {
		t.Errorf("R6: ir.TableFixedFormRoots answers %v, want [Small]", roots)
	}
	// (2) THE REFUSAL NAMES THE TABLE, with the ceiling it refuses by.
	warnings, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Errorf("R6: a table past the form's ceiling is a warning and not a compile refusal: %v", errs)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "Huge") || !strings.Contains(joined, "65536") {
		t.Errorf("R6: the warning must name the table and the ceiling: %s", joined)
	}
	// (3) NO FORM IS EMITTED, AND NO LINEAGE ENTRY IS PARSED even when the lock
	// carries one: GENERATE MUST NOT REFUSE IT (#48), and nothing of Huge's
	// fixed surface reaches the file.
	own, ok := FixedLineageOf(u, "Small")
	if !ok {
		t.Fatal("R6: no lineage entry for Small")
	}
	over, ok := FixedLineageOf(u, "Huge")
	if !ok {
		t.Fatal("R6: no lineage entry for Huge")
	}
	text := t03EmitLineage(t, u, map[string][]FixedLineageEntry{
		"Small": {own},
		"Huge":  {over},
	})
	for _, bad := range []string{
		"HugeFixedKnown", "HugeFixedLayout", "HugeFixedRecordBytes", "HugeFixedBodyBytes",
		fmt.Sprintf("0x%016x", over.Wire),
	} {
		if strings.Contains(text, bad) {
			t.Errorf("R6: a table with no fixed form reached the emitted surface: %q", bad)
		}
	}
	t03Has(t, text, "SmallFixedKnown", "R6 the ceiling refuses Huge alone; Small keeps its form")
}

// ---- cs/R22 -----------------------------------------------------------------

// t03R22 holds R22 to ALGORITHM §5.5: "Every table or type in it is declared
// `fixed`; a pointer, map or unbounded array in it is a compile refusal; `T` is
// never in its own closure" — the rows of docs/FIXED-FORM-VERSIONING-TESTS.md
// (closure_plain_table, closure_variable_kind, closure_self), refused through
// the shared compiler this leg generates from.
func t03R22(t *testing.T) {
	rows := []struct {
		name, want, src string
	}{
		// "every table or type reached by value is itself fixed"
		{"a plain table by value", "plain table Child",
			"package probe\n\ntable Child { x int32 }\n\nfixed table T { child Child }\n"},
		{"a plain table in an array", "plain table Ship",
			"package probe\n\ntable Ship { hp int32 }\n\nfixed table T { ships [..4]Ship }\n"},
		// "a pointer, map or unbounded array in the closure is a compile refusal"
		{"a pointer", "is a pointer",
			"package probe\n\ntable Node { x int32 }\n\nfixed table T { head *Node }\n"},
		{"a map", "is a map",
			"package probe\n\nfixed table T { ships map[uint32]int32 }\n"},
		{"an unbounded array", "is an unbounded array",
			"package probe\n\nfixed table T { entries []int32 }\n"},
		// "T is never in its own closure"
		{"itself by value", "T",
			"package probe\n\nfixed table T { seq uint32\n    next T }\n"},
		{"itself in an array", "T",
			"package probe\n\nfixed table T { seq uint32\n    kids [..4]T }\n"},
		{"itself through a type", "T",
			"package probe\n\ntype Link { to T }\n\nfixed table T { seq uint32\n    link Link }\n"},
		{"itself through a pointer", "T",
			"package probe\n\nfixed table T { seq uint32\n    next *T }\n"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, perrs := parser.Parse("Probe.schema", []byte(tc.src))
			if len(perrs) > 0 {
				t.Fatalf("%s: parse: %v", tc.name, perrs[0])
			}
			_, cerrs := check.Unit([]check.SourceFile{{
				Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(tc.src), AST: f,
			}})
			if len(cerrs) == 0 {
				t.Fatalf("%s: the closure break compiled — %q is a compile refusal", tc.name, tc.want)
			}
			var got strings.Builder
			for _, e := range cerrs {
				fmt.Fprintf(&got, "%s", e)
			}
			if !strings.Contains(got.String(), tc.want) {
				t.Errorf("%s: the refusal must name %q: %s", tc.name, tc.want, got.String())
			}
		})
	}

	// THE POSITIVE CONTROL: a closure of fixed things alone compiles, and the
	// leg emits the holder's form — the refusal is the construct's, never the
	// keyword's.
	legal := `package probe

fixed table Inner
{
    x int32
}

fixed table T
{
    child Inner
}
`
	text := t03Emit(t, legal)
	t03Has(t, text, "TFixedKnown", "R22 a closure of fixed things alone keeps its form")
}
