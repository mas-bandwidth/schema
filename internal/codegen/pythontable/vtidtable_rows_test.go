package pythontable

// THE ID-TABLE WIRE ROW on the Python leg (ROADMAP.md, Variable tables:
// id-table-wire; docs/SPEC-TABLES.md §3, §5): every conformance-manifest case
// luatable's vtcells_test.go names for the row, held to the one read on
// read-then-write under both pinned interpreters.
//
//	instance  the reference's wire loads and saves back byte for byte, its
//	          measure its length; every root here is a fixed table, so the
//	          plain load and save are form 3 (docs/port-python/table-idtable.md)
//	report    the plain load answers reports.txt's verdict; for a refused
//	          wire the variable reader (load_table), where the escape (31) and
//	          payload-free (32) kinds are judged, answers the Go engine's own
//	          decode of the same bytes (read, or refused by its reason)
//
// A refused read returns nothing (it raises), so no partial value reaches the
// caller. TestPythonVtIdtableRowsNegativeControl plants, one at a time, a
// defect in a copy of the emitted runtime (the fixed save, the fixed read, the
// variable read's escape skip) and requires the cases each covers to go red.

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtIdtableRowCases are luatable's id-table-wire cases (luaVTCells).
var vtIdtableRowCases = []string{
	"vocab_wide", "vocab_low", "peer_a", "peer_b", "vocab9_wide", "vocab9_low", "login_full", "match_full",
	"escape_unnamed_field", "escape_named_field", "escape_element", "nopayload_named_field",
}

type vtIdtableRow struct {
	kind, unit, root, wire string
	refusal                bool
}

// vtIdtableManifest answers the manifest's instance and report rows by case,
// its units, and reports.txt's verdict per report case.
func vtIdtableManifest(t *testing.T, repo string) (map[string]vtIdtableRow, map[string][]string, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]vtIdtableRow{}
	units := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
			continue
		}
		if (f[0] != "instance" && f[0] != "report") || len(f) < 5 {
			continue
		}
		refusal := false
		for _, x := range f[5:] {
			if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
				refusal = true
			}
		}
		rows[f[1]] = vtIdtableRow{kind: f[0], unit: f[2], root: f[3], wire: f[4], refusal: refusal}
	}
	reports, err := os.Open(filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer reports.Close()
	expect := map[string]string{}
	scan := bufio.NewScanner(reports)
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		if len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			expect[f[0]] = f[1]
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, units, expect
}

// vtIdtableEngineVerdict is the Go engine's verdict on wire at root through
// decode: "read", or the refusal's reason name.
func vtIdtableEngineVerdict(t *testing.T, u *ir.Unit, root string, wire []byte, decode func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)) string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var report tabletext.Report
	ok, err := decode(m, inst, wire, &report)
	if ok {
		return "read"
	}
	reason := tablewire.RefusalReason(err)
	if reason == "" {
		t.Fatalf("the Go engine refuses with %v, not a reasoned refusal", err)
	}
	return reason
}

// vtIdtableProbe reads the wire at root through the plain load and, for a
// refused wire, through the variable reader too; it prints one line.
func vtIdtableProbe(pkg, root string) string {
	return fmt.Sprintf(`from %[1]s import Refused, %[2]s
from %[1]s._schema.load import load_table

with open("wire", "rb") as f:
    data = f.read()

def variable():
    try:
        load_table(%[2]s, data)
    except Refused as err:
        return "refused:" + err.reason
    return "read"

try:
    value = %[2]s.load(data)
except Refused as err:
    print("refused", err.reason, variable())
else:
    saved = value.save()
    print("saved", len(data), "same" if saved == data else "diff", value.measure())
`, pkg, root)
}

