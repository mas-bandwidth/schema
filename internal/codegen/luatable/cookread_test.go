package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaCookRead holds the cook read (docs/SPEC-TABLES.md §7.2) to the load:
// for every instance row of the tables conformance manifest, the Lua read of
// internal/tablecook's cook of the row's wire equals the Lua LoadTolerant of
// that wire. The comparison is a canonical dump of the two values, so it pins
// the storage the variable-form load produces — scalars at their width, an
// enum by its variant name, a map by its keys, a pointer by its one node, a
// byte buffer by its box, and the null placeholders a sequence keeps.
func TestLuaCookRead(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	for _, row := range manifestRows(t, manifest, "instance") {
		t.Run(row.caseName, func(t *testing.T) {
			t.Parallel()
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			if len(ir.TableWideFields(u)) > 0 {
				t.Skipf("unit %s carries the wide kinds; the cook read half does not carry them yet", row.unit)
			}
			files, err := Generate(u)
			if err != nil {
				// the Lua target refuses this unit's kinds (docs/PORT-LUA.md);
				// it emits no cook surface to read.
				t.Skipf("the Lua table backend does not generate this unit: %v", err)
			}
			m := tabletext.NewModel(u)
			wire, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			root := m.Lookup(row.root)
			if root == nil {
				t.Fatalf("%s: the unit has no table %s", row.caseName, row.root)
			}
			inst := m.New(root)
			var report tabletext.Report
			if _, err := tablewire.Decode(m, inst, wire, &report); err != nil {
				t.Fatalf("decode %s: %v", row.caseName, err)
			}
			cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
			if err != nil {
				// the tool refuses to write this cook; there is no cook to read.
				t.Skipf("the tool writes no cook for %s: %v", row.caseName, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			probe := cookReadProbe(ModuleName(u), row.root)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
				"wire": wire,
				"cook": cooked,
			})), "\n")
			if out != "ok" {
				t.Errorf("%s: %s", row.caseName, out)
			}
		})
	}
}

// cookReadProbe is the Lua main for one row: it loads the wire, opens the cook,
// reads the opened region, and compares canonical dumps of the two values.
func cookReadProbe(module, root string) string {
	return fmt.Sprintf(`local T = require(%q)
local load = require("load")
local cook = require("cook")
local uncook = require("uncook")
local inputs = require("inputs")
local root = %q

local function keyrepr(k)
  local t = type(k)
  if t == "integer" then return "i" .. string.format("%%d", k) end
  if t == "number" then return "f" .. string.format("%%.17g", k) end
  if t == "string" then return "s" .. k end
  if t == "boolean" then return k and "b1" or "b0" end
  return t .. tostring(k)
end

local function dump(v)
  local t = type(v)
  if t == "nil" then return "nil" end
  if t == "boolean" then return v and "true" or "false" end
  if t == "integer" then return "i" .. string.format("%%d", v) end
  if t == "number" then return "f" .. string.format("%%.17g", v) end
  if t == "string" then return "s" .. #v .. ":" .. v end
  if t == "table" then
    local keys = {}
    for k in pairs(v) do keys[#keys + 1] = k end
    table.sort(keys, function(a, b)
      local x, y = keyrepr(a), keyrepr(b)
      if x == y then return type(a) < type(b) end
      return x < y
    end)
    local parts = {}
    for _, k in ipairs(keys) do
      parts[#parts + 1] = keyrepr(k) .. "=" .. dump(v[k])
    end
    return "{" .. table.concat(parts, ",") .. "}"
  end
  return t
end

local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local wire = load.LoadTolerant(T, root, inputs.wire, report)
local h, why = cook.Open(T, root, inputs.cook)
if h == nil then error("cook refused: " .. tostring(why)) end
local opened = uncook.Read(T, h)
local a, b = dump(wire), dump(opened)
if a ~= b then
  local i = 1
  local n = math.min(#a, #b)
  while i <= n and a:sub(i, i) == b:sub(i, i) do i = i + 1 end
  error(string.format("cook read differs from the load at %%d:\nwire: %%s\ncook: %%s",
    i, a:sub(math.max(1, i - 80), i + 80), b:sub(math.max(1, i - 80), i + 80)))
end
print("ok")
`, module, root)
}
