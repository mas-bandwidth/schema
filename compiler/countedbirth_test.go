// A COUNTED ARRAY'S COUNT BOUND (SPEC §4.6): where a fresh value's count
// starts, and what the writer does with a count outside the bound.
//
// SPEC §4.6 refuses a scalar or element range that excludes zero, because zero
// initialization is the rule and such a field would be born outside its own
// range. A [A..B] count bound with A above zero is the stated exception. It is
// legal, and the count is born at A rather than at 0, because the bound names
// the one wire-legal count a fresh value can carry and an array takes no
// specified default to name another.
//
// What the write does with a count outside the bound is a §5 tier question,
// settled 2026-09-07: "No runtime should ever promise to keep checks in
// writing packets (asserts) in release build. Removing them is the whole
// point. ... checks are *DEBUG ONLY*". The count is a writer contract like any
// other scalar range — it is caught by an assert in debug and gone in release,
// in every target whose language HAS that idiom. Go and Elixir have none, so
// they keep the every-build refusal their languages make native.
//
// Two claims are pinned here, one per test, so a regression in either is named
// on its own:
//
//  1. the constructed form is born at the declared minimum, in all nine
//  2. the write path holds the count in its target's own debug-only idiom
//     where the language has one, and refuses in every build where it does not
package compiler

import (
	"strings"
	"testing"
)

// countedAboveZero is the smallest unit that reaches the case: one counted
// array whose count bound starts above zero, and nothing else.
const countedAboveZero = `package born

type T
{
    window [2..8]uint32
}
`

// countName is the count's spelling in each target's storage. It is the
// identifier every claim below is made about.
var countName = map[string]string{
	"c":      "window_count",
	"cpp":    "window_count",
	"cs":     "WindowCount",
	"dart":   "windowCount",
	"elixir": "window", // the list IS the count: length(value.window)
	"go":     "WindowCount",
	"java":   "windowCount",
	"js":     "WindowCount",
	"rust":   "window_count",
}

// bornAtMinimum is the text each target's CONSTRUCTED form carries when the
// count is born at its declared minimum. The §5 zero form is deliberately not
// here: it is all-zero by rule in every target, and a specified default does
// not reach it either.
var bornAtMinimum = map[string]string{
	"c":      "value.window_count = 2;",
	"cpp":    "int32_t window_count = 2;",
	"cs":     "public int WindowCount = 2;",
	"dart":   "int windowCount = 2;",
	"elixir": "defstruct window: List.duplicate(0, 2)",
	"go":     "value.WindowCount = 2",
	"java":   "public int windowCount = 2;",
	"js":     "this.WindowCount = 2;",
	"rust":   "value.window_count = 2;",
}

// refusesEveryBuild is the write path's UNCONDITIONAL range refusal, for the
// targets that still hold the count that way. Go and Elixir are here by the
// ruling — "For each language, do not force this in. If the language simply
// doesn't have this concept (Golang) then it is not something we can do" — an
// error-returning runtime in Go, an always-on raise in Elixir. C# is here only
// until its own emitter change lands; it moves to assertsInDebug then,
// alongside every other write check in that backend.
var refusesEveryBuild = map[string][]string{
	"cs":     {"if (value.WindowCount < 2 || value.WindowCount > 8)"},
	"elixir": {"if n < 2 do", "if n > 8 do", "raise ArgumentError"},
	"go":     {"if value.WindowCount < 2 || value.WindowCount > 8 {", "return serialize.ErrValueOutOfRange"},
}

// assertsInDebug is the count's DEBUG-ONLY form in each target that has one:
// the exact text the writer emits, in that target's own assert idiom.
var assertsInDebug = map[string][]string{
	"c":    {"serialize_assert( value->window_count >= 2 && value->window_count <= 8 );"},
	"cpp":  {"serialize_assert( int32_t( value.window_count ) >= int32_t( 2 ) && int32_t( value.window_count ) <= int32_t( 8 ) );"},
	"dart": {"assert(value.windowCount >= 2);", "assert(value.windowCount <= 8);"},
	"java": {"assert value.windowCount >= 2;", "assert value.windowCount <= 8;"},
	// js is not here: its debug-only idiom is not an assert statement but the
	// PRODUCTION/checked fork of the flat writer, checked separately below.
	"rust": {`debug_assert!(value.window_count >= 2 && value.window_count <= 8, "window_count out of range [2, 8]");`},
}

