package jstable

// fixedroadmap_t06_test.go — the js leg's scalar-bounds, ordinals, normalization
// and array-bounds rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"). It carries one subtest per task id whose clause this
// file proves, and names the clause each assertion implements: js/C1 and js/C2
// (the count clamp's two ends), js/C7 (the ranged-scalar clamp), js/C9 (a union
// tag past the arm set lands None), js/C10 (an enum ordinal past the top lands
// None), js/C12 and js/C13 (a bool byte and a present byte read non-zero), and
// js/R17 (a bool or present byte that is not 0/1 normalises and counts nothing).
//
// THE ROWS js/R30 (compressed float) and js/R31 (full-width lanes) are NOT
// proved here and their verdicts are `unknown`: their finer/coarser and
// fp-contract clauses are the versioning suite's (fixedversioning_cfloat_*),
// which SKIPS without the reference's byte oracle, and the full-width private
// plan is compiled at read time by TableFixedCompile, not laid down in the
// emitted module this file can read.
//
// NOTHING HERE READS THE REFERENCE'S BYTE ORACLE, so the suite RUNS on a tree
// that never built the corpus — the same standing docs/FIXED-FORM-ALGORITHM.md
// §5.4 gives the fixed form's read: whatever the wire holds, the reader's own
// bound is the one that clamps, lands None and counts one `clamped`.

import (
	"strings"
	"testing"
)

// t06Ranged is a table whose one scalar declares both bounds: §4.6's ranged
// scalar clamp, on the value the consumer will see.
const t06Ranged = `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`

// t06Union is a three-arm union whose tag is one byte: §5.4's forged-tag rule,
// a tag past the writer's arm set landing None and counting one `clamped`.
const t06Union = `package probe

type Alpha { m int32 = 0 }
type Beta  { n int32 = 0 }
type Gamma { p int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
    gamma Gamma
}

fixed table T
{
    pick Pick
    seq  int32 = 0
}
`

// t06Enum is a four-variant enum whose ordinal is one byte: §4.6's ordinal
// clamp, an ordinal past the top landing None and counting one `clamped`.
const t06Enum = `package probe

enum Tier { Bronze, Silver, Gold, Platinum }

fixed table T
{
    tier Tier
    seq  int32 = 0
}
`

// t06BoolPresent is a bool beside an optional: the two bytes that normalise.
const t06BoolPresent = `package probe

fixed table T
{
    b bool
    o ?int32
}
`

// t06Decode is the module's decode projection, from its `export function
// TFixedDecode` to the closing brace of the next top-level export.
func t06Decode(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, "export function TFixedDecode")
	if i < 0 {
		t.Fatal("the generated source has no TFixedDecode")
	}
	rest := src[i:]
	j := strings.Index(rest, "\n}\n")
	if j < 0 {
		t.Fatal("TFixedDecode has no closing brace")
	}
	return rest[:j+2]
}

func TestFixedRoadmapJsT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/C1", t06C1},
		{"js/C2", t06C2},
		{"js/C7", t06C7},
		{"js/C9", t06C9},
		{"js/C10", t06C10},
		{"js/C12", t06C12},
		{"js/C13", t06C13},
		{"js/R17", t06R17},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06C1: C1 [verify] "count clamp v<0". §4.6's count clamp has TWO ends and the
// negative one is the first: a count word read as a signed int32 that is
// negative clamps to zero and counts one `clamped`, before the bound end is
// tested. The count op is the FIRST clamp in TableFixedRun.
func t06C1(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	op := t02Block(t, src, "case TableFixedOpCount: {", "case TableFixedOpText: {")
	t02Has(t, t02Norm(op), "if (v < 0) { v = 0; report.clamped++; }",
		"C1 a negative count clamps to zero and counts one clamped")
}

// t06C2: C2 [verify] "count clamp v>Max". The other end: a count past the
// plan's own bound clamps down to it and counts one `clamped`, so the two ends
// share one clamp and one counter — never a second count.
func t06C2(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	op := t02Block(t, src, "case TableFixedOpCount: {", "case TableFixedOpText: {")
	t02Has(t, t02Norm(op), "else if (v > size) { v = size; report.clamped++; }",
		"C2 a count past the plan's bound clamps to it and counts one clamped")
}

