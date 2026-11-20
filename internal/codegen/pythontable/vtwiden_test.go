package pythontable

// THE WIDENING CELL on the Python leg (ROADMAP.md, Variable tables: "widening
// on read, and the refusal reasons"; docs/SPEC-TABLES.md §4; docs/PORT-PYTHON.md,
// THE TABLE FRAME). A narrower writer's field, union arm, array element or map
// key lands in a wider reader's kind of the same signedness (f32 into f64 is
// the only float rung), decoded at the writer's width with the value exact and
// ONE widened counted per field, array or map; every narrowing, every crossed
// signedness and every non-rung retype refuses the whole read. A refusal leaves
// no partial value and does not touch the caller's report or input buffer.
//
// THE ROWS. Every manifest report row the engine reaches a widening on is
// held, and no row is skipped: vtWidenCells is exactly that corpus. The three
// rows the engine widens are a1_arm_moved_width_as_a2 (arm), field_scalar_widen
// (field) and map_key_widen (map key); their sibling rows in the same units
// pin the boundary (the crossed Signedness, the narrowed kinds, the string
// arm, the body arm and the unknown arm all refuse, and the sane body arm
// reads), and list_scalar_widen (an array body with bytes past its elements),
// k1_as_k2/k2_as_k1 (the enum-against-integer retypes) and
// previous_form_wide_blob (a form-1 wide blob) are the manifest's own named
// refusals for the corpus.
//
// THE FORM. A root declared `fixed table` reads form 3 alone and refuses a
// form-1 file as previous_form (docs/SPEC-TABLES.md §3.4). The Python leg's
// form-3 reader is the fixed-form row's (port/python/fixed/fixed-file-roundtrip);
// this cell holds the widening RULE, which rides form 1, so the engine is asked
// through the same declaration with the root variable, exactly as
// test/conformance/harness (framingVariableUnit) asks a fixed root's framing
// rows. The declared-form verdict is still held to reports.txt for every row,
// so the manifest is answered as well as the construct.
//
// TestPythonVtWideningNegativeControl plants each widening fault in a COPY of
// the emitted runtime (the kind-pair ladder, then the field, arm and map
// widened counters) and requires its row to go red by name.

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
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtWidenCells is every manifest report row this cell holds: the rows the
// engine widens, their units' sibling rows, and the manifest's named
// widening-corpus refusals. No row is skipped; a manifest row that widens and
// is not named here fails the completeness check in TestPythonVtWidening.
var vtWidenCells = []string{
	// the arm ladder: the one widening and every retype beside it
	"a1_arm_moved_width_as_a2",
	"a1_arm_one_width_as_a2",
	"a1_arm_as_string_as_a2",
	"a1_arm_as_body_as_a2",
	"a1_arm_body_as_a2",
	"a1_arm_unknown_as_a2",
	"a2_arm_enum_as_int",
	"a2_arm_pointer_as_int",
	"a2_arm_bytes_as_string",
	"a2_arm_length_off_width",
	// the field ladder: a plain scalar written narrower than its declaration
	"field_scalar_widen",
	"scalars_full_as_2",
	"scalars_edges_as_2",
	"varevo_v1_as_v2",
	"varevo_v1_as_v3",
	"varevo_v1_ping_as_v3",
	"varevo_v2_as_v3",
	// the enum-against-integer retypes
	"k1_as_k2",
	"k2_as_k1",
	// the array element and the map key
	"list_scalar_widen",
	"map_key_widen",
	"map_depth_root_kind",
	// a form-1 file at a fixed root, kept as the refusal row
	"previous_form_wide_blob",
}

type vtWidenRow struct {
	name, unit, root, wire string
}

// vtWidenManifest reads the tables manifest's units and report rows and the
// reports.txt verdicts.
func vtWidenManifest(t *testing.T, repo string) (map[string][]string, map[string]vtWidenRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]vtWidenRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
			continue
		}
		if f[0] == "report" && len(f) >= 5 {
			rows[f[1]] = vtWidenRow{name: f[1], unit: f[2], root: f[3], wire: f[4]}
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

// vtWidenUnit builds the unit a manifest row names.
func vtWidenUnit(t *testing.T, repo string, units map[string][]string, unit string) *ir.Unit {
	t.Helper()
	var paths []string
	for _, p := range units[unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	return loadUnitPaths(t, paths...)
}

// vtWidenEngine reads wire at root through the Go engine on the variable form
// and answers its verdict, the widened counter and, when it reads, the
// canonical form-1 bytes.
func vtWidenEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (bool, int, []byte) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	st.FixedDeclared = false
	m := tabletext.NewModel(u)
	value := m.New(st)
	var report tabletext.Report
	ok, err := tablewire.Decode(m, value, wire, &report)
	if !ok || err != nil {
		return false, 0, nil
	}
	canonical, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatalf("%s: the engine does not write what it read: %v", root, err)
	}
	return true, report.Widened, canonical
}

// vtWidenDeclaredVerdict reads wire at root through the engine as the
// manifest declares the table (a fixed root reads form 3 alone).
func vtWidenDeclaredVerdict(t *testing.T, u *ir.Unit, root string, wire []byte) string {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(st), wire, &report); ok && err == nil {
		return "read"
	}
	return "refused"
}

