package pythontable

// THE MAP ROW over the conformance manifest on the Python leg (ROADMAP.md,
// Variable tables, maps; docs/SPEC-TABLES.md §2.8). TestPythonVtMaps holds the
// kind on hand-made constructs; this file holds the roadmap row on every
// manifest instance and report row whose root reaches a `map[K]V`, and on the
// two Go-engine constructs the other legs' rows name (luatable's
// vtcells_test.go, the "maps" row): unions_maps_keyed and optional_map.
//
// Each row is read through the strict load and written back, under both
// pinned interpreters. The answer is the Go engine's: the row is accepted and
// written back byte-identical to the canonical wire (the committed wire for an
// instance, the engine's re-encoding for a report row), its measure its length
// and its widened counter the engine's; or it is refused, as the engine (or
// the manifest's wire-refused marker) refuses it, with the caller's buffer and
// report untouched. A saved value saves again to the same bytes, so the write
// left the caller's objects as they were.
//
// THE COMPLETENESS CHECK. pythonVTMapRows is exactly the manifest's instance
// and report rows whose root reaches a map: a corpus row added later that
// reaches one, and is not named here, turns the test red rather than being
// skipped.
//
// TestPythonVtMapsRowsNegativeControl plants one fault at a time in a COPY of
// the map runtime (_schema/kind_map.py) and requires the named rows red.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pythonVTMapRows is every manifest instance and report row whose root
// reaches a map, in manifest order.
var pythonVTMapRows = []string{
	"feature_full",
	"blob_shared_string",
	"list_of_maps",
	"text_total_and_count",
	"map_cells",
	"map_chunks",
	"map_crews",
	"map_depth",
	"map_docs",
	"map_empty",
	"map_full",
	"map_pairs",
	"map_slots",
	"map_spans",
	"map_text",
	"map_trails",
	"map_runs",
	"map_key_widen",
	"map_depth_root_kind",
	"map_key_order",
	"map_text_order",
	"feature_key_order",
	"feature_stamp_frac",
	"varevo_v1_full",
	"varevo_v1_ping",
	"varevo_v2_full",
	"varevo_v3_full",
	"varevo_v3_compat",
	"varevo_v3_rollback",
	"varevo_2hop_v3_origin",
	"varevo_v3_default_containers",
	"fixed_array_rides_default",
	"fixed_array_rides_live",
	"varevo_v1_as_v2",
	"varevo_v1_as_v3",
	"varevo_v1_ping_as_v3",
	"varevo_v2_as_v3",
	"varevo_v3_compat_as_v1",
	"varevo_v3_full_as_v1",
	"varevo_rollback_as_v3",
	"varevo_2hop_step2_v2_report",
	"varevo_2hop_step3_v3_report",
	"varevo_cycle_root_via_unknown_report",
	"varevo_cycle_known_via_unknown_report",
	"varevo_cycle_unknown_self_report",
	"varevo_cycle_two_unknowns_report",
	"varevo_default_containers_as_v3",
	"varevo_kind31_report",
	"torture_cook_full",
	"torture_cook_empty",
	"torture_cook_extremes",
	"torture_wide_cook",
	"torture_wide_cook_extremes",
	"vd_var_defaults",
	"vd_var_values",
	"widenbound_evodoc",
}

// pythonVTMapRowConstructs are the Go-engine constructs the map row names on
// the other legs (luatable's luaVTConstructs).
var pythonVTMapRowConstructs = []struct{ name, schema, root, json string }{
	{"unions_maps_keyed", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
	{"optional_map", "package probe\ntable M\n{\n    id int32\n    tags ?map[string(8)]int32\n}\n",
		"M", `{"id":1,"tags":{}}`},
}

type pythonVTMapManifestRow struct {
	name, kind, unit, root, wire string
	refusal                      bool
}

// pythonVTMapManifest reads the manifest's units, its instance and report
// rows (with the wire-refused marker), and the reports.txt verdicts.
func pythonVTMapManifest(t *testing.T, repo string) (map[string][]string, []pythonVTMapManifestRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	var rows []pythonVTMapManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			row := pythonVTMapManifestRow{name: f[1], kind: f[0], unit: f[2], root: f[3], wire: f[4]}
			for _, x := range f[5:] {
				if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
					row.refusal = true
				}
			}
			rows = append(rows, row)
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

// pythonVTReachesMap reports whether a value of the table st holds a map
// anywhere: a field of its own, or one reached through a table, a pointer, an
// array element or a union arm.
func pythonVTReachesMap(st *ir.Struct, seen map[*ir.Struct]bool) bool {
	if st == nil || seen[st] {
		return false
	}
	seen[st] = true
	var field func(f *ir.Field) bool
	field = func(f *ir.Field) bool {
		if f == nil {
			return false
		}
		if f.IsMap() {
			return true
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return pythonVTReachesMap(ref, seen)
		case *ir.Union:
			for _, v := range ref.Variants {
				if field(v.F) {
					return true
				}
			}
		}
		return false
	}
	return slices.ContainsFunc(st.Fields, field)
}

// pythonVTMapCase is one row the cell holds: its unit's key, the root, the
// wire, and the line the probe must print for it.
type pythonVTMapCase struct {
	name, unit, root string
	wire             []byte
	want             string
}

// pythonVTMapEngineAnswer is the Go engine's answer for wire at root: the
// probe line of a strict read-then-write, or "refused".
func pythonVTMapEngineAnswer(t *testing.T, u *ir.Unit, root string, wire []byte) (string, bool, []byte) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	value := m.New(st)
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, value, wire, &report); !ok || err != nil || report.Refused {
		return "refused", false, nil
	}
	canonical, err := tablewire.Encode(m, value)
	if err != nil {
		return "refused", false, nil
	}
	return fmt.Sprintf("read %x %d True True", canonical, report.Widened), true, canonical
}

