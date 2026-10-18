package gotable

import (
	"os"
	"testing"
)

// TestGoLegSizeCore holds the core group of the hand-written Go emitter to a
// line budget: gotable.go, read.go, write.go, wire.go and view.go together.
// The BASE budget is 5% below the group's 4763 lines at the card's BASE
// (sprint/schema-go, 22e1b4f3a): 4524 lines.
func TestGoLegSizeCore(t *testing.T) {
	t.Parallel()
	const budget = 4524
	files := []string{"gotable.go", "read.go", "write.go", "wire.go", "view.go"}
	total := 0
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		n := 0
		for _, c := range src {
			if c == '\n' {
				n++
			}
		}
		total += n
	}
	if total > budget {
		t.Fatalf("Go leg core group is %d lines, above its budget of %d", total, budget)
	}
}
