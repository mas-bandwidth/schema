// A table that declares `forms = "..."` (docs/SPEC-TABLES.md §2.12) gets
// only the Lua surfaces of the forms it names; a form no table in the unit
// carries emits no runtime file at all, and a call for a form a table leaves
// out is refused by that form's name. The default — a table with no `forms`
// declaration — is every form, today's behaviour. `fixed`, `view`, `message`
// and `retain` sit outside the forms vocabulary and are governed by the
// surface list alone, so a `wire`-only table keeps them (the Go oracle,
// internal/codegen/gotable/declaredforms_test.go, does the same).
package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const declaredFormsSchema = `package probe

table Pong | forms = "wire,json"
{
    x int32
}
table Ping | forms = "wire"
{
    x int32
}
fixed table Fox | forms = "wire,block,cook"
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
`

// declaredFormsUnit writes the probe schema to a test directory, loads it and
// generates the Lua unit, the house pattern of load_values_test.go.
func declaredFormsUnit(t *testing.T, src string) (*ir.Unit, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return u, files
}

// declaredFormsRun writes the generated unit and one probe into a t.TempDir()
// and runs it under the pinned interpreter.
func declaredFormsRun(t *testing.T, files map[string][]byte, probe string, inputs map[string][]byte) []byte {
	t.Helper()
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.Run(t, dir, "main.lua", inputs)
}

// declaredFormsInstance builds one value through the Go text engine, the
// oracle's own storage, so every comparison below starts from Go's answer.
func declaredFormsInstance(t *testing.T, u *ir.Unit, root, text string) (*tabletext.Model, *tabletext.Instance) {
	t.Helper()
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("the unit declares no table %s", root)
	}
	inst := m.New(def)
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("the Go oracle could not read %s %s: %+v", root, text, report)
	}
	return m, inst
}