// pythonVTMapCases builds every case the cell holds, grouped by unit, and the
// units themselves.
func pythonVTMapCases(t *testing.T) (map[string][]pythonVTMapCase, map[string]*ir.Unit) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	unitPaths, rows, expect := pythonVTMapManifest(t, repo)
	byName := map[string]pythonVTMapManifestRow{}
	for _, row := range rows {
		byName[row.name] = row
	}
	units := map[string]*ir.Unit{}
	unit := func(key string) *ir.Unit {
		if u, ok := units[key]; ok {
			return u
		}
		var paths []string
		for _, p := range unitPaths[key] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := loadUnitPaths(t, paths...)
		units[key] = u
		return u
	}
	cases := map[string][]pythonVTMapCase{}
	for _, name := range pythonVTMapRows {
		row, ok := byName[name]
		if !ok {
			t.Fatalf("%s is not a manifest instance or report row", name)
		}
		u := unit(row.unit)
		if st := u.Tables[row.root]; st == nil || st.FixedDeclared {
			t.Fatalf("%s: %s/%s is not a variable table", name, row.unit, row.root)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: read wire: %v", name, err)
		}
		want, read, canonical := pythonVTMapEngineAnswer(t, u, row.root, wire)
		switch {
		case row.refusal:
			want = "refused"
		case row.kind == "report":
			verdict := "refused"
			if read {
				verdict = "read"
			}
			if expect[name] != verdict {
				t.Fatalf("%s: the engine's verdict is %s, reports.txt says %q", name, verdict, expect[name])
			}
		case !read || string(canonical) != string(wire):
			t.Fatalf("%s: the engine does not write the instance's own wire back", name)
		}
		cases[row.unit] = append(cases[row.unit], pythonVTMapCase{name: name, unit: row.unit, root: row.root, wire: wire, want: name + " " + want})
	}
	for _, c := range pythonVTMapRowConstructs {
		u := loadUnit(t, c.schema)
		key := "construct-" + c.name
		units[key] = u
		wire := pythonVTMapEngineWire(t, u, c.root, c.json)
		want, read, _ := pythonVTMapEngineAnswer(t, u, c.root, wire)
		if !read {
			t.Fatalf("construct %s: the engine does not read its own wire", c.name)
		}
		cases[key] = append(cases[key], pythonVTMapCase{name: "construct_" + c.name, unit: key, root: c.root, wire: wire, want: "construct_" + c.name + " " + want})
	}
	return cases, units
}

const pythonVTMapRowsProbe = `import importlib
pkg = importlib.import_module(%q)
LoadReport = pkg.LoadReport
Refused = pkg.Refused
with open("rows") as handle:
    lines = handle.read().split()
for i in range(0, len(lines), 2):
    name, root = lines[i], lines[i + 1]
    cls = getattr(pkg, root)
    with open(name + ".wire", "rb") as handle:
        original = handle.read()
    buffer = bytearray(original)
    report = LoadReport()
    report.widened = 77
    report.unknown = 66
    try:
        value = cls.load(buffer, report)
    except Refused:
        assert buffer == original, name + ": the refusal changed the caller's buffer"
        assert (report.widened, report.unknown) == (77, 66), name + ": the refusal changed the caller's report"
        print(name, "refused")
        continue
    assert buffer == original, name + ": the read changed the caller's buffer"
    saved = value.save()
    print(name, "read", saved.hex(), report.widened, value.measure() == len(saved), value.save() == saved)
print("OK")
`

