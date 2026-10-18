package gotable

import (
	"bytes"
	"os"
	"testing"
)

// TestGoLegSizeText pins the size of the hand-written text group of the Go
// emitter (docs/SPEC-TABLES.md §16.9, §2.13, §2.13.1): the four non-test files
// at BASE held 2227 lines together, so the budget is 5% under that, 2115.
func TestGoLegSizeText(t *testing.T) {
	t.Parallel()
	files := []string{"textform.go", "textlist.go", "unboundedtext.go", "wstring.go"}
	const budget = 2115
	total := 0
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		total += bytes.Count(data, []byte("\n"))
	}
	if total > budget {
		t.Fatalf("the Go text emitter group is %d lines, over its budget of %d (BASE was 2227)", total, budget)
	}
}
