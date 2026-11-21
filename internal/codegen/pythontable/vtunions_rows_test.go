package pythontable

// THE UNION ROWS on the Python leg (ROADMAP.md, Variable tables:
// port/python/vt/union-table-arms, port/python/vt/union-arrays and
// port/python/vt/union-arms-any; docs/SPEC-TABLES.md §2.6, §3, §3.4, §4). One
// subtest per roadmap row, over every conformance-manifest instance and report
// row that exercises it and the Go-engine construct beside them: the same cut
// as internal/codegen/luatable/vtcells_test.go (TestLuaVariableTableCells).
//
// THE ONE READ. Each row's wire is read by the Go engine (internal/tablewire)
// and by the generated Python package under both pinned interpreters, at the
// root as the unit declares it: a `fixed table` root reads form 3 alone and
// refuses a form-1 file as previous_form, a variable root reads form 1. The
// read either accepts and writes back the canonical wire byte for byte (an
// instance row's own file, a report row's engine re-encoding, a construct's
// engine wire), with its measure equal to the length, or refuses with no
// value; either way the caller's buffer and LoadReport are left untouched.
// The engine's verdict is itself held to the manifest (an instance row reads
// and re-encodes to its file; a report row's verdict is reports.txt's).
//
// TestPythonVtUnionsRowsNegativeControl plants union faults in a COPY of the
// emitted runtime, on the variable form (kind_union.py) and on the fixed form
// (fixedform.py), on the save and on the read, and requires each control's
// rows to go red by name; every row is named by at least one control.

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

type vtUnionRow struct {
	row        string
	cases      []string
	constructs []string
}

// vtUnionRows are luatable's union rows (luaVTCells), case for case.
var vtUnionRows = []vtUnionRow{
	{"union-table-arms", []string{"m1_open", "m1_save", "m2_close", "m2_open", "m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
		"message_arms", "message_arm_edits"}, []string{"unions_maps_keyed"}},
	{"union-arrays", []string{"message_batch", "message_arm_edits", "batch_elem_kind", "batch_unknown_arm"}, []string{"unions_maps_keyed"}},
	{"union-arms-any", []string{"stream_arm_first", "stream_arm_pointer", "stream_arm_tag", "a2_arm_enum_as_int", "a2_arm_pointer_as_int",
		"a2_arm_bytes_as_string", "a2_arm_length_off_width", "a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2", "a1_arm_as_string_as_a2",
		"a1_arm_as_body_as_a2", "a1_arm_body_as_a2", "a1_arm_unknown_as_a2", "nested_arm_trailing", "escape_arm"}, []string{"unions_maps_keyed"}},
}

// vtUnionConstructs is luatable's unions_maps_keyed construct: a table arm, a
// scalar arm, a text arm and a payload-free arm, in a field and in an array.
var vtUnionConstructs = map[string]struct{ schema, root, json string }{
	"unions_maps_keyed": {"package probe\nenum Slot { head, body, feet }\ntable Item\n{\n    w uint16\n}\nunion Pick\n{\n    item Item\n    count uint32\n    label string(8)\n    empty\n}\ntable Kit\n{\n    gear [Slot]Item\n    pick Pick\n    picks []Pick\n    byid map[uint32]Item\n    flags_ok bool\n}\n",
		"Kit", `{"gear":{"head":{"w":1},"feet":{"w":3}},"pick":{"label":"x"},"picks":[{"count":4},{"empty":null},{"item":{"w":2}}],"byid":{"9":{"w":9},"2":{"w":2}},"flags_ok":true}`},
}

type vtUnionManifestRow struct {
	kind, unit, root, wire string
	refused                bool
}

// vtUnionManifest reads the manifest's units, its instance and report rows,
// and reports.txt's verdicts.
func vtUnionManifest(t *testing.T, repo string) (map[string][]string, map[string]vtUnionManifestRow, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	rows := map[string]vtUnionManifestRow{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) >= 3 && f[0] == "unit":
			units[f[1]] = f[2:]
		case len(f) >= 5 && (f[0] == "instance" || f[0] == "report"):
			r := vtUnionManifestRow{kind: f[0], unit: f[2], root: f[3], wire: f[4]}
			for _, x := range f[5:] {
				if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
					r.refused = true
				}
			}
			rows[f[1]] = r
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

