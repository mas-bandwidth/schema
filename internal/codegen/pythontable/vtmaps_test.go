package pythontable

// THE MAP CELL on the Python leg (ROADMAP.md, Variable tables, maps;
// docs/SPEC-TABLES.md §2.8): Go-engine constructs that use a `map[K]V` alone
// and with the kinds the table frame carries, read through the strict load and
// saved back byte for byte. The writer holds the ascending key order and the
// reader refuses a key that is not ascending, a repeated key included.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type pythonVTMapConstruct struct {
	name   string
	schema string
	root   string
	json   string
	extra  string
}

var pythonVTMapConstructs = []pythonVTMapConstruct{
	{
		name: "plain",
		schema: `package probe

table Root
{
    values map[int32]int32
}
`,
		root: "Root",
		json: `{"values":{"-3":-30,"2":20,"0":0}}`,
		extra: `r = Root()
r.values = {2: 20, -3: -30, 0: 0}
print("sorted", str(r.save() == data).lower())
d = {1: True}
r = Root()
r.values = d
try:
    r.save()
    print("invalid wrote")
except InvalidValue:
    print("invalid refused", r.values is d, d == {1: True})
d = {True: 1}
r = Root()
r.values = d
try:
    r.save()
    print("invalidkey wrote")
except InvalidValue:
    print("invalidkey refused", r.values is d, d == {True: 1})
`,
	},
	{
		name: "table-value",
		schema: `package probe

table Item
{
    count int32
    live  bool
}

table Root
{
    items map[uint32]Item
    tail  int64
}
`,
		root: "Root",
		json: `{"items":{"9":{"count":9,"live":true},"2":{"count":2}},"tail":5}`,
	},
	{
		name: "pointer-value",
		schema: `package probe

table Node
{
    value int32
    next  *Node
}

table Root
{
    heads map[uint16]*Node
}
`,
		root: "Root",
		json: `{"heads":{"1":{"value":1,"next":{"value":2}},"3":{"value":3}}}`,
	},
	{
		name: "nested-table",
		schema: `package probe

table Inner
{
    by_id map[uint8]int16
    label int32
}

table Outer
{
    inner Inner
    live  bool
}
`,
		root: "Outer",
		json: `{"inner":{"by_id":{"1":-1,"200":200},"label":7},"live":true}`,
	},
	{
		name: "map-of-maps",
		schema: `package probe

table Item
{
    n int32
}

table Root
{
    grid map[uint8]map[uint8]Item
}
`,
		root: "Root",
		json: `{"grid":{"2":{"1":{"n":1},"3":{"n":3}},"1":{"2":{"n":2}}}}`,
	},
	{
		name: "bits-value",
		schema: `package probe

table Root
{
    flags map[uint8]bits(12)
}
`,
		root: "Root",
		json: `{"flags":{"2":4095,"5":0}}`,
	},
}

// pythonVTMapEngineWire is the Go engine's form-1 wire of the JSON text at
// root, the canonical wire every construct is held to.
func pythonVTMapEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
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

// pythonVTMapDuplicateWire writes a map whose entries hold one key twice,
// through the engine's own encoder: the reader must refuse it, because no
// conforming writer emits a repeated key.
func pythonVTMapDuplicateWire(t *testing.T, u *ir.Unit, root string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	at := -1
	for i, f := range st.Fields {
		if f.IsMap() {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("table %s declares no map", root)
	}
	f := st.Fields[at]
	entries := make([]tabletext.Cell, 2)
	for i := range entries {
		entry := m.NewMapEntry(f)
		entry.Fields[0].Cell.I = 2
		entry.Fields[1].Cell.I = int64(20 + i)
		entries[i] = tabletext.Cell{Tab: entry}
	}
	inst.Fields[at].Entries = entries
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write the duplicate: %v", err)
	}
	return wire
}

func pythonVTMapProbe(root, extra string) string {
	return pyrun.ImportPrelude + fmt.Sprintf(`from probe import %[1]s, InvalidValue, Refused

with open("wire", "rb") as handle:
    data = handle.read()
try:
    value = %[1]s.load(data)
except Refused as err:
    print("refused", err.reason)
else:
    saved = value.save()
    print("read", len(data), str(saved == data).lower(), str(value.measure() == len(saved)).lower())
%[2]s
print("OK")
`, root, extra)
}

