package gotable

// THE RED TEAM AGAINST THE COMPRESSED FLOAT UNDER WIDENING (§3.4's row).
//
// docs/SPEC-TABLES.md §3.4 says a compressed float in the FIXED form "rides as
// the float, not as a quantized index". The rule under test is Rowan's: because
// `min`, `max` and `res` are DEFINITIONS in the digest and not bytes in the
// record, the RANGE may widen and the RESOLUTION may refine and an old file
// reads EXACTLY under the new definition.
//
// A FINDING is a wrong value on a lawful widening, a value outside the
// declared range that the read-side bounds pass does not hold, or a difference
// between this leg and the reference.
//
// The harness is the versioning suite's: stage one generates the OLD unit and
// writes a file with its own `FixedSave` — lawful by construction; stage two
// generates the NEW unit with the OLD unit's locked entry as its lineage and
// reads it. Every assertion is on the RAW BITS, because a compressed float's
// question is exactly whether a bit moved.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cfRuntime is the one thing outside the repository this suite needs: the Go
// runtime as a SIBLING checkout, the path runVersionProbeRetired compiles
// against. Without it no probe builds at all, so the suite says so and skips —
// unless SCHEMA_REQUIRE_CORPUS promised the build, where a skip would report
// green over a suite that never ran.
func cfRuntime(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the Go runtime is not a sibling checkout at %s: %v", path, err)
		}
		t.Skipf("the Go runtime is not a sibling checkout at %s", path)
	}
}

// cfWrite generates `schema` alone and runs `body` in its package, which must
// write the file at the path it is handed as `out`.
func cfWrite(t *testing.T, dir, name, schema, body string) string {
	t.Helper()
	cfRuntime(t)
	out := filepath.Join(dir, name+".bin")
	src := fmt.Sprintf(`package probe

import ("math"; "os"; "testing")

var _ = math.Float32bits

func TestWrite(t *testing.T) {
	out := %q
	_ = out
%s
}
`, out, body)
	o, err := runVersionProbe(t, schema, nil, src)
	if err != nil {
		t.Fatalf("the OLD writer did not write its own file: %v\n%s", err, o)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the OLD writer wrote nothing: %v", err)
	}
	return out
}

// cfRead generates `schema` with `older`'s locked entries as its lineage and
// runs `body` in its package.
func cfRead(t *testing.T, schema string, older []string, body string) {
	t.Helper()
	src := fmt.Sprintf(`package probe

import ("math"; "os"; "testing")

var _ = math.Float32bits
var _ = os.ReadFile

func TestRead(t *testing.T) {
%s
}
`, body)
	o, err := runVersionProbe(t, schema, older, src)
	if err != nil {
		t.Fatalf("the NEW reader failed its read: %v\n%s", err, o)
	}
}

// ---- the schemas, one field each ------------------------------------------
//
// 0.3 is the value every case carries, because it is OFF the 0.1 grid in
// float32 (0x3E99999A) and it is exactly what a re-quantization would move.

const cfOld = `package cfold

fixed table Reading
{
    v float32 | min = 0, max = 1, resolution = 0.1
}
`

// THE RANGE WIDENS AND THE MIN MOVES BY A NON-MULTIPLE OF res. Under Rowan's
// rule this is lawful and 0.3 must come back as 0.3; under a reader that
// re-quantizes onto its OWN grid, 0.3 lands on -0.05 + k*0.105 and is wrong.
const cfWideMin = `package cfold

fixed table Reading
{
    v float32 | min = -0.05, max = 1, resolution = 0.1
}
`

// THE RESOLUTION REFINES BY A NON-INTEGER FACTOR, 0.1 -> 0.03, with the range
// held: the grid the new definition names does not contain the old one at all.
const cfFineRes = `package cfold

fixed table Reading
{
    v float32 | min = 0, max = 1, resolution = 0.03
}
`

// THE RANGE WIDENS AT BOTH ENDS, which is what lets a value past the OLD max
// ride lawfully under the new definition.
const cfWideBoth = `package cfold

fixed table Reading
{
    v float32 | min = -10, max = 10, resolution = 0.1
}
`

// writeOne writes one record holding the float32 whose BITS are `bits`, so a
// NaN payload and a negative zero are spelled exactly and not through a
// literal a compiler may fold.
func writeOne(bits uint32) string {
	return fmt.Sprintf(`
	var r Reading
	ReadingReset(&r)
	r.V = math.Float32frombits(%#x)
	buf := make([]byte, ReadingFixedMeasure(1))
	if ReadingFixedSave([]Reading{r}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(out, buf, 0o600); err != nil {
		t.Fatal(err)
	}`, bits)
}

