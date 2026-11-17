package pythontable

// THE MAP CELL on the Python leg (ROADMAP.md, Variable tables, maps;
// docs/SPEC-TABLES.md §2.8): Go-engine constructs that use a `map[K]V` alone
// and with the kinds the table frame carries, read through the strict load and
// saved back byte for byte. The writer holds the ascending key order, numbers
// the nodes a map's pointer values reach in that order, and refuses a key or a
// value outside its type; the reader refuses a key that is not ascending, a
// repeated key included.

import (
	"bytes"
	"encoding/binary"
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
	// wireLen, when set, is the length the Go engine's wire must have.
	wireLen int
	// damaged names wires (pythonVTMapDamagedWire) that the reader must refuse
	// as wire_damaged.
	damaged []string
	// extra runs after the read-then-write; every line of want must appear in
	// its output.
	extra string
	want  []string
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
		root:    "Root",
		json:    `{"values":{"-3":-30,"2":20,"0":0}}`,
		damaged: []string{"duplicate", "descending"},
		extra: `r = Root()
r.values = {2: 20, -3: -30, 0: 0}
print("sorted", str(r.save() == data).lower())
outcomes = []
for d in ({1: True}, {True: 1}, {1 << 31: 1}, {1: 1 << 31}, {-(1 << 31) - 1: 1}, {1: -(1 << 31) - 1}):
    snapshot = dict(d)
    r = Root()
    r.values = d
    try:
        r.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if r.values is d and d == snapshot else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{
			"sorted true",
			"range refused refused refused refused refused refused",
		},
	},
	{
		name: "empty",
		schema: `package probe

table Root
{
    values map[int32]int32
}
`,
		root:    "Root",
		json:    `{"values":{}}`,
		wireLen: 10,
		extra: `r = Root()
r.values = {}
print("emptysave", str(r.save() == data).lower())
print("emptyread", Root.load(data).values == {})
`,
		want: []string{"emptysave true", "emptyread True"},
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
		json: `{"items":{"9":{"count":9,"live":true},"2":{"count":2}},"tail":"5"}`,
		extra: `from probe import Item
a = Item()
a.count = 9
a.live = True
b = Item()
b.count = 2
r = Root()
r.items = {9: a, 2: b}
r.tail = 5
print("unordered", str(r.save() == data).lower())
outcomes = []
for k in (1 << 32, -1):
    d = {k: Item()}
    r = Root()
    r.items = d
    try:
        r.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if r.items is d and len(d) == 1 else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{"unordered true", "range refused refused"},
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
		// Built in descending key order, so a writer that numbered the nodes in
		// the dict's own order would put node 3 first and write other bytes.
		extra: `from probe import Node
n1 = Node()
n1.value = 1
n2 = Node()
n2.value = 2
n1.next = n2
n3 = Node()
n3.value = 3
r = Root()
r.heads = {3: n3, 1: n1}
print("unordered", str(r.save() == data).lower())
outcomes = []
for k in (1 << 16, -1):
    d = {k: Node()}
    r = Root()
    r.heads = d
    try:
        r.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if r.heads is d and len(d) == 1 else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{"unordered true", "range refused refused"},
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
		extra: `from probe import Inner
outcomes = []
for k, v in ((256, 1), (-1, 1), (1, 1 << 15), (1, -(1 << 15) - 1)):
    d = {k: v}
    i = Inner()
    i.by_id = d
    o = Outer()
    o.inner = i
    try:
        o.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if i.by_id is d and d == {k: v} else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{"range refused refused refused refused"},
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
		extra: `from probe import Item
def item(n: int) -> Item:
    out = Item()
    out.n = n
    return out
r = Root()
r.grid = {2: {3: item(3), 1: item(1)}, 1: {2: item(2)}}
print("unordered", str(r.save() == data).lower())
outcomes = []
for d in ({256: {}}, {1: {256: item(1)}}, {1: {-1: item(1)}}):
    r = Root()
    r.grid = d
    try:
        r.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if r.grid is d else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{"unordered true", "range refused refused refused"},
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
		extra: `outcomes = []
for k, v in ((1, 4096), (1, -1), (256, 1)):
    d = {k: v}
    r = Root()
    r.flags = d
    try:
        r.save()
        outcomes.append("wrote")
    except InvalidValue:
        outcomes.append("refused" if r.flags is d and d == {k: v} else "touched")
print("range", " ".join(outcomes))
`,
		want: []string{"range refused refused refused"},
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

// pythonVTMapDamagedWire is a wire of the construct's first map that no
// conforming writer emits. "duplicate" holds one key twice, through the
// engine's own encoder (its stable sort keeps both entries). "descending"
// holds two keys in descending order: the encoder always writes ascending, so
// the wire is the engine's for two distinctive int32 keys with the keys' four
// bytes swapped in place, which keeps every length and frame intact.
func pythonVTMapDamagedWire(t *testing.T, u *ir.Unit, root, label string) []byte {
	t.Helper()
	switch label {
	case "duplicate":
		return pythonVTMapKeyedWire(t, u, root, []int64{2, 2})
	case "descending":
		const low, high = 0x3c3c0102, 0x3c3c0203
		wire := pythonVTMapKeyedWire(t, u, root, []int64{low, high})
		a := binary.LittleEndian.AppendUint32(nil, low)
		b := binary.LittleEndian.AppendUint32(nil, high)
		if bytes.Count(wire, a) != 1 || bytes.Count(wire, b) != 1 {
			t.Fatalf("the keys' bytes are not unique in the engine's wire %x", wire)
		}
		i, j := bytes.Index(wire, a), bytes.Index(wire, b)
		if i >= j {
			t.Fatalf("the engine's wire is not ascending: %x", wire)
		}
		copy(wire[i:], b)
		copy(wire[j:], a)
		return wire
	}
	t.Fatalf("no damaged wire %q", label)
	return nil
}

// pythonVTMapKeyedWire writes a map whose entries carry the keys given,
// through the engine's own encoder, which writes them in ascending order.
func pythonVTMapKeyedWire(t *testing.T, u *ir.Unit, root string, keys []int64) []byte {
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
	entries := make([]tabletext.Cell, len(keys))
	for i, key := range keys {
		entry := m.NewMapEntry(f)
		// a signed cell holds its value in both slots, as the engine's reader
		// leaves it
		entry.Fields[0].Cell.I = key
		entry.Fields[0].Cell.U = uint64(key)
		entry.Fields[1].Cell.I = int64(20 + i)
		entry.Fields[1].Cell.U = uint64(20 + i)
		entries[i] = tabletext.Cell{Tab: entry}
	}
	inst.Fields[at].Entries = entries
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write the keys %v: %v", keys, err)
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

func pythonVTMapCellFailures(t *testing.T, c pythonVTMapConstruct, mutate func(map[string][]byte) error) []string {
	t.Helper()
	u := loadUnit(t, c.schema)
	wire := pythonVTMapEngineWire(t, u, c.root, c.json)
	var fails []string
	if c.wireLen != 0 && len(wire) != c.wireLen {
		fails = append(fails, fmt.Sprintf("the Go engine's wire is %d bytes, want %d", len(wire), c.wireLen))
	}
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
		for _, line := range c.want {
			if !strings.Contains(out, line+"\n") {
				fails = append(fails, fmt.Sprintf("%s: %q is absent:\n%s", name, line, out))
			}
		}
	}
	if current != "" && floor != "" && current != floor {
		fails = append(fails, "the pinned interpreters disagree")
	}
	for _, label := range c.damaged {
		bad, badFail := pythonVTMapRun(t, u, c.root, pythonVTMapDamagedWire(t, u, c.root, label), "", mutate)
		for name, why := range badFail {
			fails = append(fails, fmt.Sprintf("%s %s: %s", name, label, why))
		}
		for name, out := range bad {
			if !strings.Contains(out, "refused wire_damaged\n") {
				fails = append(fails, fmt.Sprintf("%s: the %s keys were not refused:\n%s", name, label, out))
			}
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
			for _, f := range pythonVTMapCellFailures(t, c, nil) {
				t.Error(f)
			}
			files, err := Generate(loadUnit(t, c.schema))
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

// TestPythonVtMapsNegativeControl plants, one at a time, a fault in a COPY of
// the map runtime — the writer's sort, the reader's duplicate check, the
// reader's descending check, and the order the save numbers a map's nodes in —
// and requires the named construct's cell to go red under each.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtMapsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it runs the emitted table runtime)")
	plant := func(path, from, to string) func(map[string][]byte) error {
		return func(files map[string][]byte) error {
			src := string(files[path])
			if strings.Count(src, from) != 1 {
				return fmt.Errorf("%s carries %q %d times, want 1: the control would be vacuous", path, from, strings.Count(src, from))
			}
			files[path] = []byte(strings.Replace(src, from, to, 1))
			return nil
		}
	}
	const kindMap = "probe/_schema/kind_map.py"
	const save = "probe/_schema/save.py"
	construct := func(name string) pythonVTMapConstruct {
		for _, c := range pythonVTMapConstructs {
			if c.name == name {
				return c
			}
		}
		t.Fatalf("no construct %s", name)
		return pythonVTMapConstruct{}
	}
	for _, ctl := range []struct {
		name      string
		construct string
		mutate    func(map[string][]byte) error
	}{
		{"save-sort", "plain", plant(kindMap, "return sorted(mapping, key=lambda key: _comparable(key_field, key))", "return list(mapping)")},
		{"read-duplicate", "plain", plant(kindMap, "if landed and _compare(fd, last, key) >= 0:", "if landed and _compare(fd, last, key) > 0:")},
		{"read-descending", "plain", plant(kindMap, "if landed and _compare(fd, last, key) >= 0:", "if landed and _compare(fd, last, key) == 0:")},
		{"collect-order", "pointer-value", plant(save, "for key in kind_map._sorted_keys(field, value):", "for key in value:")},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			fails := pythonVTMapCellFailures(t, construct(ctl.construct), ctl.mutate)
			if len(fails) == 0 {
				t.Fatalf("%s: the map cell stayed green", ctl.name)
			}
			t.Logf("%s: %d findings, first %s", ctl.name, len(fails), fails[0])
		})
	}
}
