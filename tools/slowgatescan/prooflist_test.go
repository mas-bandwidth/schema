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
