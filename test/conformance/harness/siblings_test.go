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

// TestRequireSiblingsResolvesFromTheRepoRoot holds that a default (relative)
// sibling path is read from the repository root, the directory holding go.mod,
// not from the directory the harness runs in: make deps stages the siblings
// beside the root, and this test runs from its package directory. The tree is
// staged in a temporary directory, so the test needs no real sibling checkout.
func TestRequireSiblingsResolvesFromTheRepoRoot(t *testing.T) {
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(ws, "schema")
	pkg := filepath.Join(repo, "test", "conformance", "harness")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range siblingRuntimes {
		t.Setenv(s.env, "")
	}
	t.Chdir(pkg)

	// absent: refused, naming the checkout beside the repository root
	want := filepath.Join(ws, "serialize.js")
	err = requireSiblings([]string{"js"})
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "make deps") {
		t.Fatalf("absent sibling: want a refusal naming %s and make deps, got %v", want, err)
	}

	// staged beside the repository root: every leg is accepted
	for _, s := range siblingRuntimes {
		path := filepath.Join(repo, s.dir, s.marker)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireSiblings([]string{"cpp", "c", "cs", "unity", "go", "rust", "js"}); err != nil {
		t.Fatalf("siblings staged beside the repository root were refused from the package directory: %v", err)
	}
}
