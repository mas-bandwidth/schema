package pythontable

// THE VARIABLE-CLASS AND POINTER-ARRAYS ROWS on the Python leg (ROADMAP.md,
// Variable tables; docs/SPEC-TABLES.md §2.1, §2.9, §3, §3.1). The kind code is
// kind_pointer.py and kind_pointer_array.py on the flat node table the table
// core lays down; TestPythonVtPointerArrays holds the kind on its own
// constructs. This file holds each roadmap row over the shared conformance
// corpus: every manifest instance and report row that exercises the row (the
// corpus luatable's vtcells_test.go names for it), and the Go-engine
// construct beside them.
//
// THE ONE READ. Each row's wire is read by the generated Root.load under both
// pinned interpreters and held to the Go engine's own verdict on the same
// wire (internal/tablewire). Accepted, the value saves back byte-identical to
// the engine's canonical wire with measure equal to its length, a second save
// of the same value gives the same bytes, and the LoadReport counters are the
// engine's; refused, the load returns no value and leaves the caller's buffer
// and LoadReport as they were. An instance row's wire is itself canonical, so
// the save is the corpus file; a report row's verdict is also reports.txt's.
//
// THE COMPLETENESS CHECK. Every manifest instance or report row whose root
// table reaches a pointer array ([]*T, [N]*T, [..N]*T, [Min..N]*T) must be
// held by the pointer-arrays row, so a corpus addition that uses the
// construct goes red here rather than being skipped.
//
// TestPythonVtPointerArraysRowsNegativeControl plants, one at a time, a fault
// in a COPY of the emitted runtime (shared nodes renumbered on save, a bounded
// pointer array's count check dropped on read, the index-past-the-node-table
// check dropped from the graph judge, a pointer array's indices written one
// past their node) and requires each planted row red by name.

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

type ptrRowsCell struct {
	row        string
	cases      []string
	constructs []string
}

// ptrRowsCells is the two rows and their corpus, luatable's (vtcells_test.go).
var ptrRowsCells = []ptrRowsCell{
	{"variable-class", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep", "guard_false_no_edge",
		"guard_true_both_edges", "list_shared", "list_before_pointer"}, []string{"blobs_pointers"}},
	{"pointer-arrays", []string{"stream_parts", "stream_chain", "parts_elem_kind", "parts_count_past_bound",
		"parts_index_out_of_range", "w1_fleet", "w2_fleet",
		// every other manifest row whose root reaches a pointer array (the
		// completeness check below names them)
		"stream_header", "stream_arm_first", "stream_arm_pointer", "stream_arm_tag",
		"w1_fleet_default", "w1_fleet_as_w2", "w1_fleet_default_as_w2", "w2_fleet_as_w1",
		"list_tables", "list_scalars", "list_empty", "list_erased", "list_scalar_widen", "list_shared", "list_before_pointer",
		"widenbound_feed"},
		[]string{"blobs_pointers"}},
}

type ptrRowsConstruct struct{ name, schema, root, json string }

// ptrRowsConstructs is the Go-engine construct both rows name: a node whose
// pointers share the flat node table with two blob records, a pointer chain
// and a pointer array holding a null.
var ptrRowsConstructs = []ptrRowsConstruct{
	{"blobs_pointers", "package probe\ntable Node\n{\n    name *string\n    data *bytes\n    next *Node\n    kids []*Node\n}\n",
		"Node", `{"name":"root","data":"AQID","next":{"name":"n1"},"kids":[{"name":"k1"},null,{"name":"k2"}]}`},
}

type ptrRowsRow struct {
	kind, unit, root, wire string
}

// ptrRowsManifest reads the tables manifest's units, its instance and report
// rows, and the reports.txt verdicts.
func ptrRowsManifest(t *testing.T, repo string) (map[string][]string, map[string]ptrRowsRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]ptrRowsRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			units[f[1]] = f[2:]
		case (f[0] == "instance" || f[0] == "report") && len(f) >= 5:
			rows[f[1]] = ptrRowsRow{kind: f[0], unit: f[2], root: f[3], wire: f[4]}
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

