package luatable

// TestLuaPlainLoadIsStrict pins docs/SPEC-TABLES.md §6.5's rule that THE PLAIN
// NAME IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS OWN for the Lua
// variable-form read: load.Load answers nil exactly where refusal.ReadStrict
// refuses, with report.reason naming the refusal, and load.LoadTolerant keeps
// the recovered value and its report under the explicit name. The oracle is
// schema-go's TestGoDefaultReadIsStrict; every manifest instance and report
// row is read both ways.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// plainStrictProbe is the Lua main for one manifest row. It reads the wire
// through both names and the strict read, and checks that the plain read's
// answer and reason are the strict read's.
func plainStrictProbe(module, root string, instance bool) string {
	instanceCheck := ""
	if instance {
		instanceCheck = `assert(tolerant ~= nil, "LoadTolerant answered no root for an instance row")
`
	}
	return `local T = require("` + module + `")
local load = require("load")
local R = require("refusal")
local inputs = require("inputs")
local wire = inputs.wire

local function fresh()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
           clamped = 0, widened = 0, duplicate = 0 }
end

local tr = fresh()
local tolerant = load.LoadTolerant(T, "` + root + `", wire, tr)
` + instanceCheck + `
-- THE CALLER'S REPORT IS OVERWRITTEN WITH THIS READ'S REPORT (§6.5): seed it
-- with stale answers and every counter must come back as the tolerant walk
-- counted it.
local sr = { malformed = true, refused = 7, unknown = 5, kindMismatch = 5,
             clamped = 5, widened = 5, duplicate = 5, reason = "stale" }
local strict = load.Load(T, "` + root + `", wire, sr)

local keys = { "malformed", "refused", "unknown", "kindMismatch", "clamped", "widened", "duplicate" }
for _, k in ipairs(keys) do
  assert(sr[k] == tr[k], "plain read report " .. k .. ": got " .. tostring(sr[k]) .. ", want " .. tostring(tr[k]))
end

local ok, err = pcall(function() return R.ReadStrict(T, "` + root + `", wire) end)
local name = nil
if not ok then name = tostring(err):match("^([^:]+)") end
if name == nil then
  assert(strict ~= nil, "the plain Load refused a wire ReadStrict accepted")
  assert(load.Load(T, "` + root + `", wire, nil) ~= nil, "the plain Load with a nil report refused an accepted wire")
else
  assert(strict == nil, "the plain Load took a wire ReadStrict refused: " .. name)
  assert(sr.reason == name, "the plain Load reason is " .. tostring(sr.reason) .. ", want " .. name)
  assert(load.Load(T, "` + root + `", wire, nil) == nil, "the plain Load with a nil report took a wire ReadStrict refused: " .. name)
end
print("plain-strict-ok")
`
}

func TestLuaPlainLoadIsStrict(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)

	type generated struct {
		dir    string
		module string
	}
	cache := map[string]generated{}
	unsupported := map[string]bool{}
	root := t.TempDir()
	generate := func(t *testing.T, unitKey string) (generated, bool) {
		t.Helper()
		if g, ok := cache[unitKey]; ok {
			return g, true
		}
		if unsupported[unitKey] {
			return generated{}, false
		}
		paths := units[unitKey]
		if len(paths) == 0 {
			t.Fatalf("the manifest names no unit %s", unitKey)
		}
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			// A unit the Lua target does not generate is not part of this
			// leg's surface (docs/SPEC-TABLES.md §15; test/conformance/README.md
			// counts it as a row of work left, never as a pass).
			unsupported[unitKey] = true
			t.Logf("unit %s is not generated for lua: %v", unitKey, err)
			return generated{}, false
		}
		dir := filepath.Join(root, unitKey)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		g := generated{dir: dir, module: ModuleName(u)}
		luarun.WriteFiles(t, g.dir, files)
		cache[unitKey] = g
		return g, true
	}

	ran := 0
	for _, kind := range []string{"instance", "report"} {
		rows := manifestRows(t, manifest, kind)
		if len(rows) == 0 {
			t.Fatalf("the manifest has no %s rows", kind)
		}
		for _, row := range rows {
			instance := kind == "instance"
			g, ok := generate(t, row.unit)
			if !ok {
				continue
			}
			ran++
			t.Run(kind+"_"+row.caseName, func(t *testing.T) {
				wire, err := os.ReadFile(filepath.Join(repo, row.wire))
				if err != nil {
					t.Fatalf("case %s: %v", row.caseName, err)
				}
				probe := plainStrictProbe(g.module, row.root, instance)
				if err := os.WriteFile(filepath.Join(g.dir, "main.lua"), []byte(probe), 0o644); err != nil {
					t.Fatal(err)
				}
				out := strings.TrimRight(string(luarun.Run(t, g.dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
				if out != "plain-strict-ok" {
					t.Fatalf("case %s: got %q", row.caseName, out)
				}
			})
		}
	}
	if ran == 0 {
		t.Fatal("no manifest row was read: the Lua target generated no unit")
	}
}
