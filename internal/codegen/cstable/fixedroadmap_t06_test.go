package cstable

// fixedroadmap_t06 — the cs leg's bounds/ordinals/normalization/array-bounds
// rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed
// Tables"): one subtest per task this card proves, each asserting the clause
// the task's own title states, read off the C# this leg emits or off the ir
// walk it renders. Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (the read-side
// bounds pass), §5.2 (the static plan's Arg/ArgW lanes), §5.8 rows 4, 7, 12,
// and docs/SPEC-TABLES.md §3.4 ("it rides as the float, not as a quantized
// index"). The versioning tests that already hold C1/C2/R11/R30 end to end are
// named in the card's verdicts; this file carries the direct assertion each of
// the weak and owed clauses lacked.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCSharpT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/C7", t06C7},
		{"cs/R30", t06R30},
		{"cs/C9", t06C9},
		{"cs/C10", t06C10},
		{"cs/R31", t06R31},
		{"cs/C12", t06C12},
		{"cs/C13", t06C13},
		{"cs/R17", t06R17},
		{"cs/C1", t06C1},
		{"cs/C2", t06C2},
		{"cs/R11", t06R11},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06Has is a required substring, named by rule.
func t06Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the emitted C# does not carry %q", what, want)
	}
}

// t06C7: cs/C7 [verify] "ranged scalar clamp" — a ranged scalar's declared min
// and max are held by the read-side bounds pass and every clamp counts
// (docs/FIXED-FORM-ALGORITHM.md §4.6).
func t06C7(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    x int32 | min = -5, max = 5
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C7: the ranged scalar carries no bounds pass")
	}
	t06Has(t, body, "if (value.X < -5) { value.X = -5; clamped++; }", "C7 the low end clamps and counts")
	t06Has(t, body, "else if (value.X > 5) { value.X = 5; clamped++; }", "C7 the high end clamps and counts")
}

// t06R30: cs/R30 [owed] "compressed float rides as the float" — in the FIXED
// form min, max and resolution are DEFINITIONS in the digest (SPEC-TABLES
// §3.4), the step is the digest's 'Q' row, and the value on the wire is the
// float32 itself, not a quantized index.
func t06R30(t *testing.T) {
	u := unitFrom(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("R30: the fixture declares %d fixed roots, want one", len(roots))
	}
	d := ir.TableFixedDefinitionsDigest(roots[0])
	if len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R30: a compressed float's digest = % x, want 'R' min max 'Q' resolution", d)
	}
	src := generateCS(t, `package probe
fixed table T {
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Q = BitConverter.UInt32BitsToSingle((uint)v)",
		"R30 the compressed field rides as the float32 bits, never a quantized index")
	t06Has(t, csFn(src, "public static void TFixedClampBody("), "value.Q",
		"R30 the reader's own min/max hold the landed float")
}

// t06C9: cs/C9 [verify] "union tag past arms → None" — a tag past the writer's
// arm count names no arm: it lands None (the zero tag) and counts, on both
// plans (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.8 row 12).
func t06C9(t *testing.T) {
	src := generateCS(t, `package probe
union Pick {
    a int32
    b int32
}
fixed table T {
    p Pick
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C9: the union carries no bounds pass")
	}
	t06Has(t, body, "if ((ulong)value.P.Type > 2) { value.P.Type = (PickType)0; clamped++; }",
		"C9 a union tag past the arm count lands None and counts")
}

// t06C10: cs/C10 [verify] "enum ordinal past top → None" — an ordinal past the
// enum's top value lands None and counts (docs/FIXED-FORM-ALGORITHM.md §4.6).
func t06C10(t *testing.T) {
	src := generateCS(t, `package probe
enum Tier { bronze, silver, gold }
fixed table T {
    e Tier
}
`)
	body := csFn(src, "public static void TFixedClampBody(")
	if body == "" {
		t.Fatal("C10: the enum carries no bounds pass")
	}
	t06Has(t, body, "if ((ulong)value.E > 3) { value.E = (Tier)0; clamped++; }",
		"C10 an enum ordinal past the top lands None and counts")
}

