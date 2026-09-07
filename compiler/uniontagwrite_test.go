// A UNION TAG ON WRITE (SPEC §4.8): what each target does when the tag in
// storage names no arm.
//
// It had been argued that the tag is DISPATCH rather than a guard — an
// out-of-set tag selects no arm, so the switch's own `default` may refuse in
// every build without breaking §5's tiers. That argument is retired
// (2026-09-07): "consistency matters. writing packets correctness is the
// caller's responsibility, and it is our duty to catch it with asserts in
// debug." The tag in storage is a value the CALLER put there, exactly like a
// count past its bound, so it takes §5's tiers whole — debug-only wherever
// the language has the idiom, every-build only where it has none.
//
// The claim pinned here is the WRITE side. The read's refusal of a tag above
// the count is every-build in all nine and is not this gate's subject.
package compiler

import (
	"strings"
	"testing"
)

// unionTagUnit is the smallest unit that reaches the case: one union, and one
// struct holding it so the inlining backends (JavaScript's flat tier, Dart,
// Java) emit their field form too.
const unionTagUnit = `package tagunit

type Laser
{
    power [0..7]uint32
}

type Missile
{
    fuel [0..7]uint32
}

union Shot
{
    laser Laser
    missile Missile
}

type Volley
{
    shot Shot
}
`

// tagAssertsInDebug is the tag's DEBUG-ONLY form in each target that has one:
// the exact text the writer emits, in that target's own assert idiom.
var tagAssertsInDebug = map[string][]string{
	"c":    {"serialize_assert( value->type <= SHOT_TYPE_MAX );"},
	"cpp":  {"serialize_assert( value.type <= ShotType::Max );"},
	"cs":   {`Debug.Assert(tagValue <= 2, "the union tag is outside the variant set [0, 2]");`},
	"dart": {"assert(value.type >= 0);", "assert(value.type <= 2);"},
	"java": {"assert (value.type & 0xffL) >= 0;", "assert (value.type & 0xffL) <= 2;"},
	// js is not here: its debug-only idiom is the PRODUCTION/checked fork of
	// the flat writer, not a statement. Checked separately below.
}

// tagGoneFromRelease is the every-build refusal each assert target used to
// carry. An assert BESIDE a live refusal removes nothing, so none of it may
// survive.
var tagGoneFromRelease = map[string][]string{
	"c":   {"if ( value->type > SHOT_TYPE_MAX )", "not a ShotType value; nothing was written"},
	"cpp": {"not a ShotType value; nothing was written"},
	"cs":  {"if (tagValue > 2)"},
}

// tagRefusesEveryBuild is the write's UNCONDITIONAL tag refusal, for the
// targets that still hold it that way. Go and Elixir are here by the ruling —
// a language with no dormant-assert idiom keeps the form native to it. They
// are the only two left.
var tagRefusesEveryBuild = map[string][]string{
	"elixir": {`raise ArgumentError, "value.type is above the wire maximum"`},
	"go":     {"if tagValue < 0 || tagValue > 2 {"},
}

// CLAIM. The union tag is a write-side contract and nothing more: held by the
// target's own debug-only idiom where the language has one, refused in every
// build only where it has none, and needing no check at all in Rust, whose
// tag IS the enum discriminant.
func TestUnionTagOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom(t *testing.T) {
	for _, target := range New().Targets() {
		text := generatedText(t, unionTagUnit, target)

		switch {
		case target == "js":
			// the fork, not a statement: the contract lives in the checked
			// flat writer and the production writer is free of it
			const want = "value.Shot.Type < 0 || value.Shot.Type > 2"
			if got := jsFlatWriter(t, text, "writeVolleyFlatChecked"); !strings.Contains(got, want) {
				t.Errorf("js: the checked flat writer does not hold the tag, %q absent", want)
			}
			if got := jsFlatWriter(t, text, "writeVolleyFlatProduction"); strings.Contains(got, want) {
				t.Errorf("js: the PRODUCTION flat writer still holds the tag: %q survives release", want)
			}

		case target == "rust":
			// an out-of-set tag cannot be BUILT: Shot is a Rust enum and the
			// tag is its discriminant, so write_shot is a total match with
			// nothing to assert
			body := rustFn(t, text, "write_shot")
			if strings.Contains(body, "assert") {
				t.Errorf("rust: write_shot carries a tag assert, and the enum already makes an out-of-set tag unrepresentable:\n%s", body)
			}

		case tagAssertsInDebug[target] != nil:
			for _, want := range tagAssertsInDebug[target] {
				if !strings.Contains(text, want) {
					t.Errorf("%s: the tag is not held by a debug assert, %q not emitted", target, want)
				}
			}
			for _, gone := range tagGoneFromRelease[target] {
				if strings.Contains(text, gone) {
					t.Errorf("%s: the tag still carries an every-build refusal that release cannot drop: %s", target, gone)
				}
			}

		case tagRefusesEveryBuild[target] != nil:
			for _, want := range tagRefusesEveryBuild[target] {
				if !strings.Contains(text, want) {
					t.Errorf("%s: the tag's every-build refusal is gone, %q not emitted. If that backend moved to an assert, move it here", target, want)
				}
			}

		default:
			t.Errorf("target %q has no union-tag claim here. A new backend landed and this gate was not told", target)
		}
	}
}

// rustFn slices one named Rust function out of the generated text, so a claim
// about write_shot is not answered by an assert somewhere else in the file.
func rustFn(t *testing.T, text, fn string) string {
	t.Helper()
	i := strings.Index(text, "pub fn "+fn+"(")
	if i < 0 {
		t.Fatalf("rust: %s is not emitted", fn)
	}
	rest := text[i+1:]
	if before, _, ok := strings.Cut(rest, "\npub fn "); ok {
		return before
	}
	return rest
}
