package darttable

// THE DART LEG'S FIXED-TABLE FIELD-EVOLUTION, COUNTER AND TEXT ROWS
// (docs/roadmap.sexp node `fixed-tables`, work-sets `field-evolution/dart`,
// `reports/dart` and `text/dart`). Each subtest is one roadmap task id and
// asserts the clause its own title states, quoting the sentence it implements.
// One subtest per task, table-driven, t.Parallel() first.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §4.5 (the text content rule, fix 11),
// §5.2/§5.4 (the counters), §5.8 row 8 and §5.9; docs/SPEC-TABLES.md §3.4
// (the fixed form's text kinds and their lengths); ROADMAP.md "NEW Fixed
// Tables"; docs/FIXED-FORM-VERSIONING-TESTS.md.
//
// THE HARNESS READS WHAT THIS LEG EMITS and the shared runtime body it emits
// it with, so it is reachable on a tree that never built the C++ reference
// corpus and never carries a sibling runtime checkout: the compile-and-run
// half of this leg's fixed form is fixedversioning_test.go, which skips
// exactly there. A clause an existing test of this leg already holds at a
// reachable gate is named by that test in the card's verdicts and not repeated
// here.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t05Runtime is this leg's shared runtime body: the module every fixed surface
// reaches for its counters, its plan compiler and its text rules.
func t05Runtime() string { return fixedRuntime }

// t05Has is a required substring of the emitted source, named by rule.
func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t05No is a substring the emitted source must NOT carry, named by rule.
func t05No(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source still carries %q", what, bad)
	}
}

// t05Emitted is every file the emitter wrote for one unit, in name order.
func t05Emitted(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t02Source(t, files)
}