// pythonVTMapRowsFailures runs every case under both pinned interpreters, one
// process per unit and interpreter, the emitted files edited by mutate when it
// is set, and returns the failures, each naming its row, and how many rows it
// held.
func pythonVTMapRowsFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	cases, units := pythonVTMapCases(t)
	keys := slices.Sorted(maps.Keys(cases))
	var fails []string
	held := 0
	for _, key := range keys {
		u := units[key]
		files, err := Generate(u)
		if err != nil {
			for _, c := range cases[key] {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): the backend refused the unit: %v", c.name, key, c.root, err))
			}
			continue
		}
		if mutate != nil {
			files = maps.Clone(files)
			mutate(t, files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + fmt.Sprintf(pythonVTMapRowsProbe, u.Package))}
		var list strings.Builder
		for _, c := range cases[key] {
			fmt.Fprintf(&list, "%s %s\n", c.name, c.root)
			inputs[c.name+".wire"] = c.wire
		}
		inputs["rows"] = []byte(list.String())
		held += len(cases[key])
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			pin := filepath.Base(python)
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			got := map[string]string{}
			for line := range strings.SplitSeq(stdout, "\n") {
				if name, _, ok := strings.Cut(line, " "); ok {
					got[name] = line
				}
			}
			for _, c := range cases[key] {
				switch line, ok := got[c.name]; {
				case !ok:
					fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): no answer: %v: %s", c.name, key, c.root, pin, err, strings.TrimSpace(stderr)))
				case line != c.want:
					fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the read-then-write is %q, the engine says %q", c.name, key, c.root, pin, line, c.want))
				}
			}
			if err == nil && !strings.Contains(stdout, "OK\n") {
				fails = append(fails, fmt.Sprintf("%s (%s): the probe did not finish", key, pin))
			}
		}
	}
	return fails, held
}

// TestPythonVtMapsRows holds the map row over the manifest. Its completeness
// check runs in every build; the read-then-write runs the pinned interpreters
// (functional: run with SCHEMA_SLOW=1).
func TestPythonVtMapsRows(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unitPaths, rows, _ := pythonVTMapManifest(t, repo)
	cache := map[string]*ir.Unit{}
	var reach []string
	for _, row := range rows {
		u, ok := cache[row.unit]
		if !ok {
			var paths []string
			for _, p := range unitPaths[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = loadUnitPaths(t, paths...)
			cache[row.unit] = u
		}
		if pythonVTReachesMap(u.Tables[row.root], map[*ir.Struct]bool{}) {
			reach = append(reach, row.name)
		}
	}
	if !slices.Equal(reach, pythonVTMapRows) {
		missing, extra := []string{}, []string{}
		for _, name := range reach {
			if !slices.Contains(pythonVTMapRows, name) {
				missing = append(missing, name)
			}
		}
		for _, name := range pythonVTMapRows {
			if !slices.Contains(reach, name) {
				extra = append(extra, name)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		t.Fatalf("pythonVTMapRows is not the manifest's map rows in manifest order: not held %v, held but reach no map %v", missing, extra)
	}

	slowtest.Gate(t, "both pinned Python interpreters (it runs the emitted table runtime)")
	fails, held := pythonVTMapRowsFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if want := len(pythonVTMapRows) + len(pythonVTMapRowConstructs); held != want {
		t.Fatalf("the row held %d cases, want %d", held, want)
	}
	t.Logf("%d manifest rows and %d constructs held", len(pythonVTMapRows), len(pythonVTMapRowConstructs))
}

// TestPythonVtMapsRowsNegativeControl plants, one at a time, a fault in a COPY
// of the map runtime and requires the rows it guards red by name: the reader's
// key-order test inverted (every map of two or more entries refuses), and the
// writer's entry count one too many (no saved map is the canonical wire).
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtMapsRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (it runs the emitted table runtime)")
	plant := func(from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			t.Helper()
			for name := range files {
				if !strings.HasSuffix(name, "_schema/kind_map.py") {
					continue
				}
				src := string(files[name])
				if got := strings.Count(src, from); got != 1 {
					t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, got)
				}
				files[name] = []byte(strings.Replace(src, from, to, 1))
				return
			}
			t.Fatal("no emitted _schema/kind_map.py")
		}
	}
	for _, ctl := range []struct {
		name    string
		mutate  func(*testing.T, map[string][]byte)
		planted []string
	}{
		{"read-order-inverted", plant("if landed and _compare(fd, last, key) >= 0:", "if landed and _compare(fd, last, key) <= 0:"),
			[]string{"map_cells", "map_full", "map_key_order", "map_text_order", "list_of_maps", "map_key_widen", "construct_unions_maps_keyed"}},
		{"write-count-plus-one", plant("body_writer.varint(len(keys))", "body_writer.varint(len(keys) + 1)"),
			[]string{"map_cells", "map_chunks", "map_full", "map_key_order", "map_text_order", "list_of_maps", "map_key_widen", "construct_unions_maps_keyed", "construct_optional_map"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := pythonVTMapRowsFailures(t, ctl.mutate)
			joined := strings.Join(fails, "\n")
			for _, planted := range ctl.planted {
				if !strings.Contains(joined, planted+" (") {
					t.Errorf("%s: %s stayed green (findings: %s)", ctl.name, planted, joined)
				}
			}
			t.Logf("%s: %d findings", ctl.name, len(fails))
		})
	}
}
