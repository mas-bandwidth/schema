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
//  1. the contract is held in each target's own tier idiom, and nowhere else —
//     for Go, in BOTH of the tiers it folds the quantizer into: the per-field
//     object tier and the flat word codec
//  2. the release outcome is the same in all nine: in the seven debug-only
//     targets the saturating clamp's !>= form survives every build, so a
//     non-finite value that reaches it lands on a legal step index rather than
//     on an undefined float-to-unsigned cast; in the two every-build targets
//     nothing non-finite reaches the clamp at all, and what holds is that the
//     refusal stands before it
package compiler

import (
	"strings"
	"testing"
)

// compressedUnit is the smallest unit that reaches the case: one compressed
// float, and nothing else. It reaches Go's OBJECT TIER — one quantizer fold
// per field, emitWriteCompressedFold in internal/codegen/golang/functions.go.
const compressedUnit = `package cf

type T
{
    angle float32 | min = 0, max = 10, resolution = 0.01
}
`

// compressedFlatUnit is the same case at Go's FLAT TIER: two compressed floats
// pack into one stream call, and the quantizer is folded a second time inside
// the flat word codec (flatCompressedPiece in internal/codegen/golang/flat.go).
// TWO fields are the whole point. worthFlattening (flat.go) refuses a run of
// fewer than two pieces, because flattening one piece removes no stream call —
// so compressedUnit above reaches the flat tier not at all, and the flat
// tier's own copy of the refusal went unpinned until this unit existed. The
// two folds are separate code carrying the same contract; a gate that pins one
// says nothing about the other.
const compressedFlatUnit = `package cf

type T
{
    angle float32 | min = 0, max = 10, resolution = 0.01
    tilt  float32 | min = 0, max = 10, resolution = 0.01
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

		// Go folds the quantizer TWICE — once per field in the object tier,
		// once inside the flat word codec — and the refusal above is only the
		// object tier's. The flat tier's copy is pinned here, on a unit whose
		// run is worth flattening.
		if target == "go" {
			flat := generatedText(t, compressedFlatUnit, target)
			if !strings.Contains(flat, "stream.SerializeBits(&w0,") {
				t.Fatalf("go: the two-field unit no longer reaches the flat word codec — worthFlattening or the chunk policy moved, and this claim now pins the object tier twice")
			}
			for _, want := range []string{
				"if value.Angle-value.Angle != 0 {",
				"if value.Tilt-value.Tilt != 0 {",
				"return serialize.ErrValueOutOfRange",
			} {
				if !strings.Contains(flat, want) {
					t.Errorf("go: the FLAT tier does not hold the non-finite contract, %q not emitted", want)
				}
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

// CLAIM 2. The release outcome is the same in all nine, and for two different
// reasons that must not be confused.
//
// The SEVEN debug-only targets compile their assert out and still write, and
// what they write is defined, not unspecified: the quantizer's saturating clamp
// is written with its FIRST arm in the !>= form, so NaN lands on step 0 — min's
// index — instead of reaching the float-to-unsigned cast, whose result would be
// a garbage index. The form is normative for exactly that reason, so it is
// pinned as text wherever the generated code carries the clamp rather than
// calling into the runtime.
//
// Go and Elixir are NOT in that map, and pinning them there was the defect: it
// read their clamp as the thing that saves them, when nothing non-finite ever
// reaches it. Their refusal is every-build, so what has to hold for them is the
// ORDER — the refusal stands BEFORE the clamp. That is the only reading under
// which a release build of all nine agrees, and it is what is pinned for them.
func TestTheClampGroundsNonFiniteValuesOnALegalStep(t *testing.T) {
	// The debug-only targets whose generated text carries the clamp itself.
	// C, C++, C# and Rust call the runtime's audited entry point, where the
	// same form lives and their own test suites pin it.
	clampForm := map[string][]string{
		"java": {"if (!(n >= 0.0f)) {", "n = 0.0f;"},
		"dart": {"if (!(n >= 0.0)) {", "n = 0.0;"},
		"js":   {"if (!(n >= 0.0)) { n = 0.0; }"},
	}
	for target, wants := range clampForm {
		text := generatedText(t, compressedUnit, target)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the clamp is not in its !>= form, %q absent — a non-finite value in a release build would reach the cast", target, want)
			}
		}
		if everyBuild[target] {
			t.Errorf("%s refuses in every build, so its clamp is unreachable for a non-finite value; it belongs in refusesBeforeTheClamp, not here", target)
		}
	}

	// The two every-build targets. The clamp is still emitted — the bytes must
	// not depend on the tier — but it is not what holds the contract, so what
	// is pinned is that the refusal comes first.
	refusesBeforeTheClamp := map[string]struct{ refusal, clamp string }{
		"go":     {"if value.Angle-value.Angle != 0 {", "if !(normalizedValue >= 0) {"},
		"elixir": {`raise ArgumentError, "a compressed float writes a finite number"`, ":neg_inf -> 0.0"},
	}
	for target := range everyBuild {
		if _, ok := refusesBeforeTheClamp[target]; !ok {
			t.Errorf("target %q refuses in every build and has no order claim here", target)
		}
	}
	// both tiers: Go's object-tier fold and its flat word codec each emit the
	// refusal and the clamp, and each must emit them in that order.
	for _, src := range []string{compressedUnit, compressedFlatUnit} {
		for target, want := range refusesBeforeTheClamp {
			text := generatedText(t, src, target)
			ref := strings.Index(text, want.refusal)
			clamp := strings.Index(text, want.clamp)
			if ref < 0 {
				t.Errorf("%s: the every-build refusal is gone, %q absent", target, want.refusal)
				continue
			}
			if clamp < 0 {
				t.Errorf("%s: the clamp is gone, %q absent — the bytes must not depend on the tier", target, want.clamp)
				continue
			}
			if ref > clamp {
				t.Errorf("%s: the clamp at %d stands before the every-build refusal at %d — a non-finite value would be quantized to a legal step and written, where this target must refuse it", target, clamp, ref)
			}
		}
	}
}
