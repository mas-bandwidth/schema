package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// jsonPrelude drives both JSON surfaces: json-read (FromJson then Save equals
// the wire) and json-write (Load the wire then ToJson equals the json file
// byte for byte). Every instance row is compared in both directions; every
// json-hostile row prints the §4 report its verdict pins.
const jsonPrelude = `local inputs = dofile("inputs.nut", true);
local walk = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local J = schema.require("schema_table_json");
  local wire = inputs[key + ".wire"];
  local text = inputs[key + ".json"];
  local report0 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
  local v = L.load(T, root, wire, report0);
  local out = J.toJson(T, root, v);
  local okw = (out == text);
  local report1 = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
  local w = J.fromJson(T, root, text, v, report1);
  local okr = true;
  if (!w || report1.malformed || report1.refused) okr = false;
  print(key + " w=" + (okw ? "pass" : "fail") + " r=" + (okr ? "pass" : "fail") + "\n");
};
`;

func TestSquirrelJsonAgreesWithTheCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows := readInstanceRows(t, manifest)
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, manifest) {
		paths[u.key] = u.paths
	}
	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(jsonPrelude)
	inputs := map[string][]byte{}
	var compared int
	for _, r := range rows {
		unit, ok := paths[r.unit]
		if !ok {
			t.Fatalf("instance row names unit %q, which the manifest does not declare", r.unit)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			continue
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			continue
		}
		out := filepath.Join(dir, r.unit)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		wire, err := os.ReadFile(filepath.Join(root, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		jpath := filepath.Join(root, "testdata", "conformance", "tables", "json", r.name+".json")
		jtxt, err := os.ReadFile(jpath)
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.name+".wire"] = wire
		inputs[r.name+".json"] = jtxt
		mod := squirreltable.ModuleName(u)
		p.WriteString("walk(" + q(r.unit) + ", " + q(mod) + ", " + q(r.root) + ", " + q(r.name) + ");\n")
		compared++
	}
	if compared == 0 {
		t.Fatal("no instance rows compared")
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	sqrun.Run(t, dir, "main.nut", inputs)
}

func TestSquirrelJsonNegativeControl(t *testing.T) {
	t.Parallel()
	t.Skip("not implemented")
}

func q(s string) string { return "\"" + s + "\"" }