// pythonVTMapRun runs one probe over wire under both pinned interpreters and
// returns their outputs, or a failure description per interpreter.
func pythonVTMapRun(t *testing.T, u *ir.Unit, root string, wire []byte, extra string, mutate func(map[string][]byte) error) (map[string]string, map[string]string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("set %s: Generate: %v", root, err)
	}
	if mutate != nil {
		if err := mutate(files); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pythonVTMapProbe(root, extra)
	out := map[string]string{}
	fail := map[string]string{}
	for name, pin := range map[string]string{
		"python-3.15.2":  pyrun.Interpreter(t),
		"python-3.11.18": pyrun.Floor(t),
	} {
		stdout, stderr, runErr := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{
			"main.py": []byte(probe),
			"wire":    wire,
		})
		out[name] = stdout
		if runErr != nil {
			fail[name] = fmt.Sprintf("the probe failed: %v\n%s", runErr, stderr)
		}
	}
	return out, fail
}

func pythonVTMapCellFailures(t *testing.T, c pythonVTMapConstruct, wire, duplicate []byte, mutate func(map[string][]byte) error) []string {
	t.Helper()
	u := loadUnit(t, c.schema)
	var fails []string
	got, runFail := pythonVTMapRun(t, u, c.root, wire, c.extra, mutate)
	for name, why := range runFail {
		fails = append(fails, fmt.Sprintf("%s: %s", name, why))
	}
	var current, floor string
	for name, out := range got {
		switch name {
		case "python-3.15.2":
			current = out
		case "python-3.11.18":
			floor = out
		}
		if runFail[name] != "" {
			continue
		}
		switch {
		case !strings.Contains(out, fmt.Sprintf("read %d true true\n", len(wire))):
			fails = append(fails, fmt.Sprintf("%s: the read-then-write is not the canonical wire:\n%s", name, out))
		case !strings.Contains(out, "OK\n"):
			fails = append(fails, fmt.Sprintf("%s: the probe did not finish:\n%s", name, out))
		}
		for _, line := range []string{"sorted true", "invalid refused True True", "invalidkey refused True True"} {
			if strings.Contains(c.extra, strings.SplitN(line, " ", 2)[0]) && !strings.Contains(out, line+"\n") {
				fails = append(fails, fmt.Sprintf("%s: %q is absent:\n%s", name, line, out))
			}
		}
	}
	if current != "" && floor != "" && current != floor {
		fails = append(fails, "the pinned interpreters disagree")
	}
	if duplicate == nil {
		return fails
	}
	dup, dupFail := pythonVTMapRun(t, u, c.root, duplicate, c.extra, mutate)
	for name, why := range dupFail {
		fails = append(fails, fmt.Sprintf("%s duplicate: %s", name, why))
	}
	for name, out := range dup {
		if !strings.Contains(out, "refused wire_damaged\n") {
			fails = append(fails, fmt.Sprintf("%s: the duplicate key was not refused:\n%s", name, out))
		}
	}
	return fails
}

// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtMaps(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it runs the emitted table runtime)")
	for _, c := range pythonVTMapConstructs {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, c.schema)
			wire := pythonVTMapEngineWire(t, u, c.root, c.json)
			var duplicate []byte
			if c.name == "plain" {
				duplicate = pythonVTMapDuplicateWire(t, u, c.root)
			}
			for _, f := range pythonVTMapCellFailures(t, c, wire, duplicate, nil) {
				t.Error(f)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
				t.Errorf("mypy --strict over the generated package:\n%s", out)
			}
			if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
				t.Errorf("mypy --strict over the runtime:\n%s", out)
			}
		})
	}
}

// TestPythonVtMapsNegativeControl plants, one at a time, the writer's sort and
// the reader's ascending/duplicate check in a COPY of the map runtime and
// requires the cell to go red under each.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtMapsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it runs the emitted table runtime)")
	plant := func(from, to string) func(map[string][]byte) error {
		return func(files map[string][]byte) error {
			const path = "probe/_schema/kind_map.py"
			src := string(files[path])
			if strings.Count(src, from) != 1 {
				return fmt.Errorf("%s carries %q %d times, want 1: the control would be vacuous", path, from, strings.Count(src, from))
			}
			files[path] = []byte(strings.Replace(src, from, to, 1))
			return nil
		}
	}
	c := pythonVTMapConstructs[0]
	u := loadUnit(t, c.schema)
	wire := pythonVTMapEngineWire(t, u, c.root, c.json)
	duplicate := pythonVTMapDuplicateWire(t, u, c.root)
	for _, ctl := range []struct {
		name   string
		mutate func(map[string][]byte) error
	}{
		{"save-sort", plant("return sorted(mapping, key=lambda key: _comparable(key_field, key))", "return list(mapping)")},
		{"read-duplicate", plant("if landed and _compare(fd, last, key) >= 0:", "if landed and _compare(fd, last, key) > 0:")},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails := pythonVTMapCellFailures(t, c, wire, duplicate, ctl.mutate)
			if len(fails) == 0 {
				t.Fatalf("%s: the map cell stayed green", ctl.name)
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), fails[0])
		})
	}
}
