package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// squirrelWriteDepthSchema is internal/tabletext's depthWriteUnit: a pointer
// chain whose every node holds one container the writer always emits, an
// EMPTY one at that: an array, a union, a map, an enum-keyed array and a
// flags value.
const squirrelWriteDepthSchema = `package depthwrite

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

const squirrelWriteDepthProbe = `local schema = dofile("gen/schema_module.nut", true);
local T = schema.require(%q);
local J = schema.require("schema_table_json");
local chain = function(name, n) {
  local root = T[name]();
  local cur = root;
  for (local i = 0; i < n; i++) {
    local nxt = T[name]();
    cur.Next = nxt;
    cur = nxt;
  }
  return root;
};
foreach (name in ["ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"]) {
  foreach (n in [127, 128]) {
    local text = J.ToJson(T, name, chain(name, n));
    local read = 0;
    if (text != null) {
      local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
        bodyDamaged = false, refused = false };
      if (J.FromJson(T, name, text, r) != null) read = 1;
    }
    print(name + " " + n + " write=" + (text != null ? 1 : 0) + " read=" + read + "\n");
  }
}
`

// TestSquirrelJsonWriteDepthCapCountsEveryContainer holds docs/SPEC-TABLES.md
// §16.2 and §16.14 W2 in the Squirrel text writer, as internal/tabletext's
// TestWriteDepthCapCountsEveryContainer holds the Go one: 127 nodes under the
// root put the last node's container at depth 128, which writes and reads
// back; 128 nodes put it at 129, which the writer refuses, because its own
// reader refuses that text.
func TestSquirrelJsonWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	sqrun.Require(t)
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "depthwrite.schema")
	writeFile(t, schemaPath, []byte(squirrelWriteDepthSchema))
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	outDir := filepath.Join(dir, "gen")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(outDir, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(outDir, name), data)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(squirrelWriteDepthProbe, squirreltable.ModuleName(unit))))
	got := strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", nil)), "\n")
	want := strings.Join([]string{
		"ArrNode 127 write=1 read=1", "ArrNode 128 write=0 read=0",
		"UnionNode 127 write=1 read=1", "UnionNode 128 write=0 read=0",
		"MapNode 127 write=1 read=1", "MapNode 128 write=0 read=0",
		"KeyedNode 127 write=1 read=1", "KeyedNode 128 write=0 read=0",
		"FlagsNode 127 write=1 read=1", "FlagsNode 128 write=0 read=0",
	}, "\n")
	if got != want {
		t.Fatalf("the Squirrel text writer's depth cap:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
