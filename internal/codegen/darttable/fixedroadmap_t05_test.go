package darttable

// THE DART LEG'S FIXED-TABLE field-evolution, reports and text ROWS
// (docs/roadmap.sexp node `fixed-tables`, work-sets field-evolution/dart,
// reports/dart and text/dart). Each subtest is one roadmap task id and asserts
// the clause its own title states. Law: docs/FIXED-FORM-ALGORITHM.md §5.4
// (the counters), docs/FIXED-FORM-VERSIONING-TESTS.md (the four ways),
// docs/SPEC-TABLES.md §3.4, ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A clause an existing test of this leg
// already holds is named by that test in the card's verdicts and is not
// repeated here.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapDartT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/E6", t05E6},
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

// t05E6: E6 "Renaming uses the declared identity". §3.4 and §1: a field's
// entry id is `fnv1a64(wire name)`; `was = "Old"` declares the identity the
// field KEEPS, so a rename moves the spelling and not the id an old file's
// layout carries — the field's entry id is the OLD name's.
func t05E6(t *testing.T) {
	old := unitFrom(t, "package probe\n\nfixed table T { b int32 }\n")
	renamed := unitFrom(t, "package probe\n\nfixed table T { a int32 | was = \"b\" }\n")
	plain := unitFrom(t, "package probe\n\nfixed table T { a int32 }\n")

	wo := fixedWalkRoot(findTable(t, old, "T")).entries
	wr := fixedWalkRoot(findTable(t, renamed, "T")).entries
	wp := fixedWalkRoot(findTable(t, plain, "T")).entries

	if len(wo) < 2 || len(wr) < 2 || len(wp) < 2 {
		t.Fatal("E6: the fixture needs the field entry after the root, or the id cannot bite")
	}
	if got, want := wr[1].id, wo[1].id; got != want {
		t.Errorf("E6: the renamed field's entry id is 0x%016x, want the declared identity 0x%016x — renaming uses the declared identity", got, want)
	}
	if wr[1].id == wp[1].id {
		t.Errorf("E6: the renamed field's id 0x%016x equals the un-renamed spelling's, so identity is not the old name", wr[1].id)
	}
}

// t05E9: E9 "duplicate never raised". §0: "this form raises all but
// `duplicate`". The report declares the counter and the fixed decode never
// moves it, so a generated library carries no `duplicate++` anywhere.
func t05E9(t *testing.T) {
	const src = `package probe

fixed table T
{
    a int32
    b string(8)
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("E9: generate: %v", err)
	}
	text := t02Source(t, files)
	if !strings.Contains(text, "duplicate") {
		t.Fatal("E9: the generated report carries no `duplicate` field, so the clause cannot bite")
	}
	if n := strings.Count(text, "duplicate++"); n != 0 {
		t.Errorf("E9: the generated library raises `duplicate` %d times — this form raises all but duplicate", n)
	}
}

// t05C3: C3 "text length clamp". fixeddart.go's emitTextLengthClamp holds a
// string/bytes used-length to [0, cap] and counts one clamp at each end: a
// stored length past the declared capacity lands at the capacity and moves
// `clamped` once.
func t05C3(t *testing.T) {
	const src = `package probe

fixed table T
{
    s string(8)
}
`
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("C3: generate: %v", err)
	}
	text := t02Source(t, files)
	// THE OVER-CAP END: the used length past the declared capacity 8 lands at 8.
	if !strings.Contains(text, "value.sLength > 8") {
		t.Errorf("C3: the generated decode carries no clamp of sLength past the capacity 8 — text length clamp")
	}
	// THE UNDER-ZERO END: a negative used length lands at 0.
	if !strings.Contains(text, "value.sLength < 0") {
		t.Errorf("C3: the generated decode carries no clamp of sLength below 0 — text length clamp")
	}
}

// t05C5: C5 "wide text code units". §1: "only `string(N)` (`12`) and
// `wstring(N)` (`33`) are text kinds in a layout"; a `wstring(N)` field rides
// as kind 33 and its decode reads one Uint16 CODE UNIT per slot, so the
// capacity is counted in UTF-16 code units and not bytes.
func t05C5(t *testing.T) {
	const src = `package probe

fixed table T
{
    w wstring(8)
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)

	at := -1
	for i, e := range w.entries {
		if e.note == "w" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("C5: no layout entry for the wstring field")
	}
	if got := w.entries[at].kind; got != 33 {
		t.Errorf("C5: the wstring entry's kind is %d, want 33 — wstring(N) is layout kind 33", got)
	}
	if got := w.dst[at].arg; got != fixedTextWide {
		t.Errorf("C5: the wstring entry's flavour is %d, want the wide flavour — its unit is a code unit", got)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C5: generate: %v", err)
	}
	text := t02Source(t, files)
	// ONE UINT16 CODE UNIT PER SLOT, two bytes each.
	if !strings.Contains(text, "value.wLength > 8") {
		t.Errorf("C5: the generated decode carries no clamp of wLength past 8 code units")
	}
	if !strings.Contains(text, "getUint16") {
		t.Errorf("C5: the generated decode reads no Uint16 code unit — wide text code units")
	}
}
