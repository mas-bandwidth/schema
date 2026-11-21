package luatable

// THE GROWN FIXED BOUND IN THE FIXED FORM, ON THE REFERENCE'S BYTES
// (docs/FIXED-FORM-VERSIONING-TESTS.md, row array_fixed_grow: a record
// array and two scalar arrays, [4] grown to [8]; F-092). The C++ reference writes the older
// record; the newer Lua unit, handed the older layout as its lineage, reads
// it. Every grown slot must hold one born element: 0 for an integer, a record at its declared defaults (x = 7, y = 9). Before F-092 the
// old-layout read left the array at the writer's length.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

var luaGrownBoundRows = []struct {
	row, root, check string
}{
	{
		row:  "array_fixed_grow",
		root: "ArrayFixedGrow",
		check: `if #v.vals ~= 8 then error("vals holds " .. #v.vals .. " slots, not 8", 0) end
for i = 1, 4 do
  if v.vals[i].x ~= -500 - (i - 1) or v.vals[i].y ~= -600 - (i - 1) then error("the writer's slot " .. i .. " did not land", 0) end
end
for i = 5, 8 do
  if v.vals[i] == nil or v.vals[i].x ~= 7 or v.vals[i].y ~= 9 then error("grown slot " .. i .. " is not the ELEMENT default", 0) end
end
for _, name in ipairs({ "ints", "octet" }) do
  if #v[name] ~= 8 then error(name .. " holds " .. #v[name] .. " slots, not 8", 0) end
end
for i = 1, 8 do
  local wi, wo = 0, 0
  if i <= 4 then wi, wo = -800 - (i - 1), 200 + (i - 1) end
  if v.ints[i] ~= wi or v.octet[i] ~= wo then error("scalar slot " .. i .. ": " .. tostring(v.ints[i]) .. " " .. tostring(v.octet[i]), 0) end
end`,
	},
}

// luaGrownBound reads each row's older reference record under the newer unit,
// fixed.lua edited by patch when set, and answers the first failure.
func luaGrownBound(t *testing.T, patch func([]byte) []byte) error {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	for _, r := range luaGrownBoundRows {
		older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_"+r.row+".schema"))
		newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_"+r.row+".schema"))
		entry, ok := FixedLineageOf(older, r.root)
		if !ok {
			t.Fatalf("%s: no lineage entry for %s", r.row, r.root)
		}
		files, err := GenerateLineage(newer, map[string][]FixedLineageEntry{r.root: {entry}})
		if err != nil {
			t.Fatal(err)
		}
		if patch != nil {
			files["fixed.lua"] = patch(files["fixed.lua"])
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
local v = T.ReadFixed(%q, I.wire)
if v.lead ~= 0xAAAAAAAA or v.trail ~= 0xBBBBBBBB then error("the row moved a neighbour", 0) end
%s
print("ok")
`, ModuleName(newer), r.root, r.check))
		wire := readFile(t, filepath.Join(corpus, "old_"+r.row+".bin"))
		stdout, stderr, err := luarun.Try(t, dir, "main.lua", map[string][]byte{"wire": wire})
		if err != nil || strings.TrimSpace(stdout) != "ok" {
			return fmt.Errorf("%s: %v: %s%s", r.row, err, stdout, stderr)
		}
	}
	return nil
}

func TestLuaFixedFormGrownBoundOnTheReferenceBytes(t *testing.T) {
	t.Parallel()
	if err := luaGrownBound(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestLuaFixedFormGrownBoundNegativeControl takes the grown-slot padding out
// of the emitted fixed.lua, one anchor matched exactly once, and requires the
// rows to go red.
func TestLuaFixedFormGrownBoundNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "          t[i] = L.zero_elem(T, d.elem)\n"
	err := luaGrownBound(t, func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("fixed.lua carries the anchor %d times, want 1", n)
		}
		return []byte(strings.Replace(string(src), anchor, "", 1))
	})
	if err == nil {
		t.Fatal("the grown bound still reads with the padding taken out of fixed.lua")
	}
}
