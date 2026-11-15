package luatable

// THE BLOCK GRAPH IS A CONSTANT (docs/PORTING.md M13, "Descriptors are
// constants"): a table's block layout, its projection and the row layout of
// each out-of-line array, is built once when the module's descriptors are
// sealed, and BlockOpen reads it. Opening the same block again derives
// nothing from the descriptors: B.Layout, wrapped with a counter after the
// module is required and sealed, runs zero times across a hundred opens, and
// every open reads the same values as the Go-pinned canonical dump.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLuaBlockLayoutIsBuiltOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := blockManifestRows(t, manifest)
	if len(rows) == 0 {
		t.Fatal("the manifest names no block image")
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			paths := units[row.unit]
			if len(paths) == 0 {
				t.Fatalf("block row %s names unknown unit %s", row.name, row.unit)
			}
			var full []string
			for _, p := range paths {
				full = append(full, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, full...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", row.unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			image, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(repo, row.dump))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(want), "\narray ") {
				t.Fatalf("%s: the block row carries no out-of-line array", row.name)
			}
			root := blockRoot(t, want)
			probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local inputs = require("inputs")
local layout = B.Layout
local calls = 0
B.Layout = function(...)
  calls = calls + 1
  return layout(...)
end
for i = 1, 100 do
  local h, why = B.BlockOpen(T, %q, inputs.block, 0x%016x)
  assert(h ~= nil, "BlockOpen refused open " .. i .. ": " .. tostring(why))
  assert(B.Dump(h) == inputs.dump, "open " .. i .. " reads values that differ from Go's dump")
end
B.Layout = layout
print("layout calls during 100 opens: " .. calls)
`, ModuleName(u), root, ir.BuildVersion(u))
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := string(luarun.Run(t, dir, "main.lua", map[string][]byte{"block": image, "dump": want}))
			if got != "layout calls during 100 opens: 0\n" {
				t.Fatalf("%s: BlockOpen rebuilds the block layout from the descriptors: %q", row.name, got)
			}
		})
	}
}