func TestLuaDeclaredForms(t *testing.T) {
	t.Parallel()

	t.Run("declared forms select the runtime files", func(t *testing.T) {
		_, files := declaredFormsUnit(t, declaredFormsSchema)
		for _, name := range []string{
			"json.lua", "textform.lua", "textformwrite.lua",
			"block.lua", "blockbuild.lua",
			"cook.lua", "uncook.lua", "cookwrite.lua",
		} {
			if _, ok := files[name]; !ok {
				t.Errorf("a unit whose tables carry json, block and cook omitted %s", name)
			}
		}
		// fixed, view, message and retain are outside the forms vocabulary
		for _, name := range []string{"fixed.lua", "message.lua", "retain.lua", "load.lua", "save.lua"} {
			if _, ok := files[name]; !ok {
				t.Errorf("a forms declaration suppressed the independent %s surface", name)
			}
		}

		_, wireOnly := declaredFormsUnit(t, `package probe

table Solo | forms = "wire"
{
    x int32
}
fixed table SoloFox | forms = "wire"
{
    x int32
}
`)
		for _, name := range []string{
			"json.lua", "textform.lua", "textformwrite.lua",
			"block.lua", "blockbuild.lua",
			"cook.lua", "uncook.lua", "cookwrite.lua",
		} {
			if _, ok := wireOnly[name]; ok {
				t.Errorf("a wire-only unit emitted the %s runtime", name)
			}
		}
		for _, name := range []string{"load.lua", "save.lua", "fixed.lua", "message.lua", "retain.lua"} {
			if _, ok := wireOnly[name]; !ok {
				t.Errorf("a wire-only unit lost the independent %s surface", name)
			}
		}
	})

	t.Run("undeclared units keep every form", func(t *testing.T) {
		_, files := declaredFormsUnit(t, "package probe\ntable Plain\n{\n    x int32\n}\n")
		for _, name := range []string{"json.lua", "block.lua", "cook.lua"} {
			if _, ok := files[name]; !ok {
				t.Errorf("a table with no forms declaration lost the default %s runtime", name)
			}
		}
	})

	t.Run("wire agrees with Go", func(t *testing.T) {
		u, files := declaredFormsUnit(t, declaredFormsSchema)
		m, inst := declaredFormsInstance(t, u, "Pong", `{"x":7}`)
		want, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "Pong", inputs.wire, report)
local saved = save.Save(T, "Pong", v)
assert(saved ~= nil, "Save refused a wire-carrying table")
io.write((saved:gsub(".", function(c) return string.format("%02x", c:byte()) end)), "\n")
`
		got := luarun.HexLines(t, declaredFormsRun(t, files, probe, map[string][]byte{"wire": want}))
		if len(got) != 1 || string(got[0]) != string(want) {
			t.Errorf("the Lua wire save differs from Go's: got %x want %x", got, want)
		}
	})

	t.Run("json agrees with Go and is refused when undeclared", func(t *testing.T) {
		u, files := declaredFormsUnit(t, declaredFormsSchema)
		m, inst := declaredFormsInstance(t, u, "Pong", `{"x":7}`)
		want, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local J = require("json")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "Pong", inputs.wire, report)
io.write(J.ToJson(T, "Pong", v))
local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local back = J.FromJson(T, "Pong", inputs.text, r)
assert(back ~= nil and r.malformed == false, "FromJson refused a json-carrying table")
assert(J.ToJson(T, "Pong", back) == J.ToJson(T, "Pong", v), "FromJson -> ToJson did not round-trip")
local ok, err = pcall(function() return J.ToJson(T, "Ping", T.NewPing()) end)
assert(not ok and tostring(err):find("Ping", 1, true) and tostring(err):find("json", 1, true),
  "ToJson of a wire-only table was not refused by name: " .. tostring(err))
local ok2, err2 = pcall(function() return J.FromJson(T, "Ping", "{}", r) end)
assert(not ok2 and tostring(err2):find("Ping", 1, true) and tostring(err2):find("json", 1, true),
  "FromJson of a wire-only table was not refused by name: " .. tostring(err2))
`
		got := declaredFormsRun(t, files, probe, map[string][]byte{"wire": wire, "text": want})
		if strings.TrimRight(string(got), "\n") != strings.TrimRight(string(want), "\n") {
			t.Errorf("the Lua JSON save differs from Go's:\n--- lua ---\n%s\n--- go ---\n%s", got, want)
		}
	})

	t.Run("block works when declared and is refused by name when not", func(t *testing.T) {
		u, files := declaredFormsUnit(t, declaredFormsSchema)
		probe := `local T = require("` + ModuleName(u) + `")
local B = require("block")
local BB = require("blockbuild")
local fox, why = BB.Begin(T, "Fox", {})
assert(fox ~= nil, "the block builder refused a block-carrying table: " .. tostring(type(why) == "table" and why.array or why))
local h, openWhy = B.BlockOpen(T, "Fox", fox.bytes, T.BuildVersion)
assert(h ~= nil, "BlockOpen refused a block-carrying table: " .. tostring(openWhy))
local ok, err = pcall(function() return B.BlockOpen(T, "FoxWire", "\0", T.BuildVersion) end)
assert(not ok and tostring(err):find("FoxWire", 1, true) and tostring(err):find("block", 1, true),
  "BlockOpen of a wire-only table was not refused by name: " .. tostring(err))
local ok2, err2 = pcall(function() return BB.Begin(T, "FoxWire", {}) end)
assert(not ok2 and tostring(err2):find("FoxWire", 1, true) and tostring(err2):find("block", 1, true),
  "Begin of a wire-only table was not refused by name: " .. tostring(err2))
print("block-forms-ok")
`
		out := string(declaredFormsRun(t, files, probe, nil))
		if !strings.Contains(out, "block-forms-ok") {
			t.Errorf("the block probe answered %q", out)
		}
	})

	t.Run("cook agrees with Go and is refused by name when undeclared", func(t *testing.T) {
		u, files := declaredFormsUnit(t, declaredFormsSchema)
		m, inst := declaredFormsInstance(t, u, "Fox", `{"x":7}`)
		want, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		probe := `local T = require("` + ModuleName(u) + `")
local C = require("cook")
local CW = require("cookwrite")
local v = T.NewFox()
v.x = 7
io.write(CW.Cook(T, "Fox", v))
local ok, err = pcall(function() return CW.Cook(T, "FoxWire", T.NewFox(), nil) end)
assert(not ok and tostring(err):find("FoxWire", 1, true) and tostring(err):find("cook", 1, true),
  "Cook of a wire-only table was not refused by name: " .. tostring(err))
local ok2, err2 = pcall(function() return C.Open(T, "FoxWire", nil) end)
assert(not ok2 and tostring(err2):find("FoxWire", 1, true) and tostring(err2):find("cook", 1, true),
  "Open of a wire-only table was not refused by name: " .. tostring(err2))
`
		got := declaredFormsRun(t, files, probe, nil)
		if string(got) != string(want) {
			t.Errorf("the Lua cook differs from Go's: got %d bytes want %d", len(got), len(want))
		}
	})

	t.Run("fixed and message stay independent of forms", func(t *testing.T) {
		u, files := declaredFormsUnit(t, declaredFormsSchema)
		module := string(files[ModuleName(u)+".lua"])
		for _, want := range []string{`_M.Fixed["FoxWire"]`, `_M.MessageRoots["FoxWire"]`} {
			if !strings.Contains(module, want) {
				t.Errorf("forms = wire suppressed the independent surface %s", want)
			}
		}
		if _, ok := files["fixed.lua"]; !ok {
			t.Error("forms = wire suppressed fixed.lua")
		}
		if _, ok := files["message.lua"]; !ok {
			t.Error("forms = wire suppressed message.lua")
		}
	})
}
