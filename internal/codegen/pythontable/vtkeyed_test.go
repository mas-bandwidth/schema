package pythontable

// THE ENUM-KEYED ARRAY [E]T on the Python table leg (docs/SPEC-TABLES.md §2.4,
// §3.2; docs/PORT-PYTHON.md, THE VALUE MAPPING). Every construct here is a
// Go-engine construct: the engine reads the JSON text and writes its form-1
// wire, the strict Python read loads that wire and saves it back byte for
// byte under both pinned interpreters, and a Python value built by hand saves
// to the same bytes. Hand-built damaged wires are held to the engine's own
// verdict, and a refused read leaves the caller's report as it was.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type pyKeyedConstruct struct{ name, schema, root, json string }

const pyKeyedEnum = "enum Slot { head, body, feet }\n"

// The constructs use the keyed kind alone and with the kinds the table core
// carries: bool, the integers and fixed-point, tables by value and single
// pointers. Each names where the keyed array sits.
var pyKeyedConstructs = []pyKeyedConstruct{
	// a keyed array of every scalar element class, in a field
	{"scalars", "package probe\n" + pyKeyedEnum + `table K
{
    ints  [Slot]int32
    flags [Slot]bool
    wide  [Slot]uint64
    small [Slot]int8
    money [Slot]ufixed(8, 8) | min = 0, max = 200
    n     int32
}
`, "K", `{"ints":{"head":-5,"feet":7},"flags":{"body":true},"wide":{"head":"18446744073709551615","body":"1"},"small":{"feet":-128},"money":{"body":3.25},"n":3}`},
	// every slot present, the last variant included
	{"scalars_full", "package probe\n" + pyKeyedEnum + `table K
{
    ints [Slot]int32
}
`, "K", `{"ints":{"head":1,"body":2,"feet":3}}`},
	// no slot present: the field is not written at all
	{"scalars_default", "package probe\n" + pyKeyedEnum + `table K
{
    ints [Slot]int32
    n    int32
}
`, "K", `{"n":1}`},
	// a keyed array of tables, an all-default slot elided by the engine
	{"tables", "package probe\n" + pyKeyedEnum + `table Item
{
    w  uint16
    on bool
}

table Kit
{
    gear [Slot]Item
    tail int32
}
`, "Kit", `{"gear":{"head":{"w":1},"body":{"w":0},"feet":{"w":3,"on":true}},"tail":9}`},
	// a keyed array inside a nested table, and a keyed array of tables that
	// hold keyed arrays
	{"nested", "package probe\n" + pyKeyedEnum + `table Cell
{
    counts [Slot]uint8
}

table Grid
{
    cells [Slot]Cell
}

table Outer
{
    inner Grid
    id    int8
}
`, "Outer", `{"inner":{"cells":{"body":{"counts":{"head":1,"feet":2}},"feet":{"counts":{"body":9}}}},"id":1}`},
	// a union-free closure: pointers inside keyed table elements and beside
	// them, numbered depth-first through the slots
	{"pointers", "package probe\n" + pyKeyedEnum + `table Leaf
{
    v int32
}

table Item
{
    leaf *Leaf
    w    uint16
}

table Root
{
    gear  [Slot]Item
    first *Leaf
    next  *Root
}
`, "Root", `{"gear":{"head":{"leaf":{"v":2},"w":1},"feet":{"leaf":{"v":3}}},"first":{"v":1},"next":{"gear":{"body":{"w":4}}}}`},
}

// pyKeyedEngineWire is the Go engine's form-1 wire of the JSON text at root.
func pyKeyedEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
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

// pyKeyedEngineVerdict is the Go engine's verdict on a wire at root: "read
// <unknown>" or "refused".
func pyKeyedEngineVerdict(t *testing.T, u *ir.Unit, root string, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var rep tabletext.Report
	ok, _ := tablewire.Decode(m, inst, wire, &rep)
	if !ok {
		return "refused"
	}
	return fmt.Sprintf("read %d", rep.Unknown)
}

