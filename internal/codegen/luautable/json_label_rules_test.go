package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauJsonLabelRules holds docs/SPEC-TABLES.md §16.7's label rules in the
// Luau JSON read, placed and skipped, against the conformance corpus's
// graphdemo rows (testdata/conformance/tables/MANIFEST.txt, json-hostile): a
// label is followed by a comma or the object's close, never the next key; a
// skipped `{"&node": N}` alone refers, so N must be defined earlier and
// closed; a skipped `&node` with fields defines, so N must be new to the
// whole text. The verdict is the contract, so no reason is compared.
func TestLuauJsonLabelRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	const repo = "../../.."
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, root, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "json-hostile" || f[2] != "graphdemo" {
			continue
		}
		if !strings.HasPrefix(f[1], "label-") && !strings.HasPrefix(f[1], "skipped-") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repo, f[4], f[3]+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[1]] = body
		rows = append(rows, row{name: f[1], root: f[3], verdict: f[5]})
	}
	for _, must := range []string{"label-missing-comma", "skipped-reference-undefined", "skipped-label-defined-twice"} {
		if _, ok := inputs[must]; !ok {
			t.Fatalf("the manifest has no graphdemo json-hostile row %s", must)
		}
	}

	schemas := map[string]string{}
	unitDir := filepath.Join(repo, "tables", "pointers")
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".schema") {
			text, err := os.ReadFile(filepath.Join(unitDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			schemas[e.Name()] = string(text)
		}
	}
	g := generateFiles(t, schemas)
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(`local json = require("./schema_json")
local inputs = require("./inputs")
local cases = {
`)
	for _, r := range rows {
		fmt.Fprintf(&b, "\t{ %q, %q },\n", r.name, r.root)
	}
	b.WriteString(`}
for _, c in cases do
	local name, root = c[1], c[2]
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson(root, inputs[name], report)
	print(name .. "|" .. (if v ~= nil then "read" else "refused"))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", inputs))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, verdict, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			t.Fatalf("probe line %q: want name|verdict", line)
		}
		got[name] = verdict
	}
	for _, r := range rows {
		if got[r.name] != r.verdict {
			t.Errorf("%s (root %s): the Luau JSON read answered %q, want %q", r.name, r.root, got[r.name], r.verdict)
		}
	}
}
