package luatable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// Real Lua writer output with multiple node types, repeated node types and
// nested fields must read in Lua and the independent Go engine. This tests
// deferred node IDs in their actual writer order, not a manufactured frame.
func TestLuaWriterNodeIdsFirstUse(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)
	probe := `local i = require("inputs")
local T, save, load = require(i.module), require("save"), require("load")
local s, first, second, tree = T.NewScene(), T.NewListNode(), T.NewListNode(), T.NewTreeNode()
s.name = "scene"
first.value, first.name, first.next = 11, "first", second
second.value, second.name = 22, "second"
tree.label = "tree"
s.head, s.alias, s.tree = first, first, tree
local wire = assert(save.Save(T, "Scene", s))
local r = {}
local got = assert(load.Load(T, "Scene", wire, r), r.reason)
assert(got.head.value == 11 and got.head.next.value == 22 and got.tree.label == "tree")
assert(got.head == got.alias)
require("hexline")(wire)
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	lines := luarun.HexLines(t, luarun.Run(t, dir, "main.lua", map[string][]byte{"module": []byte(ModuleName(u))}))
	if len(lines) != 1 {
		t.Fatalf("writer produced %d wires", len(lines))
	}
	m := tabletext.NewModel(u)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(m.Lookup("Scene")), lines[0], &report)
	if !ok || err != nil || report.Refused {
		t.Fatalf("Go engine refused Lua writer's graph: ok=%v err=%v report=%+v", ok, err, report)
	}
}
