package jstable

// fixedroadmap_t04 — the js leg's numeric-evolution and optional-values rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.1/§5.2 (the widen rungs and their sign), §4
// (the widening ladders), docs/FIXED-FORM-VERSIONING-TESTS.md, and
// docs/SPEC-TABLES.md §3.4. One subtest per task id, and the assertion under
// each is the clause the task's own title states, read off the JavaScript this
// leg emits or off the ir walk it renders.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE: every clause is the
// compiled runtime body this leg emits with the table, so the suite RUNS on a
// tree that never built the corpus.

import (
	"strings"
	"testing"
)

// t04Fixture is the smallest table that carries a widen ladder (an int16 the
// reader may widen), a float rung, and one optional: the three shapes the
// numeric-evolution and optional-values rows are about.
const t04Fixture = `package probe

fixed table T
{
    v int16
    q float32
    link ?int32
}
`

// t04Source generates the fixture and answers the emitted module that carries
// the unit's shared runtime, where the widening and optional bodies land for a
// single-file unit.
func t04Source(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, t04Fixture)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files[runtimeHome(u)+"Table.js"]
	if !ok {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		t.Fatalf("Generate emitted no runtime home; files are: %v", names)
	}
	return string(body)
}

// t04Norm collapses a run of whitespace to one space, so an assertion reads the
// emitted statement and not the indentation the string builder chose.
func t04Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t04Has is a required substring, named by rule.
func t04Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t04Lacks is a forbidden substring, named by rule.
func t04Lacks(t *testing.T, got, unwanted, what string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("%s: the generated source still carries %q", what, unwanted)
	}
}

func TestFixedRoadmapJsT04Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/E3/other-required-widens", t04E3},
		{"js/C8", t04C8},
		{"js/R20", t04R20},
		{"js/R21", t04R21},
		{"js/R29", t04R29},
		{"js/E5", t04E5},
		{"js/W2", t04W2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t04E3: "Numeric widening and backward-read landing rules" [capability]. The
// leg carries BOTH widen ops — the integer widen (op 4) and the float widen
// (op 6) — and the integer widen sign-extends by the WRITER's sign bit
// (FIXED-FORM-ALGORITHM §5.2: "TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT"). The
// write/read running of the ladder is TestJSFixedVersioningNewReadsOld's
// int_widen, uint_widen and float_widen rows (a newer generated reader loads a
// file written from the older generation — int16 -> int32 at -1, INT16_MIN and
// INT16_MAX, and the f32 bits into an f64), which the `tables-js-versioning`
// gate runs under node; the evidence form names it beside this subtest, which
// holds the emitted runtime those rows drive on a tree that never built the
// corpus.
func t04E3(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "const TableFixedOpWiden = 4;", "E3 the integer widen op is the plan's")
	t04Has(t, n, "const TableFixedOpWidenF = 6;", "E3 the float widen op is the plan's")
	t04Has(t, n, "if (sign !== 0 && (src[s + size - 1] & 0x80) !== 0) { fill = 0xff; }",
		"E3 an integer widen sign-extends by the writer's sign bit")
}

// t04C8: "fixed-point F-shift / bits(N)" [owed]. A fixed(I,F) into a wider I at
// equal F is a LADDER widen and not a kind that moved (FIXED-FORM-ALGORITHM
// §1, §5.1), and a bits(N) field rides at its declared storage width under the
// unsigned kind its bit count picks (SPEC-TABLES §3.4, stated at the leaf-size
// table). No earlier assertion on this leg names either clause.
func t04C8(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "if (from >= 20 && from <= 24 && to >= 20 && to <= 24) { return to > from; }",
		"C8 a signed fixed(I,F) into a wider I at equal F is a ladder widen")
	t04Has(t, n, "if (from >= 25 && from <= 29 && to >= 25 && to <= 29) { return to > from; }",
		"C8 an unsigned fixed(I,F) into a wider I at equal F is a ladder widen")
	t04Has(t, n, "case 6: return size === 1 || size === 4 ? 1 : 0; // u8, and bits( 1 .. 8 )",
		"C8 bits(1..8) rides under kind 6 at its declared width")
	t04Has(t, n, "case 9: return size === 8 ? 1 : 0; // u64, flags, and bits( 33 .. 64 )",
		"C8 bits(33..64) rides under kind 9 at eight bytes")
}

// t04R20: §21.2's landing rules [weak]. An added field lands its declared
// default through the prefill that fills the plan's holes; an unnamed older
// field is counted ONCE under unknown per endpoint; a narrower integer or float is
// widened exactly and counts once; the writer's template zeros are what a
// shorter array or string leaves as slack; and an older enum's ordinals are the
// reader's, the list being a prefix (SPEC-TABLES §3.4; FIXED-FORM-ALGORITHM
// §5.2 "the prefill already put the declared default there").
func t04R20(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "for (let i = 0; i < z; i++) { image[o + i] = TFixedPrefill[o + i]; }",
		"R20 the declared default lands where the plan writes no byte")
	t04Has(t, n, "if (!named) { report.unknown++; }",
		"R20 a field of theirs the reader cannot name is one unknown")
	t04Has(t, n, "censusUnknown = lane.unknown;",
		"R20 the unknown census is once per endpoint, taken from the compiled lane")
	t04Has(t, n, "report.widened++;",
		"R20 a narrow value widened exactly counts one widened")
	t04Has(t, n, "bytes.fill(0, at + 8, at + TFixedRecordBytes); // the template's zeros",
		"R20 the writer's template zeros are what shorter array or string slack keeps")
	// THE FIFTH CLAUSE: the writer's ordinal j remaps to the reader's variant k
	// matched BY ID, so with the reader's list a PREFIX of the writer's the
	// match is the same position — k == j and the reader lands ordinal j+1, the
	// ordinal the writer wrote (FIXED-FORM-ALGORITHM §5.2 case 30: "the ordinal
	// is the layout's position"; the forged-ordinal lane in the read loop then
	// answers a byte the writer could not have written, fixedruntime.go's
	// TableFixedOpOrdinal case).
	t04Has(t, n, "if (TableFixedSameId(mine, mi + 1 + k, theirs, ti + 1 + j)) { landed = k + 1; break; }",
		"R20 an older enum's ordinal is the reader's by matched id, so a prefixed list keeps it")
}

