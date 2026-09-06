// THE GUARD SPELLING (SPEC §4.5): one spelling for the branch condition a
// reader meets in a storage comment and the branch condition a table-JSON
// walker parses at runtime.
//
// Generated storage marks each branch field with the guard that puts it on the
// wire. The comment used to spell it "if on_radar", nesting with " / " and
// negating with a trailing " else", so the else side of a branch read "if
// on_radar else" — which is not a sentence, and says the opposite of what a
// beginner parses on first read (#447 F-12). The reflection descriptors always
// carried the right spelling, built by each table backend's guardWalk:
// "at_rest", "!at_rest", "active && has_target". The comment now spells it the
// same way, so a reader who compares the two sees one language.
package compiler

import (
	"strings"
	"testing"
)

// guardedUnit nests three branches and takes both sides of the innermost, so
// the negation, the conjunction and the else side are all in the output.
const guardedUnit = `package guarded

type A
{
    at_rest bool
    if !at_rest
    {
        active bool
        if active
        {
            has_target bool
            if has_target
            {
                target uint16
            }
            else
            {
                idle uint8
            }
        }
    }
    else
    {
        anchor uint16
    }
}
`

// guardComment is the comment lead each target writes for a guarded field —
// the text the guard is pasted in front of.
var guardComment = map[string]string{
	"c":      "", // C emits no branch storage comment
	"cpp":    " — wire branch",
	"cs":     " — wire branch",
	"dart":   " — wire branch",
	"elixir": " — wire branch",
	"go":     " — wire branch",
	"java":   " — wire branch",
	"js":     " — wire branch",
	"rust":   " — wire branch",
}

func TestGuardCommentsSpellTheConditionTheWayDescriptorsDo(t *testing.T) {
	// the five guards this unit produces, in the descriptors' own spelling
	want := []string{
		"!at_rest",
		"!at_rest && active",
		"!at_rest && active && has_target",
		"!at_rest && active && !has_target",
		"at_rest",
	}
	for _, target := range New().Targets() {
		lead, known := guardComment[target]
		if !known {
			t.Errorf("target %q has no guard-comment claim here — a new backend landed and this gate was not told", target)
			continue
		}
		text := generatedText(t, guardedUnit, target)
		// the stale spelling is gone everywhere, comment or descriptor
		for _, stale := range []string{"if at_rest", "if !at_rest", " else —", " / if "} {
			if strings.Contains(text, stale) {
				t.Errorf("%s: the stale guard spelling survives: %q", target, stale)
			}
		}
		if lead == "" {
			continue
		}
		for _, guard := range want {
			if !strings.Contains(text, guard+lead) {
				t.Errorf("%s: no branch storage comment spells the guard %q", target, guard)
			}
		}
	}
}