func ptrRowsUnit(t *testing.T, repo string, units map[string][]string, unit string) *ir.Unit {
	t.Helper()
	var paths []string
	for _, p := range units[unit] {
		paths = append(paths, filepath.Join(repo, p))
	}
	return loadUnitPaths(t, paths...)
}

// ptrRowsReachesPointerArray reports whether the table root reaches a pointer
// array through its fields, nested tables and union arms.
func ptrRowsReachesPointerArray(root *ir.Struct) bool {
	seen := map[*ir.Struct]bool{}
	stack := []*ir.Struct{root}
	for len(stack) > 0 {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if st == nil || seen[st] {
			continue
		}
		seen[st] = true
		fields := slices.Clone(st.Fields)
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			if codecOf(f) == "pointer_array" {
				return true
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				stack = append(stack, ref)
			case *ir.Union:
				for _, v := range ref.Variants {
					if v.F != nil {
						fields = append(fields, v.F)
					}
				}
			}
		}
	}
	return false
}

// ptrRowsEngine is the Go engine's verdict on wire at root: whether it reads,
// its widened and unknown counters, and its canonical form-1 bytes.
func ptrRowsEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (ok bool, widened, unknown int, canonical []byte) {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	m := tabletext.NewModel(u)
	value := m.New(st)
	var report tabletext.Report
	if accepted, err := tablewire.Decode(m, value, wire, &report); !accepted || err != nil {
		return false, 0, 0, nil
	}
	canonical, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatalf("%s: the engine does not write what it read: %v", root, err)
	}
	return true, report.Widened, report.Unknown, canonical
}

// ptrRowsEngineWire is the Go engine's form-1 wire of the construct's JSON.
func ptrRowsEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

// ptrRowsProbe reads every case named in the file "cases" (one "<name>
// <root>" per line, the wire in "<name>.bin") through the one read and prints
// one verdict line per case.
const ptrRowsProbe = `import importlib
from %s import LoadReport, Refused
module = importlib.import_module(%q)
with open("cases") as handle:
    cases = [line.split() for line in handle.read().splitlines() if line]
for name, root in cases:
    with open(name + ".bin", "rb") as handle:
        original = handle.read()
    buffer = bytearray(original)
    report = LoadReport()
    report.widened = 77
    report.unknown = 66
    try:
        value = getattr(module, root).load(buffer, report)
    except Refused:
        if buffer != original:
            print(name, "the refusal changed the caller's buffer")
        elif (report.widened, report.unknown) != (77, 66):
            print(name, "the refusal changed the caller's report")
        else:
            print(name, "refused")
        continue
    except Exception as error:
        print(name, "the read raised", type(error).__name__)
        continue
    if buffer != original:
        print(name, "the read changed the caller's buffer")
        continue
    try:
        saved = value.save()
        again = value.save()
        measure = value.measure()
    except Exception as error:
        print(name, "the save raised", type(error).__name__)
        continue
    if again != saved:
        print(name, "a second save of the same value differs")
        continue
    print(name, "read", saved.hex(), report.widened, report.unknown, measure == len(saved))
`

type ptrRowsJob struct {
	name, root string
	wire       []byte
	want       string
}

