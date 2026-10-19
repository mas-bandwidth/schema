package luatable

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaMapPresence holds docs/SPEC-TABLES.md §2.8's OPTIONAL MAP, as
// TestGoMapPresence holds it in Go: `tags ?map[string(8)]int32` in a variable
// table carries a tags_present companion, and ABSENT, PRESENT-EMPTY and
// PRESENT-WITH-ENTRIES are three values on the binary wire, in base64
// (§16.8) and in the JSON text (§16.2). Each pinned wire is the Go oracle's
// own, and a reader generated WITHOUT the `?` reads all three silently.
func TestLuaMapPresence(t *testing.T) {
	t.Parallel()

	optionalDir := t.TempDir()
	optionalSchema := filepath.Join(optionalDir, "M.schema")
	if err := os.WriteFile(optionalSchema, []byte(luaMapPresenceSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, optionalSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("optional map: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaMapPresenceProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	want := strings.Join([]string{
		"absent\t" + luaMapAbsent + "\t" + wireB64(t, luaMapAbsent) + "\tabsent\t" + luaMapAbsent + "\tabsent\tabsent\t{\"id\":1}\t" + luaMapAbsent,
		"empty\t" + luaMapEmpty + "\t" + wireB64(t, luaMapEmpty) + "\tempty\t" + luaMapEmpty + "\tempty\tempty\t{\"id\":1,\"tags\":{}}\t" + luaMapEmpty,
		"entries\t" + luaMapEntries + "\t" + wireB64(t, luaMapEntries) + "\tentries\t" + luaMapEntries + "\tentries\tentries\t{\"id\":1,\"tags\":{\"a\":5}}\t" + luaMapEntries,
		"null\tabsent\t" + luaMapAbsent,
	}, "\n")
	if out != want {
		t.Errorf("optional map:\n got:\n%s\nwant:\n%s", out, want)
	}

	plainDir := t.TempDir()
	plainSchema := filepath.Join(plainDir, "M.schema")
	if err := os.WriteFile(plainSchema, []byte(luaMapPlainSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := luarun.LoadUnit(t, plainSchema)
	plainFiles, err := Generate(plain)
	if err != nil {
		t.Fatalf("plain map: %v", err)
	}
	plainRun := t.TempDir()
	luarun.WriteFiles(t, plainRun, plainFiles)
	if err := os.WriteFile(filepath.Join(plainRun, "main.lua"), []byte(luaMapPlainProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	plainOut := strings.TrimRight(string(luarun.Run(t, plainRun, "main.lua", map[string][]byte{
		"absent":  wireBytes(t, luaMapAbsent),
		"empty":   wireBytes(t, luaMapEmpty),
		"entries": wireBytes(t, luaMapEntries),
	})), "\n")
	if plainOut != "absent:0:empty\nempty:0:empty\nentries:1:5" {
		t.Errorf("plain reader:\n got:\n%s\nwant:\n%s", plainOut, "absent:0:empty\nempty:0:empty\nentries:1:5")
	}
}

func wireBytes(t *testing.T, text string) []byte {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wireB64(t *testing.T, text string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(wireBytes(t, text))
}

const luaMapPresenceSchema = `package probe

table M
{
    id   int32
    tags ?map[string(8)]int32
}
`

const luaMapPlainSchema = `package probe

table M
{
    id   int32
    tags map[string(8)]int32
}
`

// The three pinned wires are the Go oracle's (internal/codegen/gotable/
// mappresence_test.go): form byte 01, the root body, the zero reference, then
// the id table. absent writes no `tags` bytes; empty writes `02 0e 02 0d 00`
// (the field header, L = 2, element kind 13, count 0); entries is the plain
// map's bytes (§2.8).
const (
	luaMapAbsent  = "01" + "010401000000" + "00" + "c03a5cb5072eb708" + "0100000000000000"
	luaMapEmpty   = "01" + "010401000000" + "020e020d00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "0200000000000000"
	luaMapEntries = "01" + "010401000000" + "020e0e0d01" + "0b" + "030c0161" + "040405000000" + "00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "ec105b36194ac93d" + "ea0ce83094fde47c" + "0400000000000000"
)

const luaMapPresenceProbe = `local T = require("ProbeTables")
local load = require("load")
local save = require("save")
local text = require("text")
local json = require("json")
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local function report() return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 } end
local function describe(v)
  if v.tags_present ~= true then return "absent" end
  local n = 0
  for _ in pairs(v.tags) do n = n + 1 end
  if n == 0 then return "empty" end
  if n == 1 and v.tags["a"] == 5 then return "entries" end
  return "wrong"
end
local function compact(s) return (s:gsub("%s+", "")) end
local shapes = {
  { "absent", function() local v = T.NewM(); v.id = 1; return v end },
  { "empty", function() local v = T.NewM(); v.id = 1; v.tags = {}; v.tags_present = true; return v end },
  { "entries", function() local v = T.NewM(); v.id = 1; v.tags = { a = 5 }; v.tags_present = true; return v end },
}
for _, s in ipairs(shapes) do
  local name, build = s[1], s[2]
  local v = build()
  local wire = save.Save(T, "M", v)
  local back = load.Load(T, "M", wire, report())
  local b64 = text.SaveText(T, "M", v)
  local tback = text.LoadText(T, "M", b64)
  local jtext = json.ToJson(T, "M", v)
  local jback = json.FromJson(T, "M", jtext, report())
  print(table.concat({ name, hex(wire), b64, describe(back), hex(save.Save(T, "M", back)),
    describe(tback), describe(jback), compact(jtext), hex(save.Save(T, "M", jback)) }, "\t"))
end
local nv = json.FromJson(T, "M", '{"id":1,"tags":null}', report())
print(table.concat({ "null", describe(nv), hex(save.Save(T, "M", nv)) }, "\t"))
`

const luaMapPlainProbe = `local T = require("ProbeTables")
local load = require("load")
local inputs = require("inputs")
local function run(name)
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local v = load.Load(T, "M", inputs[name], report)
  local n = 0
  for _ in pairs(v.tags) do n = n + 1 end
  local val = "empty"
  if n == 1 and v.tags["a"] == 5 then val = "5" end
  print(name .. ":" .. tostring(n) .. ":" .. val)
end
run("absent")
run("empty")
run("entries")
`