// t06C7: C7 [owed] "ranged scalar clamp". §4.6's ranged-scalar clamp is held
// AFTER the load, on the value the consumer sees, against THIS reader's own
// declared min and max — and both ends count one `clamped`. The plan never
// clamps a range; the decode projection does.
func t06C7(t *testing.T) {
	_, src := t02Table(t, t06Ranged)
	dec := t06Decode(t, src)
	t02Has(t, t02Norm(dec), "if (value.X < -5) { value.X = -5; report.clamped++; } else if (value.X > 5) { value.X = 5; report.clamped++; }",
		"C7 a ranged scalar clamps at both declared ends and counts")
}

// t06C9: C9 [owed] "union tag past arms → None". §5.4: a union tag past the
// writer's arm set is not a refusal and not malformed — it lands None (0) and
// counts one `clamped`. The top arm's ordinal is the count of arms, so a tag
// strictly greater than it is out of range.
func t06C9(t *testing.T) {
	_, src := t02Table(t, t06Union)
	dec := t06Decode(t, src)
	t02Has(t, t02Norm(dec), "value.Pick.Type = view.getUint8(at);",
		"C9 the union tag is read at its own width")
	t02Has(t, t02Norm(dec), "if (value.Pick.Type > 3) { value.Pick.Type = 0; report.clamped++; }",
		"C9 a tag past the three arms lands None and counts one clamped")
}

// t06C10: C10 [owed] "enum ordinal past top → None". §4.6's ordinal clamp: an
// enum ordinal past the top wire value lands None (0) and counts one `clamped`,
// exactly as a count past its bound does.
func t06C10(t *testing.T) {
	_, src := t02Table(t, t06Enum)
	dec := t06Decode(t, src)
	t02Has(t, t02Norm(dec), "if (value.Tier > 4) { value.Tier = 0; report.clamped++; }",
		"C10 an enum ordinal past the top lands None and counts one clamped")
}

// t06C12: C12 [verify] "bool byte != 0". A bool rides as one byte and is TRUE
// for every non-zero byte, never only for 1.
func t06C12(t *testing.T) {
	_, src := t02Table(t, t06BoolPresent)
	dec := t06Decode(t, src)
	t02Has(t, t02Norm(dec), "value.B = view.getUint8(at) !== 0;",
		"C12 a bool byte is true for every non-zero byte")
}

// t06C13: C13 [weak] "present flag byte != 0". An optional's present byte is
// the same rule: the payload is present for every non-zero byte.
func t06C13(t *testing.T) {
	_, src := t02Table(t, t06BoolPresent)
	dec := t06Decode(t, src)
	t02Has(t, t02Norm(dec), "value.OPresent = view.getUint8(at + 1) !== 0;",
		"C13 a present byte is true for every non-zero byte")
}

// t06R17: R17 [verify] "a bool or present byte that is not 0/1 normalises and
// counts nothing". Normalisation is `!== 0` on the byte, so 2 lands true and 0
// lands false; and NEITHER byte moves a counter — a normalised byte is not an
// event, so `clamped` stands for both.
func t06R17(t *testing.T) {
	_, src := t02Table(t, t06BoolPresent)
	dec := t06Decode(t, src)
	for _, line := range []string{
		"value.B = view.getUint8(at) !== 0;",
		"value.OPresent = view.getUint8(at + 1) !== 0;",
	} {
		t02Has(t, t02Norm(dec), line, "R17 the byte normalises to a bool by !== 0")
		if strings.Contains(line, "report.") {
			t.Errorf("R17: the normalising line %q counts; a normalised byte is not an event", line)
		}
	}
	// The two normalising lines carry no counter: the only report mentions in
	// the decode are elsewhere.
	for _, l := range strings.Split(dec, "\n") {
		if strings.Contains(l, "getUint8(at) !== 0") || strings.Contains(l, "getUint8(at + 1) !== 0") {
			if strings.Contains(l, "report.") {
				t.Errorf("R17: a normalising line counts: %q", strings.TrimSpace(l))
			}
		}
	}
}