const pyKeyedRoundTrip = `import importlib
pkg = importlib.import_module("probe")
from probe import LoadReport, Refused
cls = getattr(pkg, open("root").read())
wire = open("wire", "rb").read()
report = LoadReport()
try:
    value = cls.load(wire, report=report)
except Refused as err:
    print("refused", err.reason)
    raise SystemExit(0)
saved = value.save()
again = cls.load(saved)
print("%d:%s:%d:%s:%d" % (len(saved), "same" if saved == wire else "diff", value.measure(), again == value, report.unknown))
`

// pyKeyedRun generates u (its files edited by mutate when set) into a fresh
// directory and runs probe there under both pinned interpreters, which must
// agree; it answers the current interpreter's stdout.
func pyKeyedRun(t *testing.T, u *ir.Unit, probe string, inputs map[string][]byte, mutate func(*testing.T, map[string][]byte)) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	in := map[string][]byte{}
	maps.Copy(in, inputs)
	in["main.py"] = []byte(pyrun.ImportPrelude + probe)
	got := string(pyrun.Run(t, dir, "main.py", in))
	floor := string(pyrun.RunFloor(t, dir, "main.py", in))
	if got != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	return got
}

// pyKeyedConstructFailures runs every construct's round trip and answers each
// construct whose save is not the engine's wire.
func pyKeyedConstructFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	var fails []string
	for _, c := range pyKeyedConstructs {
		u := loadUnit(t, c.schema)
		wire := pyKeyedEngineWire(t, u, c.root, c.json)
		got := strings.TrimSpace(pyKeyedRun(t, u, pyKeyedRoundTrip, map[string][]byte{"wire": wire, "root": []byte(c.root)}, mutate))
		if want := fmt.Sprintf("%d:same:%d:True:0", len(wire), len(wire)); got != want {
			fails = append(fails, fmt.Sprintf("construct %s: Python %s, want %s (wire %x)", c.name, got, want, wire))
		}
	}
	return fails
}

// pyKeyedBuild is a hand-built form-1 file: the root body, then the id table.
type pyKeyedBuild struct {
	ids  []uint64
	body []byte
}

func (b *pyKeyedBuild) ref(id uint64) uint64 {
	for i, v := range b.ids {
		if v == id {
			return uint64(i + 1)
		}
	}
	b.ids = append(b.ids, id)
	return uint64(len(b.ids))
}

func pyLeb(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			out = append(out, c|0x80)
			continue
		}
		return append(out, c)
	}
}

func (b *pyKeyedBuild) file() []byte {
	out := []byte{1}
	out = append(out, b.body...)
	out = append(out, 0)
	for _, id := range b.ids {
		out = append(out, le64(id)...)
	}
	return append(out, le64(uint64(len(b.ids)))...)
}

func le64(v uint64) []byte {
	out := make([]byte, 8)
	for i := range out {
		out[i] = byte(v >> (8 * i))
	}
	return out
}

type pyKeyedSlot struct {
	key     uint64 // a wire id, or 0 to write the raw reference below
	raw     uint64 // the raw key reference when key is 0
	payload []byte
}

// keyedField appends one keyed field to the body: its header, the element
// kind, the count and the triples, then extra bytes inside L.
func (b *pyKeyedBuild) keyedField(fieldID uint64, fieldKind byte, elemKind byte, count uint64, slots []pyKeyedSlot, extra []byte) {
	fref := b.ref(fieldID)
	inner := []byte{elemKind}
	inner = append(inner, pyLeb(count)...)
	for _, s := range slots {
		r := s.raw
		if s.key != 0 {
			r = b.ref(s.key)
		}
		inner = append(inner, pyLeb(r)...)
		inner = append(inner, pyLeb(uint64(len(s.payload)))...)
		inner = append(inner, s.payload...)
	}
	inner = append(inner, extra...)
	b.body = append(b.body, pyLeb(fref)...)
	b.body = append(b.body, fieldKind)
	b.body = append(b.body, pyLeb(uint64(len(inner)))...)
	b.body = append(b.body, inner...)
}

