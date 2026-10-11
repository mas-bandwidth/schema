package gotable

// THE ROADMAP'S BOUNDS, ORDINAL AND NORMALISATION ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/SPEC-TABLES.md §3.4 (the fixed form), docs/FIXED-FORM-ALGORITHM.md §5.4
// (the read-side bounds pass) and docs/FIXED-FORM-VERSIONING-TESTS.md (the
// hostile half of C1/C2/C7, the exact-once clamp counts). One subtest per task
// id, table-driven, t.Parallel() first.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/ -run TestFixedRoadmapGoT06Bounds` and
// under the leg gates: the read-side bounds are read off the emitted Go this
// leg lays down (TFixedClampBody), and the runtime ops a compiled plan runs
// (tableFixedCount, tableFixedBool, tableFixedTagAt, tableFixedOrdinal) are
// read off the emitted runtime's own case bodies.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapGoT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/C7", t06GoC7},
		{"go/R30", t06GoR30},
		{"go/C9", t06GoC9},
		{"go/C10", t06GoC10},
		{"go/R31", t06GoR31},
		{"go/C12", t06GoC12},
		{"go/C13", t06GoC13},
		{"go/R17", t06GoR17},
		{"go/C1", t06GoC1},
		{"go/C2", t06GoC2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06ClampBody is the emitted read-side bounds body for a fixed root, the
// straight-line Go §3.4 and §5.4 lay down after the copy.
func t06ClampBody(t *testing.T, src string) string {
	t.Helper()
	body := funcSource(src, "func TFixedClampBody(")
	if body == "" {
		t.Fatal("the leg emitted no TFixedClampBody; the read-side bounds pass is absent")
	}
	return body
}

// ---- go/C7 "ranged scalar clamp" --------------------------------------------

// t06GoC7 holds C7 to the emitted bounds pass: a ranged scalar's declared min
// and max, and both ends clamp and count. docs/SPEC-TABLES.md §3.4: the fixed
// form carries a ranged scalar's min and max, and the read-side pass holds the
// landed value to them. docs/FIXED-FORM-VERSIONING-TESTS.md "the range clamp
// across versions (C7's cross-version half; the hostile half stays)": the clamp
// is straight-line over storage, so the identity plan and a compiled plan use
// the same numbers.
func t06GoC7(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`))
	body := t06ClampBody(t, src)
	t02GoHas(t, t02GoNorm(body),
		"if value.X < -5 { value.X = -5 (*clamped)++ } else if value.X > 5 { value.X = 5 (*clamped)++ }",
		"C7 a ranged scalar clamps at both declared ends and counts each")
}

// ---- go/R30 "compressed float rides as the float" ---------------------------

// t06GoR30 holds R30's emitter and digest clauses. docs/SPEC-TABLES.md §3.4:
// "it rides as the float, not as a quantized index"; docs/FIXED-FORM-VERSIONING-TESTS.md
// §cfloat_res_refine: "the resolution is in the DEFINITIONS DIGEST ('Q', bill
// §13), so the wire hash moves when the step does". In the fixed form the stored
// value is the float32 itself, min/max are the reader's bounds the clamp uses,
// and resolution is a 'Q' row of the digest.
func t06GoR30(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t02GoRoot(t, u)
	d := ir.TableFixedDefinitionsDigest(st)
	if len(d) < 18 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R30: the digest does not carry the resolution under 'Q': % x", d)
	}
	body := t06ClampBody(t, t02GoEmit(t, u))
	t02GoHas(t, t02GoNorm(body),
		"if value.Q < 0.0 { value.Q = 0.0 (*clamped)++ } else if value.Q > 1.0 { value.Q = 1.0 (*clamped)++ }",
		"R30 the compressed float clamps to the reader's min/max, the bounds following the float")
}

// ---- go/C9 "union tag past arms → None" -------------------------------------

// t06GoC9 holds C9 to the emitted bounds pass over a union: a tag past the arm
// count lands the union's None and counts. docs/SPEC-TABLES.md §3.4 and
// FIXED-FORM-VERSIONING-TESTS.md (the remap of an unknown variant to None, the
// hostile half of the ordinal rows).
func t06GoC9(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, `package probe

union Pick
{
    a int32
    b int32
}

fixed table T
{
    p Pick
}
`))
	body := t06ClampBody(t, src)
	t02GoHas(t, t02GoNorm(body),
		"if uint32(value.P.Type) > 2 { value.P.Type = PickTypeNone (*clamped)++ }",
		"C9 a union tag past the arm count lands None and counts")
}

// ---- go/C10 "enum ordinal past top → None" ----------------------------------

// t06GoC10 holds C10 to the emitted bounds pass over an enum: an ordinal past
// the enum's top value lands None and counts. docs/SPEC-TABLES.md §3.4.
func t06GoC10(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, `package probe

enum Tier
{
    Bronze
    Silver
    Gold
}

fixed table T
{
    tier Tier
}
`))
	body := t06ClampBody(t, src)
	t02GoHas(t, t02GoNorm(body),
		"if uint64(value.Tier) > 3 { value.Tier = TierNone (*clamped)++ }",
		"C10 an enum ordinal past the enum's top value lands None and counts")
}

// ---- go/R31 "full-width lanes" ----------------------------------------------

// t06GoR31 holds R31's runtime clauses in the emitted runtime text: the guard
// compares at the tag's own width and never at its first byte, so a foreign
// 0x0101 at a two-byte tag is not arm 1 (held end-to-end by
// fixedguard_test.go's TestFixedGuardIsComparedAtItsFullWidth); the ordinal
// op's own read is the 64-bit tableFixedTagAt at p.Size, so an eight-byte
// ordinal keeps its top four bytes and never goes through a 32-bit temporary;
// and the remap pool's length word is 16 bits, so the table can be as long as
// the writer's variant count. docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §1.2, §5.2, §5.4, §5.9 item 42.
func t06GoR31(t *testing.T) {
	rt := t02GoNorm(tableFixedRuntime)
	t02GoHas(t, rt, "func tableFixedTagAt(src []byte, at uint32, w uint8) uint64",
		"R31 the guard load returns a 64-bit temporary")
	t02GoHas(t, rt, "n := tableFixedArgWidth(w)", "R31 the guard reads at its own width, clamped")
	// THE ORDINAL OP'S OWN READ, not a comment beside it: §1.2 has an ordinal
	// ride at 1, 2, 4 or 8 bytes, so the op loads tableFixedTagAt at p.Size (a
	// uint64) and never tableFixedGet32. Read through a 32-bit temporary an
	// eight-byte ordinal loses its top four bytes, so a forged 0x1_0000_0001
	// would land as 1 instead of past the writer's set. Asserted inside the
	// ordinal case so a width change at the op is red by name.
	ord := t02GoBlock(t, rt, "case tableFixedOrdinal:", "case tableFixedWiden:")
	t02GoHas(t, ord, "raw := tableFixedTagAt(src, p.Src, uint8(p.Size))",
		"R31 the ordinal op loads a 64-bit temporary at the ordinal's own width")
	if strings.Contains(ord, "tableFixedGet32") {
		t.Error("R31: the ordinal op goes through a 32-bit temporary, so an eight-byte ordinal loses its top four bytes")
	}
	t02GoHas(t, rt, "const tableFixedRemapMax uint32 = 0xFFFF",
		"R31 the remap pool's length word is 16 bits, so the table is as long as the writer's variant count")
	t02GoHas(t, rt, "if n > tableFixedRemapMax {", "R31 a writer's variant count past the length word refuses")
}

// ---- go/C12 "bool byte != 0" ------------------------------------------------

// t06GoC12 holds C12 to the bool op in the emitted runtime: the wire says
// nonzero is true (docs/SPEC-TABLES.md §3), and tableFixedBool normalises
// byte != 0 to 1 rather than copying, because a Go bool is not the wire's own
// byte. (Also held end-to-end by TestFixedFormHostileBoolByte.)
func t06GoC12(t *testing.T) {
	t02GoHas(t, t02GoNorm(tableFixedRuntime),
		"if src[p.Src+k] != 0 { v = 1 }",
		"C12 a bool byte is normalised byte != 0 -> 1, never copied")
}

// ---- go/C13 "present flag byte != 0" ----------------------------------------

// t06GoC13 holds C13 to the present byte: an optional field's present flag is
// emitted as a tableFixedBool op, so the same nonzero-is-true normalisation
// applies to it. docs/SPEC-TABLES.md §3.4, §3.
func t06GoC13(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, `package probe

fixed table T
{
    v ?int32
}
`))
	t02GoHas(t, src, "Op: tableFixedBool} // v present",
		"C13 the present flag rides the bool op, so nonzero normalises to present")
}

// ---- go/R17 "normalises and counts nothing" ---------------------------------

// t06GoR17 holds R17: a bool or present byte that is not 0/1 normalises and
// counts nothing. docs/FIXED-FORM-VERSIONING-TESTS.md: the bool op is not one
// of the six events, so its block moves no counter.
func t06GoR17(t *testing.T) {
	rt := t02GoNorm(tableFixedRuntime)
	boolCase := t02GoBlock(t, rt, "case tableFixedBool:", "case tableFixedCount:")
	if !strings.Contains(boolCase, "if src[p.Src+k] != 0 { v = 1 }") {
		t.Error("R17: the bool op does not normalise byte != 0 to 1")
	}
	if strings.Contains(boolCase, "report.") {
		t.Error("R17: the bool op moves a counter; a hostile byte counts nothing")
	}
}

// ---- go/C1 "count clamp v<0" ------------------------------------------------

// t06GoC1 holds C1's hostile half: a count word below zero clamps to zero and
// counts. docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-VERSIONING-TESTS.md
// "the count clamp across bounds (C1/C2's cross-version half; the hostile half
// stays)". The count op reads a 32-bit word and holds it to the declared bound.
func t06GoC1(t *testing.T) {
	rt := t02GoNorm(tableFixedRuntime)
	countCase := t02GoBlock(t, rt, "case tableFixedCount:", "case tableFixedText:")
	t02GoHas(t, countCase, "if v < 0 { v = 0", "C1 a count below zero clamps to zero")
	t02GoHas(t, countCase, "report.Clamped++", "C1 the negative clamp counts")
}

// ---- go/C2 "count clamp v>Max" ----------------------------------------------

// t06GoC2 holds C2's hostile half: a count word above the declared maximum
// clamps to the maximum and counts. docs/SPEC-TABLES.md §3.4: a counted array's
// bound is the declared maximum and a count past it is the writer's fault.
func t06GoC2(t *testing.T) {
	rt := t02GoNorm(tableFixedRuntime)
	countCase := t02GoBlock(t, rt, "case tableFixedCount:", "case tableFixedText:")
	t02GoHas(t, countCase, "else if v > int32(p.Size) { v = int32(p.Size)", "C2 a count above the max clamps to the max")
	if n := strings.Count(countCase, "report.Clamped++"); n != 2 {
		t.Errorf("C2: the count op counts at %d sites, want the low and high ends", n)
	}
}
