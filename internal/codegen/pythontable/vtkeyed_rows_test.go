package pythontable

// THE ENUM-KEYED ARRAY ROWS on the Python leg (ROADMAP.md, Variable tables:
// port/python/vt/enum-keyed-arrays; docs/SPEC-TABLES.md §2.4, §3.2, §4). The
// kind's own test (vtkeyed_test.go, TestPythonVtKeyed) holds hand-written
// constructs and damaged wires; this file holds the conformance MANIFEST: every
// instance and report row whose root reaches a keyed array, and the construct
// luatable's vtcells_test.go names for the row, go through the one read on
// read-then-write under both pinned interpreters.
//
//	instance   the reference's wire loads and saves back byte for byte, its
//	           measure its length; a wire-refused instance refuses
//	report     the read's verdict is reports.txt's; a read saves the engine's
//	           canonical bytes
//	variable   each accepted instance at a fixed root is also read on form 1:
//	           the engine's variable-form bytes of the same value, at the same
//	           root declared variable, round-trip byte for byte, so the keyed
//	           body (kind 16) is held on the manifest's own data and not only
//	           on the fixed form's positional slots
//	construct  the Go engine's wire of the JSON text round-trips byte for byte
//
// A refused read leaves the caller's buffer and report as they were. A root
// declared `fixed table` reads form 3 alone (docs/SPEC-TABLES.md §3.4), so its
// instances are form-3 files and their form-1 copies are previous_form
// refusals, held here too; each accepted one gets the variable copy above.
// The map rows' roots (mapdemo Depth and Slots) are declared `table`, so they
// are read in their declared form 1 and get no separate copy.
//
// TestPythonVtKeyedRowsNegativeControl plants a keyed fault in a COPY of the
// emitted runtime, once in the variable form's keyed writer and once in the
// fixed form's keyed reader, and requires the rows each fault reaches to go
// red by name.

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

// pyKeyedRowCases is every manifest instance and report row the row holds:
// luatable's enum-keyed-arrays cases first, then every other row whose root
// reaches a keyed array. The completeness check in TestPythonVtKeyedRows
// requires every instance or report row whose root reaches a keyed array to
// be named here.
var pyKeyedRowCases = []string{
	// luatable's cases for the row
	"keyed_config", "keyed_default", "r1_cfg", "r2_cfg", "r1_cfg_as_r2", "r2_cfg_as_r1", "k1_as_k2", "k2_as_k1",
	// the keyed demo and the rename pair
	"previous_form_keyed_config", "previous_form_keyed_default", "previous_form_r1_cfg", "previous_form_r2_cfg",
	"r1_ping", "r1_ping_as_r2", "previous_form_r1_ping",
	// the scalar state's keyed fields, and its narrower writer
	"scalars_full", "scalars_default", "scalars_edges", "scalars_uint128_max",
	"previous_form_scalars_full", "previous_form_scalars_default", "previous_form_scalars_edges",
	"scalars_full_as_2", "scalars_edges_as_2", "field_scalar_widen",
	// the versioned config pair
	"v1_cfg", "v1_seams", "v2_cfg", "v2_seams", "v1_cfg_as_v2", "v1_seams_as_v2", "v2_cfg_as_v1", "v2_seams_as_v1",
	"previous_form_v1_cfg", "previous_form_v1_seams", "previous_form_v2_cfg", "previous_form_v2_seams",
	"form_zero", "form_six", "form_ff", "message_as_file",
	// keyed arrays inside map values
	"map_depth", "map_slots", "map_depth_root_kind",
	// cooktorture and varevo keyed rows
	"torture_cook_empty", "torture_cook_extremes", "torture_cook_full",
	"torture_fixed_wref", "torture_fixed_wref_str",
	"torture_wide_cook", "torture_wide_cook_extremes",
	"varevo_2hop_step2_v2_report", "varevo_2hop_step3_v3_report", "varevo_2hop_v3_origin",
	"varevo_cycle_known_via_unknown_report", "varevo_cycle_root_via_unknown_report",
	"varevo_cycle_two_unknowns_report", "varevo_cycle_unknown_self_report",
	"varevo_default_containers_as_v3", "varevo_kind31_report", "varevo_rollback_as_v3",
	"varevo_v1_as_v2", "varevo_v1_as_v3", "varevo_v1_full", "varevo_v1_ping",
	"varevo_v1_ping_as_v3", "varevo_v2_as_v3", "varevo_v2_full",
	"varevo_v3_compat", "varevo_v3_compat_as_v1", "varevo_v3_default_containers",
	"varevo_v3_full", "varevo_v3_full_as_v1", "varevo_v3_rollback",
	// the F-092 grown-bound evolution document (#4544)
	"widenbound_evodoc",
}

