package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatrixDocMatchesManifest is the CI gate: the committed page is the
// manifest rendered, byte for byte. A hand edit to docs/MATRIX.md, or a
// manifest change nobody regenerated, fails here rather than shipping a table
// that drifted from what the tree means. It is the same failure `go run
// ./tools/matrix check` prints, in the `go test ./...` that `make test` runs.
func TestMatrixDocMatchesManifest(t *testing.T) {
	root := testRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	want := Render(m)
	got, err := os.ReadFile(filepath.Join(root, pagePath))
	if err != nil {
		t.Fatalf("%s is missing: run `go run ./tools/matrix write` (%v)", pagePath, err)
	}
	if string(got) != want {
		t.Errorf("%s is stale: it does not match %s rendered; run `go run ./tools/matrix write`", pagePath, manifestPath)
	}
}

// TestEveryStateIsOneOfTheThree holds the state grammar: a language's state is
// performant and production ready, done but not yet mature, or coming, and
// nothing else can reach the page. A fourth spelling is a typo the page would
// publish.
func TestEveryStateIsOneOfTheThree(t *testing.T) {
	root := testRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{statePerformant: true, stateDone: true, stateComing: true}
	for _, l := range m.Languages {
		state, basis := StateOf(m, l)
		if !valid[state] {
			t.Errorf("%s carries a state %q that is not one of the three", l.ID, state)
		}
		if strings.TrimSpace(basis) == "" {
			t.Errorf("%s carries the state %q with no basis", l.ID, state)
		}
	}
}

// TestEveryBuiltLanguageStatesFromItsCells refuses a state the cells do not
// support: a language with an ❌ row cannot read as done or production ready,
// and a language with no type-wire proof cannot either.
func TestEveryBuiltLanguageStatesFromItsCells(t *testing.T) {
	root := testRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range m.Languages {
		state, _ := StateOf(m, l)
		if state == stateComing {
			continue
		}
		if !l.TypeWire {
			t.Errorf("%s reads %q with no type-wire proof", l.ID, state)
		}
		for _, f := range m.Features {
			if !f.Cells[l.ID] {
				t.Errorf("%s reads %q while row %q is ❌", l.ID, state, f.Title)
			}
		}
		if !l.Conformance {
			t.Errorf("%s reads %q with the conformance leg not green", l.ID, state)
		}
	}
}

// TestMatrixPageIsLinkedFromREADME holds the page reachable: a published page
// nobody links to is a page nobody reads.
func TestMatrixPageIsLinkedFromREADME(t *testing.T) {
	root := testRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), pagePath) {
		t.Errorf("README.md does not link %s: add it to the Documentation table", pagePath)
	}
}

func testRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