const pyKeyedDamageSchema = "package probe\n" + pyKeyedEnum + `table R
{
    vals [Slot]int16
    n    int32
}
`

type pyKeyedDamage struct {
	name   string
	build  func(field uint64, keys []uint64) []byte
	python string // the Python verdict: "read <unknown>" or "refused <reason>"
	engine string // the engine's verdict: "read <unknown>" or "refused"
}

func i16(v int16) []byte { return []byte{byte(v), byte(uint16(v) >> 8)} }

var pyKeyedDamages = []pyKeyedDamage{
	{"canonical", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 2, []pyKeyedSlot{{key: k[0], payload: i16(5)}, {key: k[2], payload: i16(-7)}}, nil)
		return b.file()
	}, "read 0", "read 0"},
	{"unknown_key_skipped", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 2, []pyKeyedSlot{{key: k[0], payload: i16(5)}, {key: ir.TableWireId("gone"), payload: i16(1)}}, nil)
		return b.file()
	}, "read 1", "read 1"},
	{"unknown_key_wrong_width", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{key: ir.TableWireId("gone"), payload: []byte{1}}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"key_reference_zero", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{raw: 0, payload: i16(5)}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"key_reference_past_ids", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{raw: 9, payload: i16(5)}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"key_repeated", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 2, []pyKeyedSlot{{key: k[1], payload: i16(5)}, {key: k[1], payload: i16(6)}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"slot_short", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{key: k[0], payload: []byte{5}}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"slot_long", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{key: k[0], payload: []byte{5, 0, 0}}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"count_past_slots", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 3, []pyKeyedSlot{{key: k[0], payload: i16(5)}, {key: k[1], payload: i16(6)}}, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"bytes_left_in_L", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 3, 1, []pyKeyedSlot{{key: k[0], payload: i16(5)}}, []byte{0})
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"element_kind_outside_list", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 12, 0, nil, nil)
		return b.file()
	}, "refused wire_damaged", "refused"},
	{"element_kind_mismatch", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 16, 1, 1, []pyKeyedSlot{{key: k[0], payload: []byte{1}}}, nil)
		return b.file()
	}, "refused kind_mismatch", "refused"},
	{"field_as_plain_array", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		b.keyedField(f, 14, 3, 1, []pyKeyedSlot{{key: k[0], payload: i16(5)}}, nil)
		return b.file()
		// As an array body the keyed slots leave bytes inside L, which the
		// engine's wire check refuses before its decode compares the kind
		// (tablewire.RefusalReason answers wire_damaged for this wire).
	}, "refused wire_damaged", "refused"},
	{"keyed_body_too_short", func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		fref := b.ref(f)
		b.body = append(b.body, pyLeb(fref)...)
		b.body = append(b.body, 16, 1, 3)
		return b.file()
	}, "refused wire_damaged", "refused"},
}

const pyKeyedTableDamageSchema = "package probe\n" + pyKeyedEnum + `table Item
{
    w uint16
}

table R
{
    gear [Slot]Item
    n    int32
}
`

// pyKeyedUnknownTable builds a keyed field of tables (element kind 13) whose
// one slot names a key this reader cannot name and carries the table payload
// that body builds. The payload's own ids are used before the field's.
func pyKeyedUnknownTable(body func(b *pyKeyedBuild) []byte) func(f uint64, k []uint64) []byte {
	return func(f uint64, k []uint64) []byte {
		b := &pyKeyedBuild{}
		payload := body(b)
		b.keyedField(f, 16, 13, 1, []pyKeyedSlot{{key: ir.TableWireId("gone"), payload: payload}}, nil)
		return b.file()
	}
}

