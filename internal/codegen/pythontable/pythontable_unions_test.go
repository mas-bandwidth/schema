package pythontable

// TABLE-SIDE UNIONS on the Python leg (docs/SPEC-TABLES.md §2.6, §3): a union
// in a field, an array element, a nested by-value table and a pointer target,
// with arms that are tables, scalars, a pointer, a nested union and no
// payload at all. Every verdict is the Go engine's (internal/tablewire),
// asked in the test itself: each wire below is read by tablewire.Decode and by
// the pinned interpreters, and the two must agree on the refusal's reason or
// on the value, judged by writing it back and comparing the bytes with
// tablewire.Encode's.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
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

const unionsSchema = `package unions

table Leaf
{
    value int32 = 5
    flag  bool
}

table Other
{
    label int32
}

table Plain
{
    value int32
    flag  bool
}

union Pick
{
    leaf   Leaf
    other  Other
    number int32
    toggle bool
    next   *Leaf
    ping
}

union Inner
{
    pick  Pick
    count uint16
}

table Holder
{
    pick Pick
    tail int32
}

table Root
{
    pick   Pick
    inner  Inner
    trail  [2]Pick
    many   [..3]Pick
    least  [1..2]Pick
    chain  Pick
    plain  Plain
    holder Holder
    ptr    *Holder
}
`

const unionsText = `{
  "pick": {"leaf": {"value": 7, "flag": true}},
  "inner": {"pick": {"number": -5}},
  "trail": [{"leaf": {"value": 1, "flag": false}}, {"ping": null}],
  "many": [{"other": {"label": 9}}, {"number": 0}],
  "least": [{"toggle": true}],
  "chain": {"next": {"value": 42, "flag": false}},
  "plain": {"value": 11, "flag": true},
  "holder": {"pick": {"number": 3}, "tail": 4},
  "ptr": {"pick": {"next": {"value": 8}}, "tail": 6}
}`

// unionTrailElidedText is the value the probe's inactive-arm write must land
// on: trail at its default, so the engine leaves the field out.
const unionTrailElidedText = `{
  "least": [{"toggle": true}]
}`

// unionEngineWire is the Go engine's canonical wire of text.
func unionEngineWire(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("the text did not place cleanly: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the engine refused the value: %v", err)
	}
	return wire
}

// unionEngineVerdict is the line the probe must print for wire: the engine's
// refusal reason, or the unknown count and the bytes the engine writes back
// for the value it read ("invalid" where its writer refuses that value).
func unionEngineVerdict(t *testing.T, u *ir.Unit, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &report)
	if !ok {
		reason := tablewire.RefusalReason(err)
		if reason == "" {
			t.Fatalf("the engine's refusal names no reason: %v", err)
		}
		return "refused " + reason
	}
	back, err := tablewire.Encode(m, inst)
	if err != nil {
		return fmt.Sprintf("read %d invalid", report.Unknown)
	}
	return fmt.Sprintf("read %d %s", report.Unknown, hex.EncodeToString(back))
}

// unionWire builds form-1 files by hand, interning each name's wire id in the
// id table in first-use order.
type unionWire struct{ ids []uint64 }

func unionLeb(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func unionCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func unionI32(v int32) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }

func (w *unionWire) ref(name string) []byte {
	id := ir.TableWireId(name)
	for i, v := range w.ids {
		if v == id {
			return unionLeb(uint64(i + 1))
		}
	}
	w.ids = append(w.ids, id)
	return unionLeb(uint64(len(w.ids)))
}

// arm is one arm header: the arm id reference, the kind, L and the payload.
func (w *unionWire) arm(name string, kind byte, payload []byte) []byte {
	return unionCat(w.ref(name), []byte{kind}, unionLeb(uint64(len(payload))), payload)
}

// union is a union field: its reference, kind 15, then the union payload.
func (w *unionWire) union(field string, payload []byte) []byte {
	return unionCat(w.ref(field), []byte{15}, payload)
}

