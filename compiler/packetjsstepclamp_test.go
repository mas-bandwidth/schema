// THE COMPRESSED-FLOAT INTEGER CLAMP, on the JavaScript leg's two backends
// (SPEC §4.3; serialize#88, schema#109).
//
// The two JS backends derive the step count differently — generated/js/Wire.js
// passes the raw (min, max, resolution) triple and serialize.js derives the
// count at runtime; generated/js/WireFlat.js folds the count at GENERATION
// time, in float32. The derivations agree, because both are the same chain:
// fround(delta/resolution), clamped to [1, 4294967040], ceil. What did NOT
// agree was the clamp AFTER the floor.
//
// Once the step count reaches 2^23 the float32 ulp at the top of the range is
// 1, so the rounded sum can land one PAST the count: with 8388609 steps and
// the value at max, fround(8388609 + 0.5) is 8388610 under round-to-even, and
// the floor keeps it. The runtime tier clamps it back; the flat tier did not,
// so the flat tier wrote 0x800002 where the runtime tier wrote 0x800001 — a
// code the field's OWN reader rejects, on a wire two tiers of one language are
// required to agree on byte for byte.
//
// No byte moves for any declaration whose count is outside [2^23, 2^24), which
// is every schema in the corpus — which is why every gate was green.
package compiler

import (
	"strings"
	"testing"
)

// oddStepCount is the smallest unit that reaches the case: 8388609 steps, an
// ODD count in [2^23, 2^24), so the value at max rounds up past it.
const oddStepCount = `package stepclamp

type Probe
{
    v float32 | min = 0, max = 8388609, resolution = 1
}
`

// smallStepCount is the neighbour: a count below 2^23, where the ulp is under
// 1 and the clamp is unreachable. It must stay unemitted, so the fix costs no
// instruction on the declarations people actually write.
const smallStepCount = `package stepclamp

type Probe
{
    v float32 | min = 0, max = 10, resolution = 0.01
}
`

func TestJSFlatTierClampsTheStepCountAtTheTop(t *testing.T) {
	u := unitFromSource(t, oddStepCount)
	files, err := New().Generate(u, "js", Options{})
	if err != nil {
		t.Fatalf("js refused the unit: %v", err)
	}
	flat, ok := files["ProbeFlat.js"]
	if !ok {
		var names []string
		for name := range files {
			names = append(names, name)
		}
		t.Fatalf("no flat-tier file among %s", strings.Join(names, ", "))
	}
	// the clamp, in the spelling the emitter writes it
	const clamp = "if (v > 8388609) { v = 8388609; }"
	// once per write body — the flat tier emits a production write and a
	// checked write, and the clamp is part of the WIRE, not of the checking
	if n := strings.Count(string(flat), clamp); n != 2 {
		t.Errorf("the flat tier carries the integer clamp %d times, want 2 (both write bodies):\n%s", n, flat)
	}
	// and the runtime tier gets it from serialize.js, so it passes the triple
	// through untouched — the two tiers must not BOTH fold it
	runtime, ok := files["Probe.js"]
	if !ok {
		t.Fatal("no runtime-tier file")
	}
	if !strings.Contains(string(runtime), "serializeCompressedFloat(NUMBER_SCRATCH, 0.0, 8.388609e+06, 1.0)") {
		t.Errorf("the runtime tier no longer passes the raw triple:\n%s", runtime)
	}
}

func TestJSFlatTierEmitsNoClampBelowTwoToTheTwentyThree(t *testing.T) {
	u := unitFromSource(t, smallStepCount)
	files, err := New().Generate(u, "js", Options{})
	if err != nil {
		t.Fatalf("js refused the unit: %v", err)
	}
	flat := string(files["ProbeFlat.js"])
	// the WRITE-side clamp, not the read side's range refusal (which every
	// count carries and which reads `if (v > 1000) { return false; }`)
	if strings.Contains(flat, "{ v = 1000; }") {
		t.Errorf("a 1000-step declaration pays for an unreachable write clamp:\n%s", flat)
	}
}
