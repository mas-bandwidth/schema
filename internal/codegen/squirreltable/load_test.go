package squirreltable_test

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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadPrelude is the report probe's one helper: load a wire file through the
// emitted load runtime and print the §4 report exactly as the conformance
// report surface does (test/conformance/README.md).
const loadPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  L.load(T, root, inputs[key], report);
  local verdict = report.refused ? "refused" : "read";
  local m = report.malformed ? "true" : "false";
  print(key + " " + report.unknown.tostring() + "," + report.kindMismatch.tostring() + "," +
        report.widened.tostring() + "," + report.clamped.tostring() + "," +
        report.duplicate.tostring() + "," + m + "," + verdict + "\n");
};
`

type reportRow struct {
	name string
	unit string
	root string
	wire string
}

func readReportRows(t *testing.T, path string) []reportRow {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []reportRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "report" {
			rows = append(rows, reportRow{name: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func readReportCounts(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("%s: %q is not a report line", path, line)
		}
		out[fields[0]] = fields[1]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestSquirrelTableLoadMatchesTheReports loads every report row of
// testdata/conformance/tables/MANIFEST.txt whose unit the Squirrel table
// surface generates, prints u,k,w,c,d,m,verdict as test/conformance/README.md
// defines it, and compares with reports.txt (docs/SPEC-TABLES.md §4). Rows of
// a refused unit are counted and named.
func TestSquirrelTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	rows := readReportRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))

	byUnit := map[string][]reportRow{}
	var order []string
	for _, r := range rows {
		if _, ok := byUnit[r.unit]; !ok {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
		if _, ok := pinned[r.name]; !ok {
			t.Fatalf("%s: reports.txt names no such case", r.name)
		}
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(loadPrelude)
	inputs := map[string][]byte{}
	var refused []string
	refusedRows, compared := 0, 0
	for _, key := range order {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("report rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			refused = append(refused, fmt.Sprintf("%s (%d rows): %v", key, len(byUnit[key]), err))
			refusedRows += len(byUnit[key])
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mod := squirreltable.ModuleName(u)
		for _, r := range byUnit[key] {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.wire)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[r.name] = data
			fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", key, mod, r.root, r.name)
			compared++
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	got := map[string]string{}
	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	for _, line := range strings.Split(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("probe line %q is not a case and a report", line)
		}
		got[fields[0]] = fields[1]
	}
	for _, r := range rows {
		isRefused := false
		for _, name := range refused {
			if strings.HasPrefix(name, r.unit+" ") {
				isRefused = true
				break
			}
		}
		if isRefused {
			continue
		}
		if got[r.name] != pinned[r.name] {
			t.Errorf("%s: the load reports %q, reports.txt pins %q", r.name, got[r.name], pinned[r.name])
		}
	}
	if compared+refusedRows != len(rows) {
		t.Errorf("compared %d and refused %d rows, want %d", compared, refusedRows, len(rows))
	}
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	t.Logf("compared %d of %d report rows; %d rows in %d refused units", compared, len(rows), refusedRows, len(refused))
}

// TestSquirrelTableLoadNegativeControl patches the emitted load runtime in
// memory, one anchor matched exactly once, so a repeated field id is NOT
// counted duplicate. The same reports comparison over a wire carrying that
// repeated id must then go red (docs/SPEC-TABLES.md §4).
func TestSquirrelTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var paths []string
	for _, u := range units {
		if u.key == "tblv1" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tblv1 unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tblv1")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	// a form-1 root body carrying field `a` twice: last wins, duplicate once
	cfg := unit.Tables["Cfg"]
	var a *ir.Field
	for _, f := range cfg.Fields {
		if f.Name == "a" {
			a = f
		}
	}
	if a == nil {
		t.Fatal("tblv1 Cfg declares no field a")
	}
	id := ir.TableFieldWireId(a)
	kind := byte(ir.TableWireFieldKind(a))
	wire := []byte{1}
	wire = append(wire, 1, kind, 1, 0, 0, 0)
	wire = append(wire, 1, kind, 2, 0, 0, 0)
	wire = append(wire, 0)
	for i := 0; i < 8; i++ {
		wire = append(wire, byte(id>>(8*i)))
	}
	wire = append(wire, 1, 0, 0, 0, 0, 0, 0, 0)

	mod := squirreltable.ModuleName(unit)
	var p strings.Builder
	p.WriteString(loadPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "tblv1", mod, "Cfg", "dup")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	inputs := map[string][]byte{"dup": wire}
	want := "0,0,0,0,1,false,read"

	got := reportLine(t, sqrun.Run(t, dir, "main.nut", inputs))
	if got != want {
		t.Fatalf("the load reports %q, want %q", got, want)
	}

	const anchor = "report.duplicate = report.duplicate + 1;"
	runtime := squirreltable.Runtime()["schema_table_load.nut"]
	if n := strings.Count(string(runtime), anchor); n != 1 {
		t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
	}
	patched := strings.Replace(string(runtime), anchor, "report.duplicate = report.duplicate;", 1)
	writeFile(t, filepath.Join(out, "schema_table_load.nut"), []byte(patched))

	got = reportLine(t, sqrun.Run(t, dir, "main.nut", inputs))
	if got == want {
		t.Fatalf("the patched runtime still reports %q; the reports comparison did not go red", got)
	}
	if got != "0,0,0,0,0,false,read" {
		t.Fatalf("the patched runtime reports %q, want the duplicate counter gone", got)
	}
	t.Logf("unpatched %q, patched %q: the reports comparison goes red", want, got)
}

func reportLine(t *testing.T, stdout []byte) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the probe printed %d lines, want one: %q", len(lines), stdout)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 2 {
		t.Fatalf("the probe line %q is not a case and a report", lines[0])
	}
	return fields[1]
}