// pyKeyedTableDamages hold an UNKNOWN keyed slot of tables to the engine's
// declaration-free wire check (docs/SPEC-TABLES.md §4, skipped is not unread):
// a well-formed payload is skipped and counted, and a damaged one refuses the
// whole load as the engine refuses it.
var pyKeyedTableDamages = []pyKeyedDamage{
	{"unknown_table_slot_empty", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return []byte{0}
	}), "read 1", "read 1"},
	{"unknown_table_slot_field", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		out := append(pyLeb(b.ref(ir.TableWireId("w"))), 3)
		return append(append(out, i16(9)...), 0)
	}), "read 1", "read 1"},
	{"unknown_table_slot_bytes_after_terminator", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return []byte{0, 0}
	}), "refused wire_damaged", "refused"},
	{"unknown_table_slot_no_terminator", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return append(pyLeb(b.ref(ir.TableWireId("w"))), 3, 9, 0)
	}), "refused wire_damaged", "refused"},
	{"unknown_table_slot_field_twice", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		w := pyLeb(b.ref(ir.TableWireId("w")))
		out := append(append([]byte{}, w...), 3, 1, 0)
		out = append(append(out, w...), 3, 2, 0)
		return append(out, 0)
	}), "refused wire_damaged", "refused"},
	{"unknown_table_slot_bool_byte", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return append(pyLeb(b.ref(ir.TableWireId("on"))), 1, 2, 0)
	}), "refused wire_damaged", "refused"},
	{"unknown_table_slot_array_element_kind", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return append(pyLeb(b.ref(ir.TableWireId("xs"))), 14, 2, 99, 0, 0)
	}), "refused wire_damaged", "refused"},
	{"unknown_table_slot_pointer_past_nodes", pyKeyedUnknownTable(func(b *pyKeyedBuild) []byte {
		return append(pyLeb(b.ref(ir.TableWireId("p"))), 17, 9, 0)
	}), "refused wire_damaged", "refused"},
}

const pyKeyedDamageProbe = `from probe import LoadReport, R, Refused
for name in open("cases").read().split():
    report = LoadReport()
    report.unknown = 7
    try:
        R.load(open(name, "rb").read(), report=report)
        print("case", name, "read", report.unknown)
    except Refused as err:
        print("case", name, "refused", err.reason, report.unknown)
`

// pyKeyedWriteProbe builds the "scalars" and "tables" constructs' values by
// hand, prints each save's hex for the engine's wire, and holds the keyed
// accessor's refusals and the write's InvalidValue, with the value untouched.
const pyKeyedWriteProbe = `from probe import InvalidValue, K, KInts, Kit, Item
k = K()
k.ints[1] = -5
k.ints[3] = 7
k.flags[2] = True
k.wide[1] = (1 << 64) - 1
k.wide[2] = 1
k.small[3] = -128
k.money[2] = 3 * 256 + 64
k.n = 3
print("scalars", k.save().hex(), k.measure() == len(k.save()))
for key in (None, 0, 4, -1):
    try:
        k.ints[key]
        print("get", key, "read")
    except KeyError:
        print("get", key, "KeyError")
for key in (0, 4):
    try:
        k.ints[key] = 1
        print("set", key, "stored")
    except KeyError:
        print("set", key, "KeyError")
print("len", len(k.ints), K.descriptor.field("ints").codec, KInts.key_max)

def refuses(label, mutate):
    v = K()
    v.ints[2] = 4
    mutate(v)
    before = repr(v)
    try:
        v.save()
        print("refuse", label, "wrote")
    except InvalidValue as err:
        print("refuse", label, "InvalidValue", err.path, repr(v) == before)

refuses("int_range", lambda v: v.ints.__setitem__(1, 1 << 40))
refuses("int_type", lambda v: v.ints.__setitem__(1, True))
refuses("bool_type", lambda v: v.flags.__setitem__(1, 1))
refuses("fixed_range", lambda v: v.money.__setitem__(3, 201 * 256))
refuses("not_keyed", lambda v: setattr(v, "ints", [0, 0, 0]))
refuses("short_storage", lambda v: v.ints._values.pop())

kit = Kit()
kit.gear[1].w = 1
kit.gear[3].w = 3
kit.gear[3].on = True
kit.tail = 9
print("tables", kit.save().hex())
wrong = Kit()
wrong.gear[2] = K()
try:
    wrong.save()
    print("refuse table_type wrote")
except InvalidValue as err:
    print("refuse table_type InvalidValue", err.path)
`

