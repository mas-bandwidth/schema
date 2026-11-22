package squirreltable_test

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// TestSquirrelFixedFormGrownBound reads the grown-bound versioning rows
// (array_fixed_grow, array_fixed_grow_scalar; F-092) from the C++ reference's
// older records under the newer unit: every grown slot holds its born element.
func TestSquirrelFixedFormGrownBound(t *testing.T) {
	t.Parallel()
	fixedproof.CheckGrownFixedBound(t, filepath.Join("..", "..", ".."), fixedCorpus(t), squirrelFixedProofRunner{})
}
