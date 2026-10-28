package luatable

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// packProbe builds the corpus tree's instance from its canonical text, saves
// it, then loads the packed wire, saves and writes it. It prints the three
// pieces the goldens compare: the save of the text-built instance, the save of
// the loaded instance, and the loaded instance's text (docs/SPEC-TABLES.md
// §17.1).
func packProbe(module, root string) string {
	return `local T = require("` + module + `")
local J = require("json")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local function hex(s)
  if s == nil then return "nil" end
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local r1 = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local built = J.FromJson(T, "` + root + `", inputs.text, r1)
print(hex(save.Save(T, "` + root + `", built)))
local r2 = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local loaded = load.LoadTolerant(T, "` + root + `", inputs.wire, r2)
print(hex(save.Save(T, "` + root + `", loaded)))
print(hex(J.ToJson(T, "` + root + `", loaded)))
`
}

// packHex decodes one probe line, where "nil" means the runtime refused.
func packHex(t *testing.T, line, what string) []byte {
	t.Helper()
	if line == "nil" {
		t.Fatalf("%s refused (nil)", what)
	}
	data, err := hex.DecodeString(line)
	if err != nil {
		t.Fatalf("%s: %v (line %q)", what, err, line)
	}
	return data
}

// TestLuaPackGoldensAgree holds the Lua text and wire walks to the engine over
// internal/tablepack's corpus trees (docs/SPEC-TABLES.md §17.1, §17.5): pack's
// bytes equal the Lua Save of the same instance built from the tree's text;
// pack -> Lua Load -> Lua ToJson -> unpack is byte-stable; and unpack
// --one-file's text is byte-identical to Lua ToJson.
func TestLuaPackGoldensAgree(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths, ok := units["tabledemo"]
	if !ok {
		t.Fatal("the manifest declares no tabledemo unit")
	}
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, full...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit tabledemo: %v", err)
	}
	model := tabletext.NewModel(u)
	trees := []struct{ name, root, dir string }{
		{"config", "PackConfig", filepath.Join(repo, "tables", "pack", "config")},
		{"root", "RootConfig", filepath.Join(repo, "tables", "pack", "root")},
	}
	for _, tree := range trees {
		t.Run(tree.name, func(t *testing.T) {
			t.Parallel()
			wire, _, report, err := tablepack.Pack(model, tree.root, tree.dir)
			if err != nil {
				t.Fatalf("pack %s: %v", tree.root, err)
			}
			if !report.Silent() {
				t.Fatalf("the corpus tree should match the schema exactly: %+v", report)
			}
			one := t.TempDir()
			if _, err := tablepack.UnpackOneFile(model, tree.root, wire, one); err != nil {
				t.Fatalf("unpack --one-file %s: %v", tree.root, err)
			}
			golden, err := os.ReadFile(filepath.Join(one, tree.root+".json"))
			if err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(packProbe(ModuleName(u), tree.root)), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.Split(strings.TrimRight(string(luarun.Run(t, dir, "main.lua",
				map[string][]byte{"text": golden, "wire": wire})), "\n"), "\n")
			if len(out) != 3 {
				t.Fatalf("probe printed %d lines, want 3: %q", len(out), out)
			}
			builtSave := packHex(t, out[0], "Save of the text-built instance")
			loadedSave := packHex(t, out[1], "Save of the loaded instance")
			loadedText := packHex(t, out[2], "ToJson of the loaded instance")

			if !bytes.Equal(builtSave, wire) {
				t.Errorf("pack's bytes differ from Save of the same instance (%d against %d bytes)", len(wire), len(builtSave))
			}
			if !bytes.Equal(loadedSave, wire) {
				t.Errorf("Save of the packed instance differs from pack's bytes (%d against %d bytes)", len(wire), len(loadedSave))
			}
			if !bytes.Equal(loadedText, golden) {
				t.Errorf("unpack --one-file's text differs from ToJson\n    want %q\n    got  %q", golden, loadedText)
			}

			again := t.TempDir()
			if err := os.WriteFile(filepath.Join(again, tree.root+".json"), loadedText, 0o644); err != nil {
				t.Fatal(err)
			}
			repacked, _, report, err := tablepack.Pack(model, tree.root, again)
			if err != nil {
				t.Fatalf("pack of ToJson's text: %v", err)
			}
			if !report.Silent() {
				t.Fatalf("ToJson's text should pack silently: %+v", report)
			}
			if !bytes.Equal(repacked, wire) {
				t.Errorf("pack -> Load -> ToJson -> pack moved bytes (%d against %d)", len(repacked), len(wire))
			}
			stable := t.TempDir()
			if _, err := tablepack.UnpackOneFile(model, tree.root, repacked, stable); err != nil {
				t.Fatalf("unpack --one-file after ToJson: %v", err)
			}
			text, err := os.ReadFile(filepath.Join(stable, tree.root+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(text, loadedText) {
				t.Errorf("pack -> Load -> ToJson -> unpack moved the text\n    want %q\n    got  %q", loadedText, text)
			}
		})
	}
}
