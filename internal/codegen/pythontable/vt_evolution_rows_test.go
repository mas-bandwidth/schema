package pythontable

// THE EVOLUTION ROWS on the Python table leg (ROADMAP.md, Variable tables:
// "string, bytes and flags defaults, and renaming a table" and "renaming
// variants, arms and type fields"; docs/SPEC-TABLES.md §3, §4, §5). The
// evolution constructs themselves are held in evolution_test.go; this file
// holds each of the two roadmap rows over EVERY conformance manifest instance
// that uses it (internal/codegen/luatable/vtcells_test.go names the same
// corpus), under both pinned interpreters, to the one read on read-then-write:
//
//	instance  the wire loads and saves back byte for byte, its measure its
//	          length, with the engine's unknown count
//	report    a type that did not write the bytes reads them: the verdict is
//	          reports.txt's; a read saves the manifest's canonical rewrite
//	          bytes (rewrite/<case>.bin) with the engine's unknown count
//	construct a Go-engine construct (evolution_test.go): the engine's wire of
//	          the JSON text loads and saves back byte for byte
//
// A refusal leaves no partial value and does not touch the caller's report or
// input buffer; a read does not touch the input buffer. A root declared
// `fixed table` (tblr1, tblr2 Cfg) reads and writes form 3 through the
// generated load and save, as the luatable cells read it through ReadFixed.
//
// TestPythonVtEvolutionRows also holds the corpus whole: every manifest
// instance and report row of the evolution units (tblw1, tblw2, tblr1, tblr2)
// is named by a row here, except the previous_form_* rows, which are the
// fixed-class row's. TestPythonVtEvolutionRowsNegativeControl plants a fault
// in a COPY of the emitted runtime and of the generated package and requires
// each row to go red by name.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

type vtEvoRow struct {
	row        string
	cases      []string
	constructs []string
}

// vtEvoRows is the two roadmap rows and the manifest cases and constructs
// that exercise each, as the luatable cells name them.
var vtEvoRows = []vtEvoRow{
	{"defaults-and-table-rename",
		[]string{"w1_fleet", "w1_fleet_default", "w2_fleet", "w1_fleet_as_w2", "w2_fleet_as_w1", "w1_fleet_default_as_w2"},
		[]string{"defaults_at", "defaults_off"}},
	{"renames",
		[]string{"r1_cfg", "r1_ping", "r2_cfg", "r1_cfg_as_r2", "r1_ping_as_r2", "r2_cfg_as_r1"}, nil},
}

// vtEvoUnits is the evolution units: every instance and report row over them
// is a case of one row above, but the fixed-class row's previous_form_* rows.
var vtEvoUnits = []string{"tblw1", "tblw2", "tblr1", "tblr2"}

type vtEvoManifestRow struct {
	kind, name, unit, root, wire string
}

type vtEvoManifest struct {
	units    map[string][]string
	rows     map[string]vtEvoManifestRow
	verdicts map[string]string
}

func vtEvoReadManifest(t *testing.T, repo string) vtEvoManifest {
	t.Helper()
	dir := filepath.Join(repo, "testdata", "conformance", "tables")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	m := vtEvoManifest{units: map[string][]string{}, rows: map[string]vtEvoManifestRow{}, verdicts: map[string]string{}}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			m.units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			m.rows[f[1]] = vtEvoManifestRow{kind: f[0], name: f[1], unit: f[2], root: f[3], wire: f[4]}
		}
	}
	verdicts, err := os.ReadFile(filepath.Join(dir, "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(verdicts), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			m.verdicts[f[0]] = f[1]
		}
	}
	return m
}

// vtEvoProbeCase is one wire the probe reads at root, and the line the engine
// and the manifest owe it.
type vtEvoProbeCase struct {
	name, root string
	wire       []byte
	want       string
}

// vtEvoProbe reads each case's wire through the generated root's load and
// writes it back. A refusal must leave no value and the caller's buffer and
// report as they were; a read must leave the buffer as it was (its report
// holds the read's own counters).
const vtEvoProbe = `
for name, root in CASES:
    with open("w_" + name, "rb") as handle:
        original = handle.read()
    buffer = bytearray(original)
    report = LoadReport()
    report.unknown = 66
    report.widened = 77
    value = None
    try:
        value = root.load(buffer, report)
    except Refused:
        untouched = value is None and buffer == original and (report.unknown, report.widened) == (66, 77)
        print("case", name, "refused", "untouched=" + str(untouched))
        continue
    saved = value.save()
    print("case", name, "read", saved.hex(), "unknown=" + str(report.unknown),
          "measure=" + str(value.measure() == len(saved)), "untouched=" + str(buffer == original))
`

// vtEvoEngine is the Go engine's verdict on wire at root of u (as the unit
// declares the table) and, when it reads, its unknown count.
func vtEvoEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (bool, int) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, m.New(st), wire, &rep)
	if !ok || err != nil {
		return false, 0
	}
	return true, rep.Unknown
}

