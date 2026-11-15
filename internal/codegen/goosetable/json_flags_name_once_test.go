package goosetable

// A FLAGS ARRAY NAMES EACH KNOWN VARIANT ONCE (docs/SPEC-TABLES.md §16.2): a
// flags array that names one known variant twice refuses the whole read, as a
// repeated key does, because a conforming writer writes each set bit's name
// once. An unknown name keeps its treatment (skipped and counted), and a flags
// array inside a value the read only skips is held to the JSON grammar alone.
// The rows are the vectors under testdata/wire/json-flags-name-once; the Go
// engine is first held to the verdict the manifest states, then the generated
// Goose text read is held to the same verdict.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const jsonFlagsNameOnceDir = "../../../testdata/wire/json-flags-name-once"

type jsonFlagsNameOnceRow struct {
	name, unit, root, text, verdict string
}

func jsonFlagsNameOnceRows(t *testing.T) []jsonFlagsNameOnceRow {
	t.Helper()
	f, err := os.Open(filepath.Join(jsonFlagsNameOnceDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []jsonFlagsNameOnceRow
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
			t.Fatalf("MANIFEST.tsv: verdict %q, want read or refused", c[4])
		}
		rows = append(rows, jsonFlagsNameOnceRow{c[0], c[1], c[2], c[3], c[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

func TestGooseJsonFlagsNameOnce(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	rows := jsonFlagsNameOnceRows(t)
	unit, root := rows[0].unit, rows[0].root
	for _, row := range rows {
		if row.unit != unit || row.root != root {
			t.Fatalf("%s: unit %s root %s; this test reads one unit and root, %s %s", row.name, row.unit, row.root, unit, root)
		}
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join(fixedRoot, unit)})
	if err != nil {
		t.Fatalf("gather %s: %v", unit, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", unit, err)
	}

	// The Go engine is the oracle, and it answers what the vectors state.
	model := tabletext.NewModel(u)
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("%s declares no root %s", unit, root)
	}
	texts := make(map[string][]byte, len(rows))
	want := make([]string, len(rows))
	for i, row := range rows {
		src, err := os.ReadFile(filepath.Join(jsonFlagsNameOnceDir, row.text))
		if err != nil {
			t.Fatal(err)
		}
		texts[fmt.Sprintf("in%d.json", i)] = src
		var report tabletext.Report
		verdict := "refused"
		if model.Read(model.New(def), src, &report) {
			verdict = "read"
		}
		if verdict != row.verdict {
			t.Fatalf("%s: the Go engine answers %s, the vectors state %s", row.name, verdict, row.verdict)
		}
		want[i] = verdict
	}

	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("generate %s type surface: %v", unit, err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s table surface: %v", unit, err)
	}
	maps.Copy(files, tables)

	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\nfn probe(name: const u8[:]) {\n")
	b.WriteString("    var text: u8[>..] = [];\n    if !read_file(name, text) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var v = %s::New%s();\n", ns, root)
	b.WriteString("    var r = schema_rt::json_report();\n")
	fmt.Fprintf(&b, "    let ok = %s::%sFromJson(v, text, r);\n", ns, root)
	b.WriteString("    if ok && !r.damaged { print(\"read\"); } else { print(\"refused\"); }\n")
	b.WriteString("}\n\nfn main() {\n")
	for i := range rows {
		fmt.Fprintf(&b, "    probe(\"in%d.json\");\n", i)
	}
	b.WriteString("}\n")

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := gooserun.Run(t, dir, "main.goose", texts)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(rows) {
		t.Fatalf("the probe answered %d lines for %d rows: %q", len(got), len(rows), out)
	}
	for i, row := range rows {
		if got[i] != want[i] {
			t.Errorf("%s: Goose answered %q, the vectors state %q", row.name, got[i], want[i])
		}
	}
}
