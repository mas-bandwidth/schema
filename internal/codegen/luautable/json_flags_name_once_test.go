package luautable_test

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// held to the verdict the vectors state. Running the pinned Luau interpreter
// makes this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const luauJsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type luauJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func luauJsonFlagsNameOnceRows(t *testing.T) []luauJsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(luauJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []luauJsonFlagsNameOnceRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 6 {
			t.Fatalf("MANIFEST.tsv: %d columns, want 6: %q", len(c), line)
		}
		if c[4] != "read" && c[4] != "refused" {
			t.Fatalf("MANIFEST.tsv: verdict %q, want read or refused: %q", c[4], line)
		}
		rows = append(rows, luauJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// luauJsonFlagsNameOnceGenerate emits both surfaces for the unit directory
// and names the emitted files and the flags metadata.
func luauJsonFlagsNameOnceGenerate(t *testing.T, unit string) *generatedUnit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", unit, err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true, flags: flagsMeta(u)}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// luauJsonFlagsNameOnceOracle answers the Go engine's verdict for each row,
// and holds it to the verdict the vectors state.
func luauJsonFlagsNameOnceOracle(t *testing.T, rows []luauJsonFlagsNameOnceRow) []string {
	t.Helper()
	models := map[string]*tabletext.Model{}
	var want []string
	for _, row := range rows {
		model := models[row.unit]
		if model == nil {
			paths, err := compiler.GatherPaths([]string{filepath.Join(root, row.unit)})
			if err != nil {
				t.Fatalf("%s: %v", row.unit, err)
			}
			u, err := compiler.New().Load(paths)
			if err != nil {
				t.Fatalf("%s: %v", row.unit, err)
			}
			model = tabletext.NewModel(u)
			models[row.unit] = model
		}
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src, err := os.ReadFile(filepath.Join(luauJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want = append(want, verdict)
	}
	return want
}

// luauJsonFlagsNameOnceRun answers each row's verdict from the emitted Luau
// JSON read, "read" or "refused".
func luauJsonFlagsNameOnceRun(t *testing.T, g *generatedUnit, rows []luauJsonFlagsNameOnceRow) []string {
	t.Helper()
	var b strings.Builder
	b.WriteString(jsonModules(g))
	b.WriteString(jsonFlags(g))
	b.WriteString("local json = require(\"./schema_json\")\njson.SetFlags(SCHEMA_JSON_FLAGS)\nlocal inputs = require(\"./inputs\")\nlocal cases = {\n")
	inputs := map[string][]byte{}
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(luauJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		inputs[row.name] = src
		fmt.Fprintf(&b, "\t{ %q, %q },\n", row.name, row.root)
	}
	b.WriteString(`}
for _, c in cases do
	local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	local v = json.FromJson(c[2], inputs[c[1]], r)
	print("@" .. c[1] .. "|" .. (if v ~= nil then "read" else "refused"))
end
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "@"); ok {
			if name, verdict, ok := strings.Cut(after, "|"); ok {
				got[name] = verdict
			}
		}
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		v, ok := got[row.name]
		if !ok {
			t.Fatalf("%s: the Luau probe printed no verdict", row.name)
		}
		out[i] = v
	}
	return out
}

func TestLuauJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")

	rows := luauJsonFlagsNameOnceRows(t)
	unit, rootName := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != rootName {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, rootName)
		}
	}
	want := luauJsonFlagsNameOnceOracle(t, rows)
	g := luauJsonFlagsNameOnceGenerate(t, unit)
	got := luauJsonFlagsNameOnceRun(t, g, rows)
	for i, row := range rows {
		if got[i] != want[i] {
			t.Errorf("%s: Luau answered %q, the vectors state %q", row.name, got[i], want[i])
		}
	}
}
