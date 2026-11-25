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
	t.Run("computed", func(t *testing.T) {
		// a names list make computes with $(shell …): read off the
		// package's own test files it is accepted whole; read off any
		// other files (PLANTED) it is reported, never split into its
		// function words and checked as names
		const own = `sh test/slowgate/proof rows '$(sort $(shell sed -n 's/^func \(TestRow[A-Za-z0-9_]*\)(t \*testing\.T).*/\1/p' pkg/rows/*_test.go))' ./pkg/rows -count=1`
		planted := strings.Replace(own, "pkg/rows/*_test.go", "pkg/other/*_test.go", 1)
		for _, c := range []struct {
			cmd    string
			unread int
		}{{own, 0}, {planted, 1}} {
			refs, unread := proofRefsFromRecipes([]recipe{{file: "Makefile", line: 1, target: "rows", cmd: c.cmd, packages: []string{"./pkg/rows"}}})
			if len(refs) != 0 {
				t.Errorf("%s: a computed list was read as names: %v", c.cmd, refs)
			}
			if len(unread) != c.unread {
				t.Errorf("%s: want %d unread, got %v", c.cmd, c.unread, unread)
			}
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
