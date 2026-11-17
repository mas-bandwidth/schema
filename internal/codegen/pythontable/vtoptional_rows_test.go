package pythontable

// THE OPTIONAL ROWS on the Python leg (ROADMAP.md, Variable tables:
// port/python/vt/optional-fields and port/python/vt/optional-arrays;
// docs/SPEC-TABLES.md §2.3). Each row is held over every conformance-manifest
// instance the Lua leg names for it (internal/codegen/luatable/vtcells_test.go,
// luaVTCells) and over the Go-engine constructs it names, to THE ONE READ:
//
//	instance   the committed wire loads through the Python read and saves back
//	           byte for byte, its measure its length; a wire-refused instance
//	           refuses
//	report     the read's verdict is reports.txt's and the Go engine's, and a
//	           wire that reads saves back as the engine's canonical write of it
//	construct  the Go engine's wire of the JSON text reads and saves back byte
//	           for byte
//
// A refusal leaves no value and does not touch the caller's buffer or report;
// a read does not touch the caller's buffer. Both pinned interpreters answer
// every row. A root declared `fixed table` reads and writes form 3 through the
// same load and save (the fixed-form card), so the manifest's fixed rows are
// held as the manifest declares them.
//
// TestPythonVtOptionalRowsNegativeControl plants each optional fault in a COPY
// of the emitted runtime and requires its rows to go red by name, and every
// row of both cells to go red under at least one control.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type vtOptionalCell struct {
	row        string
	cases      []string
	constructs []string
}

// vtOptionalCells are luaVTCells' optional-fields and optional-arrays rows,
// case for case.
var vtOptionalCells = []vtOptionalCell{
	{"optional-fields", []string{"chain_optional", "chain_optional_empty", "p1_as_p3", "p3_as_p1", "p1_empty_as_p3", "p3_empty_as_p1",
		"p2_as_p3", "p2_empty_as_p3", "p3_as_p2", "optional_absent_no_edge", "optional_present_edge"},
		[]string{"optional_variable", "optional_map"}},
	{"optional-arrays", []string{"message_trace", "message_save", "message_empty", "trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant"}, nil},
}

type vtOptionalConstruct struct{ name, schema, root, json string }

// vtOptionalConstructs are luaVTConstructs' optional constructs.
var vtOptionalConstructs = []vtOptionalConstruct{
	{"optional_variable", "package probe\ntable Inner\n{\n    text *string\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n    tail int32\n}\n",
		"Outer", `{"inner":{"text":"hi","n":4},"tail":5}`},
	{"optional_map", "package probe\ntable M\n{\n    id int32\n    tags ?map[string(8)]int32\n}\n",
		"M", `{"id":1,"tags":{}}`},
}

type vtOptionalRow struct {
	kind, unit, root, wire string
	refusal                bool
}

// vtOptionalManifest reads the manifest's units, its instance and report rows
// (an instance marked wire-refused, or by its legacy go-refusal= spelling,
// must refuse) and reports.txt's verdicts.
func vtOptionalManifest(t *testing.T, repo string) (map[string][]string, map[string]vtOptionalRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]vtOptionalRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			row := vtOptionalRow{kind: f[0], unit: f[2], root: f[3], wire: f[4]}
			if f[0] == "instance" {
				for _, x := range f[5:] {
					if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
						row.refusal = true
					}
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
		if len(f) >= 2 && !strings.HasPrefix(f[0], "#") {
			expect[f[0]] = f[1]
		}
	}
	return units, rows, expect
}