// ptrRowsRun generates u (its emitted files edited by mutate when set), runs
// the probe over jobs under both pinned interpreters, and answers the named
// failures.
func ptrRowsRun(t *testing.T, label string, u *ir.Unit, jobs []ptrRowsJob, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		var fails []string
		for _, j := range jobs {
			fails = append(fails, fmt.Sprintf("%s (%s): the backend refused the unit: %v", j.name, label, err))
		}
		return fails
	}
	if mutate != nil {
		files = maps.Clone(files)
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + fmt.Sprintf(ptrRowsProbe, u.Package, u.Package))}
	var cases strings.Builder
	want := map[string]string{}
	for _, j := range jobs {
		fmt.Fprintf(&cases, "%s %s\n", j.name, j.root)
		inputs[j.name+".bin"] = j.wire
		want[j.name] = j.want
	}
	inputs["cases"] = []byte(cases.String())
	var fails []string
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		pin := filepath.Base(python)
		stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
			if name, rest, ok := strings.Cut(line, " "); ok {
				got[name] = rest
			}
		}
		for _, j := range jobs {
			g, ok := got[j.name]
			switch {
			case !ok:
				fails = append(fails, fmt.Sprintf("%s (%s, %s): no verdict: %v: %s", j.name, label, pin, err, strings.TrimSpace(stderr)))
			case g != want[j.name]:
				fails = append(fails, fmt.Sprintf("%s (%s, %s): the read-then-write is %q, the engine says %q", j.name, label, pin, g, want[j.name]))
			}
		}
	}
	return fails
}

// ptrRowsFailures holds every case and construct of one row, and answers the
// named failures and how many cases it held.
func ptrRowsFailures(t *testing.T, c ptrRowsCell, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := ptrRowsManifest(t, repo)
	var fails []string
	held := 0
	byUnit := map[string][]ptrRowsJob{}
	var order []string
	for _, name := range c.cases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		u := ptrRowsUnit(t, repo, units, row.unit)
		if st := u.Tables[row.root]; st == nil || st.FixedDeclared {
			t.Fatalf("%s: %s/%s is not a variable root", name, row.unit, row.root)
		}
		ok, widened, unknown, canonical := ptrRowsEngine(t, u, row.root, wire)
		switch row.kind {
		case "report":
			verdict := map[bool]string{true: "read", false: "refused"}[ok]
			if want, named := expect[name]; !named || want != verdict {
				t.Fatalf("report %s: the engine says %s, reports.txt %q", name, verdict, want)
			}
		case "instance":
			if !ok || string(canonical) != string(wire) {
				t.Fatalf("instance %s: the engine does not read it back canonical (read %v)", name, ok)
			}
		}
		want := "refused"
		if ok {
			want = fmt.Sprintf("read %x %d %d True", canonical, widened, unknown)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], ptrRowsJob{name: name, root: row.root, wire: wire, want: want})
	}
	for _, unit := range order {
		jobs := byUnit[unit]
		held += len(jobs)
		fails = append(fails, ptrRowsRun(t, unit, ptrRowsUnit(t, repo, units, unit), jobs, mutate)...)
	}
	for _, name := range c.constructs {
		i := slices.IndexFunc(ptrRowsConstructs, func(cc ptrRowsConstruct) bool { return cc.name == name })
		if i < 0 {
			t.Fatalf("%s names construct %s, which ptrRowsConstructs does not carry", c.row, name)
		}
		cc := ptrRowsConstructs[i]
		u := loadUnit(t, cc.schema)
		wire := ptrRowsEngineWire(t, u, cc.root, cc.json)
		ok, widened, unknown, canonical := ptrRowsEngine(t, u, cc.root, wire)
		if !ok || string(canonical) != string(wire) {
			t.Fatalf("construct %s: the engine does not read its own wire back canonical", name)
		}
		held++
		fails = append(fails, ptrRowsRun(t, "construct", u, []ptrRowsJob{{name: name, root: cc.root, wire: wire,
			want: fmt.Sprintf("read %x %d %d True", canonical, widened, unknown)}}, mutate)...)
	}
	return fails, held
}

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtPointerArraysRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")

	// THE COMPLETENESS CHECK: every manifest row whose root reaches a
	// pointer array is held by the pointer-arrays row.
	repo := filepath.Join("..", "..", "..")
	units, rows, _ := ptrRowsManifest(t, repo)
	named := map[string]bool{}
	for _, c := range ptrRowsCells {
		if c.row == "pointer-arrays" {
			for _, name := range c.cases {
				named[name] = true
			}
		}
	}
	cache := map[string]*ir.Unit{}
	var reaching []string
	for name, row := range rows {
		u, ok := cache[row.unit]
		if !ok {
			u = ptrRowsUnit(t, repo, units, row.unit)
			cache[row.unit] = u
		}
		if st := u.Tables[row.root]; st != nil && !st.FixedDeclared && ptrRowsReachesPointerArray(st) {
			reaching = append(reaching, name)
		}
	}
	sort.Strings(reaching)
	if len(reaching) == 0 {
		t.Fatal("the manifest names no row whose root reaches a pointer array")
	}
	for _, name := range reaching {
		if !named[name] {
			t.Errorf("%s (%s/%s) reaches a pointer array but is not held by the pointer-arrays row", name, rows[name].unit, rows[name].root)
		}
	}

	for _, c := range ptrRowsCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := ptrRowsFailures(t, c, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if want := len(c.cases) + len(c.constructs); held != want {
				t.Fatalf("the row held %d cases, want %d", held, want)
			}
			t.Logf("%s: %d corpus rows and %d constructs held under both pins", c.row, len(c.cases), len(c.constructs))
		})
	}
}

