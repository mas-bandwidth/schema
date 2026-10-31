package luautable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauFormsDeclaration holds docs/SPEC-TABLES.md §2.12 on the Luau leg:
// the closed forms vocabulary is wire, json, block, cook; fixed, view, message
// and retain are governed by surface, not forms; and wire bytes match
// schema-go's tablewire oracle on the same schema.
func TestLuauFormsDeclaration(t *testing.T) {
	t.Parallel()
	wireOnly := `package probe

table Pong | forms = "wire"
{
    x int32
}
fixed table Fox | forms = "wire"
{
    x int32
}
`
	g := generateFiles(t, map[string]string{"Probe.schema": wireOnly})
	for _, name := range []string{"schema_json.luau", "schema_textform.luau", "schema_cook.luau", "schema_cookread.luau", "schema_block.luau", "schema_blockread.luau"} {
		if _, ok := g.files[name]; ok {
			t.Errorf("wire-only unit emitted the %s runtime", name)
		}
	}
	module := string(g.files["ProbeTable.luau"])
	if strings.Count(module, "forms = { wire = true, },") != 2 {
		t.Error("Pong and Fox descriptors do not both name their declared wire form")
	}
	if !strings.Contains(module, "TableMessageAnnouncement") {
		t.Error("forms = wire suppressed the independent message surface")
	}
	if !strings.Contains(module, "FoxFixedLayout") || !strings.Contains(module, "FoxFixedHash") {
		t.Error("forms = wire suppressed the independent fixed surface")
	}
	for _, indep := range []string{"schema_view.luau", "schema_retain.luau", "schema_message.luau", "schema_fixed.luau", "schema_load.luau", "schema_save.luau"} {
		if _, ok := g.files[indep]; !ok {
			t.Errorf("forms = wire suppressed the independent %s surface", indep)
		}
	}
	analyzeUnit(t, g)

	// The expected wire is computed by Go's engine from the same declaration
	// and instance, rather than transcribed from the Luau result.
	unitDir := t.TempDir()
	p := filepath.Join(unitDir, "Probe.schema")
	if err := os.WriteFile(p, []byte(wireOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Pong"))
	var report tabletext.Report
	if !m.Read(inst, []byte(`{"x":7}`), &report) || !report.Silent() {
		t.Fatalf("Go oracle could not read instance: %+v", report)
	}
	want, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	got, measured, err := saveRoot(t, g, want, "Pong")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || measured != len(want) {
		t.Errorf("Luau wire save/load differs from Go: saved=%x want=%x measured=%d", got, want, measured)
	}

	mixed := generateFiles(t, map[string]string{"Probe.schema": `package probe

table Pong | forms = "wire"
{
    x int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`})
	for _, name := range []string{"schema_json.luau", "schema_textform.luau", "schema_cook.luau", "schema_cookread.luau", "schema_block.luau", "schema_blockread.luau"} {
		if _, ok := mixed.files[name]; !ok {
			t.Fatalf("mixed unit lost the undeclared %s surface", name)
		}
	}
	analyzeUnit(t, mixed)
	probe := `local T = require("./ProbeTable")
local J = require("./schema_json")
local X = require("./schema_textform")
assert(type(X.LoadTextForm) == "function")
local B = require("./schema_blockread")
local C = require("./schema_cookread")
local CW = require("./schema_cook")
assert(T.PongDescriptor.forms.wire == true)
assert(T.PongDescriptor.forms.json == nil)
assert(T.FoxWireDescriptor.forms.wire == true)
assert(T.FoxWireDescriptor.forms.block == nil)
assert(T.PingDescriptor.forms == nil)
assert(T.FoxAllDescriptor.forms == nil)
local ok1, e1 = pcall(function() J.FromJson("Pong", buffer.fromstring("{}"), {}) end)
local ok2, e2 = pcall(function() J.ToJson("Pong", T.NewPong()) end)
assert(not ok1 and string.find(tostring(e1), "Pong") and string.find(tostring(e1), "json"))
assert(not ok2 and string.find(tostring(e2), "Pong") and string.find(tostring(e2), "json"))
assert(J.ToJson("Ping", T.NewPing()) ~= nil)
local okTf, eTf = pcall(function() X.LoadTextForm("Pong", "()") end)
assert(not okTf and string.find(tostring(eTf), "Pong") and string.find(tostring(eTf), "json"))
local doc, val, tfErr = X.LoadTextForm("Ping", "(:x 5)")
assert(tfErr == nil and doc ~= nil and val.X == 5)
local ok3, e3 = pcall(function() B.BlockOpen(T.PongDescriptor :: any, nil) end)
assert(not ok3 and string.find(tostring(e3), "Pong") and string.find(tostring(e3), "block"))
local ok3f, e3f = pcall(function() B.BlockOpen(T.FoxWireDescriptor :: any, nil) end)
assert(not ok3f and string.find(tostring(e3f), "FoxWire") and string.find(tostring(e3f), "block"))
local blk, reason = B.BlockOpen(T.FoxAllDescriptor :: any, nil)
assert(blk == nil and reason == "unaligned_base")
local ok4, e4 = pcall(function() C.open("Pong", nil) end)
assert(not ok4 and string.find(tostring(e4), "Pong") and string.find(tostring(e4), "cook"))
local ok5, e5 = pcall(function() CW.Cook(T.PongDescriptor, T.NewPong(), 1) end)
assert(not ok5 and string.find(tostring(e5), "Pong") and string.find(tostring(e5), "cook"))
local cooked, cookReason = C.open("Ping", nil)
assert(cooked == nil and cookReason == "unaligned_base")
print("forms refusal by Pong and FoxWire across json, textform, block and cook")
`
	if err := os.WriteFile(filepath.Join(mixed.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, mixed.dir, "main.luau", nil)
	if !strings.Contains(string(out), "forms refusal by Pong and FoxWire across json, textform, block and cook") {
		t.Errorf("mixed-unit probe output %q", out)
	}
}

func analyzeUnit(t *testing.T, g *generatedUnit) {
	t.Helper()
	var names []string
	for name := range g.files {
		names = append(names, name)
	}
	sort.Strings(names)
	luaurun.Analyze(t, g.dir, names...)
}