// t05Between is the text between two markers: the first `end` after `start`.
// It fails rather than answering "" so a moved section is red by name.
func t05Between(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the emitted source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the emitted source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t05OpCase is the body of one `case TableFixedOp.<name>:` in the read loop,
// from the label to the next case or the loop's default. It fails rather than
// answering "" so a moved case is red by name.
func t05OpCase(t *testing.T, src, name string) string {
	t.Helper()
	label := "      case TableFixedOp." + name + ":"
	i := strings.Index(src, label)
	if i < 0 {
		t.Fatalf("the read loop has no %s", label)
	}
	rest := src[i+len(label):]
	j := strings.Index(rest, "      case TableFixedOp.")
	if k := strings.Index(rest, "      default:"); j < 0 || (k >= 0 && k < j) {
		j = k
	}
	if j < 0 {
		t.Fatalf("the %s case has no end", name)
	}
	return rest[:j]
}

// ---- dart/E6 ----------------------------------------------------------------

// t05E6 holds dart/E6: "Renaming uses the declared identity". SPEC-TABLES §5:
// a field's table-wire id is the name it was, so `b int32 was = "a"` walks as
// the id of `a` and its layout bytes are byte for byte a plain `a int32`'s. A
// bare `b int32` has a different id, which is the control that makes the
// assertion bite.
func t05E6(t *testing.T) {
	const renamed = `package probe

fixed table T
{
    b int32 | was = "a"
}
`
	const plain = `package probe

fixed table T
{
    a int32
}
`
	const bare = `package probe

fixed table T
{
    b int32
}
`
	f := findTable(t, unitFrom(t, renamed), "T").Fields[0]
	if f.Name != "b" {
		t.Fatalf("E6: the fixture's field is named %q, want b", f.Name)
	}
	if got := ir.TableFieldWireName(f); got != "a" {
		t.Fatalf("E6: the renamed field's declared identity = %q, want a", got)
	}
	renamedWalk := fixedWalkRoot(findTable(t, unitFrom(t, renamed), "T"))
	plainWalk := fixedWalkRoot(findTable(t, unitFrom(t, plain), "T"))
	if got, want := string(fixedLayoutBytes(renamedWalk.entries)), string(fixedLayoutBytes(plainWalk.entries)); got != want {
		t.Errorf("E6: the rename moved the layout bytes:\n renamed % x\n was     % x", got, want)
	}
	renamedHash, _, _ := t03Emitted(t, renamed)
	plainHash, _, _ := t03Emitted(t, plain)
	if renamedHash != plainHash {
		t.Errorf("E6: the emitted hash moved on a rename: %#x, want the old name's %#x", renamedHash, plainHash)
	}
	// THE CONTROL: a field that really is `b` has `b`'s id, so it is a different
	// layout and a different hash. Without this the assertion above would pass
	// on an emitter that ignored every field name.
	bareHash, _, _ := t03Emitted(t, bare)
	if bareHash == renamedHash {
		t.Error("E6: a bare `b` published the renamed field's hash, so the fixture cannot tell the declared identity from the new name")
	}
}

// ---- dart/E8 ----------------------------------------------------------------

// t05E8 holds dart/E8: "Append and deprecate under the backward-read
// contract". A `| deprecated` field is RETIRED IN PLACE — its slot stays where
// it is and a reader reads what it finds there — and an APPENDED field lands
// its declared default, because a field the writer does not carry has no plan
// entry at all and the prefill answers for it (docs/FIXED-FORM-ALGORITHM.md
// §5.2 COMPILE, the fill list).
func t05E8(t *testing.T) {
	const dep = `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`
	const plain = `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`
	depTable := findTable(t, unitFrom(t, dep), "T")
	if len(depTable.Fields) != 3 {
		t.Fatalf("E8: the deprecated field's slot did not stay: %d fields", len(depTable.Fields))
	}
	if !depTable.Fields[1].Deprecated {
		t.Error("E8: the `| deprecated` marker is not on the IR field")
	}
	if got, want := len(fixedWalkRoot(depTable).entries), len(fixedWalkRoot(findTable(t, unitFrom(t, plain), "T")).entries); got != want {
		t.Errorf("E8: the deprecated field's slot did not stay: %d walked entries, want the plain table's %d", got, want)
	}
	// THE DEPRECATED FIELD IS STILL A PLAN ENTRY: the identity walk copies its
	// slot, so a reader reads what the writer left there and nothing skips it.
	// Field b's four bytes start at offset 4 and the plan lands them.
	covered := false
	for _, e := range fixedIdentityPlan(depTable) {
		if e.dst <= 4 && e.dst+e.size > 4 {
			covered = true
		}
	}
	if !covered {
		t.Error("E8: the deprecated field got no plan entry, so it was skipped rather than read in place")
	}

	// THE APPEND LANDS ITS DECLARED DEFAULT in the prefill, and the load
	// copies the prefill into exactly the ranges the plan does not land.
	const appended = `package probe

fixed table T
{
    x int32 = 7
    y int32 = 9
}
`
	img := fixedPrefillBytes(findTable(t, unitFrom(t, appended), "T"))
	if got := int32(img[0]) | int32(img[1])<<8 | int32(img[2])<<16 | int32(img[3])<<24; got != 7 {
		t.Errorf("E8: the appended x = %d, want its declared default 7", got)
	}
	t05Has(t, t05Runtime(), "tableFixedFillRun(", "E8 the read fills the plan's holes from the prefill")
}

// ---- dart/R19 ---------------------------------------------------------------

// t05R19 holds dart/R19: "a clean NEW-READS-OLD of an appended field, variant,
// arm, flag or keyed slot moves no counter at all". The report starts at zero;
// the COMPILE census is the only `unknown` bump and it fires once per writer
// field no reader field names, so a lawful lineage (every field named) adds
// zero; and the widen/clamp bumps live only inside the ops a moved kind or an
// out-of-range length earns, never on a clean copy.
func t05R19(t *testing.T) {
	src := t05Emitted(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	t05Has(t, src, "report.reset();", "R19 the report starts at zero")
	t05Has(t, t05Runtime(), "if (!named && census) {\n        report.unknown++;\n      }",
		"R19 unknown counts an UNNAMED writer field, never a record")
	// THE CENSUS LANDS ONCE, AFTER THE LOOP, and comes from the plan's own
	// numbers: a lawful lineage's plan names every field, so it is zero.
	t05Has(t, src, "report.unknown += censusUnknown;", "R19 the census lands once per peer")
	t05Has(t, src, "report.kindMismatch += censusKind;", "R19 kind_mismatch lands once per peer")
	if n := strings.Count(src, "report.unknown += censusUnknown;"); n != 1 {
		t.Errorf("R19: the census lands %d times, want once per peer", n)
	}
	// WIDENED MOVES ONLY WHERE A KIND GREW, AND CLAMPED ONLY WHERE A LENGTH OR
	// BOUND WAS OUT OF RANGE — the ops, not the loop.
	for _, name := range []string{"widen", "widenFloat"} {
		if c := t05OpCase(t, t05Runtime(), name); !strings.Contains(c, "report.widened++;") {
			t.Errorf("R19/R16: the %s op does not move `widened` once for its entry", name)
		}
	}
	for _, name := range []string{"count", "text"} {
		if c := t05OpCase(t, t05Runtime(), name); !strings.Contains(c, "report.clamped++;") {
			t.Errorf("R19/R16: the %s op does not move `clamped` once for its entry", name)
		}
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapDartT05Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "dart/E6", check: t05E6},
		{id: "dart/E8", check: t05E8},
		{id: "dart/R19", check: t05R19},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
