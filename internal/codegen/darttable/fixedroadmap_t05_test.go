package darttable

// THE DART LEG'S FIXED-TABLE FIELD-EVOLUTION, REPORT AND TEXT ROWS
// (docs/roadmap.sexp node `fixed-tables`, rows field-evolution, reports and
// text). Each subtest is one roadmap task id and asserts the clause its own
// title states against THIS leg's own output — the layout walk of
// internal/codegen/darttable/fixedform.go, the identity plan of
// internal/codegen/darttable/fixedplan.go and the generated runtime of
// internal/codegen/darttable/fixedruntime.go.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §5 (the counters and the backward-read
// contract), docs/FIXED-FORM-VERSIONING-TESTS.md, docs/SPEC-TABLES.md §3.4
// ("the values in declared order, every field at its DECLARED STORAGE WIDTH")
// and §5 (the wire identity a rename keeps through `was`).
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A task this card cannot settle is
// reported `unknown` in the verdicts, never decided here.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapDartT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/E6", t05E6},
		{"dart/E8", t05E8},
		{"dart/E9", t05E9},
		{"dart/C3", t05C3},
		{"dart/C5", t05C5},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05Layout is the layout bytes and the published hash of one `fixed table`
// fixture, read out of THIS leg's own walk so an assertion is over the leg's
// output and not over a hand-spelled constant.
func t05Layout(t *testing.T, src string) (*ir.Struct, []byte, uint64) {
	t.Helper()
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	return st, layout, ir.TableFixedLayoutHash(layout, st)
}

// t05E6: E6 "Renaming uses the declared identity". SPEC-TABLES §5: "A field's
// wire id ... is the hash of its `was = \"old_name\"` alias when one is
// declared — so wire identity survives the rename — and of its own name
// otherwise", and the fixed layout entry for a field rides under that same id
// (internal/codegen/darttable/fixedform.go, fixedWalkField: `id :=
// ir.TableFieldWireId(f)`). A rename that carries `was` therefore moves no
// layout byte; a rename without `was` moves the id and is a different layout.
func t05E6(t *testing.T) {
	renamed := `package probe

fixed table T
{
    b   int32 = 0 | was = "a"
    seq int32 = 0
}
`
	u := unitFrom(t, renamed)
	st := findTable(t, u, "T")
	f := st.Fields[0]
	if f.Name != "b" {
		t.Fatalf("E6: the fixture's first field is %q, not the renamed b", f.Name)
	}
	// (1) THE DECLARED IDENTITY IS THE `was` NAME: the wire id is the hash of
	// `a`, never of the new spelling `b`.
	got := ir.TableFieldWireId(f)
	if want := ir.TableWireId("a"); got != want {
		t.Errorf("E6: the renamed field's wire id is 0x%016x, want the declared identity 0x%016x (`was = \\\"a\\\"`)", got, want)
	}
	if got == ir.TableWireId("b") {
		t.Errorf("E6: the wire id is the hash of the NEW spelling b — a rename must use the declared identity `was`")
	}
	// (2) THE LAYOUT ENTRY RIDES UNDER THAT IDENTICAL ID, so the two layouts are
	// byte-identical and a rename through `was` is no version at all.
	w := fixedWalkRoot(st)
	var entryID uint64
	found := false
	for _, e := range w.entries {
		if e.note == "b" {
			entryID, found = e.id, true
		}
	}
	if !found {
		t.Fatal("E6: the layout carries no entry for the renamed field b")
	}
	if entryID != got {
		t.Errorf("E6: the layout entry id is 0x%016x, want the field's declared identity 0x%016x", entryID, got)
	}
	if entryID == ir.TableWireId("b") {
		t.Errorf("E6: the layout entry names the new spelling, so a rename would move every file")
	}
}

// t05E8: E8 "Append and deprecate under the backward-read contract". An APPEND
// is ONE definition change that grows the layout and moves the hash — a NEW
// reader lands the appended field's declared default and an OLD reader refuses
// by hash (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3). A `| deprecated` field is
// RETIRED IN PLACE (docs/SPEC-TABLES.md §2.10): its slot stays exactly where it
// is, so it moves no layout byte and no hash and is not a version.
func t05E8(t *testing.T) {
	base := `package probe

fixed table T
{
    a int32 = 0
    b int32 = 0
}
`
	appended := `package probe

fixed table T
{
    a int32 = 0
    b int32 = 0
    c int32 = 7
}
`
	_, lOld, hOld := t05Layout(t, base)
	_, lNew, hNew := t05Layout(t, appended)
	// (1) THE APPEND GREW THE LAYOUT BY ONE ENTRY AND MOVED THE HASH: it is a
	// version, and the OLD reader meets the NEW writer's hash with no lineage
	// entry and refuses `layout_newer` (§5.3).
	if len(lNew) <= len(lOld) {
		t.Errorf("E8: the appended field did not grow the layout (%d -> %d bytes)", len(lOld), len(lNew))
	}
	if hNew == hOld {
		t.Errorf("E8: appending a field left the published hash at 0x%016x — an append is a version", hOld)
	}

	deprecated := `package probe

fixed table T
{
    a int32 = 0
    // RETIRED IN PLACE (SPEC-TABLES §2.10): the slot stays exactly where it is
    b int32 = 0 | deprecated
}
`
	_, lDep, hDep := t05Layout(t, deprecated)
	// (2) THE RETIREMENT MOVES NOTHING: the slot stays, so the layout bytes and
	// the hash are the plain side's own — a deprecate is no version.
	if string(lDep) != string(lOld) {
		t.Errorf("E8: `| deprecated` moved the layout bytes, want the plain side's byte-identical layout")
	}
	if hDep != hOld {
		t.Errorf("E8: `| deprecated` moved the hash 0x%016x -> 0x%016x, want no version at all", hOld, hDep)
	}
}

