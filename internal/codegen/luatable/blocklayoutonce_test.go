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
	// A declared type is not block-eligible, so the seal used to leave its
	// block_row unset. Opening a block whose out-of-line element is that type
	// must read the row, and must not call B.Layout to do it.
	t.Run("declared element", func(t *testing.T) {
		t.Parallel()
		const src = "package blockrow\n" +
			"type Inner { value uint128 }\n" +
			"fixed table Frame { rows [..2]Inner }\n"
		dir := t.TempDir()
		schema := filepath.Join(dir, "Blockrow.schema")
		if err := os.WriteFile(schema, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		luarun.WriteFiles(t, dir, files)
		probe := fmt.Sprintf(`local T = require(%q)
local B = require("block")
local W = require("blockbuild")
local built, why = W.Begin(T, "Frame", {rows=1})
assert(built ~= nil, "Begin refused: " .. tostring(why))
local at = built.arrays[1].offset
local mark = 0x1122334455667788
local bytes = built.bytes:sub(1, at) .. string.pack("=I8", mark) .. built.bytes:sub(at + 9)
local layout = B.Layout
local calls = 0
B.Layout = function(...)
  calls = calls + 1
  return layout(...)
end
local dump
for i = 1, 100 do
  local h, open_why = B.BlockOpen(T, "Frame", bytes)
  assert(h ~= nil, "BlockOpen refused open " .. i .. ": " .. tostring(open_why))
  dump = B.Dump(h)
end
B.Layout = layout
local want = "value = " .. string.format("%%d", mark)
assert(string.find(dump, "array rows Inner", 1, true), dump)
assert(string.find(dump, "\nrow 0 @", 1, true), dump)
assert(string.find(dump, want, 1, true), dump)
print("layout calls during 100 opens: " .. calls)
print(dump)
`, ModuleName(u))
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		got := string(luarun.Run(t, dir, "main.lua", nil))
		if !strings.HasPrefix(got, "layout calls during 100 opens: 0\n") {
			t.Fatalf("BlockOpen rebuilds the out-of-line row layout: %q", got)
		}
		if !strings.Contains(got, "array rows Inner") || !strings.Contains(got, "\nrow 0 @") || !strings.Contains(got, "value = ") {
			t.Fatalf("the opened block did not read the declared-type row: %q", got)
		}
	})
}