// pyKeyedRowConstructs are luatable's constructs for the row
// (internal/codegen/luatable/vtcells_test.go, unions_maps_keyed).
var pyKeyedRowConstructs = []pyKeyedConstruct{
	{"unions_maps_keyed", "package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
}

type pyKeyedRow struct {
	kind, name, unit, root, wire string
	wireRefused                  bool
}

// pyKeyedRowsManifest reads the tables manifest's units, its instance and
// report rows, and the reports.txt verdicts.
func pyKeyedRowsManifest(t *testing.T, repo string) (map[string][]string, map[string]pyKeyedRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]pyKeyedRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			row := pyKeyedRow{kind: f[0], name: f[1], unit: f[2], root: f[3], wire: f[4]}
			for _, x := range f[5:] {
				// wire-refused (testdata/conformance/tables/FORMAT.md);
				// go-refusal= is its legacy spelling
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

// pyKeyedReaches reports whether the table or type st reaches a keyed array
// through its fields, union arms, map entries, by-value tables and pointers.
func pyKeyedReaches(st *ir.Struct, seen map[*ir.Struct]bool) bool {
	if st == nil || seen[st] {
		return false
	}
	seen[st] = true
	var field func(f *ir.Field) bool
	field = func(f *ir.Field) bool {
		if f == nil {
			return false
		}
		if f.KeyEnumRef != nil {
			return true
		}
		if f.MapEntry != nil && pyKeyedReaches(f.MapEntry, seen) {
			return true
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			return pyKeyedReaches(ref, seen)
		case *ir.Union:
			for _, v := range ref.Variants {
				if field(v.F) || pyKeyedReaches(v.Ref, seen) {
					return true
				}
			}
		}
		return false
	}
	return slices.ContainsFunc(st.Fields, field)
}

// pyKeyedRowsUnit builds the unit a manifest row names.
func pyKeyedRowsUnit(t *testing.T, repo string, units map[string][]string, unit string) *ir.Unit {
	t.Helper()
	paths, ok := units[unit]
	if !ok {
		t.Fatalf("the manifest declares no unit %s", unit)
	}
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, p))
	}
	return loadUnitPaths(t, full...)
}

// pyKeyedRowsEngine reads wire at root through the Go engine as the unit
// declares the root, and answers the verdict, the counters and, when it
// reads, the canonical bytes in the declared form and in form 1.
func pyKeyedRowsEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (accepted bool, unknown, widened int, canonical, variable []byte) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	inst := m.New(st)
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil {
		return false, 0, 0, nil, nil
	}
	canonical, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: the engine does not write what it read: %v", root, err)
	}
	variable, err = tablewire.EncodeVariable(m, inst)
	if err != nil {
		t.Fatalf("%s: the engine does not write what it read on form 1: %v", root, err)
	}
	return true, report.Unknown, report.Widened, canonical, variable
}

// pyKeyedRowsProbe reads every case the file "cases" names ("<case> <root>")
// from the file of the case's name and answers one line per case.
const pyKeyedRowsProbe = `import importlib
pkg = importlib.import_module(open("package").read())
for line in open("cases").read().splitlines():
    name, root = line.split()
    cls = getattr(pkg, root)
    with open(name, "rb") as handle:
        original = handle.read()
    buffer = bytearray(original)
    report = pkg.LoadReport()
    report.widened = 77
    report.unknown = 66
    try:
        value = cls.load(buffer, report)
    except pkg.Refused as err:
        assert buffer == original, name + ": the refusal changed the caller's buffer"
        assert (report.widened, report.unknown) == (77, 66), name + ": the refusal changed the caller's report"
        print("case", name, "refused")
        continue
    assert buffer == original, name + ": the read changed the caller's buffer"
    saved = value.save()
    again = cls.load(saved)
    print("case", name, "read", saved.hex(), report.unknown, report.widened, value.measure() == len(saved), again == value)
`

// pyKeyedRowsJob is one generated package and the cases run against it.
type pyKeyedRowsJob struct {
	label string // the package's label in a finding
	u     *ir.Unit
	cases map[string]pyKeyedRowsCase
}

type pyKeyedRowsCase struct {
	root, origin string // origin is the manifest row or construct, for a finding
	wire         []byte
	want         string // the probe's expected line after "case <name> "
}