// vtIdtableCase runs one case under both pins, with the emitted files edited
// by mutate when set, and answers its failure ("" when held).
func vtIdtableCase(t *testing.T, repo, name string, mutate func(*testing.T, map[string][]byte)) string {
	t.Helper()
	rows, units, expect := vtIdtableManifest(t, repo)
	row, ok := rows[name]
	if !ok {
		t.Fatalf("id-table-wire names case %s, which the manifest does not carry", name)
	}
	var paths []string
	for _, p := range units[row.unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	if len(paths) == 0 {
		t.Fatalf("case %s: the manifest names no unit %s", name, row.unit)
	}
	u := loadUnitPaths(t, paths...)
	files, err := Generate(u)
	if err != nil {
		return fmt.Sprintf("%s: the backend refused unit %s: %v", name, row.unit, err)
	}
	if mutate != nil {
		files = maps.Clone(files)
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	wire, err := os.ReadFile(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"wire": wire, "main.py": []byte(pyrun.ImportPrelude + vtIdtableProbe(u.Package, row.root))}
	var answers []string
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil {
			return fmt.Sprintf("%s (%s/%s): the probe failed under %s: %v\n%s", name, row.unit, row.root, pin, err, stderr)
		}
		answers = append(answers, strings.TrimSpace(out))
	}
	got := answers[0]
	if got != answers[1] {
		return fmt.Sprintf("%s: the pinned interpreters disagree: %q vs %q", name, answers[0], answers[1])
	}
	t.Logf("%s (%s %s/%s): %s", name, row.kind, row.unit, row.root, got)
	f := strings.Fields(got)
	switch {
	case row.kind == "report":
		want, ok := expect[name]
		if !ok {
			t.Fatalf("report %s: reports.txt names no such case", name)
		}
		verdict := "read"
		if len(f) > 0 && f[0] == "refused" {
			verdict = "refused"
		}
		if verdict != want {
			return fmt.Sprintf("%s (%s/%s): Python %s, the Go engine %s", name, row.unit, row.root, got, want)
		}
		if verdict == "refused" {
			// The refusal is the Go engine's by name: the plain load is held
			// to Decode's reason, and the variable reader to DecodeVariable's
			// verdict on the same bytes (read when an escape it cannot name
			// is skipped and counted unknown, else refused by its reason).
			plain := vtIdtableEngineVerdict(t, u, row.root, wire, tablewire.Decode)
			variable := vtIdtableEngineVerdict(t, u, row.root, wire, tablewire.DecodeVariable)
			if variable != "read" {
				variable = "refused:" + variable
			}
			if want := "refused " + plain + " " + variable; got != want {
				return fmt.Sprintf("%s (%s/%s): Python %q, the Go engine %q", name, row.unit, row.root, got, want)
			}
		}
	case row.refusal:
		if len(f) == 0 || f[0] != "refused" {
			return fmt.Sprintf("%s (%s/%s): the engine refuses this instance and Python answered %s", name, row.unit, row.root, got)
		}
	default:
		want := fmt.Sprintf("saved %d same %d", len(wire), len(wire))
		if got != want {
			return fmt.Sprintf("%s (%s/%s): the read-then-write is %q, want %q", name, row.unit, row.root, got, want)
		}
	}
	return ""
}

// TestPythonVtIdtableRows holds every manifest case of the id-table-wire row
// to the one read on read-then-write under both pinned interpreters.
//
// Functional (python pins): run with SCHEMA_SLOW=1; make test-python does.
func TestPythonVtIdtableRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range vtIdtableRowCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if fail := vtIdtableCase(t, repo, name, nil); fail != "" {
				t.Error(fail)
			}
		})
	}
}

// TestPythonVtIdtableRowsNegativeControl plants, one at a time, a defect in a
// copy of the emitted runtime: the fixed save's layout hash one bit off (no
// saved file is the reference's), the fixed read's record-hash test inverted
// (every reference record is refused), each required red on every instance
// case; and the variable read refusing an escape it cannot name, required red
// on escape_unnamed_field.
//
// Functional (python pins): run with SCHEMA_SLOW=1; make test-python does.
func TestPythonVtIdtableRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := vtIdtableManifest(t, repo)
	plant := func(module, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			planted := 0
			for name, data := range files {
				if !strings.HasSuffix(name, "/_schema/"+module) {
					continue
				}
				src := string(data)
				if strings.Count(src, from) != 1 {
					t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, strings.Count(src, from))
				}
				files[name] = []byte(strings.Replace(src, from, to, 1))
				planted++
			}
			if planted != 1 {
				t.Fatalf("the control planted %d copies of %s, want 1", planted, module)
			}
		}
	}
	instances := func() []string {
		var names []string
		for _, name := range vtIdtableRowCases {
			if rows[name].kind == "instance" {
				names = append(names, name)
			}
		}
		return names
	}()
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
		cases  []string
	}{
		// the fixed save's layout hash one bit off: no saved file is the
		// reference's
		{"save-layout-hash", plant("fixedform.py", `root["prefix"] = root["hash"].to_bytes(8, "little")`, `root["prefix"] = (root["hash"] ^ 1).to_bytes(8, "little")`), instances},
		// the fixed read's record-hash test inverted: every reference record
		// is refused
		{"load-record-hash", plant("fixedform.py", `if _integer(source, at, 8) != wire_hash:`, `if _integer(source, at, 8) == wire_hash:`), instances},
		// the variable read refuses an escape it cannot name instead of
		// skipping it by its L and counting it unknown
		{"skip-escape", plant("load.py", "        return  # opaque: the one kind whose meaning a reader may not know (§3)\n", "        raise Refused(\"wire_damaged\", \"planted\")\n"), []string{"escape_unnamed_field"}},
	} {
		for _, name := range ctl.cases {
			t.Run(ctl.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				fail := vtIdtableCase(t, repo, name, ctl.mutate)
				if fail == "" {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s left case %s green", ctl.name, name)
				}
				t.Logf("%s: red as required: %s", ctl.name, fail)
			})
		}
	}
}