// vtUnionEngine is the Go engine's read of wire at root as the unit declares
// it, and its canonical re-encoding when it reads.
func vtUnionEngine(t *testing.T, u *ir.Unit, root string, wire []byte) (bool, []byte) {
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

// vtUnionProbe reads every case named in cases.txt at its root, through the
// root's public load (form 3 at a fixed root, form 1 at a variable one), and
// prints one verdict line per case.
const vtUnionProbe = `import importlib
from %[1]s import LoadReport, Refused
package = importlib.import_module("%[1]s")
with open("cases.txt") as listing:
    cases = [line.split() for line in listing if line.strip()]
for name, root in cases:
    with open(name + ".bin", "rb") as handle:
        original = handle.read()
    buffer = bytearray(original)
    report = LoadReport()
    report.widened = 77
    report.unknown = 66
    try:
        value = getattr(package, root).load(buffer, report)
    except Refused:
        touched = buffer != original or (report.widened, report.unknown) != (77, 66)
        print(name, "refused-but-touched" if touched else "refused")
        continue
    if buffer != original:
        print(name, "read-but-touched")
        continue
    saved = value.save()
    print(name, "read", saved.hex(), value.measure() == len(saved))
print("OK")
`

type vtUnionCase struct{ name, root, want string }

// vtUnionRun generates u (its emitted files edited by mutate when set), runs
// the probe over every case under both pins and names every line that is not
// the one owed.
func vtUnionRun(t *testing.T, u *ir.Unit, label string, cases []vtUnionCase, wires map[string][]byte, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		return []string{fmt.Sprintf("%s: the backend refuses the unit: %v", label, err)}
	}
	if mutate != nil {
		files = maps.Clone(files)
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + fmt.Sprintf(vtUnionProbe, u.Package))}
	var listing strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&listing, "%s %s\n", c.name, c.root)
		inputs[c.name+".bin"] = wires[c.name]
	}
	inputs["cases.txt"] = []byte(listing.String())
	var fails []string
	outs := map[string]string{}
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		pin := filepath.Base(python)
		stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
		outs[pin] = stdout
		if err != nil || !strings.HasSuffix(stdout, "OK\n") {
			fails = append(fails, fmt.Sprintf("%s (%s): the probe did not run to its end: %v\n%s", label, pin, err, strings.TrimSpace(stderr)))
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(stdout, "\n") {
			if name, rest, ok := strings.Cut(line, " "); ok {
				got[name] = rest
			}
		}
		for _, c := range cases {
			if got[c.name] != c.want {
				fails = append(fails, fmt.Sprintf("%s (%s/%s, %s): the read-then-write is %q, the engine says %q", c.name, label, c.root, pin, got[c.name], c.want))
			}
		}
	}
	if len(outs) == 2 && len(slices.Compact(slices.Sorted(maps.Values(outs)))) != 1 {
		fails = append(fails, label+": the pinned interpreters disagree")
	}
	return fails
}