// readOne reads the one record and checks the BITS and the clamp count.
func readOne(file string, bits uint32, clamped int32) string {
	return fmt.Sprintf(`
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	var got [1]Reading
	var plan [64]TableFixedEntry
	var report TableReport
	if n := ReadingFixedLoad(got[:], data, plan[:], &report); n != 1 {
		t.Fatalf("the NEW reader refused a lawful old file: n=%%d why=%%q", n, report.Reason)
	}
	if b := math.Float32bits(got[0].V); b != %#x {
		t.Fatalf("the value moved: stored %%#x, read %%#x (%%v)", uint32(%#x), b, got[0].V)
	}
	if report.Clamped != %d {
		t.Fatalf("clamped is %%d, want %d", report.Clamped)
	}`, file, bits, bits, clamped, clamped)
}

// ---- case 1: THE WRITER DOES NOT QUANTIZE ---------------------------------
//
// Hypothesis (1). An off-grid value saved and loaded by ONE unit: if the
// writer quantized by the packet wire's rule, 0.3 would come back as the grid
// point 0.30000001192092896's nearest neighbour under (0, 1, 0.1) and the bits
// would move. It does not: the record holds the float32 itself.

func TestFixedCFloatWriterDoesNotQuantize(t *testing.T) {
	dir := t.TempDir()
	const bits = 0x3E99999A // 0.3, OFF the 0.1 grid in float32
	file := cfWrite(t, dir, "same", cfOld, writeOne(bits))
	cfRead(t, cfOld, nil, readOne(file, bits, 0))
}

// ---- case 2: THE RANGE WIDENS, THE MIN MOVES OFF THE GRID -----------------
//
// Hypothesis (2) and (3). min 0 -> -0.05, which is NOT a multiple of res, so
// the new grid shares no point with the old but the zero. A reader that
// re-quantized on read or in the bounds pass would land a different number.

func TestFixedCFloatRangeWidensMinOffGrid(t *testing.T) {
	dir := t.TempDir()
	const bits = 0x3E99999A
	file := cfWrite(t, dir, "widemin", cfOld, writeOne(bits))
	cfRead(t, cfWideMin, []string{cfOld}, readOne(file, bits, 0))
}

// ---- case 3: THE RESOLUTION REFINES BY A NON-INTEGER FACTOR ---------------
//
// Hypothesis (4). 0.1 -> 0.03 with the range held. `res` is not a byte of the
// fixed record at all, so the storage is identical and the read must be exact.

func TestFixedCFloatResolutionRefinesNonInteger(t *testing.T) {
	dir := t.TempDir()
	const bits = 0x3E99999A
	file := cfWrite(t, dir, "fineres", cfOld, writeOne(bits))
	cfRead(t, cfFineRes, []string{cfOld}, readOne(file, bits, 0))
}

// ---- case 4: THE BOUNDS, END BY END --------------------------------------
//
// Hypothesis (5). The OLD max exactly, one ulp past it, and the ends under a
// widened range. A value AT a bound is inside and must not count.

func TestFixedCFloatAtTheBoundsExactly(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		bits    uint32
		want    uint32
		clamped int32
	}{
		// 1.0 is the OLD max exactly: inside, no clamp.
		{"old_max", 0x3F800000, 0x3F800000, 0},
		// one ulp PAST the old max, read under the OLD definition: clamped to
		// max and counted.
		{"one_ulp_past_old_max", 0x3F800001, 0x3F800000, 1},
		// 0 is the OLD min exactly.
		{"old_min", 0x00000000, 0x00000000, 0},
		// one ulp BELOW the old min is the smallest negative subnormal.
		{"one_ulp_below_old_min", 0x80000001, 0x00000000, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			file := cfWrite(t, dir, c.name, cfOld, writeOne(c.bits))
			cfRead(t, cfOld, nil, readOne(file, c.want, c.clamped))
		})
	}
}

// ---- case 5: NaN, THE INFINITIES AND THE NEGATIVE ZERO -------------------
//
// Hypothesis (5)'s hostile end. The fixed form's bounds pass is
// `if v < min { .. } else if v > max { .. }` in both legs, and EVERY
// comparison against a NaN is false, so a NaN rides a compressed float
// THROUGH the pass: a value outside the declared range, uncounted, handed to a
// caller the row promises a number inside [min, max] to. The infinities do
// clamp, because a comparison against an infinity is not false.
//
// KNOWN RED: TestFixedCFloatNaNSurvivesTheBoundsPass. It is the finding, and
// it asserts what the wire DOES today so the day the rule changes this test is
// what says so.