// pyKeyedDamageRun holds each hand-built wire at the root R of schema to the
// Go engine's verdict first, then to the Python verdict under both pinned
// interpreters. It answers the unit, R's keyed field id and its key ids.
func pyKeyedDamageRun(t *testing.T, schema string, damages []pyKeyedDamage) (*ir.Unit, uint64, []uint64) {
	t.Helper()
	u := loadUnit(t, schema)
	st := u.Tables["R"]
	field := ir.TableFieldWireId(st.Fields[0])
	en := st.Fields[0].KeyEnumRef
	keys := make([]uint64, len(en.Variants))
	for i := range en.Variants {
		keys[i] = ir.TableWireId(en.VariantWireName(i))
	}
	inputs := map[string][]byte{}
	names := make([]string, 0, len(damages))
	for _, d := range damages {
		wire := d.build(field, keys)
		if got := pyKeyedEngineVerdict(t, u, "R", wire); got != d.engine {
			t.Errorf("%s: the Go engine answers %s, want %s (wire %x)", d.name, got, d.engine, wire)
		}
		inputs[d.name] = wire
		names = append(names, d.name)
	}
	sort.Strings(names)
	inputs["cases"] = []byte(strings.Join(names, "\n"))
	got := pyKeyedRun(t, u, pyKeyedDamageProbe, inputs, nil)
	for _, d := range damages {
		want := "case " + d.name + " " + d.python
		if strings.HasPrefix(d.python, "refused") {
			want += " 7" // a refused read leaves the caller's report as it was
		}
		if !strings.Contains(got, want+"\n") {
			t.Errorf("%s: probe output lacks %q:\n%s", d.name, want, got)
		}
	}
	return u, field, keys
}

// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtKeyed(t *testing.T) {
	t.Parallel()
	t.Run("constructs", func(t *testing.T) {
		t.Parallel()
		for _, f := range pyKeyedConstructFailures(t, nil) {
			t.Error(f)
		}
	})
	t.Run("damage", func(t *testing.T) {
		t.Parallel()
		u, field, keys := pyKeyedDamageRun(t, pyKeyedDamageSchema, pyKeyedDamages)
		// the canonical hand-built wire is the engine's own
		canonical := pyKeyedDamages[0].build(field, keys)
		if engine := pyKeyedEngineWire(t, u, "R", `{"vals":{"head":5,"feet":-7}}`); !bytes.Equal(canonical, engine) {
			t.Errorf("the hand-built canonical wire %x is not the engine's %x", canonical, engine)
		}
	})
	t.Run("damage_unknown_table_slot", func(t *testing.T) {
		t.Parallel()
		pyKeyedDamageRun(t, pyKeyedTableDamageSchema, pyKeyedTableDamages)
	})
	t.Run("write", func(t *testing.T) {
		t.Parallel()
		src := pyKeyedConstructs[0].schema + strings.TrimPrefix(pyKeyedConstructs[3].schema, "package probe\n"+pyKeyedEnum)
		u := loadUnit(t, src)
		got := pyKeyedRun(t, u, pyKeyedWriteProbe, nil, nil)
		scalars := pyKeyedEngineWire(t, u, "K", pyKeyedConstructs[0].json)
		tables := pyKeyedEngineWire(t, u, "Kit", `{"gear":{"head":{"w":1},"feet":{"w":3,"on":true}},"tail":9}`)
		for _, line := range []string{
			"scalars " + hex.EncodeToString(scalars) + " True",
			"get None KeyError",
			"get 0 KeyError",
			"get 4 KeyError",
			"get -1 KeyError",
			"set 0 KeyError",
			"set 4 KeyError",
			"len 3 keyed 3",
			"refuse int_range InvalidValue ints[1] True",
			"refuse int_type InvalidValue ints[1] True",
			"refuse bool_type InvalidValue flags[1] True",
			"refuse fixed_range InvalidValue money[3] True",
			"refuse not_keyed InvalidValue ints True",
			"refuse short_storage InvalidValue ints True",
			"tables " + hex.EncodeToString(tables),
			"refuse table_type InvalidValue gear[2]",
		} {
			if !strings.Contains(got, line+"\n") {
				t.Errorf("probe output lacks %q:\n%s", line, got)
			}
		}
		dir := t.TempDir()
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		writeTree(t, dir, files)
		if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("mypy --strict over the generated package:\n%s", out)
		}
		if out, err := pyrun.Analyze(t, dir, "probe/_schema"); err != nil {
			t.Fatalf("mypy --strict over the runtime:\n%s", out)
		}
	})
}

