package gotable

import (
	"bufio"
	"os"
	"testing"
)

// TestGoLegSizeContainers holds the containers group of the hand-written Go
// emitter to the line budget the contraction reached (docs/SPEC-TABLES.md §8).
func TestGoLegSizeContainers(t *testing.T) {
	t.Parallel()
	// The group's non-test files; BASE measured 1558 lines, and the budget is
	// 5% under that: 1480.
	files := []string{
		"lists.go",
		"maps.go",
		"unions.go",
		"wide.go",
		"integrity.go",
		"refusal.go",
		"source.go",
		"surface.go",
		"testgen.go",
	}
	const budget = 1480
	total := 0
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			total++
		}
		if err := sc.Err(); err != nil {
			f.Close()
			t.Fatalf("scan %s: %v", name, err)
		}
		f.Close()
	}
	if total > budget {
		t.Fatalf("containers group is %d lines, budget %d", total, budget)
	}
}
