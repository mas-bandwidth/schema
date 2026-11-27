package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// luaWriteDepthSchema is internal/tabletext's depthWriteUnit: a pointer chain
// whose every node holds one container the writer always emits, an EMPTY one
// at that: an array, a union, a map, an enum-keyed array and a flags value.
const luaWriteDepthSchema = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

// TestLuaJsonWriteDepthCapCountsEveryContainer holds docs/SPEC-TABLES.md
// §16.2 and §16.14 W2 in the Lua text writer, as internal/tabletext's
// TestWriteDepthCapCountsEveryContainer holds the Go one: 127 nodes under the
// root put the last node's container at depth 128, which writes and reads
// back; 128 nodes put it at 129, which the writer refuses, because its own
// reader refuses that text.
func TestLuaJsonWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")

	work := t.TempDir()
	path := filepath.Join(work, "depthwrite.schema")
	if err := os.WriteFile(path, []byte(luaWriteDepthSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("DepthwriteTables")
local J = require("json")
local function chain(name, n)
  local root = T["New" .. name]()
  local cur = root
  for _ = 1, n do
    local nxt = T["New" .. name]()
    cur.next = nxt
    cur = nxt
  end
  return root
end
for _, name in ipairs({ "ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode" }) do
  for _, n in ipairs({ 127, 128 }) do
    local text = J.ToJson(T, name, chain(name, n))
    local read = 0
    if text ~= nil then
      local report = {}
      local v = J.FromJson(T, name, text, report)
      if v ~= nil and not report.damaged then read = 1 end
    end
    print(name .. " " .. n .. " write=" .. (text ~= nil and 1 or 0) .. " read=" .. read)
  end
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	want := strings.Join(writeDepthWant, "\n")
	if got != want {
		t.Fatalf("the Lua text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// writeDepthWant is every row's verdict: 127 writes and reads back, 128 is
// refused by the writer.
var writeDepthWant = []string{
	"ArrNode 127 write=1 read=1", "ArrNode 128 write=0 read=0",
	"UnionNode 127 write=1 read=1", "UnionNode 128 write=0 read=0",
	"MapNode 127 write=1 read=1", "MapNode 128 write=0 read=0",
	"KeyedNode 127 write=1 read=1", "KeyedNode 128 write=0 read=0",
	"FlagsNode 127 write=1 read=1", "FlagsNode 128 write=0 read=0",
}
