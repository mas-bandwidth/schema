package luatable

// Unbounded text (docs/SPEC-TABLES.md §2.13.1) on the binary wire: a text
// list's bare `string` element and a map's bare `string` key read and write
// exactly as the generated Go does. The schema and the pinned wire are the Go
// backend's own (internal/codegen/gotable/unboundedtext_test.go,
// TestGoTableUnboundedTextListsAndMapKeys): whereGolden is what its generated
// Save writes for rules ["a","b"], first ["x",absent], capped ["c"] and byname
// {"k1":1,"k2":2}. The Lua save must write those bytes, the Lua load must read
// them back to the same values (each optional element as the element record's
// `value`/`value_present` pair, as the bounded text lists spell it) and
// re-save them unchanged, and a 70 KB element in each list and a 70 KB key
// must round-trip with no clamp and no bound.

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// luaUnboundedTextSchema is the Go test's unit: a plain unbounded list, a
// counted `?` list, a counted plain list and an unbounded map key.
const luaUnboundedTextSchema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules []string(8)
    first [..4]?string(8)
    capped [..2]string(8)
    byname map[string(8)]int32
}
`

// luaUnboundedWhereGolden is the Go test's pinned wire.
const luaUnboundedWhereGolden = "01010e060c0201610162020e0a0d0205030c0178000100040e040c010163050e1c0d020c060c026b31030401000000000c060c026b320304020000000000c05351286a4f1267411d6f997fedd789ea0ce83094fde47ce86f9207409840f48f702bcb07334de7ec105b36194ac93d0600000000000000"

// luaUnboundedTextProbe builds the Go test's short value, saves it, loads the
// pinned wire, checks the values and re-saves; then builds 70 KB elements and
// keys and round-trips them with no clamp.
const luaUnboundedTextProbe = `local T = require("ProbeTables")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
           clamped = 0, widened = 0, duplicate = 0 }
end
local v = T.NewWhere()
v.rules = { { value = "a" }, { value = "b" } }
v.first = { { value = "x", value_present = true }, { value = "", value_present = false } }
v.capped = { { value = "c" } }
v.byname = { k1 = 1, k2 = 2 }
local saved = save.Save(T, "Where", v)
assert(saved ~= nil, "the save refused the short value")
print("save:" .. hex(saved))
local r = report()
local back = load.LoadTolerant(T, "Where", inputs.wire, r)
assert(back ~= nil, "the load returned no root")
assert(not r.malformed and r.clamped == 0, "the load reported damage")
assert(#back.rules == 2 and back.rules[1].value == "a" and back.rules[2].value == "b", "rules moved")
assert(back.first[1].value == "x" and back.first[1].value_present == true
  and back.first[2].value_present == false and #back.first == 2, "first moved")
assert(#back.capped == 1 and back.capped[1].value == "c", "capped moved")
assert(back.byname.k1 == 1 and back.byname.k2 == 2, "byname moved")
local again = save.Save(T, "Where", back)
assert(again ~= nil, "the resave refused")
print("resave:" .. hex(again))

local long = string.rep("\195\169", 35000)
local keyA = string.rep("k", 70000) .. "a"
local keyB = string.rep("k", 70000) .. "b"
local big = T.NewWhere()
big.rules = { { value = long }, { value = "z" } }
big.first = { { value = "", value_present = false }, { value = long .. "1", value_present = true } }
big.capped = { { value = long .. "2" } }
big.byname = { [keyA] = 7, [keyB] = 8 }
local bw = save.Save(T, "Where", big)
assert(bw ~= nil, "the save refused the long value")
local br = report()
local bv = load.LoadTolerant(T, "Where", bw, br)
assert(bv ~= nil and not br.malformed and br.clamped == 0, "the long load reported damage")
assert(#bv.rules[1].value == #long and bv.rules[2].value == "z", "long rules moved")
assert(bv.first[1].value_present == false and #bv.first[2].value == #long + 1, "long first moved")
assert(#bv.capped[1].value == #long + 1, "long capped moved")
assert(bv.byname[keyA] == 7 and bv.byname[keyB] == 8, "long keys moved")
local b2 = save.Save(T, "Where", bv)
assert(b2 ~= nil and b2 == bw, "the long round trip moved the wire")
print("long:" .. #bw)
`

// TestLuaUnboundedTextBinary holds docs/SPEC-TABLES.md §2.13.1 through the
// binary wire: the Lua save of the Go test's short value equals its pinned
// wire byte for byte, the Lua load of that wire gives the same values and
// re-saves it unchanged, and 70 KB elements and keys round-trip with no bound
// and no clamp.
func TestLuaUnboundedTextBinary(t *testing.T) {
	t.Parallel()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "Where.schema")
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
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaUnboundedTextProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString(luaUnboundedWhereGolden)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
	lines := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("probe line %q is not key:value", line)
		}
		lines[key] = value
	}
	if got := lines["save"]; got != luaUnboundedWhereGolden {
		t.Fatalf("the Lua save is not the generated Go's wire\n got %s\nwant %s", got, luaUnboundedWhereGolden)
	}
	if got := lines["resave"]; got != luaUnboundedWhereGolden {
		t.Fatalf("the Lua load did not re-save the generated Go's wire\n got %s\nwant %s", got, luaUnboundedWhereGolden)
	}
	if lines["long"] == "" || lines["long"] == "0" {
		t.Fatalf("the 70 KB round trip wrote nothing: %q", lines["long"])
	}
}
