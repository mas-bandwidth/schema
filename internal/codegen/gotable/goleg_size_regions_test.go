package gotable

import (
	"bytes"
	"os"
	"testing"
)

// This group's BASE line count was 1291 (block.go 798, blockbuild.go 63,
// regions.go 405, allocator.go 25). The budget is 5% under it: 1226.
const (
	goLegRegionsBase   = 1291
	goLegRegionsBudget = 1226
)

var goLegRegionsFiles = []string{
	"block.go",
	"blockbuild.go",
	"regions.go",
	"allocator.go",
}

// TestGoLegSizeRegions pins the hand-written size of the regions group, so a
// later cut lowers the budget and the contraction cannot silently regress.
func TestGoLegSizeRegions(t *testing.T) {
	t.Parallel()
	total := 0
	for _, name := range goLegRegionsFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += bytes.Count(data, []byte("\n"))
	}
	t.Logf("regions group: %d lines (BASE %d, budget %d)", total, goLegRegionsBase, goLegRegionsBudget)
	if total > goLegRegionsBudget {
		t.Fatalf("regions group is over budget: %d lines (BASE %d, budget %d)",
			total, goLegRegionsBase, goLegRegionsBudget)
	}
}
