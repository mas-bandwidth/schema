package cstable

import (
	"strings"
	"testing"
)

// TestCsFiniteFloatRule pins the emitted shape of the finite rule
// (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go finite-only too,
// same rule as packets") on this leg: the refusal reason, the field's name on
// its descriptor, the bit-compare elision that lets a -0 ride under a +0
// default, the typed writer's refusal, and the fixed form's finite pass on read
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
		// the reason, appended to the C# enum (its order is its own)
		"layout_record_too_large, non_finite_float }",
		// the descriptor names the field for the refusal
		`NonFiniteField = "T.v", NonFiniteReason = "non_finite_float: T.v"`,
		// elision compares BITS, so -0 rides under +0 and +0 under 1.5's bits
		"BitConverter.SingleToUInt32Bits(v.V) != 0x00000000u",
		"BitConverter.DoubleToUInt64Bits(v.W) != 0x3ff8000000000000ul",
		// the typed writer refuses before collect, so measure and save answer -1
		"if (!float.IsFinite(v.V)) return false;",
		"if (!double.IsFinite(v.W)) return false;",
		// the fixed form's finite pass, on read and on write
		"public static string TFixedFiniteBody(T value)",
		`if (!float.IsFinite(value.Arr[i])) return "T.arr";`,
		"string finite_why = TFixedFiniteBody(values[k]);",
		"if (TFixedFiniteBody(values[k]) != null) { return -1; }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the emitted source does not carry %q", want)
		}
	}
	for _, retired := range []string{"WidenF32", "TableBitsToFloat((uint)raw) == TableBitsToFloat((uint)f.DefaultRaw)"} {
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
