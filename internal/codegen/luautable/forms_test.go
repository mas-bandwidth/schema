package luautable_test

import (
	"bytes"
	"fmt"
	"maps"
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

const mixedFormsSchema = `package probe

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
`

const mixedFormsProbe = `local T = require("./ProbeTable")
local J = require("./schema_json")
local X = require("./schema_textform")
local B = require("./schema_blockread")
local C = require("./schema_cookread")
local CW = require("./schema_cook")
local function check(cond: boolean, why: string)
	if not cond then
		print("CLAUSE " .. why)
	end
end
check(type(X.LoadTextForm) == "function", "LoadTextForm.missing")
check(T.PongDescriptor.forms ~= nil and T.PongDescriptor.forms.wire == true, "Pong.forms.wire")
check(T.PongDescriptor.forms ~= nil and T.PongDescriptor.forms.json == nil, "Pong.forms.json")
check(T.FoxWireDescriptor.forms ~= nil and T.FoxWireDescriptor.forms.wire == true, "FoxWire.forms.wire")
check(T.FoxWireDescriptor.forms ~= nil and T.FoxWireDescriptor.forms.block == nil, "FoxWire.forms.block")
check(T.PingDescriptor.forms == nil, "Ping.forms.not_nil")
check(T.FoxAllDescriptor.forms == nil, "FoxAll.forms.not_nil")
local ok1, e1 = pcall(function() J.FromJson("Pong", buffer.fromstring("{}"), {}) end)
local ok2, e2 = pcall(function() J.ToJson("Pong", T.NewPong()) end)
check(not ok1 and string.find(tostring(e1), "Pong") ~= nil and string.find(tostring(e1), "json") ~= nil, "Pong.FromJson.refusal")
check(not ok2 and string.find(tostring(e2), "Pong") ~= nil and string.find(tostring(e2), "json") ~= nil, "Pong.ToJson.refusal")
local okPingJson, pingJson = pcall(function() return J.ToJson("Ping", T.NewPing()) end)
check(okPingJson and pingJson ~= nil, "Ping.ToJson.allowed")
local okTf, eTf = pcall(function() X.LoadTextForm("Pong", "()") end)
check(not okTf and string.find(tostring(eTf), "Pong") ~= nil and string.find(tostring(eTf), "json") ~= nil, "Pong.LoadTextForm.refusal")
local okPingTf, doc, val, tfErr = pcall(function() return X.LoadTextForm("Ping", "(:x 5)") end)
check(okPingTf and tfErr == nil and doc ~= nil and val.X == 5, "Ping.LoadTextForm.allowed")
local ok3, e3 = pcall(function() B.BlockOpen(T.PongDescriptor :: any, nil) end)
check(not ok3 and string.find(tostring(e3), "Pong") ~= nil and string.find(tostring(e3), "block") ~= nil, "Pong.BlockOpen.refusal")
local ok3f, e3f = pcall(function() B.BlockOpen(T.FoxWireDescriptor :: any, nil) end)
check(not ok3f and string.find(tostring(e3f), "FoxWire") ~= nil and string.find(tostring(e3f), "block") ~= nil, "FoxWire.BlockOpen.refusal")
local blk, reason = B.BlockOpen(T.FoxAllDescriptor :: any, nil)
check(blk == nil and reason == "unaligned_base", "FoxAll.BlockOpen.allowed")
local ok4, e4 = pcall(function() C.open("Pong", nil) end)
check(not ok4 and string.find(tostring(e4), "Pong") ~= nil and string.find(tostring(e4), "cook") ~= nil, "Pong.CookRead.refusal")
local ok5, e5 = pcall(function() CW.Cook(T.PongDescriptor, T.NewPong(), 1) end)
check(not ok5 and string.find(tostring(e5), "Pong") ~= nil and string.find(tostring(e5), "cook") ~= nil, "Pong.CookWrite.refusal")
local okPingCook, cooked, cookReason = pcall(function() return C.open("Ping", nil) end)
check(okPingCook and cooked == nil and cookReason == "unaligned_base", "Ping.CookRead.allowed")
print("forms refusal by Pong and FoxWire across json, textform, block and cook")
`

func verifyLuauFormsMixedUnit(t *testing.T, files map[string][]byte) error {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(mixedFormsProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, dir, "main.luau", nil))
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if after, ok := strings.CutPrefix(line, "CLAUSE "); ok {
			return fmt.Errorf("forms clause violation: %s", after)
		}
	}
	if !strings.Contains(out, "forms refusal by Pong and FoxWire across json, textform, block and cook") {
		return fmt.Errorf("mixed-unit probe output %q", out)
	}
	return nil
}

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

	mixed := generateFiles(t, map[string]string{"Probe.schema": mixedFormsSchema})
	for _, name := range []string{"schema_json.luau", "schema_textform.luau", "schema_cook.luau", "schema_cookread.luau", "schema_block.luau", "schema_blockread.luau"} {
		if _, ok := mixed.files[name]; !ok {
			t.Fatalf("mixed unit lost the undeclared %s surface", name)
		}
	}
	analyzeUnit(t, mixed)
	if err := verifyLuauFormsMixedUnit(t, mixed.files); err != nil {
		t.Fatal(err)
	}
}

// TestLuauFormsDeclarationNegativeControl mutates the forms map on declared
// and undeclared tables in ProbeTable.luau and requires
// verifyLuauFormsMixedUnit to fail on that clause.
func TestLuauFormsDeclarationNegativeControl(t *testing.T) {
	t.Parallel()
	mixed := generateFiles(t, map[string]string{"Probe.schema": mixedFormsSchema})

	controls := []struct {
		name   string
		from   string
		to     string
		expect string
	}{
		{
			name:   "allow-undeclared-json",
			from:   "table = true,\n\tforms = { wire = true, },\n\tfixed = false,",
			to:     "table = true,\n\tforms = { wire = true, json = true, },\n\tfixed = false,",
			expect: "Pong.forms.json",
		},
		{
			name:   "drop-declared-forms",
			from:   "table = true,\n\tforms = { wire = true, },\n\tfixed = false,",
			to:     "table = true,\n\tfixed = false,",
			expect: "Pong.forms.wire",
		},
		{
			name:   "restrict-undeclared-ping",
			from:   "table = true,\n\tfixed = false,",
			to:     "table = true,\n\tforms = { wire = true, },\n\tfixed = false,",
			expect: "Ping.forms.not_nil",
		},
	}

	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			files := maps.Clone(mixed.files)
			src := string(files["ProbeTable.luau"])
			if strings.Count(src, ctl.from) != 1 {
				t.Fatalf("ProbeTable.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
			}
			files["ProbeTable.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			err := verifyLuauFormsMixedUnit(t, files)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): broken ProbeTable.luau passed verifyLuauFormsMixedUnit", ctl.name)
			}
			if !strings.Contains(err.Error(), ctl.expect) {
				t.Fatalf("NEGATIVE CONTROL (%s) failed with %q, want substring %q", ctl.name, err.Error(), ctl.expect)
			}
			t.Logf("red as required (%s): %v", ctl.name, err)
		})
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
