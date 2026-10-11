package jstable

// fixedroadmap_t05 — the js leg's field-evolution, reports and text rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.4 (the §5.4 counters, exactly), §4.6 (a ranged scalar's clamp, and a clamp
// that cannot fire), docs/SPEC-TABLES.md §3.4 (count and text length clamp, the
// USED units), and the titles in docs/roadmap.sexp. One subtest per task id this
// card proves; a task already held by an existing test of this leg is named by
// that test in the verdict and carries no assertion here.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE: every clause is the
// runtime body or the decode projection this leg EMITS, read straight out of the
// generated module, so the suite runs and BITES on a tree that never built the
// corpus.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapJsT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/E9", t05E9},
		{"js/R16", t05R16},
		{"js/R18", t05R18},
		{"js/C3", t05C3},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05E9: E9 [verify] "duplicate never raised" (docs/FIXED-FORM-ALGORITHM.md
// §5.4: `duplicate` is a counter this form defines and never fires — the fixed
// form's plan has one entry per field, so two entries can never claim one
// destination byte and the duplicate event does not exist here). The emitted
// report declares `duplicate`, resets it to zero on both the constructor and the
// reset, and NO site in the whole runtime ever increments it.
func t05E9(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	if n := strings.Count(src, "duplicate++"); n != 0 {
		t.Errorf("E9: the runtime raises `duplicate` %d time(s); the fixed form never raises it (§5.4)", n)
	}
	if n := strings.Count(src, "this.duplicate = 0;"); n != 1 {
		t.Errorf("E9: the fresh report declares `duplicate` %d time(s), want once", n)
	}
	if n := strings.Count(src, "report.duplicate = 0;"); n != 1 {
		t.Errorf("E9: the reset clears `duplicate` %d time(s), want once", n)
	}
}

// t05R16: R16 [weak] — the §5.4 counters a plan moves, EXACTLY, and the ones it
// does not. Every clause of the title is one lane of the emitted runtime
// (docs/FIXED-FORM-ALGORITHM.md §5.4):
//
//   - widened once per entry per record: the widen op moves it, once, and no
//     fold across a widen run;
//   - clamped once per entry per record for a count and for a text length;
//   - a forged ordinal remapped to None counts on BOTH plans: the compiled
//     plan's ordinal op and the identity projection each count it once, and the
//     same forged bytes land the same `clamped == 1` on either;
//   - copy and const move nothing on the clean path.
func t05R16(t *testing.T) {
	_, src := t02Table(t, `package probe

enum Tier { Bronze, Silver, Gold }

fixed table T
{
    rank Tier
}
`)
	run := jsFn(src, "TableFixedRun")
	if run == "" {
		t.Fatal("R16: the emitted runtime has no TableFixedRun")
	}

	// widened once per entry: the widen op and the float-widen op each move it
	// once, and nowhere else.
	if n := strings.Count(run, "report.widened++;"); n != 2 {
		t.Errorf("R16: report.widened++ occurs %d time(s) in the run loop, want the two widen ops once each", n)
	}

	// clamped once per entry, per record: a count clamp and a text clamp each
	// count once, at the one site that reads the number.
	count := jsFn(src, "TableFixedRun")
	if !strings.Contains(t02Norm(count), "else if (v > size) { v = size; report.clamped++; }") {
		t.Error("R16: the count op does not clamp once to the writer's bound the plan carries")
	}
	if !strings.Contains(t02Norm(count), "const cap = (size / unit) | 0;") ||
		!strings.Contains(t02Norm(count), "else if (v > cap) { v = cap; report.clamped++; }") {
		t.Error("R16: the text op does not clamp its length once in the USED units (§3.4)")
	}

	// A forged ordinal remapped to None counts on BOTH plans: the compiled
	// plan's ordinal op counts it, and the identity projection counts it too.
	if !strings.Contains(t02Norm(count), "if (raw > arms) { report.clamped++; }") {
		t.Error("R16: the compiled plan does not count a forged ordinal remapped to None")
	}
	if !strings.Contains(t02Norm(jsFn(src, "TFixedDecode")), "if (value.Rank > 3) { value.Rank = 0; report.clamped++; }") {
		t.Error("R16: the identity projection does not count a forged ordinal remapped to None")
	}

	// copy moves nothing on the clean path: its case body names no counter.
	copyCase := t02Block(t, run, "case TableFixedOpCopy: {", "break;")
	if strings.Contains(copyCase, "report.") {
		t.Error("R16: the COPY op moves a counter; a clean copy moves nothing")
	}
	// const moves nothing except the forged-ordinal guard it carries.
	constCase := t02Block(t, run, "case TableFixedOpConst: {", "break;")
	if n := strings.Count(constCase, "report.clamped++"); n != 1 {
		t.Errorf("R16: the CONST op counts %d time(s), want only the forged-ordinal guard", n)
	}
	if n := strings.Count(constCase, "report.widened++") + strings.Count(constCase, "report.unknown++"); n != 0 {
		t.Error("R16: the CONST op moves widened or unknown; a const moves neither")
	}
}

// t05R18: R18 [owed] "a clamp that cannot fire is not emitted and nothing moves"
// (docs/FIXED-FORM-ALGORITHM.md §4.6; fixedClampEnds answers which ends a
// DataView load can actually lose). A bound sitting on the storage width's own
// limit is a comparison no loaded value can fail, so no clamp statement is
// emitted for it and no `clamped` is ever owed; a bound INSIDE the storage
// width is emitted, on the end that can fire.
func t05R18(t *testing.T) {
	// Vacuous: min = 0 and max = the uint32 ceiling are the storage width's own
	// ends, so neither comparison can be lost.
	_, vacuous := t02Table(t, `package probe

fixed table T
{
    x uint32 | min = 0, max = 4294967295
}
`)
	if decode := jsFn(vacuous, "TFixedDecode"); strings.Contains(decode, "report.clamped") {
		t.Errorf("R18: a clamp that cannot fire was emitted:\n%s", decode)
	}

	// Not vacuous: min = 1 is inside the width, so the low clamp IS emitted.
	_, live := t02Table(t, `package probe

fixed table T
{
    x uint32 = 1 | min = 1, max = 2
}
`)
	if decode := jsFn(live, "TFixedDecode"); !strings.Contains(decode, "report.clamped") {
		t.Errorf("R18: a clamp that CAN fire was elided:\n%s", decode)
	}
}

// t05C3: C3 [verify] "text length clamp" (docs/SPEC-TABLES.md §3.4: a `string(N)`
// or `bytes(N)` field rides a used length then that many units; the reader
// clamps the length to ITS OWN declared capacity, counting one `clamped`
// (docs/FIXED-FORM-ALGORITHM.md §5.4)). The emitted decode reads the length once
// and clamps it at both ends against the reader's own `N`.
func t05C3(t *testing.T) {
	_, src := t02Table(t, `package probe

fixed table T
{
    label string(8)
}
`)
	decode := jsFn(src, "TFixedDecode")
	want := "if (n < 0) { n = 0; report.clamped++; } else if (n > 8) { n = 8; report.clamped++; }"
	if !strings.Contains(t02Norm(decode), want) {
		t.Errorf("C3: the text length is not clamped once at both ends to the reader's own 8:\n%s", decode)
	}
}