// t04R21: the float rung under the finite rule of 2026-10-27
// (docs/SPEC-TABLES.md §3.3; FIXED-FORM-ALGORITHM §5.2: "both are exact by
// construction (a non-finite f32 never reaches widenf: the read refuses it)").
// The widen is the ordinary conversion, which is exact for every finite f32,
// and the read's content pass over the image refuses a NaN or an infinity as
// non_finite_float before the projection lands one; the NaN payload surgery
// that used to carry a signalling NaN is gone.
func t04R21(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "case TableFixedOpWidenF: {", "R21 the float widen is a plan op")
	t04Has(t, n, "TableFixedConv.setUint32(0, TableFixedGetU32(src, s), true); TableFixedConv.setFloat64(8, TableFixedConv.getFloat32(0, true), true);",
		"R21 the f32 rides into the f64 by the ordinary conversion")
	t04Has(t, n, "for (let k = 0; k < 8; k++) { dst[d + k] = TableFixedConv.getUint8(8 + k); }",
		"R21 the widened value lands whole")
	t04Has(t, n, "NonFiniteFloat: 18,", "R21 a non-finite float is refused by name")
	t04Has(t, n, `case TableFixedRefusal.NonFiniteFloat: return "non_finite_float";`, "R21 the refusal's own spelling")
	t04Lacks(t, n, "(payload >>> 3)", "R21 the retired NaN payload surgery")
}

// t04R29: "the band case: the widening across the 65536 ceiling, and the bounds
// pass clamping to the writer's bounds" [owed]. The record-size ceiling is the
// one number 65536 (SPEC-TABLES §1.1 rule 6, FIXED-FORM-ALGORITHM §5.2's
// TableFixedRecordMaxBytes), and the bounds pass clamps an array's live count
// to the writer's declared bound and a string's length to its cap, counting one
// clamped each (FIXED-FORM-ALGORITHM §5.2).
func t04R29(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "export const TableFixedRecordMaxBytes = 65536;", "R29 the band's ceiling is 65536")
	t04Has(t, n, "if (size > TableFixedRecordMaxBytes) { TableFixedFail(b, TableFixedRefusal.LayoutRecordTooLarge); return 0; }",
		"R29 a record past the ceiling is refused by name")
	t04Has(t, n, "if (v < 0) { v = 0; report.clamped++; }",
		"R29 a live count below the writer's floor clamps to it and counts")
	t04Has(t, n, "else if (v > size) { v = size; report.clamped++; }",
		"R29 a live count past the writer's bound clamps to it and counts")
	t04Has(t, n, "else if (v > cap) { v = cap; report.clamped++; }",
		"R29 a text length past the writer's cap clamps to it and counts")
}

// t04E5: "?T vs plain nesting" [verify]. An optional is the wrapper kind 35 —
// ONE present byte in front of ONE child, so its size is the child's plus one —
// while a plain nested table is kind 13, whose size is the SUM of its fields
// and carries no present byte (FIXED-FORM-ALGORITHM §5.2, SPEC-TABLES §3.4).
// Existing coverage: fixedform_test.go:TestFixedOptionalRowsMatchReference
// holds the optional wrapper's row against the reference.
func t04E5(t *testing.T) {
	src := t04Source(t)
	n := t04Norm(src)
	t04Has(t, n, "case 35: { // the OPTIONAL WRAPPER: one child, one present byte in front of it",
		"E5 the optional wrapper is kind 35")
	t04Has(t, n, "if (size !== sum + 1) { TableFixedFail(b, TableFixedRefusal.LayoutSizeMismatch); return 0; }",
		"E5 the optional wrapper's size is the child's plus its present byte")
	t04Has(t, n, "case 13: { // a TABLE: its size is the SUM of its fields'",
		"E5 a plain nested table is kind 13, summing its fields with no present byte")
}

// t04W2: "absent optional skips store" [verify]. The writer lays the present
// byte and stores the payload only inside the present branch, so an absent
// optional's payload is never written (SPEC-TABLES §3.4). Existing coverage:
// fixedform_test.go:TestDecodeClampsEnumOrdinalAndRangedInt asserts the absent
// payload guard `if (value.NotePresent)` on the decode side.
func t04W2(t *testing.T) {
	src := t04Source(t)
	body := t04Norm(jsFn(src, "TFixedWriteBody"))
	if body == "" {
		t.Fatal("W2: the fixture emits no TFixedWriteBody")
	}
	t04Has(t, body, "view.setUint8(at + 6, value.linkPresent ? 1 : 0);",
		"W2 the present byte is written for every record")
	t04Has(t, body, "if (value.linkPresent) { view.setInt32(at + 7, value.link, true); }",
		"W2 the payload store is inside the present branch, so an absent optional skips it")
}
