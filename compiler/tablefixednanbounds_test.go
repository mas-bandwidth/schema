// A NaN IN A BOUNDED FLOAT LANDS `min` AND COUNTS ONE (docs/SPEC-TABLES.md
// §3.4, docs/FIXED-FORM-ALGORITHM.md §4.6 and §5.4).
//
// IEEE says every ordered comparison against a NaN is false, so the integer
// clamp's shape — `v < lo` then `v > hi` — is BOTH false for a NaN and lets it
// land whole, counting nothing: a value outside the declared range reaching the
// consumer, which is the one thing the bounds pass exists to stop. -inf clamps
// and counts correctly under the same code, which is what made this quiet.
//
// THE FIX IS THE LOW TEST, on every leg that clamps a float range: `!(v >= lo)`
// is true for a NaN and for every value below the minimum, and FALSE for -0.0
// against a min of +0.0, which compares EQUAL and is therefore in range and
// lands as written, sign bit and all.
package compiler

import (
	"strings"
	"testing"
)

// A BOUNDED FLOAT IS A COMPRESSED FLOAT on this wire — min, max and resolution
// are all three together (SPEC §4.6) — and in the fixed form it rides as the
// float it is, so this one field is the whole case.
const nanBoundedSrc = `package probe

fixed table Cfg
{
    lead  uint32 = 1
    v     float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.001
    trail uint32 = 2
}
`

func nanBoundedFile(t *testing.T, target, name string) string {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, nanBoundedSrc), target, Options{})
	if err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	src, ok := files[name]
	if !ok {
		t.Fatalf("the %s target emitted no %s", target, name)
	}
	return string(src)
}

// nanBoundedNoNakedLow is the rule stated as a REFUSAL: a bounds pass that
// still spells the low end as a plain `<` against the minimum is a pass a NaN
// walks through.
func nanBoundedNoNakedLow(t *testing.T, leg, body string, naked []string, want ...string) {
	t.Helper()
	for _, bad := range naked {
		if strings.Contains(body, bad) {
			t.Errorf("%s: the low end is still a naked %q, which a NaN passes", leg, bad)
		}
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("%s: the NaN-aware low test %q is missing from the bounds pass", leg, w)
		}
	}
}

func TestCppFixedFloatNaNLandsMinAndCounts(t *testing.T) {
	src := nanBoundedFile(t, "cpp", "ProbeTable.h")
	body := extractFn(src, "CfgFixedClampBody")
	if body == "" {
		t.Fatal("the reference emitted no bounds pass for a bounded float")
	}
	nanBoundedNoNakedLow(t, "cpp", body,
		[]string{"value.v < 0.0f"},
		"!( value.v >= 0.0f )",
		"clamped += (int) ( !( value.v >= 0.0f ) ) | (int) ( value.v > 1.0f );")
	// AND THE HIGH END IS UNTOUCHED: a NaN is already caught by the low test,
	// so `> hi` stays the plain comparison it was.
	if !strings.Contains(body, "value.v > 1.0f") {
		t.Error("cpp: the high end must still be the plain comparison")
	}
}

// THE C TWIN IS THE SAME TEXT (tools/fixedtwin), so it is the same assertion
// with the leg's own spelling of the value and the counter.
func TestCFixedFloatNaNLandsMinAndCounts(t *testing.T) {
	src := nanBoundedFile(t, "c", "ProbeTable.h")
	body := extractFn(src, "cfg_fixed_clamp_body_")
	if body == "" {
		t.Fatal("the C twin emitted no bounds pass for a bounded float")
	}
	nanBoundedNoNakedLow(t, "c", body,
		[]string{"value->v < 0.0f"},
		"!( value->v >= 0.0f )",
		"(*clamped) += (int) ( !( value->v >= 0.0f ) ) | (int) ( value->v > 1.0f );")
}

func TestGoFixedFloatNaNLandsMinAndCounts(t *testing.T) {
	src := nanBoundedFile(t, "go", "ProbeTable.go")
	if !strings.Contains(src, "if !(value.V >= 0) {") && !strings.Contains(src, "if !(value.V >= 0.0) {") {
		t.Errorf("go: the NaN-aware low test is missing from the bounds pass")
	}
	if strings.Contains(src, "if value.V < 0 {") || strings.Contains(src, "if value.V < 0.0 {") {
		t.Errorf("go: the low end is still a naked `<`, which a NaN passes")
	}
}

func TestRustFixedFloatNaNLandsMinAndCounts(t *testing.T) {
	src := nanBoundedFile(t, "rust", "probe_fixed.rs")
	if !strings.Contains(src, "!(value.v >= 0") {
		t.Error("rust: the NaN-aware low test is missing from the bounds pass")
	}
	// f32::clamp PROPAGATES a NaN rather than pinning it, so the intrinsic is
	// exactly what this field may not use.
	if strings.Contains(src, "value.v = value.v.clamp(") {
		t.Error("rust: f32::clamp propagates a NaN and must not hold a float range")
	}
}
