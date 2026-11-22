package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequireSiblingsNamesTheCommand holds the harness half of the sibling
// gate: a run of a leg whose sibling runtime checkout is absent stops with one
// line that names the cause and the command, not a compile error from the
// middle of the leg.
func TestRequireSiblingsNamesTheCommand(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	for lang, s := range siblingRuntimes {
		t.Setenv(s.env, absent)
		err := requireSiblings([]string{lang})
		if err == nil {
			t.Errorf("%s: a missing sibling was accepted", lang)
			continue
		}
		if !strings.Contains(err.Error(), "run 'make deps'") || !strings.Contains(err.Error(), absent) || strings.Contains(err.Error(), "\n") {
			t.Errorf("%s: want one line naming the sibling and make deps, got %q", lang, err)
		}
	}
}

func TestRequireSiblingsAcceptsPresentAndUnlistedLegs(t *testing.T) {
	dir := t.TempDir()
	for _, s := range siblingRuntimes {
		path := filepath.Join(dir, s.marker)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(s.env, dir)
	}
	if err := requireSiblings([]string{"cpp", "c", "cs", "unity", "go", "rust", "js", "python", "moo"}); err != nil {
		t.Fatal(err)
	}
	// a leg with no sibling is never refused, whatever is set
	t.Setenv("SERIALIZE", filepath.Join(dir, "absent"))
	if err := requireSiblings([]string{"python", "moo", "lua"}); err != nil {
		t.Fatal(err)
	}
}
