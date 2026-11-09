package compiler

import (
	"strings"
	"testing"
)

// TestWstringKind33Retired is the refusal: kind 33 is retired, and every
// registered target names that with wstring retired (Glenn, 2026-10-31).
func TestWstringKind33Retired(t *testing.T) {
	const src = `package wide

type Text
{
    name wstring(4)
}

union Choice
{
    item wstring(2)
}

table Root
{
    title wstring(6)
    choice Choice
}
`
	u := unitFromSource(t, src)
	targets := New().Targets()
	if len(targets) == 0 {
		t.Fatal("no targets registered")
	}
	for _, target := range targets {
		_, err := New().Generate(u, target, nil)
		if err == nil {
			t.Errorf("%s accepted wstring", target)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, wstringRetired) || !strings.Contains(msg, "kind 33") {
			t.Errorf("%s: refusal is not the named kind-33 error: %v", target, err)
		}
		if !strings.Contains(msg, "Text.name") || !strings.Contains(msg, "Root.title") || !strings.Contains(msg, "Choice.item") {
			t.Errorf("%s: refusal does not name the wstring sites: %v", target, err)
		}
	}
}