// vtOptionalEngine reads wire at root through the Go engine as the unit
// declares the root and answers whether it reads and, when it does, the
// engine's canonical write of what it read.
func vtOptionalEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (bool, []byte) {
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

// vtOptionalProbe is THE ONE READ of the file "wire" at a root, then its save.
// It prints "refused <reason>" or "read <saved hex> <measure is length>".
const vtOptionalProbe = `from %s import %s, LoadReport, Refused
with open("wire", "rb") as handle:
    original = handle.read()
buffer = bytearray(original)
report = LoadReport()
report.widened = 77
report.unknown = 66
try:
    value = %s.load(buffer, report)
except Refused as err:
    assert buffer == original, "the refusal changed the caller's buffer"
    assert (report.widened, report.unknown) == (77, 66), "the refusal changed the caller's report"
    print("refused", err.reason)
else:
    assert buffer == original, "the read changed the caller's buffer"
    saved = value.save()
    print("read", saved.hex(), value.measure() == len(saved))
`

// vtOptionalCase is one held case: a manifest row or a construct, with its
// unit, root, wire and the answer the read must give.
type vtOptionalCase struct {
	name, label string
	u           *ir.Unit
	root        string
	wire        []byte
	want        string // "refused" or "read <hex> True"
}

// vtOptionalCases resolves a cell to its held cases, holding each manifest
// row's engine verdict to the manifest first.
func vtOptionalCases(t *testing.T, c vtOptionalCell) []vtOptionalCase {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := vtOptionalManifest(t, repo)
	var cases []vtOptionalCase
	for _, name := range c.cases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		var paths []string
		for _, p := range units[row.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := loadUnitPaths(t, paths...)
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		accepted, canonical := vtOptionalEngine(t, u, row.root, wire)
		switch {
		case row.kind == "report":
			want, ok := expect[name]
			if !ok {
				t.Fatalf("report %s: reports.txt names no such case", name)
			}
			if got := map[bool]string{true: "read", false: "refused"}[accepted]; got != want {
				t.Fatalf("report %s: the engine's verdict is %s, reports.txt says %s", name, got, want)
			}
		case row.refusal:
			if accepted {
				t.Fatalf("instance %s is wire-refused and the engine reads it", name)
			}
		default:
			if !accepted || string(canonical) != string(wire) {
				t.Fatalf("instance %s: the engine does not write the committed wire back", name)
			}
		}
		want := "refused"
		if accepted {
			want = fmt.Sprintf("read %x True", canonical)
		}
		cases = append(cases, vtOptionalCase{name: name, label: row.unit + "/" + row.root, u: u, root: row.root, wire: wire, want: want})
		// A report row at a fixed root carries a form-1 wire, which the
		// declared root refuses as previous_form (docs/SPEC-TABLES.md §3.4).
		// The optional RULE rides form 1, so it is held as well with the root
		// variable, exactly as TestPythonVtWidening asks a fixed root's rows.
		if row.kind == "report" && u.Tables[row.root].FixedDeclared {
			v := loadUnitPaths(t, paths...)
			v.Tables[row.root].FixedDeclared = false
			accepted, canonical := vtOptionalEngine(t, v, row.root, wire)
			want := "refused"
			if accepted {
				want = fmt.Sprintf("read %x True", canonical)
			}
			cases = append(cases, vtOptionalCase{name: name + "/variable", label: row.unit + "/" + row.root + " (variable root)", u: v, root: row.root, wire: wire, want: want})
		}
	}
	for _, name := range c.constructs {
		i := slices.IndexFunc(vtOptionalConstructs, func(cc vtOptionalConstruct) bool { return cc.name == name })
		if i < 0 {
			t.Fatalf("%s names construct %s, which vtOptionalConstructs does not carry", c.row, name)
		}
		cc := vtOptionalConstructs[i]
		u := loadUnit(t, cc.schema)
		wire := optionalEngineWire(t, u, cc.root, cc.json)
		cases = append(cases, vtOptionalCase{name: name, label: "construct/" + cc.root, u: u, root: cc.root, wire: wire, want: fmt.Sprintf("read %x True", wire)})
	}
	return cases
}

// vtOptionalFailures runs every case of the cell under both pinned
// interpreters, the emitted files edited by mutate when set, and answers the
// named failures and how many cases it held.
func vtOptionalFailures(t *testing.T, c vtOptionalCell, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	var fails []string
	held := 0
	for _, cs := range vtOptionalCases(t, c) {
		files, err := Generate(cs.u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s (%s): the backend refused the unit: %v", cs.name, cs.label, err))
			continue
		}
		if mutate != nil {
			files = maps.Clone(files)
			mutate(t, files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := fmt.Sprintf(vtOptionalProbe, cs.u.Package, cs.root, cs.root)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe), "wire": cs.wire}
		held++
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			got := strings.TrimSpace(stdout)
			if strings.HasPrefix(got, "refused") {
				got = "refused"
			}
			switch {
			case err != nil:
				fails = append(fails, fmt.Sprintf("%s (%s, %s): %v: %s", cs.name, cs.label, filepath.Base(python), err, strings.TrimSpace(stderr)))
			case got != cs.want:
				fails = append(fails, fmt.Sprintf("%s (%s, %s): the read-then-write is %q, the engine says %q", cs.name, cs.label, filepath.Base(python), got, cs.want))
			}
		}
	}
	return fails, held
}

// TestPythonVtOptionalRows holds both optional rows over every manifest
// instance and construct they name, under both pinned interpreters.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtOptionalRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, c := range vtOptionalCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := vtOptionalFailures(t, c, nil)
			for _, f := range fails {
				t.Error(f)
			}
			want := len(vtOptionalCases(t, c))
			if held != want || held < len(c.cases)+len(c.constructs) {
				t.Fatalf("the row held %d cases, want %d", held, want)
			}
			t.Logf("%s: %d manifest rows and %d constructs held, %d cases in all", c.row, len(c.cases), len(c.constructs), held)
		})
	}
}