// pyKeyedRowsJobs builds every package the row runs and the expected answer of
// each case, holding the engine itself to reports.txt and to the instances.
func pyKeyedRowsJobs(t *testing.T) []pyKeyedRowsJob {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := pyKeyedRowsManifest(t, repo)
	declared := map[string]*pyKeyedRowsJob{}
	variable := map[string]*pyKeyedRowsJob{}
	var order []*pyKeyedRowsJob
	job := func(jobs map[string]*pyKeyedRowsJob, key, label string, build func() *ir.Unit) *pyKeyedRowsJob {
		j, ok := jobs[key]
		if !ok {
			j = &pyKeyedRowsJob{label: label, u: build(), cases: map[string]pyKeyedRowsCase{}}
			jobs[key] = j
			order = append(order, j)
		}
		return j
	}
	readLine := func(canonical []byte, unknown, widened int) string {
		return fmt.Sprintf("read %x %d %d True True", canonical, unknown, widened)
	}
	for _, name := range pyKeyedRowCases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s is not a manifest instance or report row", name)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: read wire: %v", name, err)
		}
		d := job(declared, row.unit, row.unit, func() *ir.Unit { return pyKeyedRowsUnit(t, repo, units, row.unit) })
		accepted, unknown, widened, canonical, form1 := pyKeyedRowsEngine(t, d.u, row.root, wire)
		origin := row.kind + " " + row.unit + "/" + row.root
		want := "refused"
		switch row.kind {
		case "instance":
			switch {
			case row.wireRefused && accepted:
				t.Fatalf("%s: the manifest marks the wire refused and the Go engine reads it", name)
			case !row.wireRefused && !accepted:
				t.Fatalf("%s: the Go engine refuses an instance the manifest does not mark refused", name)
			case accepted:
				if string(canonical) != string(wire) {
					t.Fatalf("%s: the instance wire is not the engine's canonical bytes", name)
				}
				want = readLine(wire, unknown, widened)
			}
		case "report":
			verdict, ok := expect[name]
			if !ok {
				t.Fatalf("%s: reports.txt names no such case", name)
			}
			got := "refused"
			if accepted {
				got = "read"
				want = readLine(canonical, unknown, widened)
			}
			if got != verdict {
				t.Fatalf("%s: the Go engine answers %s, reports.txt %s", name, got, verdict)
			}
		}
		d.cases[name] = pyKeyedRowsCase{root: row.root, origin: origin, wire: wire, want: want}
		// THE VARIABLE FORM of an accepted instance at a fixed root: the same
		// value on form 1, the root declared variable
		if row.kind == "instance" && accepted && d.u.Tables[row.root].FixedDeclared {
			v := job(variable, row.unit+"/"+row.root, row.unit+" ("+row.root+" variable)", func() *ir.Unit {
				u := pyKeyedRowsUnit(t, repo, units, row.unit)
				u.Tables[row.root].FixedDeclared = false
				return u
			})
			vu, vk, vw, vc, _ := pyKeyedRowsEngine(t, v.u, row.root, form1)
			if !vu || string(vc) != string(form1) {
				t.Fatalf("%s: the Go engine does not read back its own form-1 bytes at the variable root", name)
			}
			v.cases[name+"_variable"] = pyKeyedRowsCase{root: row.root, origin: "variable " + row.unit + "/" + row.root, wire: form1, want: readLine(form1, vk, vw)}
		}
	}
	for _, c := range pyKeyedRowConstructs {
		u := loadUnit(t, c.schema)
		wire := pyKeyedEngineWire(t, u, c.root, c.json)
		j := &pyKeyedRowsJob{label: "construct " + c.name, u: u, cases: map[string]pyKeyedRowsCase{
			c.name: {root: c.root, origin: "construct", wire: wire, want: readLine(wire, 0, 0)},
		}}
		order = append(order, j)
	}
	out := make([]pyKeyedRowsJob, 0, len(order))
	for _, j := range order {
		out = append(out, *j)
	}
	return out
}

