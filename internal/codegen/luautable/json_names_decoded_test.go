package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE NAME A JSON STRING SPELLS IS ITS DECODED VALUE (docs/SPEC-TABLES.md
// §16.2 enum and union rows, §16.3): an escaped spelling names the same
// variant, union arm and keyed slot as the plain one. Every row is read by the
// emitted Luau walk and by schema-go's generated reader over the same corpus
// unit and the same text; the verdict and the value at the named field must agree.
type jsonNameRow struct {
	name, root, text string
	goValue, value   string
}

var jsonNameRows = []jsonNameRow{
	{"grade-escaped", "ProfileConfig", `{"has_loadout":true,"loadout":{"grade":"\u0047old"}}`,
		`fmt.Sprintf("%d", v.Loadout.Grade)`, `tostring(v.Loadout.Grade)`},
	{"grade-plain", "ProfileConfig", `{"has_loadout":true,"loadout":{"grade":"Gold"}}`,
		`fmt.Sprintf("%d", v.Loadout.Grade)`, `tostring(v.Loadout.Grade)`},
	{"arm-escaped", "ProfileConfig", `{"has_loadout":true,"loadout":{"primary":{"effect":{"\u0062uff":{"multiplier":2.0}}}}}`,
		`fmt.Sprintf("%d:%g", v.Loadout.Primary.Effect.Type, v.Loadout.Primary.Effect.Buff().Value.Multiplier)`,
		`string.format("%d:%s", v.Loadout.Primary.Effect.Type, tostring(v.Loadout.Primary.Effect.Buff and v.Loadout.Primary.Effect.Buff.Multiplier))`},
	{"arm-plain", "ProfileConfig", `{"has_loadout":true,"loadout":{"primary":{"effect":{"buff":{"multiplier":2.0}}}}}`,
		`fmt.Sprintf("%d:%g", v.Loadout.Primary.Effect.Type, v.Loadout.Primary.Effect.Buff().Value.Multiplier)`,
		`string.format("%d:%s", v.Loadout.Primary.Effect.Type, tostring(v.Loadout.Primary.Effect.Buff and v.Loadout.Primary.Effect.Buff.Multiplier))`},
	{"slot-escaped", "KeyedConfig", `{"teams":{"\u0052ed":{"spawn_count":7}}}`,
		`fmt.Sprintf("%d", v.Teams[0].SpawnCount)`, `tostring(v.Teams[1].SpawnCount)`},
	{"slot-plain", "KeyedConfig", `{"teams":{"Red":{"spawn_count":7}}}`,
		`fmt.Sprintf("%d", v.Teams[0].SpawnCount)`, `tostring(v.Teams[1].SpawnCount)`},
}

// jsonNamesUnit loads the tables corpus, where ProfileConfig names an enum and
// a union arm, and KeyedConfig names an enum-keyed slot.
func jsonNamesUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "tables", "examples", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no tables/examples/*.schema found")
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	return u
}

// jsonNamesGoOracle reads every row with schema-go's generated reader and
// prints its verdict and the value at the row's named field, the expected answer.
func jsonNamesGoOracle(t *testing.T, u *ir.Unit, rows []jsonNameRow) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	fmt.Fprintf(&b, `package %s

import (
	"fmt"
	"testing"
)

func TestJSONNamesOracle(t *testing.T) {
`, goPackageName(t, files))
	for _, r := range rows {
		fmt.Fprintf(&b, "\t{ var v %s; var report TableReport; if !%sFromJSON(&v, []byte(%q), &report) { fmt.Printf(\"@%s|refused\\n\"); } else { fmt.Printf(\"@%s|ok|%%s\\n\", %s) } }\n",
			r.root, r.root, r.text, r.name, r.name, r.goValue)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	files["go.mod"] = fmt.Appendf(nil, "module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", tfSerializeGoRuntime(t))
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestJSONNamesOracle$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	return jsonNameAnswers(string(out))
}

// goPackageName is the package clause the generated Go files share, which the
// oracle test must use to sit beside them.
func goPackageName(t *testing.T, files map[string][]byte) string {
	t.Helper()
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if after, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
				return strings.TrimSpace(after)
			}
		}
	}
	t.Fatal("the generated Go files name no package")
	return ""
}

// jsonNameAnswers collects the "@<name>|<verdict>[|<hex>]" lines a probe
// printed.
func jsonNameAnswers(out string) map[string]string {
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "@"); ok {
			if name, answer, ok := strings.Cut(after, "|"); ok {
				got[name] = answer
			}
		}
	}
	return got
}

// jsonNamesLuau reads every row with the emitted Luau walk and prints the same
// verdict and value at the named field.
func jsonNamesLuau(t *testing.T, g *generatedUnit, rows []jsonNameRow) map[string]string {
	t.Helper()
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(jsonFlags(g))
	b.WriteString(`local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local cases = {
`)
	for _, r := range rows {
		fmt.Fprintf(&b, "\t{ %q, %q, function(v) return %s end },\n", r.name, r.root, r.value)
	}
	b.WriteString(`}
for _, c in cases do
	local name, root, value = c[1], c[2], c[3]
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson(root, inputs[name], report)
	if v == nil then
		print("@" .. name .. "|refused")
	else
		print("@" .. name .. "|ok|" .. value(v))
	end
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, r := range rows {
		inputs[r.name] = []byte(r.text)
	}
	return jsonNameAnswers(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
}

// TestLuauJsonNamesCompareDecoded holds every name a JSON string spells to its
// DECODED value: the escaped enum, union-arm and keyed-slot rows agree with
// schema-go's reader and its value, and so do the plain controls.
func TestLuauJsonNamesCompareDecoded(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the Go toolchain")
	u := jsonNamesUnit(t)
	g := requiredFiles(t, u)
	want := jsonNamesGoOracle(t, u, jsonNameRows)
	got := jsonNamesLuau(t, g, jsonNameRows)
	var bad []string
	for _, r := range jsonNameRows {
		if want[r.name] == "" {
			t.Fatalf("%s: the Go oracle printed no answer", r.name)
		}
		if got[r.name] != want[r.name] {
			bad = append(bad, r.name)
			t.Logf("%s: Luau %q, Go %q", r.name, got[r.name], want[r.name])
		}
	}
	if len(bad) > 0 {
		t.Fatalf("the Luau JSON names disagree with schema-go on %v", bad)
	}
}
