package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// luaMeasureTextProbe saves several values of the unbounded-text unit (a text
// list and a string-keyed map, with empty and nonempty entries) and prints
// each wire with Lua LoadMeasure's answer for it.
const luaMeasureTextProbe = `local T = require("ProbeTables")
local load = require("load")
local save = require("save")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function emit(name, v)
  local w = save.Save(T, "Where", v)
  assert(w ~= nil, "the save refused " .. name)
  local n = load.LoadMeasure(T, "Where", w)
  print(name .. ":" .. hex(w) .. ":" .. (n == nil and "nil" or string.format("%d", n)))
end
local function value(rules, first, capped, byname)
  local v = T.NewWhere()
  v.rules = rules
  v.first = first
  v.capped = capped
  v.byname = byname
  return v
end
emit("empty", value({}, {}, {}, {}))
emit("emptytext", value({ { value = "" } }, { { value = "", value_present = true } }, { { value = "" } }, { [""] = 1 }))
emit("list", value({ { value = "a" }, { value = "" }, { value = "bcdefghij" } }, {}, {}, {}))
emit("opt", value({}, { { value = "x", value_present = true }, { value = "", value_present = false } }, { { value = "c" } }, {}))
emit("map", value({}, {}, {}, { k1 = 1, k2 = 2, [""] = 3, longer_key_name = 4 }))
emit("mixed", value({ { value = "a" }, { value = "b" } }, { { value = "x", value_present = true } }, { { value = "c" } }, { k1 = 1, k2 = 2 }))
emit("long", value({ { value = string.rep("\195\169", 35000) } }, {}, { { value = string.rep("z", 70001) } }, { [string.rep("k", 70000)] = 7 }))
`

// TestLuaMeasureCountsUnboundedTextExtents requires Lua LoadMeasure to equal
// tablewire.FileRegionMeasure for a text list and a string-keyed map with
// empty and nonempty entries: each nonempty unbounded text counts its framed
// extent (docs/SPEC-TABLES.md §2.13.1).
func TestLuaMeasureCountsUnboundedTextExtents(t *testing.T) {
	t.Parallel()
	schema := filepath.Join(t.TempDir(), "Where.schema")
	if err := os.WriteFile(schema, []byte(luaUnboundedTextSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the unit with unbounded text was refused: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaMeasureTextProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	root := u.Tables["Where"]
	if root == nil {
		t.Fatal("the unit declares no table Where")
	}
	cases := 0
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			t.Fatalf("probe line %q is not name:wire:measure", line)
		}
		wire, err := hex.DecodeString(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		bytes, whole, refused := tablewire.FileRegionMeasure(u, root, wire)
		want := "nil"
		if whole && !refused {
			want = strconv.FormatInt(bytes, 10)
		}
		if parts[2] != want {
			t.Errorf("%s: %s", parts[0], fmt.Sprintf("Lua LoadMeasure = %s, engine = %s", parts[2], want))
		}
		cases++
	}
	if cases != 7 {
		t.Fatalf("measured %d values, want 7", cases)
	}
}
