package pythontable

// THE UNBOUNDED-ARRAY ROWS on the Python table leg (ROADMAP.md, Variable
// tables: port/python/vt/unbounded-arrays; docs/SPEC-TABLES.md §2.9, §3, §4;
// docs/port-python/table-unbounded.md). TestPythonVtUnbounded is the kind gate
// over its own schema; this file holds the roadmap row over every manifest
// instance that uses the construct, the corpus the Lua leg names for the same
// row (internal/codegen/luatable/vtcells_test.go, "unbounded-arrays"), and the
// Go-engine construct beside it:
//
//	instance   the wire the reference wrote loads through the one read and
//	           saves back byte for byte, its measure its length
//	report     the one read's verdict is reports.txt's and the Go engine's;
//	           when it reads, the save is the engine's canonical wire
//	construct  the Go engine's wire of the JSON text reads and saves back
//	           byte for byte
//
// A refusal leaves no value, and on every row the caller's buffer and report
// are untouched. Every row runs under both pinned interpreters.
//
// TestPythonVtUnboundedRowsNegativeControl plants one fault at a time in a
// COPY of the emitted array kind module (the save's element count, the
// read's element count, the read's trailing-bytes check) and requires the
// rows each fault reaches to go red by name.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// vtUnboundedRowCases are the manifest rows of the unbounded-arrays row, as
// the Lua leg names them.
var vtUnboundedRowCases = []string{
	"list_tables", "list_scalars", "list_empty", "list_erased", "list_mixed", "list_shared", "list_nested",
	"list_of_maps", "list_migrates", "list_scalar_widen",
}

// vtUnboundedRowConstruct is the Go-engine construct the row holds beside the
// corpus: an unbounded array of pointers to tables, with a null element.
var vtUnboundedRowConstruct = struct{ name, schema, root, json string }{
	"blobs_pointers",
	"package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
	"Node", `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`,
}

type vtUnboundedManifestRow struct {
	kind, unit, root, wire string
	wireRefused            bool
}

// vtUnboundedRowsManifest reads the tables manifest's units, its instance and
// report rows, and the reports.txt verdicts.
func vtUnboundedRowsManifest(t *testing.T, repo string) (map[string][]string, map[string]vtUnboundedManifestRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]vtUnboundedManifestRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			row := vtUnboundedManifestRow{kind: f[0], unit: f[2], root: f[3], wire: f[4]}
			for _, x := range f[5:] {
				// wire-refused (testdata/conformance/tables/FORMAT.md) requires
				// semantic refusal from every port; go-refusal= is its legacy
				// spelling.
				if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
					row.wireRefused = true
				}
			}
			rows[f[1]] = row
		}
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

// vtUnboundedRowsEngine reads wire at root through the Go engine and answers
// whether it reads and, when it does, the canonical bytes it writes back.
func vtUnboundedRowsEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (bool, []byte) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	value := m.New(st)
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, value, wire, &report); !ok || err != nil {
		return false, nil
	}
	canonical, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatalf("%s: the engine does not write what it read: %v", root, err)
	}
	return true, canonical
}

// vtUnboundedRowsProbe is the one read and its write-back. A refusal must
// leave the caller's buffer and report as they were; a read must not touch
// the buffer, and its save and measure are printed.
const vtUnboundedRowsProbe = `from %s import %s, LoadReport, Refused
with open("wire", "rb") as handle:
    original = handle.read()
buffer = bytearray(original)
report = LoadReport()
report.widened = 77
report.unknown = 66
value = None
try:
    value = %s.load(buffer, report)
except Refused:
    assert value is None, "the refusal left a value"
    assert buffer == original, "the refusal changed the caller's buffer"
    assert (report.widened, report.unknown) == (77, 66), "the refusal changed the caller's report"
    print("refused")
else:
    assert buffer == original, "the read changed the caller's buffer"
    saved = value.save()
    print("read", saved.hex(), value.measure() == len(saved))
`

// vtUnboundedRowsCheck generates u (its files edited by mutate when set), runs
// the probe on wire at root under both pinned interpreters and answers each
// line that differs from want, named by label.
func vtUnboundedRowsCheck(t *testing.T, label string, u *ir.Unit, root string, wire []byte, want string, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	if st := u.Tables[root]; st == nil || st.FixedDeclared {
		t.Fatalf("%s: %s is not a variable root of its unit; this row reads the variable form", label, root)
	}
	files, err := Generate(u)
	if err != nil {
		return []string{fmt.Sprintf("%s: the backend refused the unit: %v", label, err)}
	}
	if mutate != nil {
		files = maps.Clone(files)
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := fmt.Sprintf(vtUnboundedRowsProbe, u.Package, root, root)
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe), "wire": wire}
	var fails []string
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
		got := strings.TrimSpace(stdout)
		switch {
		case err != nil:
			fails = append(fails, fmt.Sprintf("%s (%s): %v: %s", label, filepath.Base(python), err, strings.TrimSpace(stderr)))
		case got != want:
			fails = append(fails, fmt.Sprintf("%s (%s): the read-then-write is %s, want %s", label, filepath.Base(python), vtUnboundedRowsShort(got), vtUnboundedRowsShort(want)))
		}
	}
	return fails
}

