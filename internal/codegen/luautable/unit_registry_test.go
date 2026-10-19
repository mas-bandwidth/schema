package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

// TestLuauUnitViewListsTheUnit is the unit registry gate
// (docs/SPEC-TABLES.md §8.3, §8.7): UnitView() printed under the pinned
// interpreter equals internal/viewlisting's listing of the same unit, entry
// for entry. The unit is the tables corpus: the examples are one package and
// Nested.schema names a table Tables.schema declares, so the unit is the
// directory, not one file at a time.
func TestLuauUnitViewListsTheUnit(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "tables", "examples")
	paths, err := compiler.GatherPaths([]string{corpus})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	pin := viewlisting.Listing(u)
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	for fn, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := "--!strict\nlocal view = require(\"./view\")\nlocal u = view.UnitView()\nio.write(u.listing())\n"
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	if string(out) != pin {
		t.Errorf("UnitView() listing is not the compiler's pin.\n%s", firstDifference(pin, string(out)))
	}
}

func firstDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return fmt.Sprintf("line %d:\n  compiler: %s\n  program:  %s", i+1, w, g)
		}
	}
	return "the listings differ in trailing bytes alone"
}