const vtWidenProbe = `from %s import %s, LoadReport, Refused
with open("wire", "rb") as handle:
    original = handle.read()
buffer = bytearray(original)
report = LoadReport()
report.widened = 77
report.unknown = 66
try:
    value = %s.load(buffer, report)
except Refused:
    assert buffer == original, "the refusal changed the caller's buffer"
    assert (report.widened, report.unknown) == (77, 66), "the refusal changed the caller's report"
    print("refused")
else:
    assert buffer == original, "the read changed the caller's buffer"
    saved = value.save()
    print("read", saved.hex(), report.widened, value.measure() == len(saved))
`

// vtWidenEdit replaces one occurrence of from with to in the emitted file
// whose path ends with suffix, and fails if the anchor is absent or repeated.
func vtWidenEdit(t *testing.T, files map[string][]byte, suffix, from, to string) {
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

// vtWidenFailures holds every cell row to the engine's verdict and bytes, its
// declared-form verdict to reports.txt, and (when mutate is set) the row's
// negative control. It returns the named failures and how many rows it held.
func vtWidenFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := vtWidenManifest(t, repo)
	var fails []string
	held := 0
	for _, name := range vtWidenCells {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s is not a manifest report row", name)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: read wire: %v", name, err)
		}
		declared := vtWidenUnit(t, repo, units, row.unit)
		if got, want := vtWidenDeclaredVerdict(t, declared, row.root, wire), expect[name]; got != want {
			fails = append(fails, fmt.Sprintf("%s (%s/%s): the declared read is %s, reports.txt says %s", name, row.unit, row.root, got, want))
			continue
		}
		u := vtWidenUnit(t, repo, units, row.unit)
		accepted, widened, canonical := vtWidenEngine(t, u, row.root, wire)
		files, err := Generate(u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refused the unit: %v", name, err))
			continue
		}
		if mutate != nil {
			edited := maps.Clone(files)
			mutate(t, edited)
			files = edited
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := fmt.Sprintf(vtWidenProbe, u.Package, row.root, row.root)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe), "wire": wire}
		held++
		want := "refused"
		if accepted {
			want = fmt.Sprintf("read %x %d True", canonical, widened)
		}
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			got := strings.TrimSpace(stdout)
			switch {
			case err != nil:
				fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): %v: %s", name, row.unit, row.root, filepath.Base(python), err, strings.TrimSpace(stderr)))
			case got != want:
				fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the read-then-write is %q, the engine says %q", name, row.unit, row.root, filepath.Base(python), got, want))
			}
		}
	}
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtWidening(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")

	// THE COMPLETENESS CHECK. Every manifest report row the engine widens on
	// the variable form must be named in this cell. A corpus addition that
	// widens therefore goes red here rather than being silently skipped.
	repo := filepath.Join("..", "..", "..")
	units, rows, _ := vtWidenManifest(t, repo)
	cache := map[string]*ir.Unit{}
	var engineWidened []string
	for name, row := range rows {
		u, ok := cache[row.unit]
		if !ok {
			u = vtWidenUnit(t, repo, units, row.unit)
			cache[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: read wire: %v", name, err)
		}
		if accepted, widened, _ := vtWidenEngine(t, u, row.root, wire); accepted && widened > 0 {
			engineWidened = append(engineWidened, name)
		}
	}
	sort.Strings(engineWidened)
	named := map[string]bool{}
	for _, name := range vtWidenCells {
		named[name] = true
	}
	for _, name := range engineWidened {
		if !named[name] {
			t.Errorf("%s widens on the variable form but is not held by this cell", name)
		}
	}
	if len(engineWidened) == 0 {
		t.Fatal("the manifest names no widening report row")
	}

	fails, held := vtWidenFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if held != len(vtWidenCells) {
		t.Fatalf("the cell held %d rows, want %d", held, len(vtWidenCells))
	}
	t.Logf("%d manifest rows held; the engine widens on %v", held, engineWidened)
}

// TestPythonVtWideningNegativeControl plants, one at a time, a fault in a
// COPY of the emitted runtime: the kind-pair ladder is off (every widening
// refuses), then the field, the arm and the map widened counters stop
// counting. Each must turn its own rows red by name.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtWideningNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const ladder = `def _widens(wire: int, declared: int) -> bool:`
	for _, ctl := range []struct {
		name    string
		mutate  func(*testing.T, map[string][]byte)
		planted []string
	}{
		{"widen-ladder-off", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/kind_array.py", ladder, ladder+"\n    return False")
		}, []string{"field_scalar_widen", "a1_arm_moved_width_as_a2", "map_key_widen"}},
		{"field-widened-not-counted", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/load.py", "ctx.widened += 1", "pass")
		}, []string{"field_scalar_widen"}},
		{"arm-widened-not-counted", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/kind_union.py", "ctx.widened += 1", "pass")
		}, []string{"a1_arm_moved_width_as_a2"}},
		{"map-widened-not-counted", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/kind_map.py", "ctx.widened += 1", "pass")
		}, []string{"map_key_widen"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := vtWidenFailures(t, ctl.mutate)
			joined := strings.Join(fails, "\n")
			for _, planted := range ctl.planted {
				if !strings.Contains(joined, planted) {
					t.Errorf("%s: %s stayed green (findings: %s)", ctl.name, planted, joined)
				}
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), func() string {
				if len(fails) == 0 {
					return "-"
				}
				return fails[0]
			}())
		})
	}
}
