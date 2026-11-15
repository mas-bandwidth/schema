package squirreltable_test

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; each is
// compared on the verdict alone with the Go engine's (internal/tabletext), and
// the engine is first held to the verdict the vectors state. Running the
// generated Squirrel makes this a functional test, gated by slowtest.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const squirrelJsonFlagsNameOnceDir = "testdata/wire/json-flags-name-once"

type squirrelJsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func squirrelJsonFlagsNameOnceRows(t *testing.T) []squirrelJsonFlagsNameOnceRow {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(root, squirrelJsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []squirrelJsonFlagsNameOnceRow
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
		rows = append(rows, squirrelJsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// squirrelJsonFlagsNameOnceOracle answers the Go engine's verdict for each row,
// and holds it to the verdict the vectors state.
func squirrelJsonFlagsNameOnceOracle(t *testing.T, rows []squirrelJsonFlagsNameOnceRow) []string {
	t.Helper()
	var want []string
	for _, row := range rows {
		root, err := sqrun.RepoRoot()
		if err != nil {
			t.Fatal(err)
		}
		u := loadUnit(t, root, []string{row.unit})
		model := tabletext.NewModel(u)
		def := model.Lookup(row.root)
		if def == nil {
			t.Fatalf("%s: the unit declares no %s", row.name, row.root)
		}
		src, err := os.ReadFile(filepath.Join(root, squirrelJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		inst := model.New(def)
		var rep tabletext.Report
		verdict := "refused"
		if model.Read(inst, src, &rep) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want = append(want, verdict)
	}
	return want
}

func TestSquirrelJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()

	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rows := squirrelJsonFlagsNameOnceRows(t)
	want := squirrelJsonFlagsNameOnceOracle(t, rows)

	unit, rootName := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != rootName {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, rootName)
		}
	}
	u := loadUnit(t, root, []string{unit})
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "examples")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	inputs := map[string][]byte{}
	names := make([]string, len(rows))
	for i, row := range rows {
		src, err := os.ReadFile(filepath.Join(root, squirrelJsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		inputs[row.name] = src
		names[i] = fmt.Sprintf("%q", row.name)
	}
	var probe strings.Builder
	probe.WriteString(`local inputs = dofile("inputs.nut", true);
local schema = dofile("examples/schema_module.nut", true);
local T = schema.require(` + fmt.Sprintf("%q", squirreltable.ModuleName(u)) + `);
local J = schema.require("schema_table_json");
foreach (key in [` + strings.Join(names, ", ") + `]) {
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, ` + fmt.Sprintf("%q", rootName) + `, inputs[key], r);
  print(key + " " + (v == null ? "refused" : "read") + "\n");
}
`)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("probe line %q is not a name and a verdict", line)
		}
		got[f[0]] = f[1]
	}
	for i, row := range rows {
		if got[row.name] != want[i] {
			t.Errorf("%s: Squirrel answers %s, the Go engine answers %s", row.name, got[row.name], want[i])
		}
	}
}