// ptrRowsEdit replaces one occurrence of from with to in the emitted file
// whose path ends with suffix, and fails if the anchor is absent or repeated.
func ptrRowsEdit(t *testing.T, files map[string][]byte, suffix, from, to string) {
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

// TestPythonVtPointerArraysRowsNegativeControl plants, one at a time, a fault
// in a COPY of the emitted runtime and requires each planted row red by name.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtPointerArraysRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, ctl := range []struct {
		name    string
		mutate  func(*testing.T, map[string][]byte)
		planted map[string][]string
	}{
		// the save numbers a node again each time it is reached, so a
		// shared node is written twice and the node table is not the
		// engine's
		{"shared-node-renumbered", func(t *testing.T, files map[string][]byte) {
			ptrRowsEdit(t, files, "_schema/save.py", "        if key in indices:\n            if key in onstack:", "        if key in onstack:\n            if key in indices:")
		}, map[string][]string{"variable-class": {"graph_shared", "list_shared"}}},
		// the read accepts a bounded pointer array's count past its bound
		{"pointer-array-bound-off", func(t *testing.T, files map[string][]byte) {
			ptrRowsEdit(t, files, "_schema/kind_pointer_array.py", `if fd.array_mode != "list" and count > fd.bound:`, `if False:`)
		}, map[string][]string{"pointer-arrays": {"parts_count_past_bound"}}},
		// the graph judge accepts a pointer index past the node table
		{"index-past-node-table-off", func(t *testing.T, files map[string][]byte) {
			ptrRowsEdit(t, files, "_schema/load.py", "            if index - 1 >= n:\n", "            if False:\n")
		}, map[string][]string{"pointer-arrays": {"parts_index_out_of_range"}}},
		// the save writes each pointer array element one past its node
		{"pointer-array-index-shifted", func(t *testing.T, files map[string][]byte) {
			ptrRowsEdit(t, files, "_schema/kind_pointer_array.py", "                _put_varint(payload, node_idx)\n", "                _put_varint(payload, node_idx + 1)\n")
		}, map[string][]string{"pointer-arrays": {"stream_parts", "blobs_pointers"}, "variable-class": {"blobs_pointers"}}},
	} {
		for _, c := range ptrRowsCells {
			planted := ctl.planted[c.row]
			if len(planted) == 0 {
				continue
			}
			t.Run(ctl.name+"/"+c.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := ptrRowsFailures(t, c, ctl.mutate)
				joined := strings.Join(fails, "\n")
				for _, name := range planted {
					if !strings.Contains(joined, name+" (") {
						t.Errorf("%s: %s stayed green (findings: %s)", ctl.name, name, joined)
					}
				}
				t.Logf("%s/%s: %d findings", ctl.name, c.row, len(fails))
			})
		}
	}
}
