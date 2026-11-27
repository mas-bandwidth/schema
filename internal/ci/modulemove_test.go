package ci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/modulemove"
)

// TestGoModMatchesPinBoundary holds go.mod's module path and `schema pin`'s
// boundary (internal/modulemove.FirstV3ModuleTag) in step on every pull
// request: ci-policy runs this package whatever the diff touches. go.mod names
// /v3 and the boundary is the first tag cut with it; a change to either one
// without the other fails here.
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