// vtUnboundedRowsShort keeps a probe answer readable in a finding: the verdict
// and the head of the saved bytes.
func vtUnboundedRowsShort(s string) string {
	if len(s) > 48 {
		return fmt.Sprintf("%q... (%d chars)", s[:48], len(s))
	}
	return fmt.Sprintf("%q", s)
}

// vtUnboundedRowsFailures holds every row and the construct and answers the
// named failures and how many it held.
func vtUnboundedRowsFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := vtUnboundedRowsManifest(t, repo)
	cache := map[string]*ir.Unit{}
	var fails []string
	held := 0
	for _, name := range vtUnboundedRowCases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s is not a manifest instance or report row", name)
		}
		u, ok := cache[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = loadUnitPaths(t, paths...)
			cache[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: read wire: %v", name, err)
		}
		label := fmt.Sprintf("%s (%s/%s)", name, row.unit, row.root)
		accepted, canonical := vtUnboundedRowsEngine(t, u, row.root, wire)
		switch row.kind {
		case "instance":
			if accepted == row.wireRefused {
				t.Fatalf("%s: the engine's verdict (read %v) is not the manifest's (wire-refused %v)", label, accepted, row.wireRefused)
			}
			if accepted && string(canonical) != string(wire) {
				t.Fatalf("%s: the engine's canonical write is not the instance's wire", label)
			}
		case "report":
			verdict := "refused"
			if accepted {
				verdict = "read"
			}
			if want := expect[name]; verdict != want {
				t.Fatalf("%s: the engine's verdict is %s, reports.txt says %q", label, verdict, want)
			}
		}
		want := "refused"
		if accepted {
			want = fmt.Sprintf("read %x True", canonical)
		}
		held++
		fails = append(fails, vtUnboundedRowsCheck(t, label, u, row.root, wire, want, mutate)...)
	}
	c := vtUnboundedRowConstruct
	u := loadUnit(t, c.schema)
	wire := vtUnboundedEngineWire(t, u, c.root, c.json)
	held++
	fails = append(fails, vtUnboundedRowsCheck(t, "construct "+c.name, u, c.root, wire, fmt.Sprintf("read %x True", wire), mutate)...)
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtUnboundedRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	fails, held := vtUnboundedRowsFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if want := len(vtUnboundedRowCases) + 1; held != want {
		t.Fatalf("the row held %d cases, want %d", held, want)
	}
	t.Logf("%d manifest rows and 1 construct held under both pins", len(vtUnboundedRowCases))
}

// vtUnboundedRowsEdit replaces the one occurrence of from with to in the
// emitted runtime module whose path ends with module, and fails if the anchor
// is absent or repeated.
func vtUnboundedRowsEdit(module, from, to string) func(*testing.T, map[string][]byte) {
	return func(t *testing.T, files map[string][]byte) {
		t.Helper()
		for name := range files {
			if !strings.HasSuffix(name, "_schema/"+module) {
				continue
			}
			src := string(files[name])
			if got := strings.Count(src, from); got != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, got)
			}
			files[name] = []byte(strings.Replace(src, from, to, 1))
			return
		}
		t.Fatalf("no emitted file ends with _schema/%s", module)
	}
}

// TestPythonVtUnboundedRowsNegativeControl plants, one at a time, a fault in
// a COPY of the emitted array kind modules (kind_array.py for []T,
// kind_pointer_array.py for []*T) and requires every row the fault reaches to
// go red by name: the save writes a count one too many, the read takes one
// element too few, and the read stops refusing bytes left after an array's
// elements.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtUnboundedRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	byValue := []string{"list_tables", "list_scalars", "list_empty", "list_erased", "list_mixed", "list_nested",
		"list_of_maps", "list_migrates"}
	byPointer := []string{"list_shared", "construct blobs_pointers"}
	for _, ctl := range []struct {
		name    string
		mutate  func(*testing.T, map[string][]byte)
		planted []string
	}{
		{"save-count", vtUnboundedRowsEdit("kind_array.py", "        writer.varint(len(value))\n", "        writer.varint(len(value) + 1)\n"), byValue},
		{"load-count", vtUnboundedRowsEdit("kind_array.py", "        count = sub.varint()\n", "        count = max(sub.varint() - 1, 0)\n"), byValue},
		{"load-trailing", vtUnboundedRowsEdit("kind_array.py", "        if sub.pos != length:\n", "        if False:\n"), []string{"list_scalar_widen"}},
		{"pointer-save-count", vtUnboundedRowsEdit("kind_pointer_array.py", "        _put_varint(payload, len(value))\n", "        _put_varint(payload, len(value) + 1)\n"), byPointer},
		{"pointer-load-count", vtUnboundedRowsEdit("kind_pointer_array.py", "        count: int = sub.varint()\n", "        count: int = max(sub.varint() - 1, 0)\n"), byPointer},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := vtUnboundedRowsFailures(t, ctl.mutate)
			joined := strings.Join(fails, "\n")
			for _, planted := range ctl.planted {
				if !slices.ContainsFunc(fails, func(f string) bool { return strings.HasPrefix(f, planted+" ") }) {
					t.Errorf("%s: %s stayed green (findings: %s)", ctl.name, planted, joined)
				}
			}
			if len(fails) == 0 {
				t.Fatalf("%s: every row stayed green", ctl.name)
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), fails[0])
		})
	}
}
