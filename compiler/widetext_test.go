// Wide text is retired. Kind 33 is not emitted. The named refusal is
// TestWstringKind33Retired.
package compiler

import (
	"strings"
	"testing"
)

const wideTextInATableClosure = `package wide

type Text
{
    name wstring(4)
}

fixed table Root
{
    t     Text
    title wstring(6)
}
`

func TestWideTextInATableClosureIsRefused(t *testing.T) {
	u := unitFromSource(t, wideTextInATableClosure)
	for _, target := range New().Targets() {
		_, err := New().Generate(u, target, nil)
		if err == nil || !strings.Contains(err.Error(), wstringRetired) {
			t.Errorf("%s: %v", target, err)
		}
	}
}