// t06R31: cs/R31 [weak] "full-width lanes: arg/arg2 full width in the emitted
// static plan with the compare at the tag's own width; a 64-bit ordinal
// temporary; the remap table as long as the WRITER's variant count". The guard
// carries ArgW and the run loop compares at that width (§5.2 EMIT kind 15,
// §5.8 row 7); the ordinal op reads through a 64-bit temporary with an 8-byte
// case; the remap table's length word is the writer's variant count and the
// op bounds the ordinal against it.
func t06R31(t *testing.T) {
	if !strings.Contains(tableFixedRuntimeTypes, "public byte ArgW;") {
		t.Error("R31: TableFixedEntry has no full-width guard lane ArgW")
	}
	t06Has(t, tableFixedWireSource, "TagAt(src, p.Guard, p.ArgW) != p.Arg",
		"R31 the guard compares at the tag's own width, never as a prefix")
	if !strings.Contains(tableFixedWireSource, "public static ulong TagAt(") {
		t.Error("R31: the runtime has no full-width tag read")
	}
	at := strings.Index(tableFixedWireSource, "case Ordinal:")
	if at < 0 {
		t.Fatal("R31: the run loop has no ordinal op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Widen:"); end > 0 {
		op = op[:end]
	}
	if !strings.Contains(op, "ulong raw = 0;") {
		t.Error("R31: the ordinal op reads through a 32-bit temporary, not a 64-bit one")
	}
	if !strings.Contains(op, "p.Size == 8") {
		t.Error("R31: the ordinal op has no eight-byte case")
	}
	if !strings.Contains(op, "ushort count = BinaryPrimitives.ReadUInt16LittleEndian(tableBytes);") {
		t.Error("R31: the remap table's length word is not read as the writer's variant count")
	}
	if !strings.Contains(op, "raw <= count") {
		t.Error("R31: the ordinal is not bounded by the remap table's length word")
	}

	var b strings.Builder
	b.WriteString("package probe\n\nunion Wide {\n")
	for i := range 256 {
		fmt.Fprintf(&b, "    a%d int32\n", i)
	}
	b.WriteString("}\n\nfixed table Root { pick Wide }\n")
	wide := generateCS(t, b.String())
	if !guardedPlanHasArgW(wide, 2) {
		t.Error("R31: a 256-arm union's emitted static plan did not carry the full-width tag lane ArgW=2")
	}
}

// t06C12: cs/C12 [weak] "bool byte != 0" — the wire byte is normalised to a
// bool by `!= 0`, so every nonzero byte is true (docs/FIXED-FORM-ALGORITHM.md
// §4.5).
func t06C12(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    ok bool
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Ok = v != 0",
		"C12 a bool byte is true for every nonzero value")
}

// t06C13: cs/C13 [weak] "present flag byte != 0" — an optional's present byte is
// normalised the same way (docs/FIXED-FORM-ALGORITHM.md §4.5).
func t06C13(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    maybe ?int32
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.MaybePresent = v != 0",
		"C13 a present flag byte is true for every nonzero value")
}

// t06R17: cs/R17 [verify] "a bool or present byte that is not 0/1 normalises
// and counts nothing" — the normalisation is `!= 0` and the slot is a plain
// setter with no report, so no counter moves (docs/FIXED-FORM-ALGORITHM.md
// §4.5, §5.4).
func t06R17(t *testing.T) {
	src := generateCS(t, `package probe
fixed table T {
    ok bool
    maybe ?int32
}
`)
	t06Has(t, src, "setRaw: (t, v) => t.Ok = v != 0", "R17 the bool byte normalises")
	t06Has(t, src, "setRaw: (t, v) => t.MaybePresent = v != 0", "R17 the present byte normalises")
	for _, bad := range []string{"t.Ok = (v == 1)", "t.MaybePresent = (v == 1)", "t.Ok ? 1 : 0"} {
		if strings.Contains(src, bad) {
			t.Errorf("R17: the byte is compared to 1 rather than normalised from any nonzero value: %q", bad)
		}
	}
	if strings.Contains(src, "= v != 0, rep") {
		t.Error("R17: the bool/present normalisation carries a report and can count")
	}
}

// t06C1: cs/C1 [weak] "count clamp v<0" — the count op clamps a negative live
// count to zero and counts (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11).
func t06C1(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("C1: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "if (v < 0) { v = 0; if (report != null) report.Clamped++; }",
		"C1 a negative count clamps to zero and counts")
}

// t06C2: cs/C2 [weak] "count clamp v>Max" — a count past the plan entry's bound
// clamps to that bound and counts (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11).
func t06C2(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("C2: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"C2 a count past the bound clamps to it and counts")
}

// t06R11: cs/R11 [owed] "the plan carries the WRITER's bounds, per plan; the
// hostile pass runs against the plan's bounds" — the bound is the plan entry's
// size, and a compiled plan is one entry per lineage peer, so the bound travels
// with the plan and never with the reader (docs/FIXED-FORM-ALGORITHM.md §5.2,
// §5.8 row 4).
func t06R11(t *testing.T) {
	at := strings.Index(tableFixedWireSource, "case Count:")
	if at < 0 {
		t.Fatal("R11: the run loop has no count op")
	}
	op := tableFixedWireSource[at:]
	if end := strings.Index(op, "case Text:"); end > 0 {
		op = op[:end]
	}
	t06Has(t, op, "else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }",
		"R11 the count clamps against the plan entry's own size, the writer's bound")

	u := unitFrom(t, `package probe
fixed table T {
    vals [..4]int32
}
`)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R11: FixedLineageOf has no entry for T")
	}
	older := FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R11: GenerateLineage: %v", err)
	}
	src := string(files[t02File])
	t06Has(t, strings.Join(strings.Fields(src), " "),
		"TableFixedWire.LineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash)",
		"R11 one plan per lineage peer, so the writer's bound travels with the peer's plan")
}
