package luatable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Functional (luaBinary): run with SCHEMA_SLOW=1.
//
// TestFixedLuaReadFixedOnePath holds docs/FIXED-FORM-ALGORITHM.md §4 ("ONE
// path") and §5.3 step 11 on the Lua leg: ReadFixed is the strict load at a
// capacity of one, so on every file it gives the verdict FixedLoad gives with
// capacity 1. The files are one record, zero records, and two records whose
// second record hash is not the file's.
func TestFixedLuaReadFixedOnePath(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table Point {\n    x int32 = 1\n    y int32 = 2\n}\n")
	files, err := luatable.Generate(u)
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	script := fmt.Sprintf(`local T = require(%q)
local function point(x, y)
  local p = T.NewPoint()
  p.x = x
  p.y = y
  return p
end
local one = T.FixedSave("Point", { point(7, -7) })
local layoutLen = string.unpack("<I4", one, 17)
local zero = one:sub(1, 20 + layoutLen)
local two = T.FixedSave("Point", { point(7, -7), point(8, -8) })
local rec = (#two - 20 - layoutLen) // 2
local at = 21 + layoutLen + rec
two = two:sub(1, at - 1) .. string.char(two:byte(at) ~ 0xff) .. two:sub(at + 1)
local function verdict(ok)
  if ok then
    return "read"
  end
  return "refused"
end
for _, c in ipairs({ { "one", one }, { "zero", zero }, { "two", two } }) do
  local okRead, errRead = pcall(T.ReadFixed, "Point", c[2])
  local okLoad, errLoad = pcall(T.FixedLoad, "Point", c[2], 1)
  print(c[1] .. " ReadFixed=" .. verdict(okRead) .. " FixedLoad=" .. verdict(okLoad))
  if okRead ~= okLoad then
    print("  ReadFixed: " .. tostring(errRead) .. "  FixedLoad: " .. tostring(errLoad))
  end
end
local v = T.ReadFixed("Point", one)
assert(v.x == 7 and v.y == -7, "the one record did not land")
print("done")
`, luatable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	t.Logf("%s", out)
	cases := 0
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.Contains(line, "ReadFixed=") {
			continue
		}
		cases++
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.TrimPrefix(fields[1], "ReadFixed=") != strings.TrimPrefix(fields[2], "FixedLoad=") {
			t.Errorf("ReadFixed and FixedLoad with capacity 1 disagree: %s", line)
		}
	}
	if cases != 3 || !strings.HasSuffix(out, "done") {
		t.Fatalf("the probe did not finish: %q", out)
	}
}