// array is an array field: element kind, count and the element payloads.
func (w *unionWire) array(field string, elem byte, count uint64, elems ...[]byte) []byte {
	body := unionCat([]byte{elem}, unionLeb(count), unionCat(elems...))
	return unionCat(w.ref(field), []byte{14}, unionLeb(uint64(len(body))), body)
}

// table is a by-value table field over a body of fields.
func (w *unionWire) table(field string, fields ...[]byte) []byte {
	body := append(unionCat(fields...), 0)
	return unionCat(w.ref(field), []byte{13}, unionLeb(uint64(len(body))), body)
}

// least is the [1..2]Pick field at a value its writer accepts, so a case's
// write-back is compared byte for byte instead of ending in the writer's
// refusal of an empty counted array below its minimum.
func (w *unionWire) least() []byte {
	return w.array("least", 15, 1, w.arm("toggle", 1, []byte{1}))
}

func (w *unionWire) file(fields ...[]byte) []byte {
	out := append([]byte{1}, unionCat(fields...)...)
	out = append(out, 0)
	for _, id := range w.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(w.ids)))
}

type unionCase struct {
	name string
	// engine is the verdict the Go engine was measured to give, as "read" or
	// "refused <reason>": the test holds it so a case that drifts from what it
	// was written to show is caught, and the probe is held to the engine's
	// own answer beside it.
	engine string
	build  func(w *unionWire) []byte
}

var unionCases = []unionCase{
	{"arm-zero-field", "read", func(w *unionWire) []byte {
		// A union FIELD whose arm reference is 0: the engine reads None.
		return w.file(w.union("pick", unionLeb(0)), w.least())
	}},
	{"zero-scalar-arm", "read", func(w *unionWire) []byte {
		// A selected scalar arm at zero still rides.
		return w.file(w.union("pick", w.arm("number", 4, unionI32(0))), w.least())
	}},
	{"table-arm-default", "read", func(w *unionWire) []byte {
		// A table arm whose body is its terminator: Leaf.value reads 5.
		return w.file(w.union("pick", w.arm("leaf", 13, []byte{0})), w.least())
	}},
	{"nested-none", "read", func(w *unionWire) []byte {
		// A nested union arm holding None: L = 1 and one zero byte.
		return w.file(w.union("inner", w.arm("pick", 15, []byte{0})), w.least())
	}},
	{"nested-trailing", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("inner", w.arm("pick", 15, []byte{0, 0})), w.least())
	}},
	{"unknown-arm", "read", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ghost", 4, unionI32(1))), w.least())
	}},
	{"unknown-arm-element", "read", func(w *unionWire) []byte {
		return w.file(w.array("many", 15, 2, w.arm("ghost", 4, unionI32(1)), w.arm("number", 4, unionI32(2))), w.least())
	}},
	{"unknown-arm-nested-table", "read", func(w *unionWire) []byte {
		return w.file(w.table("holder", w.union("pick", w.arm("ghost", 1, []byte{1}))), w.least())
	}},
	{"unknown-arm-kind32-nonzero-l", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ghost", 32, []byte{0})), w.least())
	}},
	{"unknown-arm-kind99", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ghost", 99, nil)), w.least())
	}},
	{"unknown-arm-bad-bool", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ghost", 1, []byte{2})), w.least())
	}},
	{"unknown-arm-bad-body", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ghost", 13, []byte{0, 0})), w.least())
	}},
	{"unknown-field-kind32-nonzero-l", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(unionCat(w.ref("ghost_field"), []byte{32}, unionLeb(1), []byte{0}), w.least())
	}},
	{"unknown-field-bad-union", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("ghost_field", w.arm("ghost", 99, nil)), w.least())
	}},
	{"arm-kind-mismatch", "refused kind_mismatch", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("number", 12, []byte("ab"))), w.least())
	}},
	{"arm-kind-mismatch-then-damage", "refused wire_damaged", func(w *unionWire) []byte {
		// The engine judges the whole file's framing before its decode
		// compares a kind: damage in a later field outranks the mismatch.
		return w.file(w.union("pick", w.arm("number", 12, []byte("ab"))), w.least(),
			unionCat(w.ref("ghost_field"), []byte{99}))
	}},
	{"arm-short-width", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("number", 4, []byte{1, 2, 3})), w.least())
	}},
	{"arm-long-width", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("number", 4, []byte{1, 2, 3, 4, 5})), w.least())
	}},
	{"arm-past-body", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", unionCat(w.ref("number"), []byte{4}, unionLeb(40), unionI32(1))), w.least())
	}},
	{"void-arm-nonzero-l", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("ping", 32, []byte{0})), w.least())
	}},
	{"bool-arm-two", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("toggle", 1, []byte{2})), w.least())
	}},
	{"table-arm-trailing", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.union("pick", w.arm("other", 13, []byte{0, 0})), w.least())
	}},
	{"count-over-bound", "refused count_over_bound", func(w *unionWire) []byte {
		zero := unionLeb(0)
		return w.file(w.array("many", 15, 4, zero, zero, zero, zero), w.least())
	}},
	{"count-over-extent-cap", "refused count_over_extent_cap", func(w *unionWire) []byte {
		return w.file(w.array("many", 15, 1<<31), w.least())
	}},
	{"element-kind-mismatch", "refused kind_mismatch", func(w *unionWire) []byte {
		return w.file(w.array("many", 4, 0), w.least())
	}},
	{"element-kind-closed", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.array("many", 99, 0), w.least())
	}},
	{"element-truncated", "refused wire_damaged", func(w *unionWire) []byte {
		return w.file(w.array("many", 15, 2, unionLeb(0)), w.least())
	}},
	{"below-minimum", "read", func(w *unionWire) []byte {
		// Read as it stands; the engine's writer then refuses it.
		return w.file(w.array("least", 15, 0))
	}},
	{"absent-minimum", "read", func(w *unionWire) []byte {
		// An absent counted field reads empty, as the engine builds it.
		return w.file(w.union("pick", w.arm("number", 4, unionI32(1))))
	}},
	{"counted-one-none", "read", func(w *unionWire) []byte {
		// The engine elides a counted array only at count 0: one None rides.
		return w.file(w.array("least", 15, 1, unionLeb(0)))
	}},
	{"fixed-all-none", "read", func(w *unionWire) []byte {
		// A fixed array whose elements are all None: the engine elides it.
		return w.file(w.array("trail", 15, 2, unionLeb(0), unionLeb(0)), w.least())
	}},
	{"fixed-short", "read", func(w *unionWire) []byte {
		return w.file(w.array("trail", 15, 1, w.arm("ping", 32, nil)), w.least())
	}},
	{"nested-table-union", "read", func(w *unionWire) []byte {
		return w.file(w.table("holder", w.union("pick", w.arm("number", 4, unionI32(3)))), w.least())
	}},
}

