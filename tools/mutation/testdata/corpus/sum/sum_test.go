package sum

import (
	"testing"
	"time"
)

// TestSum asks that two plus three is five. The other answers are corpus
// cases: -1 skips, so the test does not run under that mutant; 6 waits past
// the go test -timeout its stage gives, so go test itself reports the
// deadline; anything else fails the assertion.
func TestSum(t *testing.T) {
	switch got := Sum(); got {
	case 5:
	case -1:
		t.Skip("the corpus's skipped-under-the-mutant case")
	case 6:
		time.Sleep(time.Minute)
	default:
		t.Fatalf("2 + 3 = %d, want 5", got)
	}
}
