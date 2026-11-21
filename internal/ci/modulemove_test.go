package ci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/modulemove"
)

// TestGoModMatchesPinBoundary holds go.mod's module path and `schema pin`'s
// boundary (internal/modulemove.FirstV3ModuleTag) in step on every pull
// request: ci-policy runs this package whatever the diff touches. Today go.mod
// names /v2 and the boundary is unset, so pin refuses every v3 tag; #4445 moves
// go.mod to /v3 and must set the boundary in the same commit, or this fails.
// tools/releasetag holds the tag being cut to the same rule at release time.
func TestGoModMatchesPinBoundary(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := modulemove.TreeGate(string(src), modulemove.FirstV3ModuleTag); err != nil {
		t.Fatal(err)
	}
}
