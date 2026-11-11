package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// luaWalkDamageRows are framing vectors whose damage the walk itself must
// discard, with no wire-alone check in front of it: a node table carried
// twice in one body (a field id repeated) and a node record whose body ends
// before its L (docs/SPEC-TABLES.md §3, §3.1, §4; docs/LAYERS/1-framing.md).
// The engine's decode refuses both, so the tolerant walk load.loadWalk, a
// public read, must discard them too: DAMAGE DISCARDS THE WHOLE LOAD.
var luaWalkDamageRows = []string{"node_table_twice", "node_record_bytes_after_terminator"}

const luaWalkDamageProbe = `local inputs = require("inputs")
local T = require(inputs.module)
local load = require("load")
for line in inputs.rows:gmatch("[^\n]+") do
  local name, root = line:match("^(%S+) (%S+)$")
  local r = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local v = load.loadWalk(T, root, inputs["wire:" .. name], r)
  print(name .. " " .. (v == nil and r.damaged and "discarded" or "read"))
end
`

// TestLuaTolerantWalkDiscardsTheDecodeDamage holds load.loadWalk to the
// engine's decode on the rows above: the engine refuses each as damage, and
// the walk must answer no value and a damaged report.
func TestLuaTolerantWalkDiscardsTheDecodeDamage(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	byName := map[string]luaFramingRow{}
	for _, r := range rows {
		byName[r.name] = r
	}
	for _, name := range luaWalkDamageRows {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("the manifest has no framing row %s", name)
		}
		var full []string
		for _, p := range units[row.unit] {
			full = append(full, filepath.Join("..", "..", "..", p))
		}
		u := luarun.LoadUnit(t, full...)
		wire, err := os.ReadFile(filepath.Join("..", "..", "..", row.wire))
		if err != nil {
			t.Fatal(err)
		}
		// THE ORACLE: the engine's decode discards the row as damage
		m := tabletext.NewModel(u)
		var rep tabletext.Report
		if got, derr := tablewire.Decode(m, m.New(m.Lookup(row.root)), wire, &rep); got || derr == nil || !strings.HasPrefix(derr.Error(), "wire_damaged") {
			t.Fatalf("%s: the engine's decode answers read=%t err=%v, want wire damage", name, got, derr)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		writeFile(t, filepath.Join(dir, "main.lua"), luaWalkDamageProbe)
		out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
			"module":       []byte(ModuleName(u)),
			"rows":         []byte(name + " " + row.root + "\n"),
			"wire:" + name: wire,
		})))
		if out != name+" discarded" {
			t.Errorf("%s: the tolerant walk answers %q, want discarded", name, out)
		}
	}
}
