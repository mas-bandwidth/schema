package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuaJsonWriterRefusesZeroByte holds docs/SPEC-TABLES.md §16.3 and §16.14
// W0/W8 in the Lua writer: strings, text-list elements and string map keys
// containing a zero byte refuse, while ordinary text keeps its canonical JSON.
func TestLuaJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")

	work := t.TempDir()
	schema := `package probe

table Root
{
    name string(8)
    names []string(8)
    entries map[string(8)]int32
}
`
	path := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("ProbeTables")
local J = require("json")
local zero = string.char(0)

local clean = T.NewRoot()
clean.name = "ok"
clean.names = { { value = "one" } }
clean.entries = { good = 7 }
local expected = [[{
  "name": "ok",
  "names": [
    "one"
  ],
  "entries": {
    "good": 7
  }
}
]]
assert(J.ToJson(T, "Root", clean) == expected, "ordinary text output changed")

local string_value = T.NewRoot()
string_value.name = "a" .. zero .. "b"
assert(J.ToJson(T, "Root", string_value) == nil, "zero byte in string was written")

local list_value = T.NewRoot()
list_value.names = { { value = "a" .. zero .. "b" } }
assert(J.ToJson(T, "Root", list_value) == nil, "zero byte in text-list element was written")

local map_value = T.NewRoot()
map_value.entries = { ["a" .. zero .. "b"] = 7 }
assert(J.ToJson(T, "Root", map_value) == nil, "zero byte in string map key was written")
print("Lua JSON writer refuses zero bytes")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	if out != "Lua JSON writer refuses zero bytes" {
		t.Fatalf("unexpected Lua output: %s", out)
	}
}
