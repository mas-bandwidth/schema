package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// TestLuauTableFeatureWitnessesFullAndArrayRoot loads the featurewitnesses
// unit's feature_full and feature_array_root rows and holds, for each, the
// three things the unit's descriptors, runtime and layout have to agree on:
// the generated record layout equals the compiler's, FromJson then Save and
// Load then ToJson give the corpus wire and JSON bytes (docs/SPEC-TABLES.md
// §16.1, §16.10), and Load then Save returns the wire unchanged
// (§2.13, §2.13.1).
func TestLuauTableFeatureWitnessesFullAndArrayRoot(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "featurewitnesses")
	if g.err != nil {
		t.Fatalf("featurewitnesses does not generate for Luau: %v", g.err)
	}

	found := false
	for _, lu := range luauLayoutUnits(t) {
		if lu.key != "featurewitnesses" {
			continue
		}
		found = true
		bad, records := layoutDisagreements(t, lu, luauLayoutLines(t, lu.g, lu.names))
		for _, b := range bad {
			t.Errorf("layout: %s", b)
		}
		if records == 0 {
			t.Error("layout: no record compared")
		}
	}
	if !found {
		t.Fatal("featurewitnesses carries no cook reader: the layout check is blind")
	}

	for _, row := range []struct{ name, root string }{
		{"feature_full", "Root"},
		{"feature_array_root", "ArrayRoot"},
	} {
		text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", row.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "feature-witnesses", "full", row.root+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"text": text, "wire": wire}

		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonInstanceProbe(g, row.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n")
		parts := strings.Split(out, "\n")
		if len(parts) != 3 {
			t.Errorf("%s: json probe wrote %q, want 3 lines", row.name, out)
			continue
		}
		if parts[2] != "00" {
			t.Errorf("%s: a direction reported malformed (%s)", row.name, parts[2])
		}
		if parts[0] != hexBytes(wire) {
			t.Errorf("%s: FromJson then Save differs from the wire", row.name)
		}
		if parts[1] != hexBytes(text) {
			t.Errorf("%s: Load then ToJson differs from the json text", row.name)
		}

		probe := jsonModules(g) + fmt.Sprintf(`local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.load(%q, inputs.wire, report)
if v == nil then print("nil") return end
local b = save.save(St.lookup(%q), v)
if b == nil then print("nil") return end
local parts = table.create(buffer.len(b))
for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%%02x", buffer.readu8(b, i)) end
print(table.concat(parts))
`, row.root, row.root)
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n")
		if got != hexBytes(wire) {
			t.Errorf("%s: Load then Save returns %q, want the wire %s", row.name, got, hexBytes(wire))
		}
	}
}
