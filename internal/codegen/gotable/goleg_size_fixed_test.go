package gotable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestGoLegSizeFixed pins the fixed group's hand-written size. The budget is
// 5% under the group's line count at this card's BASE (3502 lines), so 3326
// lines: fixedform.go, fixedlineage.go and fixedruntime.go together.
func TestGoLegSizeFixed(t *testing.T) {
	t.Parallel()
	const (
		baseLines = 3502
		budget    = 3326
	)
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	total := 0
	for _, name := range []string{"fixedform.go", "fixedlineage.go", "fixedruntime.go"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += bytes.Count(data, []byte("\n"))
	}
	if total > budget {
		t.Fatalf("fixed group is %d lines, over the %d-line budget (BASE %d)", total, budget, baseLines)
	}
}
