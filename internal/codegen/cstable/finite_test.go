package cstable

import (
	"strings"
	"testing"
)

// TestCsFiniteFloatRule pins the emitted shape of the finite rule
// (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go finite-only too,
// same rule as packets") on this leg: the refusal reason, the field's name on
// its descriptor, the bit-compare elision that lets a -0 ride under a +0
// default, the typed writer's debug assert, and the fixed form's finite pass on read
// and on write. The behaviour itself is held by the C# leg
// (test/cs-tables/src/FiniteChecks.cs) and its negative control
// (test/cs-tables/finite-negative-control).
func TestCsFiniteFloatRule(t *testing.T) {
	t.Parallel()
	src := generateCS(t, `package probe

fixed table T
{
    lead int32
    v    float32
    w    float64 = 1.5
    arr  [2]float32
}
`)
	for _, want := range []string{
		// The finite reason remains an enum member when later reasons are added.
		", non_finite_float,",
		// the descriptor names the field for the refusal
		`NonFiniteField = "T.v", NonFiniteReason = "non_finite_float: T.v"`,
		// elision compares BITS, so -0 rides under +0 and +0 under 1.5's bits
		"BitConverter.SingleToUInt32Bits(v.V) != 0x00000000u",
		"BitConverter.DoubleToUInt64Bits(v.W) != 0x3ff8000000000000ul",
		// the typed writer's check is a debug assert (docs/SPEC.md §5.1)
		"System.Diagnostics.Debug.Assert(float.IsFinite(v.V));",
		"System.Diagnostics.Debug.Assert(double.IsFinite(v.W));",
		// the fixed form's finite pass: a refusal on read, a debug assert on write
		"public static string TFixedFiniteBody(T value)",
		`if (!float.IsFinite(value.Arr[i])) return "T.arr";`,
		"string finite_why = TFixedFiniteBody(values[k]);",
		"System.Diagnostics.Debug.Assert(TFixedFiniteBody(values[k]) == null);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the emitted source does not carry %q", want)
		}
	}
	for _, retired := range []string{"WidenF32", "TableBitsToFloat((uint)raw) == TableBitsToFloat((uint)f.DefaultRaw)",
		// a release-build write refusal is not this leg's mechanism (docs/SPEC.md §5.1)
		"if (!float.IsFinite(v.V)) return false;", "if (TFixedFiniteBody(values[k]) != null) { return -1; }",
		"return !NonFiniteBits(f.Kind, f.GetRaw(value, i));"} {
		if strings.Contains(src, retired) {
			t.Errorf("the emitted source still carries the retired %q", retired)
		}
	}
	// A unit with no float emits no finite pass at all (§2.2's zero-cost rule).
	plain := generateCS(t, `package probe

fixed table T
{
    lead int32
}
`)
	if strings.Contains(plain, "FixedFiniteBody") {
		t.Error("a unit with no float emits a finite pass")
	}
}