func TestFixedCFloatNaNSurvivesTheBoundsPass(t *testing.T) {
	dir := t.TempDir()
	const quietNaN = 0x7FC00000
	file := cfWrite(t, dir, "nan", cfOld, writeOne(quietNaN))
	// THE FINDING, asserted as it stands: the NaN comes back whole and the
	// clamp counter never moved, over a field declared [0, 1].
	cfRead(t, cfOld, nil, readOne(file, quietNaN, 0))
}

func TestFixedCFloatInfinitiesClampAndCount(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		bits    uint32
		want    uint32
		clamped int32
	}{
		{"plus_inf", 0x7F800000, 0x3F800000, 1},  // +inf > max: clamps to max
		{"minus_inf", 0xFF800000, 0x00000000, 1}, // -inf < min: clamps to min
		// NEGATIVE ZERO is not below a min of +0: `-0.0 < 0.0` is false in
		// IEEE-754, so it survives the pass with its SIGN BIT — a bit pattern
		// the declaration cannot name, equal to min under `==` and distinct
		// under `math.Float32bits`.
		{"negative_zero", 0x80000000, 0x80000000, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			file := cfWrite(t, dir, c.name, cfOld, writeOne(c.bits))
			cfRead(t, cfOld, nil, readOne(file, c.want, c.clamped))
		})
	}
}

// ---- case 6: THE HOSTILE ROW UNDER A WIDENING ----------------------------
//
// Hypothesis (6). A value PAST THE OLD MAX in the old file, read by a NEW
// reader whose range contains it. The bounds pass is the READER's (§3.4, and
// the meaning row at docs/SPEC-TABLES.md's §20 table: "§4 clamps a load to the
// reader's bounds"), so 5.0 is inside [-10, 10] and lands WHOLE and uncounted
// — the new definition has made lawful what the old one would have clamped.
// That is the widening's stated price and not a finding; it is pinned here
// because a plan that carried the WRITER's range instead would clamp to 1.0.

func TestFixedCFloatPastOldMaxLandsUnderTheWidenedRange(t *testing.T) {
	dir := t.TempDir()
	const bits = 0x40A00000 // 5.0, past the OLD max of 1
	file := cfWrite(t, dir, "hostile", cfOld, writeOne(bits))
	cfRead(t, cfWideBoth, []string{cfOld}, readOne(file, bits, 0))
}

// ---- case 7: THE RESOLUTION IS IN THE FIXED LAYOUT HASH -------------------
//
// THE FINDING THAT MATTERED MOST, and it was not a value but an IDENTITY.
// When this red team was written, `ir.tableFixedDigestField` wrote `'R'` and
// then `FMin` and `FMax` for a compressed float and never the RESOLUTION, so
// refining `res` with the range held produced a byte-identical layout AND THE
// SAME LAYOUT HASH: to a fixed-form reader the two generations were one
// generation, while the lock refused the change by name and the cook digest
// carried `step=`. Two peers could agree on a fixed hash and disagree on the
// grid they would quantize that record onto at the message-form boundary.
//
// The ruling (#956, docs/FIXED-FORM-ALGORITHM.md, the digest table): the
// resolution goes into the definitions digest under `'Q'`, as the step's
// IEEE-754 bits beside the `'R'` its min and max went into. The float still
// rides whole (§3.4), so the RECORD does not move; the HASH does, and the lock,
// the wire and the cook digest now say the same thing. This test pins that.
func TestFixedCFloatResolutionIsInTheLayoutHash(t *testing.T) {
	of := func(src string) FixedLineageEntry {
		e, ok := FixedLineageOf(unitOf(t, src), "Reading")
		if !ok {
			t.Fatal("no lineage entry for Reading")
		}
		return e
	}
	old, fine := of(cfOld), of(cfFineRes)
	// The resolution moved 0.1 -> 0.03: the layout hash moved with it ('Q').
	if old.Wire == fine.Wire {
		t.Fatalf("the resolution is not in the layout hash: both %#016x", old.Wire)
	}
	// The float rides whole (§3.4): the record did not move.
	if old.Record != fine.Record {
		t.Fatalf("the record size moved on a resolution change: %d -> %d", old.Record, fine.Record)
	}
	// The range is in it too ('R'), and moves it independently of the step.
	wide := of(cfWideMin)
	if wide.Wire == old.Wire || wide.Wire == fine.Wire {
		t.Fatalf("the range is not in the layout hash: old %#016x fine %#016x wide %#016x", old.Wire, fine.Wire, wide.Wire)
	}
}
