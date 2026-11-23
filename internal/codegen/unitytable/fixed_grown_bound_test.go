package unitytable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityFixedFormGrownBound reads the grown-bound versioning rows
// (array_fixed_grow, array_fixed_grow_scalar; F-092) from the C++ reference's
// older records under the newer unit: every grown slot holds its born element.
func TestUnityFixedFormGrownBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet and the C++ fixed-form corpus")
	repo := filepath.Join("..", "..", "..")
	corpus := filepath.Join(repo, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		if slowtest.CorpusRequired() {
			t.Fatalf("the fixed-form corpus is absent: %v (run make tables-fixedform-corpus)", err)
		}
		t.Skipf("the fixed-form corpus is not built (make tables-fixedform-corpus): %v", err)
	}
	fixedproof.CheckGrownFixedBound(t, repo, corpus, unityFixedProofRunner{})
}