// TestPythonVtKeyedNegativeControl plants two faults, one at a time, in a copy
// of the runtime. The first writes the keyed slots in DESCENDING variant
// order, and the construct gate must go red by name: the order is the one
// thing a positional or reordered writer gets wrong while every slot still
// lands on read. The second skips an unknown keyed slot by its L alone,
// without the wire check, and the unknown-table-slot damage gate must go red
// on the payload with bytes after its terminator.
//
// Functional (pyrun): run with SCHEMA_SLOW=1.
func TestPythonVtKeyedNegativeControl(t *testing.T) {
	t.Parallel()
	t.Run("descending-order", pyKeyedDescendingControl)
	t.Run("unknown-slot-unwalked", pyKeyedUnwalkedControl)
}

// pyKeyedUnwalkedControl runs the unknown-table-slot damage probe on a copy of
// the runtime whose unknown keyed slot is not walked, and requires the damaged
// payload to read.
func pyKeyedUnwalkedControl(t *testing.T) {
	t.Parallel()
	const path = "probe/_schema/kind_keyed.py"
	const anchor = "_skip_content(element, elem_kind, ctx, ctx.depth)"
	u := loadUnit(t, pyKeyedTableDamageSchema)
	st := u.Tables["R"]
	field := ir.TableFieldWireId(st.Fields[0])
	var damaged pyKeyedDamage
	for _, d := range pyKeyedTableDamages {
		if d.name == "unknown_table_slot_bytes_after_terminator" {
			damaged = d
		}
	}
	mutate := func(t *testing.T, files map[string][]byte) {
		src := string(files[path])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", path, anchor, strings.Count(src, anchor))
		}
		files[path] = []byte(strings.Replace(src, anchor, "None", 1))
	}
	inputs := map[string][]byte{damaged.name: damaged.build(field, nil), "cases": []byte(damaged.name)}
	got := pyKeyedRun(t, u, pyKeyedDamageProbe, inputs, mutate)
	if want := "case " + damaged.name + " read 1\n"; !strings.Contains(got, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: with the unknown slot unwalked the damaged payload did not read: %q, want %q", got, want)
	}
}

func pyKeyedDescendingControl(t *testing.T) {
	t.Parallel()
	const path = "probe/_schema/kind_keyed.py"
	const anchor = "for slot in range(len(fd.keys)):"
	mutate := func(t *testing.T, files map[string][]byte) {
		src := string(files[path])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", path, anchor, strings.Count(src, anchor))
		}
		files[path] = []byte(strings.Replace(src, anchor, "for slot in reversed(range(len(fd.keys))):", 1))
	}
	fails := pyKeyedConstructFailures(t, mutate)
	if len(fails) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: writing the slots in descending order left every construct green")
	}
	named := false
	for _, f := range fails {
		if strings.HasPrefix(f, "construct scalars_full:") {
			named = true
		}
	}
	if !named {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the construct scalars_full:\n%s", strings.Join(fails, "\n"))
	}
}