// unionsProbe prints one line per check, and a check that raises prints its
// error on its own line instead of stopping the probe, so a planted fault
// takes out the lines of the check it breaks and leaves the others standing.
const unionsProbe = `from unions import Holder, Inner, InvalidValue, Leaf, Other, Pick, Plain, Refused, Root
from unions._schema import LoadReport

def arm(cls, name):
    for i, a in enumerate(cls.descriptor.arms, 1):
        if a.name == name:
            return i
    raise SystemExit("union_arm_missing")

def check(label, fn):
    try:
        print(label, *fn())
    except Exception as err:
        print(label, "error", type(err).__name__, err)

def canonical():
    r = Root()
    leaf = Leaf()
    leaf.value = 7
    leaf.flag = True
    r.pick.type = arm(Pick, "leaf")
    r.pick.leaf = leaf
    r.inner.type = arm(Inner, "pick")
    r.inner.pick.type = arm(Pick, "number")
    r.inner.pick.number = -5
    r.trail[0].type = arm(Pick, "leaf")
    r.trail[0].leaf.value = 1
    r.trail[0].leaf.flag = False
    r.trail[1].type = arm(Pick, "ping")
    m0 = Pick()
    m0.type = arm(Pick, "other")
    m0.other.label = 9
    m1 = Pick()
    m1.type = arm(Pick, "number")
    m1.number = 0
    r.many = [m0, m1]
    l0 = Pick()
    l0.type = arm(Pick, "toggle")
    l0.toggle = True
    r.least = [l0]
    r.chain.type = arm(Pick, "next")
    r.chain.next = Leaf()
    r.chain.next.value = 42
    r.plain.value = 11
    r.plain.flag = True
    r.holder.pick.type = arm(Pick, "number")
    r.holder.pick.number = 3
    r.holder.tail = 4
    r.ptr = Holder()
    r.ptr.pick.type = arm(Pick, "next")
    r.ptr.pick.next = Leaf()
    r.ptr.pick.next.value = 8
    r.ptr.tail = 6
    return r

def read(name):
    with open(name, "rb") as handle:
        return handle.read()

want = read("canonical.bin")
check("union_canonical_bytes", lambda: [canonical().save() == want])
check("union_load_value", lambda: [Root.load(want) == canonical()])
check("union_read_back", lambda: [Root.load(want).save() == want])
check("union_measure", lambda: [canonical().measure() == len(want)])
check("union_pointer_arm", lambda: [Root.load(want).chain.next.value, Root.load(want).ptr.pick.next.value])

def case(name):
    wire = read("case_" + name + ".bin")
    report = LoadReport()
    try:
        value = Root.load(wire, report)
    except Refused as err:
        return ["refused", err.reason]
    try:
        out = value.save().hex()
    except InvalidValue:
        out = "invalid"
    return ["read", report.unknown, out]

for name in read("cases.txt").decode().split():
    check("case " + name, lambda: case(name))

def arm_default():
    v = Root.load(read("case_table-arm-default.bin"))
    return [v.pick.type == arm(Pick, "leaf"), v.pick.leaf.value, v.pick.leaf.flag]

def nested_none():
    v = Root.load(read("case_nested-none.bin"))
    return [v.inner.type == arm(Inner, "pick"), v.inner.pick.type]

check("union_arm_default", arm_default)
check("union_nested_none", nested_none)
check("union_arm_zero", lambda: [Root.load(read("case_arm-zero-field.bin")).pick.type])

def refusal(value):
    try:
        value.save()
    except InvalidValue as err:
        return err.path + " | " + err.rule
    return "wrote"

def write_type():
    r = canonical()
    r.pick.type = arm(Pick, "number")
    r.pick.number = "x"
    return [refusal(r), "|", r.pick.number == "x"]

def write_range():
    r = canonical()
    r.pick.type = arm(Pick, "number")
    r.pick.number = 1 << 31
    return [refusal(r), "|", r.pick.number == 1 << 31]

def write_tag():
    r = canonical()
    r.pick.type = len(Pick.descriptor.arms) + 1
    return [refusal(r), "|", r.pick.type == len(Pick.descriptor.arms) + 1]

def write_element():
    r = canonical()
    r.many[1].number = -(1 << 31) - 1
    return [refusal(r)]

def write_nested():
    r = canonical()
    r.holder.pick.type = arm(Pick, "toggle")
    r.holder.pick.toggle = 1
    return [refusal(r)]

def write_minimum():
    r = canonical()
    r.least = []
    return [refusal(r)]

check("union_write_type", write_type)
check("union_write_range", write_range)
check("union_write_tag", write_tag)
check("union_write_element", write_element)
check("union_write_nested", write_nested)
check("union_write_minimum", write_minimum)

# Elision is judged per element by its selected arm, never by ==: a fixed
# array of None elements whose inactive arm members are set is left out, and
# a user __eq__ cannot change the bytes.
def inactive():
    e = Root()
    e.trail[0].leaf.value = 3
    e.trail[1].number = 9
    l0 = Pick()
    l0.type = arm(Pick, "toggle")
    l0.toggle = True
    e.least = [l0]
    return e

elided = read("trail_elided.bin")
check("union_elide_inactive", lambda: [inactive().save() == elided])
for label, eq in (("true", lambda self, other: True), ("false", lambda self, other: False)):
    Pick.__eq__ = eq
    check("union_elide_eq_" + label, lambda: [inactive().save() == elided, canonical().save() == want])
print("OK")
`