func vtEvoReadLine(name string, canonical []byte, unknown int) string {
	return fmt.Sprintf("case %s read %x unknown=%d measure=True untouched=True", name, canonical, unknown)
}

// vtEvoGroup is the probe cases one generated unit answers.
type vtEvoGroup struct {
	label string
	unit  *ir.Unit
	cases []vtEvoProbeCase
}

// vtEvoGroups builds, for one row, the probe cases grouped by the unit that
// reads them, each with the line the engine and the manifest owe it.
func vtEvoGroups(t *testing.T, r vtEvoRow) []vtEvoGroup {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	man := vtEvoReadManifest(t, repo)
	byUnit := map[string]*vtEvoGroup{}
	var order []string
	group := func(label string, u func() *ir.Unit) *vtEvoGroup {
		g, ok := byUnit[label]
		if !ok {
			g = &vtEvoGroup{label: label, unit: u()}
			byUnit[label] = g
			order = append(order, label)
		}
		return g
	}
	for _, name := range r.cases {
		row, ok := man.rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", r.row, name)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		g := group(row.unit, func() *ir.Unit {
			var paths []string
			for _, p := range man.units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			return loadUnitPaths(t, paths...)
		})
		read, unknown := vtEvoEngine(t, g.unit, row.root, wire)
		var want string
		switch row.kind {
		case "instance":
			if !read {
				t.Fatalf("%s: the engine refuses the instance wire", name)
			}
			want = vtEvoReadLine(name, wire, unknown)
		case "report":
			verdict, ok := man.verdicts[name]
			if !ok {
				t.Fatalf("%s: reports.txt names no such case", name)
			}
			if got := map[bool]string{true: "read", false: "refused"}[read]; got != verdict {
				t.Fatalf("%s: the engine's verdict is %s, reports.txt says %s", name, got, verdict)
			}
			if !read {
				want = fmt.Sprintf("case %s refused untouched=True", name)
				break
			}
			canonical, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "rewrite", name+".bin"))
			if err != nil {
				t.Fatalf("%s reads, and the manifest has no canonical rewrite: %v", name, err)
			}
			want = vtEvoReadLine(name, canonical, unknown)
		}
		g.cases = append(g.cases, vtEvoProbeCase{name: name, root: row.root, wire: wire, want: want})
	}
	for _, name := range r.constructs {
		c := vtCase(t, name)
		wire := vtEvolutionWire(t, c)
		g := group("construct "+name, func() *ir.Unit { return loadUnit(t, c.schema) })
		g.cases = append(g.cases, vtEvoProbeCase{name: name, root: c.root, wire: wire, want: vtEvoReadLine(name, wire, 0)})
	}
	groups := make([]vtEvoGroup, 0, len(order))
	for _, label := range order {
		groups = append(groups, *byUnit[label])
	}
	return groups
}

// vtEvoPlant is a negative control's fault: an edit of a copy of the emitted
// files (the runtime or the generated package), or of the unit the package is
// generated from after the engine's expectations are taken.
type vtEvoPlant struct {
	files func(*testing.T, map[string][]byte)
	unit  func(*ir.Unit)
}

// vtEvoRowFailures runs one row's cases under both pinned interpreters (with
// plant's fault when set) and answers every case whose Python line is not the
// one the engine and the manifest owe, by case name, and how many cases it
// held.
func vtEvoRowFailures(t *testing.T, r vtEvoRow, plant *vtEvoPlant) (fails []string, held int) {
	t.Helper()
	for _, g := range vtEvoGroups(t, r) {
		if plant != nil && plant.unit != nil {
			plant.unit(g.unit)
		}
		files, err := Generate(g.unit)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refuses the unit: %v", g.label, err))
			continue
		}
		files = maps.Clone(files)
		if plant != nil && plant.files != nil {
			plant.files(t, files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs := map[string][]byte{}
		roots := map[string]bool{}
		var list strings.Builder
		list.WriteString("CASES = [\n")
		for _, c := range g.cases {
			inputs["w_"+c.name] = c.wire
			roots[c.root] = true
			fmt.Fprintf(&list, "    (%q, %s),\n", c.name, c.root)
		}
		list.WriteString("]\n")
		names := slices.Sorted(maps.Keys(roots))
		probe := pyrun.ImportPrelude + "from " + g.unit.Package + " import " + strings.Join(names, ", ") +
			", LoadReport, Refused\n" + list.String() + vtEvoProbe
		inputs["main.py"] = []byte(probe)
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			lines := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "case" {
					lines[f[1]] = line
				}
			}
			for _, c := range g.cases {
				switch {
				case err != nil && lines[c.name] == "":
					fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): %v: %s", c.name, g.label, c.root, filepath.Base(python), err, strings.TrimSpace(stderr)))
				case lines[c.name] != c.want:
					fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the read-then-write is %q, want %q", c.name, g.label, c.root, filepath.Base(python), lines[c.name], c.want))
				}
			}
		}
		held += len(g.cases)
	}
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")

	// THE COMPLETENESS CHECK: every instance and report row of the evolution
	// units is a case of one row here, but the previous_form_* rows.
	man := vtEvoReadManifest(t, filepath.Join("..", "..", ".."))
	named := map[string]bool{}
	for _, r := range vtEvoRows {
		for _, name := range r.cases {
			named[name] = true
		}
	}
	var missing []string
	for name, row := range man.rows {
		if slices.Contains(vtEvoUnits, row.unit) && !strings.HasPrefix(name, "previous_form_") && !named[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s is a manifest row of an evolution unit and no evolution row holds it", name)
	}

	for _, r := range vtEvoRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			fails, held := vtEvoRowFailures(t, r, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if want := len(r.cases) + len(r.constructs); held != want {
				t.Fatalf("the row held %d cases, want %d", held, want)
			}
			t.Logf("%s: %d manifest cases and %d constructs held under both pins", r.row, len(r.cases), len(r.constructs))
		})
	}
}