// goneFromRelease is the every-build refusal each assert target used to carry.
// None of it may survive: an assert BESIDE a live refusal removes nothing.
var goneFromRelease = map[string][]string{
	"c": {"if ( value->window_count < 2 || value->window_count > 8 )"},
	"cpp": {
		"if ( int32_t( value.window_count ) < int32_t( 2 ) || int32_t( value.window_count ) > int32_t( 8 ) )",
	},
	"dart": {"if (value.windowCount < 2 || value.windowCount > 8) {"},
	"java": {"if (value.windowCount < 2 || value.windowCount > 8) {"},
	"rust": {"if value.window_count < 2 || value.window_count > 8 {"},
}

// assertToken is the build-removable predicate each target spells its writer
// contracts with. For a refusesEveryBuild target, none of them may reach the
// count.
var assertToken = map[string][]string{
	"cs":     {"Debug.Assert"},
	"elixir": nil, // the BEAM has no compile-out assert, so the raise is always on
	"go":     nil, // the runtime returns an error, in every build
}

// generatedText renders every file the target emits for the unit, joined, so a
// claim can be made about the target's whole output rather than one file name.
func generatedText(t *testing.T, src, target string) string {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), target, Options{})
	if err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	var all strings.Builder
	for _, body := range files {
		all.Write(body)
		all.WriteByte('\n')
	}
	return all.String()
}

// jsFlatWriter slices one named function out of the JavaScript flat codec.
// JavaScript's debug-only idiom is the emitted fork, not a statement: the
// checked writer holds the contract and the production writer holds none, and
// `PRODUCTION ? production : checked` picks between them at import time.
func jsFlatWriter(t *testing.T, text, fn string) string {
	t.Helper()
	i := strings.Index(text, "function "+fn+"(")
	if i < 0 {
		t.Fatalf("js: %s is not emitted; the flat writer fork is gone", fn)
	}
	rest := text[i+1:]
	if before, _, ok := strings.Cut(rest, "\nfunction "); ok {
		return before
	}
	return rest
}

// CLAIM 1. A [A..B] count is born at A in every target: the one wire-legal
// count a fresh value can carry, since an array takes no specified default to
// name another one.
func TestCountedArrayIsBornAtItsDeclaredMinimum(t *testing.T) {
	for _, target := range New().Targets() {
		want, known := bornAtMinimum[target]
		if !known {
			t.Errorf("target %q has no birth claim here. A new backend landed and this gate was not told", target)
			continue
		}
		if text := generatedText(t, countedAboveZero, target); !strings.Contains(text, want) {
			t.Errorf("%s: a [2..8] count is not born at 2, %q is absent from the constructed form", target, want)
		}
	}
}

// CLAIM 2. The count is a write-side contract and nothing more. Where the
// language has a debug-only idiom the writer holds it there and release
// carries nothing; where the language has none the writer refuses in every
// build, because forcing an idiom the language lacks would not be native.
func TestCountOnWriteIsDebugOnlyWhereTheLanguageHasThatIdiom(t *testing.T) {
	for _, target := range New().Targets() {
		text := generatedText(t, countedAboveZero, target)

		if target == "js" {
			// the fork, not a statement: the contract lives in the checked
			// writer and the production writer is free of it
			const want = "value.WindowCount < 2 || value.WindowCount > 8"
			if got := jsFlatWriter(t, text, "writeTFlatChecked"); !strings.Contains(got, want) {
				t.Errorf("js: the checked flat writer does not hold the count, %q absent", want)
			}
			if got := jsFlatWriter(t, text, "writeTFlatProduction"); strings.Contains(got, want) {
				t.Errorf("js: the PRODUCTION flat writer still holds the count: %q survives release", want)
			}
			continue
		}

		if wants, ok := assertsInDebug[target]; ok {
			for _, want := range wants {
				if !strings.Contains(text, want) {
					t.Errorf("%s: the count is not held by a debug assert, %q not emitted", target, want)
				}
			}
			for _, gone := range goneFromRelease[target] {
				if strings.Contains(text, gone) {
					t.Errorf("%s: the count still carries an every-build refusal that release cannot drop: %s", target, gone)
				}
			}
			continue
		}

		wants, known := refusesEveryBuild[target]
		if !known {
			t.Errorf("target %q has no count claim here. A new backend landed and this gate was not told", target)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the count's range refusal is absent, %q not emitted", target, want)
			}
		}
		// and it is not an assert: no line naming the count may carry the
		// target's build-removable predicate.
		for line := range strings.SplitSeq(text, "\n") {
			if !strings.Contains(line, countName[target]) {
				continue
			}
			for _, token := range assertToken[target] {
				if strings.Contains(line, token) {
					t.Errorf("%s: the count is held by %s, which the build can remove: %s", target, token, strings.TrimSpace(line))
				}
			}
		}
	}
}
