package pythontable

import (
	"encoding/hex"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE BY-VALUE ELISION CASES. A by-value nested table is left out of its
// holder's body exactly when the engine leaves it out: its body is built, each
// field inside judged against its DECLARED default (a plain float by its bit
// pattern, a compressed float by value), and the field drops, with every id it
// interned, when the body is its terminator alone (internal/tablewire/
// encode.go, kind 13). A fixed array of tables always rides, and a counted
// array rides whenever its count is above zero, so a table holding either is
// never elided. Every case below is held to the Go engine's bytes, both by the
// load-then-save round trip and by a value Python builds from scratch.
const vtElideSchema = `package vtel

const LIMIT = 4

enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
flags Cap { Trace, Verbose, Extra }

table Deep
{
    d int32 = 5
    z float64 = 2.5
}

table Mid
{
    m    float32 = 1.5
    deep Deep
}

table Inner
{
    a     int32 = 7
    f     float32
    g     float64 = -1.25
    s     string(8) = "hi"
    cf    float32 | min = -1.0, max = 1.0, resolution = 0.25
    cr    float32 | min = -0.1, max = 0.1, resolution = 0.05
    pair  [2]float32
    ship  Ship = Scout
    fleet [..2]Ship
    cap   Cap
}

table Box
{
    pair [2]Deep
}

table Cnt
{
    c [1..2]int32
}

table Holder
{
    inner Inner
    mid   Mid
    tail  int32 = 3
}

table Packed
{
    held Box
    cnt Cnt
}
`

// vtElideLiveSchema is the same unit with Bomber live: the writer of a wire
// that names the variant the reader has deprecated.
var vtElideLiveSchema = strings.Replace(vtElideSchema, "Bomber | deprecated", "Bomber", 1)

// vtElideCase is one value: its JSON for the engine, and the Python lines
// that build the same value from a fresh root.
type vtElideCase struct {
	name  string
	root  string
	json  string
	build string
}

var vtElideCases = []vtElideCase{
	{"all_default", "Holder", `{}`, ``},
	{"declared_defaults", "Holder",
		`{"inner": {"a": 7, "f": 0.0, "g": -1.25, "s": "hi", "cf": 0.0, "pair": [0.0, 0.0], "ship": "Scout"}, "mid": {"m": 1.5, "deep": {"d": 5, "z": 2.5}}, "tail": 3}`,
		`v.inner.a = 7; v.inner.g = -1.25; v.inner.s = "hi"; v.inner.ship = 3; v.mid.m = 1.5; v.mid.deep.d = 5; v.mid.deep.z = 2.5; v.tail = 3`},
	{"one_off", "Holder", `{"inner": {"a": 8}}`, `v.inner.a = 8`},
	{"neg_zero", "Holder", `{"inner": {"f": -0.0}}`, `v.inner.f = -0.0`},
	{"neg_zero_element", "Holder", `{"inner": {"pair": [0.0, -0.0]}}`, `v.inner.pair = [0.0, -0.0]`},
	{"neg_zero_compressed", "Holder", `{"inner": {"cf": -0.0}}`, `v.inner.cf = -0.0`},
	{"two_deep", "Holder", `{"mid": {"deep": {"d": 6}}}`, `v.mid.deep.d = 6`},
	{"two_deep_inner_default", "Holder", `{"mid": {"m": 2.0}}`, `v.mid.m = 2.0`},
	{"fixed_array_rides", "Packed", `{"cnt": {"c": [0]}}`, ``},
}

// vtElideEngineWire is the Go engine's form-1 bytes for one case.
func vtElideEngineWire(t *testing.T, u *ir.Unit, c vtElideCase) []byte {
	t.Helper()
	return vtFixedClassEngineWire(t, u, c.root, c.json)
}

// vtElideNaNWire is a one_off wire whose Inner.g carries a quiet NaN's bits:
// the engine's writer refuses a NaN, so the bytes are planted.
func vtElideNaNWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	wire := vtFixedClassEngineWire(t, u, "Holder", `{"inner": {"g": -2.0}}`)
	from := []byte{0, 0, 0, 0, 0, 0, 0, 0xc0}
	to := []byte{0, 0, 0, 0, 0, 0, 0xf8, 0x7f}
	at := strings.Index(string(wire), string(from))
	if at < 0 {
		t.Fatalf("the NaN anchor is not in the wire %x", wire)
	}
	out := append([]byte(nil), wire...)
	copy(out[at:], to)
	return out
}