func unionTree(t *testing.T, u *ir.Unit, mutate func(map[string][]byte) error) (string, []string) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		if err := mutate(files); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	put := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("canonical.bin", unionEngineWire(t, u, unionsText))
	put("trail_elided.bin", unionEngineWire(t, u, unionTrailElidedText))
	var names []string
	for _, c := range unionCases {
		var w unionWire
		put("case_"+c.name+".bin", c.build(&w))
		names = append(names, c.name)
	}
	put("cases.txt", []byte(strings.Join(names, "\n")+"\n"))
	return dir, names
}

// unionWant is every line the probe must print, the engine's verdict for
// each case among them.
func unionWant(t *testing.T, u *ir.Unit) []string {
	t.Helper()
	want := []string{
		"union_canonical_bytes True",
		"union_load_value True",
		"union_read_back True",
		"union_measure True",
		"union_pointer_arm 42 8",
		"union_arm_default True 5 False",
		"union_nested_none True 0",
		"union_arm_zero 0",
		"union_write_type Root.pick.number | not an int | True",
		"union_write_range Root.pick.number | 2147483648 is outside [-2147483648, 2147483647] | True",
		"union_write_tag Root.pick | type 7 names no arm | True",
		"union_write_element Root.many[1].number | -2147483649 is outside [-2147483648, 2147483647]",
		"union_write_nested Root.holder.pick.toggle | not a bool",
		"union_write_minimum Root.least | a counted array holds 0, outside [1, 2]",
		"union_elide_inactive True",
		"union_elide_eq_true True True",
		"union_elide_eq_false True True",
		"OK",
	}
	for _, c := range unionCases {
		var w unionWire
		verdict := unionEngineVerdict(t, u, c.build(&w))
		if !strings.HasPrefix(verdict, c.engine) {
			t.Errorf("case %s: the engine now answers %q, written for %q", c.name, verdict, c.engine)
		}
		want = append(want, "case "+c.name+" "+verdict)
	}
	return want
}

