package luautable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauFormsDeclaration holds §2.12 to Go's tablewire bytes and checks
// that the optional JSON surface follows each table's declaration. Fixed and
// message remain separate surfaces and are unaffected by forms.
func TestLuauFormsDeclaration(t *testing.T) {
	t.Parallel()
	wireOnly := `package probe

table Pong | forms = "wire"
{
    x int32
}
`
	g := generateFiles(t, map[string]string{"Probe.schema": wireOnly})
	if _, ok := g.files["json.luau"]; ok {
		t.Error("wire-only unit emitted the JSON runtime")
	}
	module := string(g.files["ProbeTable.luau"])
	if !strings.Contains(module, "forms = { wire = true, },") {
		t.Error("Pong descriptor does not name its declared wire form")
	}
	if !strings.Contains(module, "TableMessageAnnouncement") {
		t.Error("forms suppressed the independent message surface")
	}

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
`})
	if _, ok := mixed.files["json.luau"]; !ok {
		t.Fatal("mixed unit lost the undeclared Ping JSON surface")
	}
	probe := `local T = require("./ProbeTable")
local J = require("./json")
assert(T.PongDescriptor.forms.wire == true)
assert(T.PongDescriptor.forms.json == nil)
assert(T.PingDescriptor.forms == nil)
local ok1, e1 = pcall(function() J.FromJson("Pong", buffer.fromstring("{}"), {}) end)
local ok2, e2 = pcall(function() J.ToJson("Pong", T.NewPong()) end)
assert(not ok1 and string.find(tostring(e1), "Pong") and string.find(tostring(e1), "json"))
assert(not ok2 and string.find(tostring(e2), "Pong") and string.find(tostring(e2), "json"))
assert(J.ToJson("Ping", T.NewPing()) ~= nil)
print("forms refusal by Pong and json")
`
	if err := os.WriteFile(filepath.Join(mixed.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, mixed.dir, "main.luau", nil)
	if !strings.Contains(string(out), "forms refusal by Pong and json") {
		t.Errorf("mixed-unit probe output %q", out)
	}

	defaultUnit := generateFiles(t, map[string]string{"Probe.schema": "package probe\ntable Pong { x int32 }\n"})
	if strings.Contains(string(defaultUnit.files["ProbeTable.luau"]), "forms = {") {
		t.Error("undeclared table gained a forms marker")
	}
	if _, ok := defaultUnit.files["json.luau"]; !ok {
		t.Error("undeclared table lost the default JSON runtime")
	}

	// Block and cook are in section 2.12's closed vocabulary, but this Luau
	// leg has neither accelerator yet: accepting a declaration would drop it.
	for _, form := range []string{"block", "cook"} {
		src := "package probe\ntable Pong | forms = \"wire," + form + "\"\n{ x int32 }\n"
		dir := t.TempDir()
		path := filepath.Join(dir, "Probe.schema")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		_, err = luautable.Generate(u)
		if err == nil || !strings.Contains(err.Error(), "Pong") || !strings.Contains(err.Error(), form) {
			t.Errorf("unsupported %s declaration was not refused by name: %v", form, err)
		}
	}
}