// vtElideRoundWire is an Inner.cr wire carrying float32(0.1), which lies
// above the declared max 0.1: the engine's reader refuses it.
func vtElideRoundWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	wire := vtFixedClassEngineWire(t, u, "Holder", `{"inner": {"cr": 0.05}}`)
	from := []byte{0xcd, 0xcc, 0x4c, 0x3d} // float32(0.05)
	to := []byte{0xcd, 0xcc, 0xcc, 0x3d}   // float32(0.1)
	at := strings.Index(string(wire), string(from))
	if at < 0 {
		t.Fatalf("the float32 anchor is not in the wire %x", wire)
	}
	out := append([]byte(nil), wire...)
	copy(out[at:], to)
	return out
}

// vtElideDeprecatedWire is a wire written with Bomber live, naming it in
// Inner.ship and in Inner.fleet, and the engine's own re-save of it read
// under the schema that deprecates Bomber.
func vtElideDeprecatedWire(t *testing.T, u *ir.Unit) (wire, resaved []byte) {
	t.Helper()
	live := loadUnit(t, vtElideLiveSchema)
	wire = vtFixedClassEngineWire(t, live, "Holder", `{"inner": {"ship": "Bomber", "fleet": ["Bomber", "Fighter"]}}`)
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables["Holder"])
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if !ok || err != nil || rep.Refused || rep.Unknown != 0 {
		t.Fatalf("the engine does not read the deprecated wire cleanly: ok=%v err=%v %+v", ok, err, rep)
	}
	resaved, err = tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the engine does not re-save the deprecated read: %v", err)
	}
	return wire, resaved
}

// engineRefuses reports whether the Go engine's reader refuses a wire.
func engineRefuses(u *ir.Unit, root string, wire []byte) bool {
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	return !ok || err != nil || rep.Refused
}

// TestPythonVtFixedClassElisionEngine holds the Go engine's own answers the
// Python cases are measured against, and logs each case's bytes. It is a
// Go-only gate: no interpreter runs.
func TestPythonVtFixedClassElisionEngine(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtElideSchema)
	wires := map[string][]byte{}
	for _, c := range vtElideCases {
		wires[c.name] = vtElideEngineWire(t, u, c)
		t.Logf("%s: %s", c.name, hex.EncodeToString(wires[c.name]))
	}
	empty := string(wires["all_default"])
	for _, name := range []string{"declared_defaults", "neg_zero_compressed"} {
		if string(wires[name]) != empty {
			t.Errorf("%s: the engine writes a nested table it should elide: %x", name, wires[name])
		}
	}
	for _, name := range []string{"one_off", "neg_zero", "neg_zero_element", "two_deep", "two_deep_inner_default"} {
		if string(wires[name]) == empty {
			t.Errorf("%s: the engine elides a nested table off its default", name)
		}
	}
	if len(wires["fixed_array_rides"]) <= len(empty) {
		t.Errorf("fixed_array_rides: the engine elides a table holding a fixed array of tables: %x", wires["fixed_array_rides"])
	}
	if !engineRefuses(u, "Holder", vtElideNaNWire(t, u)) {
		t.Error("nan: the engine reads a NaN")
	}
	if !engineRefuses(u, "Holder", vtElideRoundWire(t, u)) {
		t.Error("cr_round: the engine reads float32(0.1) under max 0.1")
	}
	wire, resaved := vtElideDeprecatedWire(t, u)
	t.Logf("deprecated wire: %s", hex.EncodeToString(wire))
	t.Logf("deprecated resaved: %s", hex.EncodeToString(resaved))
}

