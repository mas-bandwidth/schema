package luatable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTestgenMeshIsLinear pins docs/SPEC-TABLES.md §16.11's bound on
// `schema testgen --lang lua`: a pointer to a table the sample has already
// written is null, so each table is written at most once per sample and the
// output grows with the declaration, not with the number of pointer paths
// through it. Seven fully meshed tables (each pointing at every table, itself
// included) stay under 256 KiB in all.
func TestLuaTestgenMeshIsLinear(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("package mesh\n")
	for i := range 7 {
		fmt.Fprintf(&b, "\ntable T%d\n{\n    v int32\n", i)
		for k := range 7 {
			fmt.Fprintf(&b, "    p%d *T%d\n", k, k)
		}
		b.WriteString("}\n")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mesh.schema"), b.String())
	tests, err := Testgen(luarun.LoadUnit(t, dir))
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