// pyKeyedRowsFailures runs every job under both pinned interpreters, its
// emitted files edited by mutate when set, and answers the findings, each
// opening with its case name, and the number of cases held.
func pyKeyedRowsFailures(t *testing.T, jobs []pyKeyedRowsJob, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	var fails []string
	held := 0
	for _, j := range jobs {
		files, err := Generate(j.u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refused the unit: %v", j.label, err))
			continue
		}
		if mutate != nil {
			files = maps.Clone(files)
			mutate(t, files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		names := slices.Sorted(maps.Keys(j.cases))
		inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + pyKeyedRowsProbe), "package": []byte(j.u.Package)}
		var lines []string
		for _, name := range names {
			inputs[name] = j.cases[name].wire
			lines = append(lines, name+" "+j.cases[name].root)
		}
		inputs["cases"] = []byte(strings.Join(lines, "\n"))
		held += len(names)
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
			if err != nil {
				fails = append(fails, fmt.Sprintf("%s (%s): %v: %s", j.label, filepath.Base(python), err, strings.TrimSpace(stderr)))
				continue
			}
			got := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
				if rest, ok := strings.CutPrefix(line, "case "); ok {
					name, answer, _ := strings.Cut(rest, " ")
					got[name] = answer
				}
			}
			for _, name := range names {
				c := j.cases[name]
				if got[name] != c.want {
					fails = append(fails, fmt.Sprintf("%s (%s, %s): the read-then-write is %q, want %q", name, c.origin, filepath.Base(python), got[name], c.want))
				}
			}
		}
	}
	sort.Strings(fails)
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtKeyedRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")

	// THE COMPLETENESS CHECK. Every manifest instance or report row whose
	// root reaches a keyed array is named in pyKeyedRowCases, so a corpus
	// addition that carries the construct goes red here rather than unheld.
	repo := filepath.Join("..", "..", "..")
	units, rows, _ := pyKeyedRowsManifest(t, repo)
	cache := map[string]*ir.Unit{}
	named := map[string]bool{}
	for _, name := range pyKeyedRowCases {
		named[name] = true
	}
	reaching := 0
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		row := rows[name]
		u, ok := cache[row.unit]
		if !ok {
			u = pyKeyedRowsUnit(t, repo, units, row.unit)
			cache[row.unit] = u
		}
		if !pyKeyedReaches(u.Tables[row.root], map[*ir.Struct]bool{}) {
			continue
		}
		reaching++
		if !named[name] {
			t.Errorf("%s (%s %s/%s) reaches a keyed array but is not held by this row", name, row.kind, row.unit, row.root)
		}
	}
	if reaching == 0 {
		t.Fatal("the manifest names no row whose root reaches a keyed array")
	}

	jobs := pyKeyedRowsJobs(t)
	fails, held := pyKeyedRowsFailures(t, jobs, nil)
	for _, f := range fails {
		t.Error(f)
	}
	want := 0
	for _, j := range jobs {
		want += len(j.cases)
	}
	if held != want || held == 0 {
		t.Fatalf("the row held %d cases, want %d", held, want)
	}
	t.Logf("%d cases held over %d packages; %d manifest rows reach a keyed array", held, len(jobs), reaching)
}

// TestPythonVtKeyedRowsNegativeControl plants, one at a time, a keyed fault in
// a COPY of the emitted runtime and requires the rows it reaches to go red by
// name: the variable form's keyed writer emits the present slots in
// DESCENDING variant order, and the fixed form's reader places a keyed
// array's slots in reverse. Each names the rows with two or more distinct
// present slots on its form: a row whose keyed arrays hold one present slot,
// or mirror-equal slots, reads the same either way.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtKeyedRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	const fixedRead = `        values = [_decode(elem, data, at + index * elem["size"]) for index in range(n)]`
	for _, ctl := range []struct {
		name    string
		mutate  func(*testing.T, map[string][]byte)
		planted []string
	}{
		{"variable-slots-descending", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/kind_keyed.py", "        for slot in range(len(fd.keys)):", "        for slot in reversed(range(len(fd.keys))):")
		}, []string{"keyed_config_variable", "scalars_full_variable", "map_slots", "unions_maps_keyed"}},
		{"fixed-slots-reversed", func(t *testing.T, files map[string][]byte) {
			vtWidenEdit(t, files, "_schema/fixedform.py", fixedRead, fixedRead+"\n        if shape == \"keyed\":\n            values.reverse()")
		}, []string{"keyed_config", "scalars_full"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails, _ := pyKeyedRowsFailures(t, pyKeyedRowsJobs(t), ctl.mutate)
			for _, planted := range ctl.planted {
				red := false
				for _, f := range fails {
					if strings.HasPrefix(f, planted+" (") {
						red = true
					}
				}
				if !red {
					t.Errorf("NEGATIVE CONTROL FAILED: %s: %s stayed green (findings: %s)", ctl.name, planted, strings.Join(fails, "\n"))
				}
			}
			t.Logf("%s: %d findings", ctl.name, len(fails))
		})
	}
}
