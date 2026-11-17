package pythontable

// THE FIXED-CLASS ROW on the Python leg (ROADMAP.md, Variable tables; the
// fixed-class kind card). The row is the manifest corpus luatable's
// vtcells_test.go names: the tabledemo roots, the V1/V2 evolution seams, the
// P1 chain and the form-byte refusals, read through the one Python read on
// read-then-write. An accepted instance saves back byte-identical to the
// canonical wire with its measure equal to the file's length; a wire-refused
// instance is refused; a report row answers reports.txt's verdict. No instance
// is read on the variable form: a root declared `fixed table` is form 3 and
// the generated fixed reader is the only reader.
//
// TestPythonVtFixedClassRowsNegativeControl moves the fixed file's last header
// byte in a COPY of the emitted runtime and requires every accepted instance
// row to go red by name.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtFixedClassRowCases is the fixed-class row's corpus, copied from
// luaVTCells' fixed-class row (internal/codegen/luatable/vtcells_test.go) so
// the two legs answer the same manifest instances.
var vtFixedClassRowCases = []string{
	"root_full", "root_default", "profile_elide", "loadout_full", "previous_form_wide_blob", "archive",
	"v1_cfg", "v1_seams", "v2_cfg", "v2_seams", "chain_value", "chain_value_empty",
	"v1_cfg_as_v2", "v1_seams_as_v2", "v2_cfg_as_v1", "v2_seams_as_v1", "form_zero", "form_six", "form_ff", "message_as_file",
}

type vtFixedClassRow struct {
	caseName string
	unit     string
	root     string
	wire     string
	refusal  bool
	report   bool
}

// vtFixedClassManifest reads the tables manifest's units, every instance and
// report row, and reports.txt's verdicts.
func vtFixedClassManifest(t *testing.T, repo string) (map[string][]string, map[string]vtFixedClassRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]vtFixedClassRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
			continue
		}
		if f[0] != "instance" && f[0] != "report" {
			continue
		}
		if len(f) < 5 {
			continue
		}
		row := vtFixedClassRow{caseName: f[1], unit: f[2], root: f[3], wire: f[4], report: f[0] == "report"}
		for _, x := range f[5:] {
			if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
				row.refusal = true
			}
		}
		rows[row.caseName] = row
	}
	verdicts, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	expect := map[string]string{}
	for line := range strings.SplitSeq(string(verdicts), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			expect[f[0]] = f[1]
		}
	}
	return units, rows, expect
}

// vtFixedClassRowProbe reads the wire at root and prints "read" or "refused".
// In strict mode (an instance row) a read that does not save back the exact
// wire with measure equal to its length exits nonzero by name.
func vtFixedClassRowProbe(pkg, root, caseName string, strict bool) string {
	return fmt.Sprintf(`import importlib
m = importlib.import_module(%q)
cls = getattr(m, %q)
with open("wire", "rb") as handle:
    data = handle.read()
try:
    value = cls.load(data)
except m.Refused:
    print("refused")
else:
    saved = value.save()
    if %s and (saved != data or value.measure() != len(saved)):
        raise SystemExit("vt_fixed_class_rows_" + %q)
    print("read")
`, pkg, root, vtFixedClassPyBool(strict), caseName)
}

func vtFixedClassPyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// vtFixedClassRowAnswer generates u (edited by mutate when set), runs the row
// probe over wire at root, and answers its line under both pinned
// interpreters. A probe that exits nonzero is returned as the error so the
// negative control can collect it by name instead of failing t.
func vtFixedClassRowAnswer(t *testing.T, u *ir.Unit, row vtFixedClassRow, wire []byte, mutate func(*testing.T, map[string][]byte)) (current, floor string, err error) {
	t.Helper()
	files, genErr := Generate(u)
	if genErr != nil {
		return "", "", fmt.Errorf("%s: the Python table backend refuses the unit: %w", row.caseName, genErr)
	}
	if mutate != nil {
		edited := maps.Clone(files)
		mutate(t, edited)
		files = edited
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := []byte(pyrun.ImportPrelude + vtFixedClassRowProbe(u.Package, row.root, row.caseName, !row.report))
	if writeErr := os.WriteFile(filepath.Join(dir, "main.py"), probe, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	inputs := map[string][]byte{"wire": wire}
	currentOut, currentErr, currentRun := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if currentRun != nil {
		return "", "", fmt.Errorf("%s (current): %v\n%s", row.caseName, currentRun, currentErr)
	}
	floorOut, floorErr, floorRun := pyrun.Try(t, pyrun.Floor(t), dir, "main.py", inputs)
	if floorRun != nil {
		return "", "", fmt.Errorf("%s (floor): %v\n%s", row.caseName, floorRun, floorErr)
	}
	current = strings.TrimSpace(string(currentOut))
	floor = strings.TrimSpace(string(floorOut))
	if current != floor {
		return "", "", fmt.Errorf("%s: the pinned interpreters disagree: %q vs %q", row.caseName, current, floor)
	}
	return current, floor, nil
}

// vtFixedClassRowFailures holds the row's cases and returns the failures,
// plus how many cases were held. mutate, when set, edits the generated files
// first (the negative control's one anchor).
func vtFixedClassRowFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, map[string]bool) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := vtFixedClassManifest(t, repo)
	unitCache := map[string]*ir.Unit{}
	var fails []string
	held := map[string]bool{}
	for _, name := range vtFixedClassRowCases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("the row names case %s, which the manifest does not carry", name)
		}
		u, ok := unitCache[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = loadUnitPaths(t, paths...)
			unitCache[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _, err := vtFixedClassRowAnswer(t, u, row, wire, mutate)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		held[name] = true
		want := "read"
		if row.refusal || (row.report && expect[name] == "refused") {
			want = "refused"
		}
		if got != want {
			fails = append(fails, fmt.Sprintf("%s (%s/%s): Python answered %s, want %s", name, row.unit, row.root, got, want))
		}
	}
	return fails, held
}

// TestPythonVtFixedClassRows holds every manifest instance and report of the
// fixed-class row to the one read on read-then-write under both pinned
// interpreters.
//
// Functional: it runs the emitted table runtime under the pins.
func TestPythonVtFixedClassRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	fails, held := vtFixedClassRowFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if len(held) != len(vtFixedClassRowCases) {
		t.Errorf("the row held %d of %d cases", len(held), len(vtFixedClassRowCases))
	}
	t.Logf("%d manifest cases held under both pinned interpreters", len(held))
}

// TestPythonVtFixedClassRowsNegativeControl moves the fixed file's last header
// byte in a copy of the emitted runtime and requires every accepted instance
// row to go red by name.
//
// Functional: it runs the emitted table runtime under the pins.
func TestPythonVtFixedClassRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	plant := func(t *testing.T, files map[string][]byte) {
		var path string
		for name := range files {
			if strings.HasSuffix(name, "/_schema/fixedform.py") {
				path = name
				break
			}
		}
		if path == "" {
			t.Fatal("the emitted unit carries no _schema/fixedform.py")
		}
		const anchor = "    result = bytes(root[\"header\"]) + b\"\".join(records)\n"
		if strings.Count(string(files[path]), anchor) != 1 {
			t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", path, anchor, strings.Count(string(files[path]), anchor))
		}
		files[path] = []byte(strings.Replace(string(files[path]), anchor,
			"    result = bytes(root[\"header\"])[:-1] + bytes([root[\"header\"][-1] ^ 1]) + b\"\".join(records)\n", 1))
	}
	fails, _ := vtFixedClassRowFailures(t, plant)
	red := map[string]bool{}
	named := map[string]bool{}
	for _, f := range fails {
		name := strings.TrimSuffix(strings.Fields(f)[0], ":")
		red[name] = true
		if strings.Contains(f, "vt_fixed_class_rows_"+name) {
			named[name] = true
		}
	}
	repo := filepath.Join("..", "..", "..")
	_, rows, _ := vtFixedClassManifest(t, repo)
	for _, name := range vtFixedClassRowCases {
		row := rows[name]
		if row.report || row.refusal {
			continue
		}
		if !red[name] {
			t.Errorf("%s: the row stayed green with the fixed header's last byte moved", name)
			continue
		}
		if !named[name] {
			t.Errorf("%s: the row went red, but not by its own name", name)
		}
	}
	if len(red) == 0 {
		t.Fatal("the negative control left every row green")
	}
}
