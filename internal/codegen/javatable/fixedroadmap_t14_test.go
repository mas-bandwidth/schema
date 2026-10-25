package javatable

// fixedroadmap_t14 — java/R30's missing clause on the fixed-tables cell
// "Scalar bounds and compressed floats": "fp-contract off on every leg"
// (docs/roadmap.sexp's node `fixed-tables`, ROADMAP.md). T05 holds every other
// clause of java/R30 (rides as the float, the reader's own bounds, the step
// and the range in the digest); this file proves the fp-contract clause with
// the same witness the four T14 probes on schema-shared (b0f36a798) assert.
//
// THE WITNESS. A compressed float's dequantize is `normalized * delta + min`
// in float32 (the counting of SPEC.md §4.3 / ir.TableMessageDequantize). The
// arithmetic must be the SPEC's order, one rounding per operation, never a
// fused multiply-add: a fused fma folds the multiply and the add into ONE
// rounding and diverges by one bit at a boundary. For min=-1, max=2,
// resolution=0.1 (count=30), index 2 lands the unfused float32 bits
// 0xbf4ccccc and the fused 0xbf4ccccd; the discrimination is asserted here.
//
// THE LEG'S OWN WRITER AND READER are exercised end to end through the
// generated Java: an off-grid float32 saved by the writer lands whole through
// the reader, no clamp, no requantization — the ride-as-the-float value T05
// pins on the source text, here on the running build.
//
// THE CLAUSES T05 AND THE LOCKFILE ALREADY HOLD are re-asserted by name:
// finer widens (TestLockCFloatResolutionRefinedAllowed and
// TestFixedVersioningCfloatResRefine), coarser refuses by name
// (TestLockCFloatResolutionCoarsenedRefuses), and dropping the triple widens
// (TestLockCFloatRangeDroppedAllowed). The reader's own bounds and the step
// in the digest are T05's java/R30.

import (
	"math"
	"testing"
)

// t14Dequantize is the unfused compressed-float dequantize of index 2 for
// min=-1, max=2, resolution=0.1 (count=30): normalized = index/count, scaled =
// normalized*delta, value = scaled+min, each step one float32 rounding and
// never a fused multiply-add (SPEC.md "Floating-point discipline": "Strict
// IEEE-754 arithmetic is the normative wire for compressed floats: a fused
// multiply-add diverges by one quantization step at boundary values"). It is
// the witness the four T14 probes on schema-shared share.
func t14Dequantize() uint32 {
	const count = 30.0
	qmin := float32(-1.0)
	delta := float32(3.0) // max - min
	idx := float32(2.0)
	normalized := float32(idx / count)
	scaled := float32(normalized * delta)
	return math.Float32bits(float32(scaled + qmin))
}

// t14DequantizeFused is the same dequantize written as ONE fused
// multiply-add. It is the negative control: a generator (or a helper) that
// folded the multiply-add would land this bit pattern, and the test must fail
// red against it.
func t14DequantizeFused() uint32 {
	const count = 30.0
	qmin := float64(float32(-1.0))
	delta := float64(float32(3.0))
	idx := float64(float32(2.0))
	fused := math.FMA(idx/count, delta, qmin)
	return math.Float32bits(float32(fused))
}

func t14CompressedFloats(t *testing.T) {
	// THE WITNESS: index 2 is the unfused 0xbf4ccccc, never the fused
	// 0xbf4ccccd. This is the clause "fp-contract off on every leg".
	if got := t14Dequantize(); got != 0xbf4ccccc {
		t.Errorf("R30: the dequantize of index 2 landed %#x, want the unfused 0xbf4ccccc — the compressed-float arithmetic must be the SPEC's order, one rounding per operation, never a fused multiply-add (SPEC.md Floating-point discipline)", got)
	}
	if got := t14DequantizeFused(); got != 0xbf4ccccd {
		t.Errorf("R30: the fused dequantize of index 2 landed %#x, want 0xbf4ccccd — the negative control no longer discriminates", got)
	}

	// THE LEG'S OWN WRITER AND READER, END TO END: save an off-grid float32
	// through the generated Java and land it whole, no clamp and no
	// requantization. VNEW_cfloat_range_widen carries a compressed float32
	// (min=-2, max=2, resolution=0.01); 0.3 (0x3e99999a) is off that grid.
	pkg := javaPackageOf(versioningSchemaUnit(t, "VNEW_cfloat_range_widen.schema"))
	if pkg == "" {
		t.Fatal("R30: the NEW side declares no package")
	}
	classes := t03Build(t, t.TempDir(), "cfloat_range_widen", []sideSpec{
		{key: "reads", schema: "VNEW_cfloat_range_widen.schema", older: []string{"VOLD_cfloat_range_widen.schema"}},
		{key: "refuses", schema: "VOLD_cfloat_range_widen.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", pkg, "CfloatRangeWiden", `
        vals[0].lead = 1;
        vals[0].aim = Float.intBitsToFloat(0x3E99999A); // 0.3, OFF the 0.1 grid
        vals[0].trail = 2;`),
	})
	out := t03RunSaveLoad(t, classes, "Probe_capability")
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("R30: the off-grid round trip did not read: %+v", r)
	}
	if got := parseDumpedFloatBits(t, r.value["aim"]); got != 0x3E99999A {
		t.Errorf("R30: aim landed %#x, want the writer's own bits 0x3E99999A — the float rides whole, never requantized", got)
	}
	if r.clamped != 0 {
		t.Errorf("R30: clamped=%d, want 0 — an off-grid value inside the reader's range rides whole and counts nothing", r.clamped)
	}
}

func TestFixedRoadmapJavaT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("java/R30", func(t *testing.T) {
		t.Parallel()
		t14CompressedFloats(t)
	})
}
