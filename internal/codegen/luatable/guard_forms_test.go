package luatable

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// guardField is one field of an instance by its text key.
func guardField(t *testing.T, inst *tabletext.Instance, key string) *tabletext.Field {
	t.Helper()
	fv, ok := inst.FieldByKey(key)
	if !ok {
		t.Fatalf("no field keyed %q on %s", key, inst.Def.Name)
	}
	return fv
}

// guardInstance builds the guard_true_both_edges value with its guard FALSE and
// both pointer slots still set: at_rest is false while always and resting each
// name a distinct Node. A walk that reads fd.guard drops resting; one that does
// not writes it, and numbers its target (§3, §3.1).
func guardInstance(t *testing.T, m *tabletext.Model, def *ir.Struct) *tabletext.Instance {
	t.Helper()
	node := m.Lookup("Node")
	if node == nil {
		t.Fatal("the unit declares no Node table")
	}
	always := m.New(node)
	guardField(t, always, "value").Cell.I = 7
	resting := m.New(node)
	guardField(t, resting, "value").Cell.I = 9
	inst := m.New(def)
	guardField(t, inst, "at_rest").Cell.B = false
	guardField(t, inst, "always").Cell.Node = always
	guardField(t, inst, "resting").Cell.Node = resting
	return inst
}

// guardProbe is the Lua main for one form: it builds the same value the Go
// engine holds, writes it with the named module verb, and prints the bytes as
// one hex line.
func guardProbe(module, verb string) string {
	return `local T = require("` + module + `")
local J = require("json")
local v = T.NewGuarded()
v.at_rest = false
v.always = T.NewNode()
v.always.value = 7
v.resting = T.NewNode()
v.resting.value = 9
local out
if "` + verb + `" == "json" then
  out = J.ToJson(T, "Guarded", v)
  assert(out ~= nil, "ToJson refused the value")
else
  local vocab = { announced = true, refBits = T.MessageRefBits }
  out = T.SaveMessages("Guarded", vocab, { v })
  assert(out ~= nil, "SaveMessages refused the value")
end
print((out:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
`
}

// TestLuaTableJsonAndMessageHonourGuards pins §3 and §3.1 on the two Lua save
// forms the variable save already keeps: a field under a false guard is elided
// whatever its storage holds, and it is not an edge, so its pointer target
// takes no node. The value is guard_true_both_edges with the guard false and
// the guarded pointer still set; each form must write what the Go engine
// (tabletext.Write for JSON, tablewire.EncodeMessage for the message form)
// writes for the same value.
func TestLuaTableJsonAndMessageHonourGuards(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	var row manifestRow
	found := false
	for _, r := range manifestRows(t, manifest, "instance") {
		if r.caseName == "guard_true_both_edges" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatal("the manifest names no instance guard_true_both_edges")
	}
	paths, ok := units[row.unit]
	if !ok {
		t.Fatalf("the manifest names no unit %s", row.unit)
	}
	var schemaPaths []string
	for _, p := range paths {
		schemaPaths = append(schemaPaths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, schemaPaths...)
	m := tabletext.NewModel(u)
	def := m.Lookup(row.root)
	if def == nil {
		t.Fatalf("unit %s declares no table %s", row.unit, row.root)
	}
	module := ModuleName(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", row.unit, err)
	}

	run := func(t *testing.T, verb string) []byte {
		t.Helper()
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := guardProbe(module, verb)
		luarun.WriteFiles(t, dir, map[string][]byte{"main.lua": []byte(probe)})
		out := luarun.Run(t, dir, "main.lua", nil)
		got, err := hex.DecodeString(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatalf("the probe printed %q, not one hex line: %v", out, err)
		}
		return got
	}

	t.Run("json_guard_false_drops_guarded_pointer", func(t *testing.T) {
		t.Parallel()
		inst := guardInstance(t, m, def)
		want, err := m.Write(inst)
		if err != nil {
			t.Fatalf("the Go engine refused the value: %v", err)
		}
		want = append(want, '\n')
		got := run(t, "json")
		if !bytes.Equal(got, want) {
			t.Errorf("the JSON save wrote the false-guard field or numbered its target\n    lua    %s\n    engine %s", hexBytes(got), hexBytes(want))
		}
	})

	t.Run("message_guard_false_drops_guarded_pointer", func(t *testing.T) {
		t.Parallel()
		inst := guardInstance(t, m, def)
		want, err := tablewire.EncodeMessage(m, inst)
		if err != nil {
			t.Fatalf("the Go engine refused the value: %v", err)
		}
		got := run(t, "message")
		if !bytes.Equal(got, want) {
			t.Errorf("the message save wrote the false-guard field or numbered its target\n    lua    %s\n    engine %s", hexBytes(got), hexBytes(want))
		}
	})
}