// unionFailures runs the probe under both pins and names every missing line.
func unionFailures(t *testing.T, u *ir.Unit, mutate func(map[string][]byte) error) []string {
	t.Helper()
	dir, _ := unionTree(t, u, mutate)
	want := unionWant(t, u)
	probe := pyrun.ImportPrelude + unionsProbe
	var fails []string
	outs := map[string]string{}
	for name, pin := range map[string]string{
		"python-3.15.2":  pyrun.Interpreter(t),
		"python-3.11.18": pyrun.Floor(t),
	} {
		stdout, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		outs[name] = stdout
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the probe failed: %v\n%s%s", name, err, stdout, stderr))
		}
		for _, line := range want {
			if !strings.Contains(stdout, line+"\n") {
				fails = append(fails, fmt.Sprintf("%s: %q is absent", name, line))
			}
		}
	}
	if outs["python-3.15.2"] != outs["python-3.11.18"] {
		fails = append(fails, "the pinned interpreters disagree")
	}
	return fails
}

// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtUnions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it runs the emitted table runtime)")
	u := loadUnit(t, unionsSchema)
	for _, f := range unionFailures(t, u, nil) {
		t.Error(f)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "unions"); err != nil {
		t.Errorf("mypy --strict over the generated union package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "unions/_schema"); err != nil {
		t.Errorf("mypy --strict over the runtime:\n%s", out)
	}
}

