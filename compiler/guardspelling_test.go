// THE GUARD SPELLING (SPEC §4.5): one spelling for the branch condition a
// reader meets in a storage comment and the branch condition a table-JSON
// walker parses at runtime.
//
// Generated storage marks each branch field with the guard that puts it on the
// wire. The comment spells that guard the way the reflection descriptors spell
// it, the way each table backend's guardWalk builds it: "at_rest", "!at_rest",
// "active && has_target". Nesting joins with " && " and the else side negates
// the condition, so both sides of a branch read as the boolean expression a
// reader can evaluate. A reader who compares a storage comment against a
// descriptor sees one language.
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

// guardComment is the comment lead each target writes for a guarded field.
// It is the text the guard is pasted in front of.
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
			t.Errorf("target %q has no guard-comment claim here. A new backend landed and this gate was not told", target)
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
