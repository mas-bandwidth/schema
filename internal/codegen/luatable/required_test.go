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
// guard or a union arm is demanded only while the guard holds or the arm is on;
// and a required field whose range excludes zero takes no default, so an older
// record that never carried it is missing_required on read. Every expectation
// and every resave matches the generated Go (internal/codegen/gotable/
// required*.go) for the same unit.
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
enum State
{
    AtRest
}
union Choice
{
    a int32
}
table R
{
    s int32 | required
    m map[string(32)]int32 | required
    b *bytes | required
    u Choice | required
}
table Keys
{
    k [State]int32 | required
}
table Nest
{
    n int32
}
table RS
{
    b bool | required
    f float32 | required
    s string(32) | required
    y bytes(8) | required
    e State | required
    k Nest | required
    arr [..4]int32 | required
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
local function hexenc(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function hexdec(s)
  assert(#s % 2 == 0)
  local t = {}
  for i = 1, #s, 2 do
    table.insert(t, string.char(tonumber(s:sub(i, i + 1), 16)))
  end
  return table.concat(t)
end
-- the Go oracle wire for R{s:int32|required, m, b, u} at zero, measured from
-- the generated Go (internal/codegen/gotable/required*.go).
local ORACLE = "01010400000000020e020d00031100040f0000220b02864cee63af58f301864ce063afa5f101864cdf63aff00002864ce863af0400000000000000"
-- resave pins the round trip (§16.2): a clean load re-saved equals the wire
-- it came from, because a required field always rides.
local function resave(label, target, w, v)
  if v == nil then return end
  local w2 = save.Save(T, target, v)
  print(string.format("resave_%s %s", label, tostring(w2 == w)))
end
-- A: an older record (before b was required) lacks b: refused, naming b.
do
  local lg = T.NewLegacy(); lg.a = 5
  local w = save.Save(T, "Legacy", lg)
  local r = rep(); local v = load.LoadTolerant(T, "Cur", w, r)
  line("root_missing", r, v)
end
-- B: a required field rides at its default, and a clean record reads back and
-- resaves byte-for-byte.
do
  local c = T.NewCur(); c.a = 5
  local w = save.Save(T, "Cur", c)
  local lg = T.NewLegacy(); lg.a = 5
  local wl = save.Save(T, "Legacy", lg)
  local r = rep(); local v = load.LoadTolerant(T, "Cur", w, r)
  print(string.format("rides %d|%s|%s|%s", r.missingRequired or 0, tostring(#w > #wl), tostring(v ~= nil), tostring(v ~= nil and v.b == 0)))
  resave("rides", "Cur", w, v)
end
-- C: a required field in an off guard is not demanded; on and missing is.
do
  local g = T.NewG()
  local w = save.Save(T, "G", g)
  local r = rep(); local v = load.LoadTolerant(T, "G", w, r)
  line("off_guard", r, v)
  resave("off_guard", "G", w, v)
end
do
  local gl = T.NewGLegacy(); gl.active = true; gl.x = 5
  local w = save.Save(T, "GLegacy", gl)
  local r = rep(); local v = load.LoadTolerant(T, "G", w, r)
  line("on_guard_missing", r, v)
end
-- D: a required field two levels deep refuses the outer record, naming the child.
do
  local oo = T.NewOldOuter(); oo.a = 3; oo.kid.y = 2
  local w = save.Save(T, "OldOuter", oo)
  local r = rep(); local v = load.LoadTolerant(T, "Outer", w, r)
  line("nested_missing", r, v)
end
-- E: a required field whose range excludes zero takes no default: an older
-- record that never carried it is missing_required; a same-named non-required
-- field elsewhere is not.
do
  local oe = T.NewOldExact()
  local w = save.Save(T, "OldExact", oe)
  local r = rep(); local v = load.LoadTolerant(T, "Exact", w, r)
  line("nodefault_missing", r, v)
end
do
  local oh = T.NewOther()
  local w = save.Save(T, "Other", oh)
  local r = rep(); local v = load.LoadTolerant(T, "Other", w, r)
  line("other_clean", r, v)
  resave("other_clean", "Other", w, v)
end
-- F: a required field inside a union arm is demanded only while the arm is on.
do
  local hp = T.NewHasPick(); hp.pid = 1; hp.pick = { tag = 0 }
  local w = save.Save(T, "HasPick", hp)
  local r = rep(); local v = load.LoadTolerant(T, "HasPick", w, r)
  line("union_arm_off", r, v)
  resave("union_arm_off", "HasPick", w, v)
end
do
  local oh = T.NewOldHasPick(); oh.pid = 1; oh.pick = { tag = 1, kid = { y = 2 } }
  local w = save.Save(T, "OldHasPick", oh)
  local r = rep(); local v = load.LoadTolerant(T, "HasPick", w, r)
  line("union_arm_on_missing", r, v)
end
-- G: a required map, blob and union ride at zero: the Lua save writes exactly
-- the Go oracle bytes, and the Lua reader admits the Go oracle cleanly and
-- resaves it byte for byte.
do
  local r = T.NewR()
  local w = save.Save(T, "R", r)
  print(string.format("oracle_save %s", tostring(hexenc(w) == ORACLE)))
  local d = hexdec(ORACLE)
  local r2 = rep(); local v = load.LoadTolerant(T, "R", d, r2)
  line("oracle_go", r2, v)
  if v ~= nil then
    local w2 = save.Save(T, "R", v)
    print(string.format("oracle_resave %s", tostring(w2 == d)))
  end
end
-- H: a required keyed array rides at zero and round-trips byte for byte.
do
  local k = T.NewKeys()
  local w = save.Save(T, "Keys", k)
  local r = rep(); local v = load.LoadTolerant(T, "Keys", w, r)
  line("keyed_zero", r, v)
  if v ~= nil then
    local w2 = save.Save(T, "Keys", v)
    print(string.format("resave_keyed_zero %s", tostring(w2 == w)))
  end
end
-- I: every remaining required shape rides at zero, so removing the required
-- gate from any one save.lua branch changes the wire and reddens this pin. The
-- bytes are the Go oracle for RS at zero, measured from the generated Go
-- (internal/codegen/gotable), extended by the empty required nested record:
-- Go's own writer elides it, but §2.3 says a required field always rides.
local SHAPES_ORACLE = "01010100020a00000000030c00040e020600051e00060d0100070e02040000a5f101864cdf63afd9ea01864cdb63af220b02864cee63af541502864cf463afc0e501864cd863af8afd01864ce663af38b3700519b156e70700000000000000"
do
  local s = T.NewRS()
  local w = save.Save(T, "RS", s)
  print(string.format("shapes_oracle_save %s", tostring(hexenc(w) == SHAPES_ORACLE)))
  local d = hexdec(SHAPES_ORACLE)
  local r = rep(); local v = load.LoadTolerant(T, "RS", d, r)
  line("shapes_oracle_go", r, v)
  if v ~= nil then
    local w2 = save.Save(T, "RS", v)
    print(string.format("shapes_oracle_resave %s", tostring(w2 == d)))
  end
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	want := `root_missing 1|missing_required: b|nil
rides 0|true|true|true
resave_rides true
off_guard 0|-|read
resave_off_guard true
on_guard_missing 1|missing_required: req|nil
nested_missing 1|missing_required: x|nil
nodefault_missing 1|missing_required: ver|nil
other_clean 0|-|read
resave_other_clean true
union_arm_off 0|-|read
resave_union_arm_off true
union_arm_on_missing 1|missing_required: x|nil
oracle_save true
oracle_go 0|-|read
oracle_resave true
keyed_zero 0|-|read
resave_keyed_zero true
shapes_oracle_save true
shapes_oracle_go 0|-|read
shapes_oracle_resave true`
	if got != want {
		t.Errorf("lua required behavior:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
