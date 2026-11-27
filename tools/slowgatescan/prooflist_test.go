package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestProofListNamesOnlyExistingTests is the audit's own witness. The fixture
// plants one stale name beside a real one and a `Parent/sub` name (which checks
// Parent) and demands the lint bite on the planted name alone; the tree subtest
// runs the same lint over the worktree, where at the branch head it named
// TestRetainMessageMapKeyProbe, TestRetainMessageMapRepeatedKeyKeepsWidening,
// TestMessageMeasureReservedFraming and TestRetainMessage.
func TestProofListNamesOnlyExistingTests(t *testing.T) {
	t.Parallel()
	t.Run("fixture", func(t *testing.T) {
		root := filepath.Join("testdata", "prooflist")
		recipes, err := scanMakefiles(root, map[string]bool{"./testpkg": true})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := proofListFindings(root, recipes)
		if err != nil {
			t.Fatal(err)
		}
		planted := false
		for _, f := range findings {
			if strings.Contains(f, "TestReal") {
				t.Fatalf("false positive on a real test: %s", f)
			}
			if strings.Contains(f, "TestPlanted names no test in ./testpkg") {
				planted = true
			}
		}
		if !planted {
			t.Fatalf("the planted stale name was not reported: %v", findings)
		}
		if len(findings) != 1 {
			t.Fatalf("the fixture must report exactly the planted name, got %d: %v", len(findings), findings)
		}
	})
	t.Run("tree", func(t *testing.T) {
		root := filepath.Join("..", "..")
		gated, err := gatedPackages(root)
		if err != nil {
			t.Fatal(err)
		}
		recipes, err := scanMakefiles(root, gated)
		if err != nil {
			t.Fatal(err)
		}
		findings, err := proofListFindings(root, recipes)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Fatalf("proof lists name %d test(s) that do not exist:\n%s", len(findings), strings.Join(findings, "\n"))
		}
	})
}

// TestProofListResolvesDerivedNames is the witness for names a recipe takes
// from a make variable computed by $(shell …) (PR 4912): the audit asks make
// for the list, checks every token it holds, and still refuses a stale literal
// beside it and a token that is no test.
func TestProofListResolvesDerivedNames(t *testing.T) {
	t.Parallel()
	root := filepath.Join("testdata", "prooflist-derived")
	recipes, err := scanMakefiles(root, map[string]bool{"./testpkg": true})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := proofListFindings(root, recipes)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range findings {
		switch {
		case strings.Contains(f, "derived-good"):
			t.Fatalf("a derived list of declared tests was refused: %s", f)
		case strings.Contains(f, "TestBogus names no test"), strings.Contains(f, "notatest names no test"):
			names = append(names, f)
		default:
			t.Fatalf("unexpected finding: %s", f)
		}
	}
	if len(names) != 2 {
		t.Fatalf("want exactly TestBogus and notatest refused, got %d: %v", len(names), findings)
	}
}
