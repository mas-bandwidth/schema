package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaRequiredBinaryReadAndSave pins the `| required` field marker
// (docs/SPEC-TABLES.md §2.3) on the binary wire: the writer always writes a
// required field, so it rides even at its default; the tolerant reader counts
// a missing required field as MissingRequired and names it in the reason, and
// refuses the whole record at any depth; a required field inside an `if`
// guard is demanded only while the guard holds; and a required field whose
// range excludes zero takes no default, so an older record that never carried
// it is missing_required on read. Each expectation matches the generated Go
// (internal/codegen/gotable/required*.go) for the same unit.
func TestLuaRequiredBinaryReadAndSave(t *testing.T) {
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
table G
{
    active bool
    if active
    {
        req int32 | required
    }
}
table GLegacy
{
    active bool
    if active
    {
        x int32
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
table OldExact
{
}
table Other
{
    ver uint8
}
`
	dir := t.TempDir()
	sf := filepath.Join(dir, "req.schema")
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
local load = require("load")
local save = require("save")
local function rep() return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 } end
local function line(label, r, v)
  print(string.format("%s %d|%s|%s", label, r.missingRequired or 0, r.reason or "-", v == nil and "nil" or "read"))
end
-- A: an older record (before b was required) lacks b: refused, naming b.
do
  local lg = T.NewLegacy(); lg.a = 5
  local w = save.Save(T, "Legacy", lg)
  local r = rep(); local v = load.Load(T, "Cur", w, r)
  line("root_missing", r, v)
end
-- B: a required field rides at its default, and a clean record reads back.
do
  local c = T.NewCur(); c.a = 5
  local w = save.Save(T, "Cur", c)
  local lg = T.NewLegacy(); lg.a = 5
  local wl = save.Save(T, "Legacy", lg)
  local r = rep(); local v = load.Load(T, "Cur", w, r)
  print(string.format("rides %d|%s|%s|%s", r.missingRequired or 0, tostring(#w > #wl), tostring(v ~= nil), tostring(v ~= nil and v.b == 0)))
end
-- C: a required field in an off guard is not demanded; on and missing is.
do
  local g = T.NewG()
  local w = save.Save(T, "G", g)
  local r = rep(); local v = load.Load(T, "G", w, r)
  line("off_guard", r, v)
end
do
  local gl = T.NewGLegacy(); gl.active = true; gl.x = 5
  local w = save.Save(T, "GLegacy", gl)
  local r = rep(); local v = load.Load(T, "G", w, r)
  line("on_guard_missing", r, v)
end
-- D: a required field two levels deep refuses the outer record, naming the child.
do
  local oo = T.NewOldOuter(); oo.a = 3; oo.kid.y = 2
  local w = save.Save(T, "OldOuter", oo)
  local r = rep(); local v = load.Load(T, "Outer", w, r)
  line("nested_missing", r, v)
end
-- E: a required field whose range excludes zero takes no default: an older
-- record that never carried it is missing_required; a same-named non-required
-- field elsewhere is not.
do
  local oe = T.NewOldExact()
  local w = save.Save(T, "OldExact", oe)
  local r = rep(); local v = load.Load(T, "Exact", w, r)
  line("nodefault_missing", r, v)
end
do
  local oh = T.NewOther()
  local w = save.Save(T, "Other", oh)
  local r = rep(); local v = load.Load(T, "Other", w, r)
  line("other_clean", r, v)
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	want := `root_missing 1|missing_required: b|nil
rides 0|true|true|true
off_guard 0|-|read
on_guard_missing 1|missing_required: req|nil
nested_missing 1|missing_required: x|nil
nodefault_missing 1|missing_required: ver|nil
other_clean 0|-|read`
	if got != want {
		t.Errorf("lua required behavior:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
