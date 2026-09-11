package cstable

import (
	"strings"
	"testing"
)

// THE BOUNDS PASS EXISTS, IT RUNS PER RECORD, AND IT IS NOT PLAN ENTRIES
// (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.3 step 11). A ranged scalar off its
// end and a bits(N) past 2^N-1 clamp and count; the pass runs over STORAGE, so
// ONE pass covers BOTH plans and a compiled plan needs no op of its own.
func TestFixedBoundsPassHoldsRangesAndBits(t *testing.T) {
	src := generateCS(t, `package probe
fixed table Holder {
    lead  uint32 = 1
    v     int32 = 0 | min = 0, max = 100
    w     bits(12)
    trail uint32 = 2
}
`)
	body := csFn(src, "public static void HolderFixedClampBody(")
	if body == "" {
		t.Fatal("there is no bounds pass: HolderFixedClampBody was not emitted")
	}
	if !strings.Contains(body, "if (value.V < 0) { value.V = 0; clamped++; }") {
		t.Errorf("the ranged scalar's low end does not clamp and count:\n%s", body)
	}
	if !strings.Contains(body, "if (value.V > 100) { value.V = 100; clamped++; }") {
		t.Errorf("the ranged scalar's high end does not clamp and count:\n%s", body)
	}
	if !strings.Contains(body, "if (value.W > 4095) { value.W = 4095; clamped++; }") {
		t.Errorf("bits(12) does not clamp at 2^12-1 and count:\n%s", body)
	}

	clamp := csFn(src, "public static void HolderFixedClamp(")
	if clamp == "" {
		t.Fatal("the root has no bounds entry point")
	}
	if !strings.Contains(clamp, "report.Clamped += clamped;") {
		t.Error("the bounds pass's count never reaches the report")
	}

	load := csFn(src, "public static long HolderFixedLoad(")
	if load == "" {
		t.Fatal("HolderFixedLoad was not emitted")
	}
	if !strings.Contains(load, "HolderFixedClamp(values[k], report);") {
		t.Error("the record loop does not run the bounds pass: §5.3 step 11 is hash check, prefill, run, BOUNDS")
	}
	run := strings.Index(load, "TableFixedWire.Run(")
	bounds := strings.Index(load, "HolderFixedClamp(")
	if run >= 0 && bounds >= 0 && bounds < run {
		t.Error("the bounds pass runs BEFORE the plan: §4.6 is straight-line code AFTER the run")
	}
}

// A TYPE THAT BOUNDS NOTHING EMITS NO PASS AT ALL (§2.2's zero-cost rule).
func TestFixedBoundsPassNotEmittedWhenNothingIsBounded(t *testing.T) {
	src := generateCS(t, `package probe
fixed table Plain {
    a uint32 = 1
    b float64 = 2.0
}
`)
	if strings.Contains(src, "PlainFixedClampBody(") {
		t.Error("an unbounded type carries a bounds pass")
	}
	load := csFn(src, "public static long PlainFixedLoad(")
	if strings.Contains(load, "FixedClamp(") {
		t.Error("an unbounded type's record loop calls a bounds pass that does not exist")
	}
}

// AN ORDINAL'S SET IS THE BOUNDS PASS'S TOO: a union tag past the arm count and
// an enum ordinal past the enum's top value land None and COUNT, on the compiled
// plan exactly as on the identity one (§4.6, §5.4, §5.8 row 12).
func TestFixedBoundsPassHoldsOrdinals(t *testing.T) {
	src := generateCS(t, `package probe

enum Colour { red, green, blue }

type Cell { n int32 }

union Pick {
    a Cell
    b Cell
}

fixed table Ords {
    c Colour
    p Pick
}
`)
	body := csFn(src, "public static void OrdsFixedClampBody(")
	if body == "" {
		t.Fatal("the ordinals carry no bounds pass")
	}
	if !strings.Contains(body, "if ((ulong)value.C > 3) { value.C = (Colour)0; clamped++; }") {
		t.Errorf("an enum ordinal past the top variant does not land None and count:\n%s", body)
	}
	if !strings.Contains(body, "if ((ulong)value.P.Type > 2) { value.P.Type = (PickType)0; clamped++; }") {
		t.Errorf("a union tag past the arm count does not land None and count:\n%s", body)
	}
}

// THE COMPILED UNION LANDS None FIRST (§5.2 EMIT kind 15). The arms' consts are
// GUARDED and Fills counts a guarded entry's destination as landed, so without
// an unguarded const 0 ahead of them a tag matching no arm left the PREVIOUS
// record's arm in the slot — wrong bytes ACROSS records.
func TestFixedCompiledUnionLandsNoneFirst(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case 15: // Union")
	if at < 0 {
		t.Fatal("the compiler has no union case")
	}
	rest := tableFixedWireSource[at:]
	none := strings.Index(rest, "c.Push(new TableFixedEntry(their_at, aux_at, my_tag, 0, guard, Const, arg, 0));")
	if none < 0 {
		t.Fatal("the compiled union never lands None: only the guarded arm consts are pushed")
	}
	arm := strings.Index(rest, "their_at, aux_at, my_tag, k + 1")
	if arm < 0 {
		t.Fatal("the arm ordinal const is gone")
	}
	if none > arm {
		t.Error("None is pushed AFTER the arms; §5.2 EMIT kind 15 says FIRST")
	}
}

// §5.8 row 7: the ordinal op reads through a 64-BIT temporary and an ordinal
// width of 8 is admissible (§4.5). Through a 32-bit temporary with no 8-byte
// case an eight-byte ordinal left raw at 0 — a silent None.
func TestFixedOrdinalOpIsSixtyFourBit(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Ordinal:")
	if at < 0 {
		t.Fatal("the run loop has no ordinal op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Widen:"); end > 0 {
		op = op[:end]
	}
	if !strings.Contains(op, "ulong raw = 0;") {
		t.Error("the ordinal op still reads through a 32-bit temporary")
	}
	if !strings.Contains(op, "p.Size == 8") {
		t.Error("the ordinal op has no eight-byte case; an ordinal width of 8 is admissible (§4.5)")
	}
	// §5.9 #27: THE COUNTER'S PLACE IS THE CONTRACT. The forged ordinal's
	// `clamped` belongs to the bounds pass, on both plans; an op that counts as
	// well counts twice.
	if strings.Contains(op, "report.Clamped++") {
		t.Error("the ordinal op still counts the forged ordinal; §5.4 puts that count in the bounds pass")
	}
}