// vtUnionFailures holds one roadmap row: every manifest case it names, grouped
// by unit so each unit is generated once, then its constructs. It returns the
// named failures and how many cases and constructs it held.
func vtUnionFailures(t *testing.T, r vtUnionRow, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	units, rows, expect := vtUnionManifest(t, repo)
	var fails []string
	held := 0
	byUnit := map[string][]vtUnionCase{}
	wires := map[string][]byte{}
	declared := map[string]*ir.Unit{}
	for _, name := range r.cases {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", r.row, name)
		}
		u, ok := declared[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = loadUnitPaths(t, paths...)
			declared[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		wires[name] = wire
		accepted, canonical := vtUnionEngine(t, u, row.root, wire)
		switch {
		case row.kind == "report":
			want, ok := expect[name]
			if !ok {
				t.Fatalf("report %s: reports.txt names no such case", name)
			}
			if got := map[bool]string{true: "read", false: "refused"}[accepted]; got != want {
				t.Fatalf("report %s: the engine's verdict is %s, reports.txt says %s", name, got, want)
			}
		case row.refused:
			if accepted {
				t.Fatalf("instance %s: the manifest says wire-refused and the engine reads it", name)
			}
		default:
			if !accepted || string(canonical) != string(wire) {
				t.Fatalf("instance %s: the engine does not read it back to its own file", name)
			}
		}
		want := "refused"
		if accepted {
			want = fmt.Sprintf("read %x True", canonical)
		}
		if mutate == nil {
			t.Logf("%s %s (%s/%s): the engine %s", row.kind, name, row.unit, row.root, strings.Fields(want)[0])
		}
		byUnit[row.unit] = append(byUnit[row.unit], vtUnionCase{name, row.root, want})
	}
	for _, unit := range slices.Sorted(maps.Keys(byUnit)) {
		fails = append(fails, vtUnionRun(t, declared[unit], unit, byUnit[unit], wires, mutate)...)
		held += len(byUnit[unit])
	}
	for _, name := range r.constructs {
		cc, ok := vtUnionConstructs[name]
		if !ok {
			t.Fatalf("%s names construct %s, which vtUnionConstructs does not carry", r.row, name)
		}
		u := loadUnit(t, cc.schema)
		accepted, wire := vtUnionEngine(t, u, cc.root, tabletextWire(t, u, cc.root, cc.json))
		if !accepted {
			t.Fatalf("construct %s: the engine does not read its own wire", name)
		}
		fails = append(fails, vtUnionRun(t, u, "construct "+name, []vtUnionCase{{name, cc.root, fmt.Sprintf("read %x True", wire)}},
			map[string][]byte{name: wire}, mutate)...)
		held++
	}
	return fails, held
}

// tabletextWire is the Go engine's wire of the JSON text at root.
func tabletextWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
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

// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtUnionsRows(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (it runs the emitted table runtime)")
	for _, r := range vtUnionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			fails, held := vtUnionFailures(t, r, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if want := len(r.cases) + len(r.constructs); held != want {
				t.Fatalf("the row held %d cases, want %d", held, want)
			}
			t.Logf("%s: %d manifest rows and %d constructs held", r.row, len(r.cases), len(r.constructs))
		})
	}
}

// vtUnionPlant replaces the one occurrence of from with to in the emitted file
// whose path ends with suffix; an absent or repeated anchor fails, so no
// control is vacuous.
func vtUnionPlant(suffix, from, to string) func(*testing.T, map[string][]byte) {
	return func(t *testing.T, files map[string][]byte) {
		for name, src := range files {
			if !strings.HasSuffix(name, suffix) {
				continue
			}
			if got := strings.Count(string(src), from); got != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", name, from, got)
			}
			files[name] = []byte(strings.Replace(string(src), from, to, 1))
			return
		}
		t.Fatalf("no emitted file ends with %s", suffix)
	}
}

