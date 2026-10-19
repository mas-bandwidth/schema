package luautable_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
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
	for _, name := range []string{"block.luau", "blockread.luau"} {
		if _, ok := g.files[name]; ok {
			t.Errorf("wire-only unit emitted the %s runtime", name)
		}
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
	for _, name := range []string{"block.luau", "blockread.luau"} {
		if _, ok := mixed.files[name]; !ok {
			t.Fatalf("mixed unit lost the undeclared Ping %s surface", name)
		}
	}
	probe := `local T = require("./ProbeTable")
local J = require("./json")
local B = require("./blockread")
assert(T.PongDescriptor.forms.wire == true)
assert(T.PongDescriptor.forms.json == nil)
assert(T.PingDescriptor.forms == nil)
local ok1, e1 = pcall(function() J.FromJson("Pong", buffer.fromstring("{}"), {}) end)
local ok2, e2 = pcall(function() J.ToJson("Pong", T.NewPong()) end)
assert(not ok1 and string.find(tostring(e1), "Pong") and string.find(tostring(e1), "json"))
assert(not ok2 and string.find(tostring(e2), "Pong") and string.find(tostring(e2), "json"))
assert(J.ToJson("Ping", T.NewPing()) ~= nil)
local ok3, e3 = pcall(function() B.BlockOpen(T.PongDescriptor, nil) end)
assert(not ok3 and string.find(tostring(e3), "Pong") and string.find(tostring(e3), "block"))
local blk, reason = B.BlockOpen(T.PingDescriptor, nil)
assert(blk == nil and reason == "unaligned_base")
print("forms refusal by Pong and json and block")
`
	if err := os.WriteFile(filepath.Join(mixed.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, mixed.dir, "main.luau", nil)
	if !strings.Contains(string(out), "forms refusal by Pong and json and block") {
		t.Errorf("mixed-unit probe output %q", out)
	}

	defaultUnit := generateFiles(t, map[string]string{"Probe.schema": "package probe\ntable Pong { x int32 }\n"})
	if strings.Contains(string(defaultUnit.files["ProbeTable.luau"]), "forms = {") {
		t.Error("undeclared table gained a forms marker")
	}
	// Unchanged BASE aeed625d's undeclared ProbeTable.luau: pin the
	// generated table module exactly. Other runtime files may evolve when
	// independent Luau cards merge, but forms must not change this module.
	const baseDefaultTableSHA256 = "0ea0a3e7cf1e869a7cefda98009725ccb595863d82c5f8a80574fd1b70ae1673"
	gotDefaultTableSHA256 := fmt.Sprintf("%x", sha256.Sum256(defaultUnit.files["ProbeTable.luau"]))
	if gotDefaultTableSHA256 != baseDefaultTableSHA256 {
		t.Errorf("undeclared table module changed from BASE: got %s want %s", gotDefaultTableSHA256, baseDefaultTableSHA256)
	}
	if _, ok := defaultUnit.files["json.luau"]; !ok {
		t.Error("undeclared table lost the default JSON runtime")
	}
	for _, name := range []string{"block.luau", "blockread.luau"} {
		if _, ok := defaultUnit.files[name]; !ok {
			t.Errorf("undeclared table lost the default %s runtime", name)
		}
	}
}