// vtEvoDropWas forgets every `was` the unit declares (a table's, a field's,
// an enum variant's and a union arm's), so the package generated from it
// spells every wire id from the declared name: the rename the rows hold,
// planted out of the generated package.
func vtEvoDropWas(u *ir.Unit) {
	for _, structs := range []map[string]*ir.Struct{u.Tables, u.Structs} {
		for _, st := range structs {
			st.WasName = ""
			for _, f := range st.Fields {
				f.WasName = ""
			}
		}
	}
	for _, e := range u.Enums {
		for i := range e.Was {
			e.Was[i] = ""
		}
	}
	for _, unions := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range unions {
			for i := range un.Variants {
				un.Variants[i].WasName = ""
				if un.Variants[i].F != nil {
					un.Variants[i].F.WasName = ""
				}
			}
		}
	}
}

// TestPythonVtEvolutionRowsNegativeControl plants, one at a time, a fault in
// a COPY of the emitted runtime or generated package and requires each row it
// aims at to go red, by the names of the cases it plants into:
//
//	runtime-bytes-default   descriptor.py judges no bytes value at its
//	                        declared default, so a default bytes field rides
//	                        and the default-holding Ship nodes and the
//	                        default D no longer save the canonical wire
//	runtime-layout-check    fixedform.py's layout comparison is inverted, so
//	                        every form-3 Cfg file refuses
//	generated-was-dropped   the package is generated with every `was`
//	                        forgotten, so the renamed Ship, Argent, shield,
//	                        pong and mult spell their ids from the declared
//	                        names and the renamed units' files no longer read
//	                        back
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	edit := func(suffix, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			for name := range files {
				if !strings.HasSuffix(name, suffix) {
					continue
				}
				src := string(files[name])
				if n := strings.Count(src, from); n != 1 {
					t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, n)
				}
				files[name] = []byte(strings.Replace(src, from, to, 1))
				return
			}
			t.Fatalf("no emitted file ends with %s", suffix)
		}
	}
	for _, ctl := range []struct {
		name    string
		plant   vtEvoPlant
		planted map[string][]string // row -> cases that must go red
	}{
		{"runtime-bytes-default", vtEvoPlant{files: edit("_schema/descriptor.py",
			"return raw is not None and bool(raw == self.default)", "return False")},
			map[string][]string{"defaults-and-table-rename": {"w1_fleet", "w1_fleet_default", "w2_fleet", "w1_fleet_as_w2", "w2_fleet_as_w1", "w1_fleet_default_as_w2", "defaults_at"}}},
		{"runtime-layout-check", vtEvoPlant{files: edit("_schema/fixedform.py",
			`if source[20:20 + length] != entry["layout"]:`, `if source[20:20 + length] == entry["layout"]:`)},
			map[string][]string{"renames": {"r1_cfg", "r1_ping", "r2_cfg"}}},
		{"generated-was-dropped", vtEvoPlant{unit: vtEvoDropWas},
			map[string][]string{"defaults-and-table-rename": {"w2_fleet", "w1_fleet_as_w2"}, "renames": {"r2_cfg"}}},
	} {
		for _, r := range vtEvoRows {
			want, aimed := ctl.planted[r.row]
			if !aimed {
				continue
			}
			t.Run(ctl.name+"/"+r.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := vtEvoRowFailures(t, r, &ctl.plant)
				if len(fails) == 0 {
					t.Fatalf("%s: the %s row stayed green", ctl.name, r.row)
				}
				for _, name := range want {
					if !slices.ContainsFunc(fails, func(f string) bool { return strings.HasPrefix(f, name+" ") }) {
						t.Errorf("%s: case %s of the %s row stayed green (findings:\n%s)", ctl.name, name, r.row, strings.Join(fails, "\n"))
					}
				}
				t.Logf("%s/%s: %d findings, first %s", ctl.name, r.row, len(fails), fails[0])
			})
		}
	}
}
