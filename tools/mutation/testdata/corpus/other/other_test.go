package other

import (
	"testing"

	"example.org/mutationcorpus/sum"
)

// TestSum has the same name as sum's TestSum and the opposite life: it skips
// while the sum is right, and runs and passes under the mutant that makes
// sum's TestSum skip. A count of tests by name alone sees one TestSum pass
// before and after that mutant, and calls it a survivor (F-051).
func TestSum(t *testing.T) {
	if sum.Sum() != -1 {
		t.Skip("runs only under the mutant that skips sum's TestSum")
	}
}
