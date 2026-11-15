package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaNodeTypeReservedIdRefused holds the Lua leg's strict read to the rule
// that a node record's TYPE reference may not resolve to one of the three ids
// the language holds back (docs/SPEC-TABLES.md §3.1, §3.3): the record is
// malformed and the whole load refuses. node_chain_two is the control that a
// lawful node table still reads.
func TestLuaNodeTypeReservedIdRefused(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths := units["graphdemo"]
	if len(paths) == 0 {
		t.Fatal("the manifest names no unit graphdemo")
	}
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, full...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate graphdemo: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)

	cases := []struct {
		name, root, verdict string
	}{
		{"node_type_ref_zero", "Scene", "refused"},
		{"node_type_node_table_id", "Scene", "refused"},
		{"node_type_build_version_id", "Scene", "refused"},
		{"node_type_vocabulary_id", "Scene", "refused"},
		{"node_chain_two", "ListNode", "read"},
	}
	inputs := map[string][]byte{}
	var rows strings.Builder
	for i, c := range cases {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "framing", c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("wire:%d", i)
		inputs[key] = wire
		fmt.Fprintf(&rows, "%s %s %s\n", key, c.name, c.root)
	}
	inputs["rows"] = []byte(rows.String())
	probe := `local inputs = require("inputs")
local T = require("` + ModuleName(u) + `")
local load = require("load")
for line in inputs.rows:gmatch("[^\n]+") do
  local key, name, root = line:match("^(%S+) (%S+) (%S+)$")
  local r = {}
  local v = load.Load(T, root, inputs[key], r, load.NewState())
  print(name .. " " .. (v ~= nil and "read" or "refused"))
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		got[name] = verdict
	}
	for _, c := range cases {
		if got[c.name] != c.verdict {
			t.Errorf("%s: the strict load answers %q, want %q", c.name, got[c.name], c.verdict)
		}
	}
}