// unionControls plants one fault per union check in a COPY of the generated
// runtime; each must turn the probe red on the line named.
var unionControls = []struct {
	name string
	path string
	from string
	to   string
	// red is a line the planted fault must take out of the probe's output.
	red string
}{
	{"canonical-arm-length", "kind_union.py", "w.varint(len(payload))", "w.varint(len(payload) + 1)", "union_canonical_bytes True"},
	{"arm-zero-read", "kind_union.py", "    if reference == 0:\n", "    if reference == 0:\n        raise Refused(\"wire_damaged\", \"arm 0\")\n", "case arm-zero-field "},
	{"unknown-arm-check", "kind_union.py", "        check_content(payload, kind, ctx.ids)\n", "", "case unknown-arm-kind99 "},
	{"unknown-field-check", "load.py", "        check_content(reader.take(reader.varint()), kind, reader.ids, depth)\n        return\n    raise", "        reader.take(reader.varint())\n        return\n    raise", "case unknown-field-kind32-nonzero-l "},
	{"damage-outranks-mismatch", "load.py", "            check_file(view, ids)\n", "            pass\n", "case arm-kind-mismatch-then-damage "},
	{"arm-kind-check", "kind_union.py", "    if kind != arm.kind:\n", "    if False:\n", "case arm-kind-mismatch "},
	{"void-arm-length", "kind_union.py", "        if length != 0:\n", "        if False:\n", "case void-arm-nonzero-l "},
	{"arm-trailing", "kind_union.py", "    if sub.pos != sub.end:\n        raise Refused(\"wire_damaged\", \"bytes left inside an arm's L after its payload\")", "    if False:\n        raise Refused(\"wire_damaged\", \"bytes left inside an arm's L after its payload\")", "case arm-long-width "},
	{"count-bound-reason", "kind_union.py", "raise Refused(\"count_over_bound\"", "raise Refused(\"out_of_range\"", "case count-over-bound "},
	{"count-extent-cap", "kind_union.py", "        if count > _EXTENT_CAP:\n", "        if False:\n", "case count-over-extent-cap "},
	{"counted-elision", "descriptor.py", "                return len(value) == 0\n", "                return len(value) <= 1 and all(element.type == 0 for element in value)\n", "case counted-one-none "},
	{"fixed-elision", "descriptor.py", "            return all(element.type == 0 for element in value)\n", "            return bool(value == self.default)\n", "union_elide_inactive True"},
	{"write-arm-path", "kind_union.py", "f\"{path}.{arm.name}\"", "path", "union_write_type "},
	{"write-minimum", "kind_union.py", "    elif len(value) < f.array_min or len(value) > f.bound:", "    elif len(value) > f.bound:", "union_write_minimum "},
	{"write-tag-check", "kind_union.py", "    if type(tag) is not int or tag < 0 or tag > len(un.descriptor.arms):", "    if type(tag) is not int or tag < 0:", "union_write_tag "},
}

// TestPythonVtUnionsNegativeControl plants each of unionControls in turn and
// requires the probe to lose the line that names the planted check.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtUnionsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters (it runs the emitted table runtime)")
	u := loadUnit(t, unionsSchema)
	want := unionWant(t, u)
	seen := map[string]bool{}
	for _, ctl := range unionControls {
		if seen[ctl.name] {
			t.Fatalf("control %s is listed twice", ctl.name)
		}
		seen[ctl.name] = true
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			path := "unions/_schema/" + ctl.path
			mutate := func(files map[string][]byte) error {
				src := string(files[path])
				if strings.Count(src, ctl.from) != 1 {
					return fmt.Errorf("%s carries %q %d times, want 1: the control would be vacuous", path, ctl.from, strings.Count(src, ctl.from))
				}
				files[path] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
				return nil
			}
			var named []string
			for _, line := range want {
				if strings.HasPrefix(line, ctl.red) {
					named = append(named, line)
				}
			}
			if len(named) == 0 {
				t.Fatalf("no expected line starts %q: the control names nothing", ctl.red)
			}
			// Red by name: a line the control names is gone, and the probe
			// still ran to its end, so the fault broke that check and not the
			// probe.
			fails := unionFailures(t, u, mutate)
			red := false
			for _, f := range fails {
				if strings.Contains(f, "\"OK\" is absent") || strings.Contains(f, "the probe failed") {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s stopped the probe instead of one check: %s", ctl.name, f)
				}
				for _, line := range named {
					if strings.Contains(f, fmt.Sprintf("%q is absent", line)) {
						red = true
					}
				}
			}
			if !red {
				sort.Strings(fails)
				t.Fatalf("NEGATIVE CONTROL FAILED: %s left %q green; findings: %v", ctl.name, ctl.red, fails)
			}
		})
	}
}
