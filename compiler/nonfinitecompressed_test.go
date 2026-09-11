// A NON-FINITE VALUE AT A COMPRESSED FLOAT (SPEC §4.3, §5).
//
// A quantized float32 — one carrying min/max/resolution — has no bit pattern on
// the wire. It has a step index, and NaN and ±Inf name no step. Writing one is
// caller error exactly like an int outside its range or a count outside [A, B]
// (§4.6), so it takes §5's tiers with no exception: the seven debug-only
// targets assert it, and the two every-build targets refuse it in every build,
// because their languages have no idiom to compile out.
//
// Two claims are pinned here, one per test, so a regression in either is named
// on its own:
//
//  1. the contract is held in each target's own tier idiom, and nowhere else
//  2. the release outcome is the same in all nine: the saturating clamp's !>=
//     form survives every build, so a non-finite value that reaches it lands on
//     a legal step index rather than on an undefined float-to-unsigned cast
package compiler

import (
	"strings"
	"testing"
)

// compressedUnit is the smallest unit that reaches the case: one compressed
// float, and nothing else.
const compressedUnit = `package cf

type T
{
    angle float32 | min = 0, max = 10, resolution = 0.01
}
`

// holdsTheContract is the finiteness check each target emits, in its own tier
// idiom. The seven debug-only targets spell it as an assert; Go and Elixir
// spell it as the refusal their language makes native, and it is every-build.
// `x - x == 0` is the family's finiteness spelling wherever the language has
// no cheap predicate: NaN and both infinities fail it, since Inf - Inf is NaN.
var holdsTheContract = map[string][]string{
	// C and C++ hold it inside the runtime's precomputed entry point, which
	// every generated call site reaches — serialize_assert( serialize_float_is_finite( value ) )
	// in serialize.c, serialize_assert( value - value == 0.0f ) in serialize.h.
	// The generated text carries the call, and that is what is pinned here.
	"c":   {"serialize_write_compressed_float_precomputed( stream, value->angle,"},
	"cpp": {"serialize_compressed_float_precomputed( stream, compressed_value,"},
	// C# and Rust hold it the same way: QuantizeCompressedFloat's Debug.Assert
	// and serialize_compressed_float_precomputed's debug_assert!.
	"cs":   {"SerializeCompressedFloatPrecomputed(ref compressedValue,"},
	"rust": {"serialize_compressed_float_precomputed("},
	// Java, Dart and Elixir hold it in the generated text itself.
	"java":   {"assert value.angle - value.angle == 0.0f;"},
	"dart":   {"assert(x.isFinite);"},
	"elixir": {"raise ArgumentError", "a compressed float writes a finite number"},
	// Go refuses in every build, exactly as it refuses an int out of range.
	"go": {"if value.Angle-value.Angle != 0 {", "return serialize.ErrValueOutOfRange"},
	// js is not here: its debug-only idiom is not a statement but the
	// PRODUCTION/checked fork of the flat writer, checked on its own below.
	"js": nil,
}

// everyBuild names the two targets whose languages have no compile-out idiom.
// They are the only two allowed to refuse a non-finite value outright.
var everyBuild = map[string]bool{"go": true, "elixir": true}

// CLAIM 1. The contract is held in each target's own tier idiom. No target may
// be silent about it, and no debug-only target may hold it with a refusal a
// release build cannot drop.
func TestNonFiniteCompressedFloatIsHeldInEachTargetsTier(t *testing.T) {
	for _, target := range New().Targets() {
		wants, known := holdsTheContract[target]
		if !known {
			t.Errorf("target %q has no non-finite claim here. A new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, compressedUnit, target)

		if target == "js" {
			// the fork, not a statement: the checked flat writer refuses with
			// -1 and the production writer holds nothing
			const want = "!Number.isFinite(x)"
			if got := jsFlatWriter(t, text, "writeTFlatChecked"); !strings.Contains(got, want) {
				t.Errorf("js: the checked flat writer does not hold finiteness, %q absent", want)
			}
			if got := jsFlatWriter(t, text, "writeTFlatProduction"); strings.Contains(got, want) {
				t.Errorf("js: the PRODUCTION flat writer still holds finiteness: %q survives release", want)
			}
			continue
		}

		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the non-finite contract is not held, %q not emitted", target, want)
			}
		}

		// A debug-only target must not refuse outright: an every-build refusal
		// beside an assert removes nothing in release.
		if !everyBuild[target] {
			for _, gone := range []string{"ErrValueOutOfRange", "ValueOutOfRange"} {
				if strings.Contains(text, "angle") && strings.Contains(text, gone) {
					t.Errorf("%s: a compressed float carries an every-build refusal (%s) release cannot drop", target, gone)
				}
			}
		}
	}
}

// CLAIM 2. The release outcome is the same in all nine. The seven that compile
// their assert out still write, and what they write is defined, not
// unspecified: the quantizer's saturating clamp is written with its FIRST arm
// in the !>= form, so NaN lands on step 0 — min's index — instead of reaching
// the float-to-unsigned cast, whose result would be a garbage index. The form
// is normative for exactly that reason, so it is pinned as text wherever the
// generated code carries the clamp rather than calling into the runtime.
func TestTheClampGroundsNonFiniteValuesOnALegalStep(t *testing.T) {
	// The targets whose generated text carries the clamp itself. C, C++, C#
	// and Rust call the runtime's audited entry point, where the same form
	// lives and their own test suites pin it.
	clampForm := map[string][]string{
		"go":     {"if !(normalizedValue >= 0) {", "normalizedValue = 0"},
		"java":   {"if (!(n >= 0.0f)) {", "n = 0.0f;"},
		"dart":   {"if (!(n >= 0.0)) {", "n = 0.0;"},
		"js":     {"if (!(n >= 0.0)) { n = 0.0; }"},
		"elixir": {":neg_inf -> 0.0"},
	}
	for target, wants := range clampForm {
		text := generatedText(t, compressedUnit, target)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the clamp is not in its !>= form, %q absent — a non-finite value in a release build would reach the cast", target, want)
			}
		}
	}
}
