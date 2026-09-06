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
// The write refuses a count outside the bound in every build. The count guards
// the element loop and the pack subtracts the low bound, so a count below A
// wraps and an unchecked write would report success on bytes no reader
// accepts. It is the one write-side contract that is never a debug-only
// assert.
//
// Two claims are pinned here, one per test, so a regression in either is named
// on its own:
//
//  1. the constructed form is born at the declared minimum, in all nine
//  2. the write path refuses a count outside the bound in EVERY build mode, in
//     all nine, never through an assert the build can remove
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

// refusesEveryBuild is the write path's unconditional range refusal in each
// target's own spelling. Every target refuses through its own convention, an
// error-returning runtime in Go, Rust and C#, an always-on raise in Elixir,
// and a plain failure return in C, C++, Dart, Java and JavaScript.
var refusesEveryBuild = map[string][]string{
	"c": {"if ( value->window_count < 2 || value->window_count > 8 )", "return 0;"},
	"cpp": {
		"if ( int32_t( value.window_count ) < int32_t( 2 ) || int32_t( value.window_count ) > int32_t( 8 ) )",
		"return false;",
	},
	"cs":     {"if (value.WindowCount < 2 || value.WindowCount > 8)"},
	"dart":   {"if (value.windowCount < 2 || value.windowCount > 8) {", "return -1;"},
	"elixir": {"if n < 2 do", "if n > 8 do", "raise ArgumentError"},
	"go":     {"if value.WindowCount < 2 || value.WindowCount > 8 {", "return serialize.ErrValueOutOfRange"},
	"java":   {"if (value.windowCount < 2 || value.windowCount > 8) {", "return -1;"},
	"js":     {"if (value.WindowCount < 2 || value.WindowCount > 8) {", "return -1;"},
	"rust":   {"if value.window_count < 2 || value.window_count > 8 {", "return Err("},
}

// assertToken is the build-removable predicate each target spells its writer
// contracts with. None of them may reach the count.
var assertToken = map[string][]string{
	"c":      {"serialize_assert"},
	"cpp":    {"serialize_assert"},
	"cs":     {"Debug.Assert"},
	"dart":   {"assert("},
	"elixir": nil, // the BEAM has no compile-out assert, so the raise is always on
	"go":     nil, // the runtime returns an error, in every build
	"java":   {"assert "},
	"js":     {"assert("},
	"rust":   {"debug_assert", "assert!"},
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

// CLAIM 2. The write path refuses a count outside its bound in every build
// mode. The count guards the element loop and the pack subtracts the low
// bound, so a count below the minimum wraps, and a build that drops the check
// would write bytes no reader accepts and report success.
func TestCountOutsideItsWireRangeIsRefusedInEveryBuild(t *testing.T) {
	for _, target := range New().Targets() {
		wants, known := refusesEveryBuild[target]
		if !known {
			t.Errorf("target %q has no refusal claim here. A new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, countedAboveZero, target)
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
