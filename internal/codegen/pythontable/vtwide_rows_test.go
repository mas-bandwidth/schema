package pythontable

// THE WIDE-SCALARS ROW on the Python leg (ROADMAP.md, Variable tables:
// wide-scalars; docs/SPEC-TABLES.md §3, §15, §16.2; docs/PORT-PYTHON.md, THE
// TABLE FRAME). fixed(I, F), ufixed(I, F), int128 and uint128 ride the integer
// codec as their raw storage integers. This file holds the roadmap row over
// the conformance manifest: every instance row whose unit carries a wide
// scalar is read once and written back byte-identical to the canonical wire,
// or refused with no partial value and the caller's buffer untouched, under
// both pinned interpreters. A root declared `fixed table` is read as form 3
// through the generated fixed form (docs/SPEC-TABLES.md §3.4); a variable root
// is read as form 1. The instances are selected from the units' own wide
// fields, so a corpus addition that uses the kinds is held without editing a
// list here.
//
// TestPythonVtWideRowsNegativeControl plants, one at a time, a fault in a COPY
// of the emitted runtime: the 128-bit integer width (the variable instance
// goes red) and the fixed form's signed read (the form-3 instances go red).

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type wideRowInstance struct {
	name, unit, root, wire string
	refusal                bool
}

// wideRowManifest reads the tables manifest and returns, per unit, the
// instance rows whose unit's table closure carries a wide scalar kind.
func wideRowManifest(t *testing.T, repo string) (map[string][]string, []wideRowInstance) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	type rawRow struct {
		name, unit, root, wire string
		refusal                bool
	}
	var raw []rawRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
			continue
		}
		if f[0] != "instance" || len(f) < 5 {
			continue
		}
		refusal := false
		for _, x := range f[5:] {
			if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
				refusal = true
			}
		}
		raw = append(raw, rawRow{name: f[1], unit: f[2], root: f[3], wire: f[4], refusal: refusal})
	}
	wide := map[string]bool{}
	var rows []wideRowInstance
	for _, r := range raw {
		ok, seen := wide[r.unit]
		if !seen {
			var paths []string
			for _, p := range units[r.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := loadUnitPaths(t, paths...)
			ok = len(ir.TableWideFields(u)) > 0
			wide[r.unit] = ok
		}
		if ok {
			rows = append(rows, wideRowInstance{name: r.name, unit: r.unit, root: r.root, wire: r.wire, refusal: r.refusal})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return units, rows
}

const wideRowProbe = `from %s import %s, Refused

with open("wire", "rb") as handle:
    original = handle.read()
buffer = bytearray(original)
try:
    value = %s.load(buffer)
except Refused as err:
    assert buffer == original, "the refusal changed the caller's buffer"
    print("refused", err.reason)
else:
    assert buffer == original, "the read changed the caller's buffer"
    before = repr(value)
    saved = value.save()
    print("read", "same" if saved == original else "diff", value.measure() == len(saved), repr(value) == before)
`

// wideRowEdit replaces one occurrence of from with to in the emitted file
// whose path ends with suffix and fails if the anchor is absent or repeated.
func wideRowEdit(t *testing.T, files map[string][]byte, suffix, from, to string) {
	t.Helper()
	for name := range files {
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		src := string(files[name])
		if got := strings.Count(src, from); got != 1 {
			t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, got)
		}
		files[name] = []byte(strings.Replace(src, from, to, 1))
		return
	}
	t.Fatalf("no emitted file ends with %s", suffix)
}

// wideRowFailures runs every wide instance row under both pins and returns the
// named findings and how many rows it held. mutate, when set, edits a copy of
// the generated files first.
func wideRowFailures(t *testing.T, rows []wideRowInstance, units map[string][]string, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	unitCache := map[string]*ir.Unit{}
	var fails []string
	held := 0
	for _, row := range rows {
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
			t.Fatalf("%s: read wire: %v", row.name, err)
		}
		files, err := Generate(u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refused the unit: %v", row.name, err))
			continue
		}
		if mutate != nil {
			edited := maps.Clone(files)
			mutate(t, edited)
			files = edited
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := fmt.Sprintf(wideRowProbe, u.Package, row.root, row.root)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe), "wire": wire}
		held++
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{
			{"current", pyrun.Interpreter},
			{"floor", pyrun.Floor},
		} {
			stdout, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", inputs)
			got := strings.TrimSpace(stdout)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): %v: %s", row.name, row.unit, row.root, pin.name, err, strings.TrimSpace(stderr)))
				continue
			}
			if row.refusal {
				if !strings.HasPrefix(got, "refused ") {
					fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the manifest refuses this wire and Python answered %q", row.name, row.unit, row.root, pin.name, got))
				}
				continue
			}
			if got != "read same True True" {
				fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the read-then-write is %q", row.name, row.unit, row.root, pin.name, got))
			}
		}
	}
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtWideRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	repo := filepath.Join("..", "..", "..")
	units, rows := wideRowManifest(t, repo)
	if len(rows) == 0 {
		t.Fatal("the manifest names no instance whose unit carries a wide scalar")
	}
	fixedRows, variableRows := 0, 0
	for _, row := range rows {
		var paths []string
		for _, p := range units[row.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		st := loadUnitPaths(t, paths...).Tables[row.root]
		if st == nil {
			t.Fatalf("%s names root %s, which the unit does not declare", row.name, row.root)
		}
		if st.FixedDeclared {
			fixedRows++
		} else {
			variableRows++
		}
	}
	if fixedRows == 0 || variableRows == 0 {
		t.Fatalf("the wide instances do not hold both forms: %d fixed, %d variable", fixedRows, variableRows)
	}
	fails, held := wideRowFailures(t, rows, units, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if held != len(rows) {
		t.Fatalf("the row held %d instances, want %d", held, len(rows))
	}
	t.Logf("%d wide instances held: %d form 3 and %d form 1", held, fixedRows, variableRows)
}

// TestPythonVtWideRowsNegativeControl plants, one at a time, a fault in a copy
// of the emitted runtime: the uint128 width (the form-1 instance goes red) and
// the fixed form's 128-bit read width (the form-3 instances go red).
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtWideRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
		row    string
	}{
		{"uint128-width", func(t *testing.T, files map[string][]byte) {
			wideRowEdit(t, files, "_schema/kind_int.py", "    19: 16,", "    19: 8,")
		}, "widelist_wide"},
		{"fixed-wide-width", func(t *testing.T, files map[string][]byte) {
			wideRowEdit(t, files, "_schema/fixedform.py", `return int.from_bytes(data[at:at + width], "little", signed=signed)`,
				`return int.from_bytes(data[at:at + (8 if width == 16 else width)], "little", signed=signed)`)
		}, "scalars_edges"},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			repo := filepath.Join("..", "..", "..")
			units, rows := wideRowManifest(t, repo)
			fails, _ := wideRowFailures(t, rows, units, ctl.mutate)
			joined := strings.Join(fails, "\n")
			if !strings.Contains(joined, ctl.row) {
				t.Fatalf("%s: NEGATIVE CONTROL FAILED: %s stayed green (findings: %s)", ctl.name, ctl.row, joined)
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), fails[0])
		})
	}
}