// vtElideProbe runs every case, prints one FAIL line per red case by name,
// and exits nonzero when any is red. Inputs: wire_<case> for each case,
// nan, round, deprecated and deprecated_resaved.
const vtElideProbe = `import math
import vtel
from vtel import Box, Cap, Holder, InvalidValue, Inner, LIMIT, Packed, Refused, Ship

CASES = %CASES%
failed = []

def fail(name):
    failed.append(name)
    print("FAIL vt_elide_" + name)

def read(name):
    with open(name, "rb") as f:
        return f.read()

for name, root, build in CASES:
    cls = Holder if root == "Holder" else Packed
    wire = read("wire_" + name)
    try:
        if cls.load(wire).save() != wire:
            fail(name + "_roundtrip")
    except (Refused, InvalidValue):
        fail(name + "_roundtrip")
    v = cls()
    exec(build)
    try:
        if v.save() != wire or v.measure() != len(wire):
            fail(name)
    except InvalidValue:
        fail(name)

# -0.0 is not 0.0 under ==, as their bytes are not (docs/PORT-PYTHON.md).
a, b = Holder(), Holder()
b.inner.f = -0.0
if a == b:
    fail("eq_neg_zero")
b = Holder()
b.inner.pair = [0.0, -0.0]
if a == b:
    fail("eq_neg_zero_element")
if Holder() != Holder() or Packed() != Packed():
    fail("eq_default")

# A NaN is refused on write, with the caller's value untouched, and on read.
v = Holder()
v.inner.g = float("nan")
try:
    v.save()
    fail("nan_write")
except InvalidValue:
    if not math.isnan(v.inner.g):
        fail("nan_write")
try:
    Holder.load(read("nan"))
    fail("nan_read")
except Refused:
    pass

# A compressed float is held to its range on the float32 the wire carries.
v = Holder()
v.inner.cr = 0.1
try:
    v.save()
    fail("cr_round_write")
except InvalidValue:
    pass
try:
    Holder.load(read("round"))
    fail("cr_round_read")
except Refused:
    pass

# A deprecated variant reads as the field's declared default (an element's as
# None), counts nothing, and is refused on write.
from vtel import LoadReport
report = LoadReport()
try:
    d = Holder.load(read("deprecated"), report)
    if d.inner.ship != Ship.Scout or d.inner.fleet != [0, Ship.Fighter] or report.unknown != 0:
        fail("deprecated_read")
    if d.save() != read("deprecated_resaved"):
        fail("deprecated_resave")
except Refused:
    fail("deprecated_read")
for spoil in ("v.inner.ship = Ship.Bomber", "v.inner.fleet = [Ship.Bomber]"):
    v = Holder()
    exec(spoil)
    try:
        v.save()
        fail("deprecated_write")
    except InvalidValue:
        pass

# A subclass cannot change what is written.
class Sneaky(Inner):
    __slots__ = ()
    def __eq__(self, other):
        return True
v = Holder()
v.inner = Sneaky()
v.inner.a = 99
try:
    v.save()
    fail("subclass")
except InvalidValue:
    pass

# The unit's constants, enums and flags ride beside its tables.
if (LIMIT != 4 or Ship.None_ != 0 or Ship.Scout != 3 or Ship.Max != 3
        or Ship.NAMES[2] != "Bomber" or Cap.Extra != 4 or Cap.Count != 3
        or Cap.NAMES[1] != "Verbose" or "Ship" not in vtel.__all__ or "LIMIT" not in vtel.__all__):
    fail("declarations")

if failed:
    raise SystemExit(1)
print("OK")
`

func vtElideInputs(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	var cases strings.Builder
	cases.WriteString("[\n")
	inputs := map[string][]byte{}
	for _, c := range vtElideCases {
		inputs["wire_"+c.name] = vtElideEngineWire(t, u, c)
		cases.WriteString("    (" + pyQuote(c.name) + ", " + pyQuote(c.root) + ", " + pyQuote(c.build) + "),\n")
	}
	cases.WriteString("]")
	inputs["nan"] = vtElideNaNWire(t, u)
	inputs["round"] = vtElideRoundWire(t, u)
	wire, resaved := vtElideDeprecatedWire(t, u)
	inputs["deprecated"] = wire
	inputs["deprecated_resaved"] = resaved
	inputs["main.py"] = []byte(pyrun.ImportPrelude + strings.Replace(vtElideProbe, "%CASES%", cases.String(), 1))
	return inputs
}

func pyQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// TestPythonVtFixedClassElision runs every elision case, the float, NaN,
// range, deprecated-variant, subclass and declaration checks under both
// pinned interpreters, each held to the Go engine's bytes.
func TestPythonVtFixedClassElision(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtElideSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the Python table backend refuses the unit: %v", err)
	}
	inputs := vtElideInputs(t, u)
	dir := t.TempDir()
	writeTree(t, dir, files)
	current := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if current != "OK\n" || floor != "OK\n" {
		t.Fatalf("the elision probe is red:\ncurrent:\n%s\nfloor:\n%s", current, floor)
	}
	if out, err := pyrun.Analyze(t, dir, "vtel"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
}

// TestPythonVtFixedClassElisionNegativeControl plants one fault per case in a
// copy of the runtime or the generated module, and requires the probe red
// naming that case.
func TestPythonVtFixedClassElisionNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtElideSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the Python table backend refuses the unit: %v", err)
	}
	inputs := vtElideInputs(t, u)
	for _, fault := range []struct {
		name, path, anchor, replacement, red string
	}{
		// the nested table never elides: every all-default case is red
		{"never_elides", "vtel/_schema/kind_table.py", "if len(body) <= 1:", "if len(body) < 1:", "vt_elide_all_default"},
		{"declared_defaults", "vtel/_schema/kind_table.py", "if len(body) <= 1:", "if len(body) < 1:", "vt_elide_declared_defaults"},
		// a small body elides: a table one field off its default is lost
		{"one_off", "vtel/_schema/kind_table.py", "if len(body) <= 1:", "if len(body) <= 8:", "vt_elide_one_off"},
		// the old equality elision: a table holding a fixed array of tables elides
		{"equality_elision", "vtel/_schema/kind_table.py", "if len(body) <= 1:", "if value == descriptor.new_by_name(fd.target):", "vt_elide_fixed_array_rides"},
		// a plain float judged by == elides -0.0 under a 0.0 default
		{"neg_zero", "vtel/_schema/descriptor.py", "return _float32_bits(value) == _float32_bits(fd.default)", "return bool(value == fd.default)", "vt_elide_neg_zero"},
		// the ids an elided body interned are kept: two levels deep is red
		{"two_deep", "vtel/_schema/kind_table.py", "for interned in ctx.ids[mark:]:\n                del ctx.refs[interned]\n            del ctx.ids[mark:]", "pass", "vt_elide_two_deep_inner_default"},
		// a NaN is let through on write
		{"nan", "vtel/_schema/kind_float.py", "if not math.isfinite(value):\n            raise InvalidValue", "if False:\n            raise InvalidValue", "vt_elide_nan_write"},
		// the range is held on the unrounded value
		{"cr_round", "vtel/_schema/kind_float.py", "stored = _unpack(fd, _pack(fd, value))", "stored = value", "vt_elide_cr_round_write"},
		// a deprecated variant reads as its ordinal
		{"deprecated_read", "vtel/_schema/kind_enum.py", "if index + 1 in fd.retired:", "if False:", "vt_elide_deprecated_read"},
		// a deprecated variant is written
		{"deprecated_write", "vtel/_schema/kind_enum.py", "if value in fd.retired:", "if False:", "vt_elide_deprecated_write"},
		// a subclass is accepted
		{"subclass", "vtel/_schema/save.py", "if target is None or type(current) is not target:\n                raise InvalidValue(where, f\"not a {field.target}\")", "if target is None or not isinstance(current, target):\n                raise InvalidValue(where, f\"not a {field.target}\")", "vt_elide_subclass"},
		// equality by value lets -0.0 equal 0.0
		{"eq_neg_zero", "vtel/Core.py", "_s_float32_bits_eq(self.f, other.f)", "self.f == other.f", "vt_elide_eq_neg_zero"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			t.Parallel()
			sabotaged := maps.Clone(files)
			text, ok := sabotaged[fault.path]
			if !ok {
				t.Fatalf("the generated package carries no %s", fault.path)
			}
			if !strings.Contains(string(text), fault.anchor) {
				t.Fatalf("the negative control's anchor %q is absent from %s", fault.anchor, fault.path)
			}
			sabotaged[fault.path] = []byte(strings.Replace(string(text), fault.anchor, fault.replacement, 1))
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)
			stdout, _, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
			if runErr == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s left the probe green", fault.name)
			}
			if !strings.Contains(stdout, "FAIL "+fault.red+"\n") {
				t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", fault.red, stdout)
			}
		})
	}
}