// vtOptionalEdit replaces one occurrence of from with to in the emitted file
// whose path ends with suffix, and fails if the anchor is absent or repeated.
func vtOptionalEdit(t *testing.T, files map[string][]byte, suffix, from, to string) {
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

// vtOptionalControls are the optional faults, each planted in a COPY of the
// emitted runtime, and the cases each must turn red by name.
var vtOptionalControls = []struct {
	name    string
	mutate  func(*testing.T, map[string][]byte)
	planted []string
}{
	// The variable form writes a present optional as absent: the walk still
	// numbers its nodes, so the save is not the wire.
	{"variable-present-elided", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/kind_optional.py",
			"            _write_array(w, fd, value, ctx)\n            return\n        self._inner(fd).write(w, fd, value, ctx)\n",
			"            return\n")
	}, []string{"optional_present_edge", "optional_variable", "optional_map", "p1_as_p3/variable", "marks_unknown_variant/variable"}},
	// The variable form reads a present optional table and forgets it.
	{"variable-present-read-absent", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/kind_optional.py",
			"        return self._inner(fd).read(r, fd, ctx)\n",
			"        self._inner(fd).read(r, fd, ctx)\n        return None\n")
	}, []string{"optional_present_edge", "optional_variable", "optional_map", "p1_as_p3/variable"}},
	// The fixed form writes a present optional's presence byte as absent.
	{"fixed-presence-dropped", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/fixedform.py",
			"        if value is not None:\n            out[at] = 1\n",
			"        if value is not None:\n            out[at] = 0\n")
	}, []string{"chain_optional", "chain_optional_empty", "message_trace"}},
	// The fixed form reads an absent optional as its present default.
	{"fixed-absent-read-present", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/fixedform.py",
			"        return _decode(spec[\"children\"][0], data, at + 1) if data[at] else None\n",
			"        return _decode(spec[\"children\"][0], data, at + 1)\n")
	}, []string{"message_empty", "message_save"}},
	// The variable form reads an optional array past its declared bound.
	{"variable-array-count-unchecked", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/kind_optional.py",
			"    if count > fd.bound or (fd.array_kind == 2 and count < fd.array_min):\n",
			"    if False:\n")
	}, []string{"trace_count_past_bound/variable"}},
	// The variable form reads an optional array under a foreign element kind.
	{"variable-array-kind-unchecked", func(t *testing.T, files map[string][]byte) {
		vtOptionalEdit(t, files, "_schema/kind_optional.py",
			"    if widened and not _widens(elem_kind, elem.kind):\n",
			"    if False:\n")
	}, []string{"trace_elem_kind/variable"}},
	// A new value's optional starts present instead of None (the emitted
	// class), so a field the wire leaves absent saves as present.
	{"absent-starts-present", func(t *testing.T, files map[string][]byte) {
		vtOptionalEditWhere(t, files, "        self.link = None\n", "        self.link = Link()\n")
		vtOptionalEditWhere(t, files, "        self.optional = None\n", "        self.optional = Wrapped()\n")
	}, []string{"optional_absent_no_edge", "p1_empty_as_p3/variable"}},
}

// vtOptionalEditWhere replaces from with to in every emitted file that
// carries it once; a file without it is left alone (the control's planted
// cases are what keep it from being vacuous).
func vtOptionalEditWhere(t *testing.T, files map[string][]byte, from, to string) {
	t.Helper()
	for name, data := range files {
		switch strings.Count(string(data), from) {
		case 0:
		case 1:
			files[name] = []byte(strings.Replace(string(data), from, to, 1))
		default:
			t.Fatalf("%s carries %q more than once: the control would be ambiguous", name, from)
		}
	}
}

// vtOptionalCellOf names the cell a case (or its "/variable" twin) belongs to.
func vtOptionalCellOf(name string) string {
	base, _, _ := strings.Cut(name, "/")
	for _, c := range vtOptionalCells {
		if slices.Contains(c.cases, base) || slices.Contains(c.constructs, base) {
			return c.row
		}
	}
	return ""
}

// TestPythonVtOptionalRowsNegativeControl plants each optional fault in a COPY
// of the emitted runtime (the optional kind's module, the fixed form's
// optional shape, or the emitted class's optional default) and requires its
// named cases to go red, and each of the two rows to be turned red by more
// than one control: no row is green by holding nothing. The declared-form
// previous_form verdicts of the form-1 report wires at fixed roots, and the
// pointer-against-optional kind_mismatch verdicts, are the shared table
// frame's (load.py, fixedform.py's form gate) and are held by verdict here.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtOptionalRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	controlled := map[string]int{}
	for _, ctl := range vtOptionalControls {
		cells := map[string]bool{}
		for _, planted := range ctl.planted {
			cell := vtOptionalCellOf(planted)
			if cell == "" {
				t.Fatalf("%s plants %s, which no row holds", ctl.name, planted)
			}
			cells[cell] = true
		}
		for cell := range cells {
			controlled[cell]++
		}
	}
	for _, c := range vtOptionalCells {
		if controlled[c.row] < 2 {
			t.Errorf("the %s row is turned red by %d controls, want at least 2", c.row, controlled[c.row])
		}
	}
	for _, ctl := range vtOptionalControls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			var all []string
			for _, c := range vtOptionalCells {
				if !slices.ContainsFunc(ctl.planted, func(p string) bool { return vtOptionalCellOf(p) == c.row }) {
					continue
				}
				fails, _ := vtOptionalFailures(t, c, ctl.mutate)
				all = append(all, fails...)
			}
			red := map[string]bool{}
			for _, f := range all {
				red[strings.Fields(f)[0]] = true
			}
			t.Logf("%s: red %v", ctl.name, slices.Sorted(maps.Keys(red)))
			for _, planted := range ctl.planted {
				if !red[planted] {
					t.Errorf("%s: %s stayed green", ctl.name, planted)
				}
			}
		})
	}
}
