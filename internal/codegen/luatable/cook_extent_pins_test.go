package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These existing independent cook pins must remain in the exercised corpus.
// In particular maps may not disappear behind a dynamic oracle refusal.
func TestLuaCookExtentPins(t *testing.T) {
	slowtest.Gate(t, "native Lua map/list extent pins and caller storage")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt")
	units := manifestUnits(t, manifest)
	selected := map[string]bool{
		"map_cells": true, "map_chunks": true, "map_full": true, "map_slots": true, "map_text": true,
		"map_runs": true, "list_scalars": true, "list_nested": true, "list_of_maps": true, "widelist_wide": true,
	}
	found := 0
	for _, row := range manifestRows(t, manifest, "instance") {
		if !selected[row.caseName] {
			continue
		}
		found++
		t.Run(row.caseName, func(t *testing.T) {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			inputs := map[string][]byte{}
			for key, path := range map[string]string{"wire": row.wire,
				"le": "testdata/conformance/tables/cook-write/" + row.caseName + ".cook",
				"be": "testdata/conformance/tables/cook-write/" + row.caseName + "-be.cook"} {
				inputs[key], err = os.ReadFile(filepath.Join(repo, path))
				if err != nil {
					t.Fatal(err)
				}
			}
			probe := fmt.Sprintf(`
local T = require(%q)
local load = require("load")
local cook = require("cookwrite")
local inputs = require("inputs")
local v = assert(load.LoadTolerant(T, %q, inputs.wire, {}))
local snapshot = {}
for _, f in ipairs(T.Tables[%q].fields) do
  if f.shape == "map" then
    local values = v[f.name]
    local entries = {}
    for key, value in pairs(values) do entries[key] = value end
    snapshot[#snapshot + 1] = { values = values, entries = entries }
  end
end
assert(cook.CookMeasure(T, %q, v) == #inputs.le)
assert(cook.Cook(T, %q, v, nil, 1) == inputs.le)
assert(cook.Cook(T, %q, v, nil, 2) == inputs.be)
for _, s in ipairs(snapshot) do
  for key, value in pairs(s.entries) do assert(rawequal(s.values[key], value)) end
  for key in pairs(s.values) do assert(s.entries[key] ~= nil) end
end
print("pinned extents and caller storage passed")
`, ModuleName(u), row.root, row.root, row.root, row.root, row.root)
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luarun.Run(t, dir, "main.lua", inputs)
			if !strings.Contains(string(out), "pinned extents and caller storage passed") {
				t.Fatalf("probe did not finish: %s", out)
			}
		})
	}
	if found != len(selected) {
		t.Fatalf("exercised %d extent witnesses, want %d", found, len(selected))
	}
}
