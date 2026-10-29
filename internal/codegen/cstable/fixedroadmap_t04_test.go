package cstable

// fixedroadmap_t04 — the cs leg's numeric-evolution and optional-values rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §4.2 (the op table: widen/widenf, NaN payloads included), §4.6 (the read-side
// bounds pass), §5.2 (EMIT and the ladder), §5.4 (the counters), and
// docs/FIXED-FORM-VERSIONING-TESTS.md (the rows' NEW-READS-OLD column),
// docs/SPEC-TABLES.md §3.4, §21.2.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none. [verify] tasks prove the holder RUNS
// and BITES (the bite is a planted break in the card's report); [weak] tasks
// assert every clause red-first; [owed] tasks are asserted here and implemented
// in the emitter or runtime.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapCSharpT04Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/E3/other-required-widens", t04E3},
		{"cs/C8", t04C8},
		{"cs/R20", t04R20},
		{"cs/R21", t04R21},
		{"cs/R29", t04R29},
		{"cs/E5", t04E5},
		{"cs/W2", t04W2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t04Norm collapses whitespace so an assertion reads an emitted statement and
// not the indentation the string builder chose.
func t04Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t04Has is a required substring, named by the rule it holds.
func t04Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Runtime is the emitted unit's own source — the fixed runtime and its
// compile step ride in it — so an assertion reads what this leg emits for a
// unit and not a template string held elsewhere.
func t04Runtime(t *testing.T) string {
	t.Helper()
	return t04Norm(generateCS(t, `package probe

fixed table T
{
    x int32
}
`))
}

// t04E3OtherWidens: E3/other-required-widens "All other required widen-ladder
// cases". ALGORITHM §5.2 EMIT and §4.2's op table: the ladder widens a narrower
// integer/float/fixed/enum into a wider one, and the SIGN of a widen is the
// WRITER's kind's (§5.2). The runtime's own ladder and sign test are read here
// directly; the behavioural per-row landings are TestFixedVersioning's rows
// (int_widen, uint_widen, float_widen, bits_grow, fixed_I_grow,
// fixed_I_grow_element, array_elem_widen, enum_width).
func t04E3(t *testing.T) {
	n := t04Runtime(t)
	// The signed integer rungs 2..5 and the unsigned rungs 2..5 (ALG §1's kind
	// table), the float rung 10 -> 11, and the fixed-point rungs.
	t04Has(t, n, "if (from >= 2 && from <= 5 && to >= 2 && to <= 5) { return to > from; }",
		"E3 the integer rungs widen at a larger width")
	t04Has(t, n, "if (from >= 6 && from <= 9 && to >= 6 && to <= 9) { return to > from; }",
		"E3 the 128-bit rungs widen at a larger width")
	t04Has(t, n, "return from == 10 && to == 11;",
		"E3 f32 into f64 is the float rung")
	t04Has(t, n, "if (from >= 20 && from <= 24 && to >= 20 && to <= 24) { return to > from; }",
		"E3 the signed fixed-point rungs widen at a larger I at equal F")
	t04Has(t, n, "if (from >= 25 && from <= 29 && to >= 25 && to <= 29) { return to > from; }",
		"E3 the unsigned fixed-point rungs widen at a larger I at equal F")
	// The SIGN is the WRITER's: only i8..i64 and a SIGNED fixed widen sign-extend.
	t04Has(t, n, "return (kind >= 2 && kind <= 5) || (kind >= 20 && kind <= 24);",
		"E3 a widen sign-extends only when the writer's kind is signed")
}

// t04C8: C8 [weak] "fixed-point F-shift / bits(N)". ALGORITHM §4.6: "A
// fixed-point field's bounds are in VALUE UNITS and its storage is raw, so both
// ends are shifted by F first; a bits(N) clamps to 2^N - 1." The emitted bounds
// pass spells the raw shifted ends and the width clamp, each with its count.
func t04C8(t *testing.T) {
	fixed := generateCS(t, `package probe

fixed table T
{
    v fixed(4, 4) | min = -8, max = 7
}
`)
	fn := t04Norm(fixed)
	// fixed(4,4) min=-8 max=7: the raw band is [-128, 112] — both ends shifted
	// by F=4. The low end sits ON the storage floor and is dropped (it cannot
	// fire, TestFixedBoundsPassDropsSignedFixedEndOnTheStorageFloor); the high
	// end is the value-unit max shifted by F and counts.
	t04Has(t, fn, "if (value.V > 112) { value.V = 112; clamped++; }",
		"C8 the fixed-point high end is the value-unit max shifted by F")

	bits := generateCS(t, `package probe

fixed table T
{
    v bits(12)
}
`)
	bn := t04Norm(bits)
	// bits(12) rides two bytes; its top is 2^12 - 1, which the storage width
	// does not enforce, so the width clamp is emitted and counts.
	t04Has(t, bn, "// bits(12) width clamp", "C8 the bits(N) width clamp is emitted")
	t04Has(t, bn, "if (value.V > 4095) { value.V = 4095; clamped++; }",
		"C8 a bits(12) clamps to 2^12 - 1 and counts")
}

// t04R20: R20 [verify] "§21.2's landing rules". Each clause's behavioural
// landing is TestFixedVersioning's NEW-READS-OLD column (a widened integer or
// float exact with `widened` counted; the shorter array's and string's slack
// the template's zeros; an older enum's ordinals the reader's; an added field
// its declared default) and TestFixedVersioningUnknownCensus' (a deprecated
// field dropped and counted once under `unknown` per plan). This subtest reads
// the runtime's own landing sites, so a planted break in one of them is red by
// name and not hidden behind the other leg's suite.
func t04R20(t *testing.T) {
	n := t04Runtime(t)
	// An added field lands its declared default: the Const op writes p.Aux.
	t04Has(t, n, "if (slots[(int)p.Dst].SetRaw != null) { slots[(int)p.Dst].SetRaw(dst, p.Aux); }",
		"R20 an added field lands its declared default")
	// A narrower integer or float is widened exactly and counts once.
	t04Has(t, n, "if (report != null) { report.Widened++; }",
		"R20 a widen counts `widened`")
	// A deprecated field is dropped and counted ONCE per field per plan, on the
	// compile census (§5.4), never per record.
	t04Has(t, n, "if (!named && census && c.Report != null) { c.Report.Unknown++; }",
		"R20 a dropped field is counted once under `unknown`")
	// A shorter counted array lands with the reader's slack as template zeros:
	// the copy clamps the ragged count to the plan's (the writer's) bound and
	// counts, so the reader's slack stays the template's zeros.
	t04Has(t, n, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"R20 a count past the writer's bound clamps and counts")
}

// t04R21: R21 holds §4's float rung under the finite rule of 2026-10-27
// (docs/SPEC-TABLES.md §3.3, §21.2): widenf is the ordinary conversion, exact
// for every finite f32, and a NaN or an infinity stays one for the root load's
// finite pass (<T>FixedFiniteBody, over storage after the run) to refuse as
// non_finite_float. The NaN payload surgery that used to carry a signalling
// NaN's quiet bit is gone, at both call sites: the scalar widenf and the
// folded flat-array widen.
func t04R21(t *testing.T) {
	n := t04Runtime(t)
	t04Has(t, n, "case WidenF:", "R21 the widenf op exists")
	t04Has(t, n, "slots[(int)p.Dst].SetDouble?.Invoke(dst, (double)f);",
		"R21 the scalar rung is the ordinary conversion")
	t04Has(t, n, "case FlatWiden:", "R21 the folded flat-array widen op exists")
	t04Has(t, n, "double d2 = (double)BinaryPrimitives.ReadSingleLittleEndian(src.Slice(at));",
		"R21 the folded flat-array rung is the ordinary conversion")
	for _, retired := range []string{"WidenF32", "<< 29", "0x7FF0000000000000UL"} {
		if strings.Contains(n, retired) {
			t.Errorf("R21: the retired NaN payload surgery is back (%q)", retired)
		}
	}
	// The finite pass is what refuses a non-finite rung, and it runs on the
	// root's load before the bounds pass.
	src := t04Norm(generateCS(t, `package probe

fixed table T
{
    x float32
}
`))
	t04Has(t, src, "string finite_why = ", "R21 the root load runs the finite pass")
	t04Has(t, src, "report.Reason = \"non_finite_float: \" + finite_why;", "R21 the refusal is non_finite_float, by name")
}

// t04R29: R29 [owed] "the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds". The band edge is the u16
// top 65535 (kind 7) widened into a u32 (kind 8) — the UNSIGNED ladder, kinds
// 6..9 (ir/tablekind.go), ZERO-extended, never sign-extended (§5.2's sign
// rule). The clause's second half is not implemented on this leg and is
// reported unknown: the emitted §4.6 pass (emitFixedClamp) clamps to the
// READER's declared ranges and the compiled plan (TableFixedEntry) carries no
// writer bound at all, so the writer-carried known-range pass the reference
// runs (bill §12.5, TableFixedClampKnownRanges) has no site here to assert.
func t04R29(t *testing.T) {
	n := t04Runtime(t)
	// The band edge: the UNSIGNED rung widens — u16 kind 7 into u32 kind 8 —
	// and the widen's sign is the writer's, so a u16 source is zero-extended
	// across the 65536 ceiling.
	t04Has(t, n, "if (from >= 6 && from <= 9 && to >= 6 && to <= 9) { return to > from; }",
		"R29 the u16 into u32 rung reaches across the 65536 ceiling")
	t04Has(t, n, "if (p.Sign != 0)",
		"R29 the widen sign-extends only when the writer's kind is signed, so the band edge is zero-extended")
}

// t04E5: E5 [verify] "?T vs plain nesting". The versioning harness's
// optional_add row (T into ?T: present == 1, the value exact) is the
// behavioural holder; this subtest reads the runtime's own compile step — the
// optional wrapper is a kind of its own (35) and a plain T met by a ?T pushes
// the present byte as a CONSTANT 1 beside the payload (bill §12.8).
func t04E5(t *testing.T) {
	n := t04Runtime(t)
	t04Has(t, n, "case 35: // Optional wrapper",
		"E5 the optional wrapper is a kind of its own")
	t04Has(t, n, "if (me.Kind == 35 && te.Kind != 35)",
		"E5 a plain T met by a ?T is one byte apart")
	t04Has(t, n, "c.Push(new TableFixedEntry(0, aux_at, 1, 1, guard, Const, arg, 0));",
		"E5 T into ?T lands the present byte as a constant 1 beside the payload")
	if got := fixedKindOptional; got != 35 {
		t.Errorf("E5: fixedKindOptional = %d, want 35", got)
	}
}

// t04W2: W2 [verify] "absent optional skips store". SPEC-TABLES §3.4: an absent
// optional's payload is the template's zeros. The end-to-end holder is
// TestFixedWriteAbsentOptionalGuarded; this subtest reads the emitted writer's
// branch directly — the store is skipped behind a false flag, and the flag byte
// is written either way.
func t04W2(t *testing.T) {
	fixed := generateCS(t, `package probe

fixed table Leaf
{
    n int32
}

fixed table T
{
    link ?Leaf
}
`)
	n := t04Norm(fixed)
	t04Has(t, n, "b[0] = (byte)(value.LinkPresent ? 1 : 0);",
		"W2 the present flag byte is written either way")
	t04Has(t, n, "if (value.LinkPresent) {",
		"W2 the payload's store is behind the present branch")
	i := strings.Index(n, "if (value.LinkPresent) {")
	if i < 0 {
		t.Fatal("W2: the writer has no present branch")
	}
	rest := n[i:]
	if !strings.Contains(rest, "LeafFixed") && !strings.Contains(rest, "TableFixed") {
		t.Errorf("W2: the taken branch does not store the payload: %s", rest)
	}
}
