package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/luau/luaurun"
)

// luauWriteDepthSchema is internal/tabletext's depthWriteUnit: a pointer
// chain whose every node holds one container the writer always emits, an
// EMPTY one at that: an array, a union, a map, an enum-keyed array and a
// flags value.
const luauWriteDepthSchema = `package depthwrite

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

// TestLuauJsonWriteDepthCapCountsEveryContainer holds docs/SPEC-TABLES.md
// §16.2 and §16.14 W2 in the Luau text writer, as internal/tabletext's
// TestWriteDepthCapCountsEveryContainer holds the Go one: 127 nodes under the
// root put the last node's container at depth 128, which writes and reads
// back; 128 nodes put it at 129, which the writer refuses, because its own
// reader refuses that text.
func TestLuauJsonWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"depthwrite.schema": luauWriteDepthSchema})
	var mod string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mod = strings.TrimSuffix(name, ".luau")
		}
	}
	if mod == "" {
		t.Fatal("the unit emitted no Table module")
	}
	probe := jsonModules(g) + `local json = require("./schema_json")
local T: any = require("./` + mod + `")
local function chain(name: string, n: number): any
  local root = T["New" .. name]()
  local cur = root
  for _ = 1, n do
    local nxt = T["New" .. name]()
    cur.Next = nxt
    cur = nxt
  end
  return root
end
for _, name in { "ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode" } do
  for _, n in { 127, 128 } do
    local text = json.ToJson(name, chain(name, n), {})
    local read = 0
    if text ~= nil and json.FromJson(name, text, {}) ~= nil then read = 1 end
    print(name .. " " .. tostring(n) .. " write=" .. (if text ~= nil then "1" else "0") .. " read=" .. tostring(read))
  end
end
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", nil)), "\n")
	want := strings.Join([]string{
		"ArrNode 127 write=1 read=1", "ArrNode 128 write=0 read=0",
		"UnionNode 127 write=1 read=1", "UnionNode 128 write=0 read=0",
		"MapNode 127 write=1 read=1", "MapNode 128 write=0 read=0",
		"KeyedNode 127 write=1 read=1", "KeyedNode 128 write=0 read=0",
		"FlagsNode 127 write=1 read=1", "FlagsNode 128 write=0 read=0",
	}, "\n")
	if got != want {
		t.Fatalf("the Luau text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
