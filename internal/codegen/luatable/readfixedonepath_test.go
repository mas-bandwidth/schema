package luatable

// ReadFixed is one record of the batch read (docs/FIXED-FORM-ALGORITHM.md §4
// "ONE path", §2 step 7, §5.3). A file this leg wrote with two records is
// refused, the same verdict FixedLoad gives at a capacity of one. One record
// plus a trailing byte is refused. The refusal's name is not the contract.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

func TestLuaReadFixedReadsExactlyOneRecord(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	writeFile(t, filepath.Join(schemaDir, "row.schema"), `package one_record

fixed table Row
{
    n int32 = 0
    name string(8) = ""
}
`)
	unit := luarun.LoadUnit(t, filepath.Join(schemaDir, "row.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local a = T.NewRow()
a.n = 7
a.name = "one"
local b = T.NewRow()
b.n = 9
b.name = "two"
local one = T.FixedSave("Row", { a })
local two = T.FixedSave("Row", { a, b })
local zero = T.FixedSave("Row", {})
local function verdict(ok)
  if ok then return "read" else return "refused" end
end
local ok1, v = pcall(T.ReadFixed, "Row", one)
local okL, rows = pcall(T.FixedLoad, "Row", one, 1)
if not ok1 or not okL or type(rows) ~= "table" or #rows ~= 1 or v == nil then
  print("one FAIL")
else
  local function show(r)
    return tostring(r.n) .. "," .. tostring(r.name)
  end
  print("one " .. show(v) .. " " .. show(rows[1]))
end
local ok2 = pcall(T.ReadFixed, "Row", two)
local okL2 = pcall(T.FixedLoad, "Row", two, 1)
print("two " .. verdict(ok2) .. " " .. verdict(okL2))
local okT = pcall(T.ReadFixed, "Row", one .. string.char(0))
local okLT = pcall(T.FixedLoad, "Row", one .. string.char(0), 1)
print("trail " .. verdict(okT) .. " " .. verdict(okLT))
local okZ = pcall(T.ReadFixed, "Row", zero)
print("zero " .. verdict(okZ))
`, ModuleName(unit)))
	out := string(luarun.Run(t, dir, "main.lua", nil))
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		key, _, _ := strings.Cut(line, " ")
		got[key] = line
	}
	want := []string{
		"one 7,one 7,one",
		"two refused refused",
		"trail refused refused",
		"zero refused",
	}
	for _, line := range want {
		key, _, _ := strings.Cut(line, " ")
		if got[key] != line {
			t.Fatalf("%s, want %s", got[key], line)
		}
	}
}
