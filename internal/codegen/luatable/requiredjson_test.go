package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaRequiredJson pins the `| required` field marker (docs/SPEC-TABLES.md
// §2.3, §6.5, §16.2) on the JSON text form: a JSON object that leaves a
// required field out counts missing_required once, naming the missing fields
// in declaration order, at the root, in a nested body and in a union arm; a
// required field inside an `if` guard is demanded only while the guard holds;
// a required field whose range excludes zero takes no default; required-ness
// is per field per table, so a same-named non-required field is not refused;
// and ToJson writes a required field even at its default. Every expectation
// is the generated Go's answer for the same unit (internal/codegen/gotable/
// required_test.go, requirednodefault_test.go, requiredoffbranch_test.go).
func TestLuaRequiredJson(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Cur
{
    a int32
    b int32 | required
}
table Legacy
{
    a int32
    b int32
}
table Multi
{
    a int32 | required
    b int32
    c int32 | required
}
table G
{
    active bool
    if active
    {
        req int32 | required
    }
}
table Kid
{
    x int32 | required
    y int32
}
table OldKid
{
    y int32
}
table Outer
{
    a int32
    kid Kid
}
table OldOuter
{
    a int32
    kid OldKid
}
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
table Other
{
    ver uint8
}
union Pick
{
    kid Kid
}
union OldPick
{
    kid OldKid
}
table HasPick
{
    pid int32
    pick Pick
}
table OldHasPick
{
    pid int32
    pick OldPick
}
`
	dir := t.TempDir()
	sf := filepath.Join(dir, "reqjson.schema")
	if err := os.WriteFile(sf, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, sf)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	luarun.WriteFiles(t, dir, files)
	main := `local T = require("` + ModuleName(u) + `")
local J = require("json")
local function rep()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
    clamped = 0, widened = 0, duplicate = 0 }
end
local function read(label, root, text)
  local r = rep()
  local v = J.FromJsonTolerant(T, root, text, r)
  print(string.format("%s|%d|%s|%s", label, r.missingRequired or 0,
    r.reason or "-", v == nil and "nil" or "read"))
end
local function write(label, root, v)
  local text = J.ToJson(T, root, v)
  if text == nil then print(label .. "|nil") return end
  local trimmed = text:gsub("\n$", "")
  print(label .. "|" .. trimmed)
end
-- A: the root object omits the required b: missing_required, naming b only.
read("root_missing", "Cur", '{"a":5}')
-- B: the required field is named, even at its default: a clean read.
read("root_present", "Cur", '{"a":5,"b":0}')
-- C: the same absence through an older layout's text, and every missing
-- field named in declaration order.
read("legacy_missing", "Cur", '{"a":5}')
read("multi_missing", "Multi", '{}')
read("multi_partial", "Multi", '{"a":1}')
-- D: a required field in an off guard is not demanded; on and missing is.
read("guard_off", "G", '{"active":false}')
read("guard_on_missing", "G", '{"active":true}')
read("guard_on_present", "G", '{"active":true,"req":7}')
-- E: the refusal is the RECORD's at any depth: a nested body and a union arm.
read("nested_missing", "Outer", '{"a":1,"kid":{"y":2}}')
read("nested_present", "Outer", '{"a":1,"kid":{"x":3,"y":2}}')
read("union_missing", "HasPick", '{"pid":1,"pick":{"kid":{"y":2}}}')
read("union_present", "HasPick", '{"pid":1,"pick":{"kid":{"x":3,"y":2}}}')
-- F: a required field whose range excludes zero takes no default; a
-- same-named non-required field in another table is not refused.
read("exact_missing", "Exact", '{}')
read("exact_present", "Exact", '{"ver":7}')
read("other_collision", "Other", '{}')
-- G: the writer never omits a required field, even at its default; the
-- guard's rule still decides whether a guarded field rides.
write("write_default", "Cur", T.NewCur())
do
  local g = T.NewG()
  write("write_guard_off", "G", g)
end
do
  local g = T.NewG()
  g.active = true
  write("write_guard_on", "G", g)
end
-- H: the writer's text reads back clean, and writes the same text again.
do
  local text = J.ToJson(T, "Cur", T.NewCur())
  local r = rep()
  local v = J.FromJsonTolerant(T, "Cur", text, r)
  print(string.format("roundtrip|%d|%s|%s", r.missingRequired or 0,
    r.reason or "-", tostring(v ~= nil and v.b == 0 and J.ToJson(T, "Cur", v) == text)))
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	want := `root_missing|1|missing_required: b|read
root_present|0|-|read
legacy_missing|1|missing_required: b|read
multi_missing|1|missing_required: a, c|read
multi_partial|1|missing_required: c|read
guard_off|0|-|read
guard_on_missing|1|missing_required: req|read
guard_on_present|0|-|read
nested_missing|1|missing_required: x|read
nested_present|0|-|read
union_missing|1|missing_required: x|read
union_present|0|-|read
exact_missing|1|missing_required: ver|read
exact_present|0|-|read
other_collision|0|-|read
write_default|{
  "a": 0,
  "b": 0
}
write_guard_off|{
  "active": false
}
write_guard_on|{
  "active": true,
  "req": 0
}
roundtrip|0|-|true
`
	if got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n"); got != strings.TrimRight(want, "\n") {
		t.Errorf("TestLuaRequiredJson: the JSON text form disagrees with the generated Go\n    got:\n%s\n    want:\n%s", got, strings.TrimRight(want, "\n"))
	}
}