// t05E9: E9 "duplicate never raised". The fixed form's report carries a
// `duplicate` counter (docs/FIXED-FORM-ALGORITHM.md §5.4) and NO path of form 3
// raises it: a fixed record's slots are positional, so a duplicate key cannot
// arise. The counter stays a report field and is never incremented — the
// generated runtime carries no `duplicate++` anywhere.
func t05E9(t *testing.T) {
	src := `package probe

fixed table T
{
    a int32 = 0
    m [..2]int32
    label string(8) = "hi"
}
`
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("E9: generate: %v", err)
	}
	text := t02Source(t, files)
	if !strings.Contains(text, "int duplicate = 0;") {
		t.Fatal("E9: the runtime carries no duplicate counter to hold the clause to")
	}
	for _, bumped := range []string{"duplicate++", "duplicate +=", "duplicate = duplicate"} {
		if strings.Contains(text, bumped) {
			t.Errorf("E9: the generated runtime raises the duplicate counter (%q) — the fixed form never raises it", bumped)
		}
	}
}

// t05C3: C3 "text length clamp". SPEC-TABLES §3.4 and docs/FIXED-FORM-ALGORITHM.md
// §5.2: a text field's length is a count in BYTES whose bound is the declared
// `string(N)` size, so a hostile length past the bound is CLAMPED to the
// reader's own bound and counted. The identity plan therefore gives a text
// field its own TEXT entry — never one whole-body copy (fixedplan.go) — and the
// runtime's TEXT op clamps a length past `cap`, counting one `clamped`.
func t05C3(t *testing.T) {
	src := `package probe

fixed table T
{
    label string(8) = "hi"
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	plan := fixedIdentityPlan(st)
	var text *fixedPlanEntry
	for i := range plan {
		if plan[i].op == fixedOpText {
			text = &plan[i]
		}
	}
	if text == nil {
		t.Fatal("C3: a text field got no TEXT entry — its length would be moved verbatim and never clamped")
	}
	if text.meta != fixedTextUtf8 {
		t.Errorf("C3: the text entry's flavour is %d, want utf8 (%d)", text.meta, fixedTextUtf8)
	}
	if want := int64(8); text.size != want {
		t.Errorf("C3: the text entry's byte bound is %d, want the declared string(8) = %d", text.size, want)
	}
	// THE RUNTIME CLAMPS AND COUNTS: the generated TEXT op holds a hostile
	// length to `cap` and moves one `clamped` when it does.
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C3: generate: %v", err)
	}
	text2 := t02Source(t, files)
	if !strings.Contains(text2, "used > cap") {
		t.Error("C3: the generated TEXT op does not hold a length past its bound (`used > cap`) — the clamp is absent")
	}
}

// t05C5: C5 "wide text code units". SPEC-TABLES §3 fixes a `wstring(N)` length
// in CODE UNITS and sixteen bits a unit, so a wide text field's byte bound is
// TWO bytes a unit and its plan entry rides the WIDE flavour — a port that
// counted bytes for a wstring would clamp at half the declared count.
func t05C5(t *testing.T) {
	src := `package probe

fixed table T
{
    wide wstring(4)
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	f := findFieldNamed(t, st, "wide")
	plan := fixedIdentityPlan(st)
	var text *fixedPlanEntry
	for i := range plan {
		if plan[i].op == fixedOpText {
			text = &plan[i]
		}
	}
	if text == nil {
		t.Fatal("C5: a wstring field got no TEXT entry")
	}
	if text.meta != fixedTextWide {
		t.Errorf("C5: the wstring entry's flavour is %d, want wide (%d)", text.meta, fixedTextWide)
	}
	if want := int64(2) * f.Type.Size; text.size != want {
		t.Errorf("C5: the wstring entry's byte bound is %d, want two bytes a code unit for wstring(%d) = %d", text.size, f.Type.Size, want)
	}
}

func findFieldNamed(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %s not found in %s", name, st.Name)
	return nil
}
