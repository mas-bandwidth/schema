package gotable

import (
	"fmt"
	"strings"
	"testing"
)

// testgenMeshSchema declares n tables, each holding a pointer to every one of
// them, itself included: the densest pointer mesh a declaration can spell.
func testgenMeshSchema(n int) string {
	var b strings.Builder
	b.WriteString("package mesh\n")
	for i := range n {
		fmt.Fprintf(&b, "\ntable T%d\n{\n    v int32\n", i)
		for k := range n {
			fmt.Fprintf(&b, "    p%d *T%d\n", k, k)
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// TestGoTestgenMeshIsLinear pins docs/SPEC-TABLES.md §16.11's bound on
// `schema testgen --lang go`: a pointer to a table the sample has already
// written is null, so each table is written at most once per sample and the
// output grows with the declaration, not with the number of pointer paths
// through it. Seven fully meshed tables stay under 256 KiB in all; a
// path-only rule writes every path of distinct tables, factorial in n.
func TestGoTestgenMeshIsLinear(t *testing.T) {
	t.Parallel()
	tests, err := Testgen(unitFrom(t, testgenMeshSchema(7)))
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	total := 0
	for _, src := range tests {
		total += len(src)
	}
	if total == 0 || total > 256<<10 {
		t.Fatalf("testgen wrote %d bytes for seven meshed tables, want at most %d", total, 256<<10)
	}
}