// vtUnionControls are the planted union faults, each with the cases it must
// turn red by name, keyed by roadmap row.
var vtUnionControls = []struct {
	name   string
	mutate func(*testing.T, map[string][]byte)
	red    map[string][]string
}{
	// the variable save writes every arm's L one byte long: no saved arm is
	// the canonical wire's
	{"variable-arm-length", vtUnionPlant("_schema/kind_union.py", "w.varint(len(payload))", "w.varint(len(payload) + 1)"), map[string][]string{
		"union-table-arms": {"unions_maps_keyed"},
		"union-arrays":     {"unions_maps_keyed"},
		"union-arms-any":   {"stream_arm_pointer", "stream_arm_tag", "unions_maps_keyed"},
	}},
	// the variable save frames a length-kind arm (a table, text) a second
	// time instead of writing its field line in place as the arm header
	{"variable-length-arm-in-place", vtUnionPlant("_schema/kind_union.py", "    if f.kind in _LENGTH_KINDS:\n", "    if False:\n"), map[string][]string{
		"union-table-arms": {"unions_maps_keyed"},
		"union-arrays":     {"unions_maps_keyed"},
		"union-arms-any":   {"stream_arm_first", "unions_maps_keyed"},
	}},
	// the variable read stops checking that an arm's payload fills its L: the
	// nested arm with trailing bytes reads
	{"variable-arm-trailing", vtUnionPlant("_schema/kind_union.py",
		"    if sub.pos != sub.end:\n        raise Refused(\"wire_damaged\", \"bytes left inside an arm's L after its payload\")",
		"    if False:\n        raise Refused(\"wire_damaged\", \"bytes left inside an arm's L after its payload\")"), map[string][]string{
		"union-arms-any": {"nested_arm_trailing"},
	}},
	// the fixed read drops every arm: each active arm reads as no arm, so the
	// write-back loses it
	{"fixed-arm-dropped", vtUnionPlant("_schema/fixedform.py", "        value.type = _integer(data, at, spec[\"tag\"])\n", "        value.type = 0\n"), map[string][]string{
		"union-table-arms": {"m1_open", "m1_save", "m2_close", "m2_open", "message_arms", "message_arm_edits"},
		"union-arrays":     {"message_batch", "message_arm_edits"},
	}},
	// the fixed read's form refusal (a form-1 file at a fixed root, every
	// report row here) writes into the caller's report before it refuses
	{"fixed-refusal-touches-report", vtUnionPlant("_schema/fixedform.py", "    if source[0] != 3:\n", "    if source[0] != 3:\n        if report is not None:\n            report.unknown = 0\n"), map[string][]string{
		"union-table-arms": {"m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1"},
		"union-arrays":     {"batch_elem_kind", "batch_unknown_arm"},
		"union-arms-any": {"a2_arm_enum_as_int", "a2_arm_pointer_as_int", "a2_arm_bytes_as_string", "a2_arm_length_off_width",
			"a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2", "a1_arm_as_string_as_a2", "a1_arm_as_body_as_a2", "a1_arm_body_as_a2",
			"a1_arm_unknown_as_a2", "escape_arm"},
	}},
}

// TestPythonVtUnionsRowsControlsCoverEveryCase requires every case and
// construct of every row to be named by some control, so no row is held only
// by a gate that cannot go red.
func TestPythonVtUnionsRowsControlsCoverEveryCase(t *testing.T) {
	t.Parallel()
	for _, r := range vtUnionRows {
		named := map[string]bool{}
		for _, ctl := range vtUnionControls {
			for _, name := range ctl.red[r.row] {
				named[name] = true
			}
		}
		for _, name := range slices.Concat(r.cases, r.constructs) {
			if !named[name] {
				t.Errorf("%s: no control names %s", r.row, name)
			}
		}
	}
}

// TestPythonVtUnionsRowsNegativeControl plants each of vtUnionControls in a
// COPY of the emitted runtime and requires every case it names to go red,
// with every row's cases covered by some control.
//
// Functional (both pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtUnionsRowsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters (it runs the emitted table runtime)")
	for _, ctl := range vtUnionControls {
		for _, r := range vtUnionRows {
			named := ctl.red[r.row]
			if len(named) == 0 {
				continue
			}
			t.Run(ctl.name+"/"+r.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := vtUnionFailures(t, r, ctl.mutate)
				joined := strings.Join(fails, "\n")
				for _, name := range named {
					if !strings.Contains(joined, name+" (") {
						t.Errorf("NEGATIVE CONTROL FAILED: %s left %s green on %s; findings:\n%s", ctl.name, name, r.row, joined)
					}
				}
				if len(fails) == 0 {
					t.Fatal("fails is empty: the loop below would check nothing")
				}
				for _, f := range fails {
					if strings.Contains(f, "did not run to its end") {
						t.Errorf("NEGATIVE CONTROL FAILED: %s stopped the probe instead of one read: %s", ctl.name, f)
					}
				}
			})
		}
	}
}
