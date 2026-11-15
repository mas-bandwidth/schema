package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// optionalSchema is the optional-field kind's construct surface: ?bool,
// ?int32, ?bits(12), an optional by-value table and an optional nested table,
// beside the base kinds this cut already carries (a by-value root scalar and
// a *T pointer). D declares non-zero defaults so present-at-default and
// present-at-zero differ on the wire; Holder (Root.h, by value) and Node
// (behind *Node) carry ?T inside a nested table and a pointer node. Nothing
// here needs a kind a later card owns.
const optionalSchema = `package optional

table Pair
{
    a int32
    b bool
}

table Inner
{
    n int32
}

table D
{
    a int32 = 9
    b bool = true
}

table Holder
{
    o ?int32
    d ?D
}

table Node
{
    value int32
    o     ?int32
    next  *Node
}

table Root
{
    flag  ?bool
    tier  ?int32
    mask  ?bits(12)
    pair  ?Pair
    inner ?Inner
    total int32
    head  *Node
    d     ?D
    h     Holder
}
`

// optionalEngineWire is the Go engine's form-1 wire of the JSON text at root.
// The engine is the oracle: the Python read is held to the bytes the tool
// writes, never to a second Python writer.
func optionalEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read %s cleanly: %+v", text, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write %s: %v", text, err)
	}
	return wire
}

// optionalProbeInputs builds the wires and the probe. The probe reads each
// canonical wire, saves it back, measures it, and clears the present optionals
// to prove presence gates content: a value made absent after it was present
// writes exactly the absent wire.
func optionalProbeInputs(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	cases := map[string]string{
		"absent":   `{"total":5}`,
		"present":  `{"flag":true,"tier":-7,"mask":1234,"pair":{"a":9,"b":true},"inner":{"n":11},"total":5}`,
		"defaults": `{"flag":false,"tier":0,"mask":0,"pair":{"a":0,"b":false},"total":5}`,
		"nested":   `{"tier":3,"head":{"value":1,"next":{"value":2}},"total":5}`,
		// D declares non-zero defaults, so present-at-default and
		// present-at-zero are different wires, and both differ from absent.
		"d_absent":  `{"total":5}`,
		"d_default": `{"d":{"a":9,"b":true},"total":5}`,
		"d_zero":    `{"d":{"a":0,"b":false},"total":5}`,
		// ?T inside a by-value nested table (Root.h) and inside a *Node.
		"holder_zero":   `{"h":{"o":0,"d":{"a":0,"b":false}},"total":5}`,
		"holder_absent": `{"h":{"d":{"a":9,"b":true}},"total":5}`,
		"node_zero":     `{"head":{"value":1,"o":0,"next":{"value":2}},"total":5}`,
		"node_absent":   `{"head":{"value":1,"next":{"value":2,"o":-4}},"total":5}`,
	}
	inputs := map[string][]byte{}
	for name, text := range cases {
		inputs[name+".wire"] = optionalEngineWire(t, u, "Root", text)
	}
	// The three D wires must be distinct on the engine side, or the probe
	// could not tell the cases apart.
	da, dd, dz := string(inputs["d_absent.wire"]), string(inputs["d_default.wire"]), string(inputs["d_zero.wire"])
	if da == dd || da == dz || dd == dz {
		t.Fatalf("the engine's absent, present-at-default and present-at-zero ?D wires are not distinct")
	}
	if string(inputs["holder_zero.wire"]) == string(inputs["holder_absent.wire"]) ||
		string(inputs["node_zero.wire"]) == string(inputs["node_absent.wire"]) {
		t.Fatalf("the engine's nested present-at-zero and absent ?T wires are not distinct")
	}
	present := inputs["present.wire"]
	inputs["damaged.wire"] = append([]byte(nil), present[:len(present)-3]...)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + optionalProbe)
	return inputs
}

const optionalProbe = `from optional import D, InvalidValue, Node, Refused, Root, Pair

def wire(name):
    with open(name, "rb") as f:
        return f.read()

def roundtrip(name):
    data = wire(name + ".wire")
    value = Root.load(data)
    saved = value.save()
    if saved != data:
        raise SystemExit("optional_roundtrip " + name)
    if value.measure() != len(saved):
        raise SystemExit("optional_measure " + name)
    return value, data

absent, absent_wire = roundtrip("absent")
present, present_wire = roundtrip("present")
defaults, defaults_wire = roundtrip("defaults")
nested, nested_wire = roundtrip("nested")

print("absent", absent.flag is None, absent.tier is None, absent.mask is None,
      absent.pair is None, absent.inner is None, absent.total, absent.head is None)
print("present", present.flag is None, present.tier, present.mask,
      present.pair is None, present.inner is None, present.total, present.head is None)
print("defaults", defaults.flag is None, defaults.tier, defaults.mask,
      defaults.pair is None, defaults.inner is None, defaults.total, defaults.head is None)
print("nested", nested.flag is None, nested.tier is None, nested.mask is None,
      nested.pair is None, nested.inner is None, nested.total, nested.head is None)
print("pair_present", defaults.pair is not None and defaults.pair == Pair())
print("absent_pair", absent.pair is None)

for name in ("flag", "tier", "mask", "pair", "inner"):
    setattr(present, name, None)
if present.save() != absent_wire:
    raise SystemExit("optional_cleared")
print("cleared", present.save() == absent_wire)

witness, witness_wire = roundtrip("present")
try:
    Root.load(wire("damaged.wire"))
    print("damaged", "read")
except Refused as err:
    print("damaged", err.reason)
print("witness", witness.save() == witness_wire)

def d_value(d):
    return None if d is None else (d.a, d.b)

d_absent, _ = roundtrip("d_absent")
d_default, _ = roundtrip("d_default")
d_zero, _ = roundtrip("d_zero")
print("d_absent", d_value(d_absent.d))
print("d_default", d_value(d_default.d))
print("d_zero", d_value(d_zero.d))
print("d_fresh", d_value(D()))

holder_zero, _ = roundtrip("holder_zero")
holder_absent, _ = roundtrip("holder_absent")
print("holder_zero", holder_zero.h.o, d_value(holder_zero.h.d))
print("holder_absent", holder_absent.h.o, d_value(holder_absent.h.d))

node_zero, _ = roundtrip("node_zero")
node_absent, _ = roundtrip("node_absent")
print("node_zero", node_zero.head.o, node_zero.head.next.o)
print("node_absent", node_absent.head.o, node_absent.head.next.o)

def refuse(label, build):
    value = build()
    try:
        value.save()
        print("invalid", label, "wrote")
    except InvalidValue as err:
        print("invalid", label, "|" + err.path + "|" + err.rule + "|")

def with_root(**fields):
    def build():
        value = Root()
        for name, item in fields.items():
            setattr(value, name, item)
        return value
    return build

def with_d(a):
    def build():
        value = Root()
        value.d = D()
        value.d.a = a
        return value
    return build

def with_holder(o):
    def build():
        value = Root()
        value.h.o = o
        return value
    return build

def with_node(o):
    def build():
        value = Root()
        value.head = Node()
        value.head.o = o
        return value
    return build

refuse("tier_type", with_root(tier=True))
refuse("tier_high", with_root(tier=2**31))
refuse("tier_low", with_root(tier=-2**31 - 1))
refuse("mask_high", with_root(mask=4096))
refuse("mask_low", with_root(mask=-1))
refuse("d_a_high", with_d(2**31))
refuse("holder_o_high", with_holder(2**31))
refuse("node_o_high", with_node(2**31))

print("OK")
`

// TestPythonVtOptional holds docs/SPEC-TABLES.md §2.3's ?T on the Python
// variable table wire: an absent optional is None and rides no field, a
// present one always rides (its default included), a read is the whole value
// or Refused with the caller's objects untouched, and a read-then-write is the
// canonical engine wire byte for byte under both pinned interpreters. The
// construct uses only the optional kind and the kinds the base carries.
//
// Functional (python pins): run with SCHEMA_SLOW=1; make/test-python does.
func TestPythonVtOptional(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, optionalSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the table emitter refused ?T over the base kinds: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := optionalProbeInputs(t, u)
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	for _, line := range []string{
		"absent True True True True True 5 True",
		"present False -7 1234 False False 5 True",
		"defaults False 0 0 False True 5 True",
		"nested True False True True True 5 False",
		"pair_present True",
		"absent_pair True",
		"cleared True",
		"witness True",
		"d_absent None",
		"d_default (9, True)",
		"d_zero (0, False)",
		"d_fresh (9, True)",
		"holder_zero 0 (0, False)",
		"holder_absent None (9, True)",
		"node_zero 0 None",
		"node_absent None -4",
		"invalid tier_type |tier|not an int|",
		"invalid tier_high |tier|2147483648 is outside [-2147483648, 2147483647]|",
		"invalid tier_low |tier|-2147483649 is outside [-2147483648, 2147483647]|",
		"invalid mask_high |mask|4096 is outside [0, 4095]|",
		"invalid mask_low |mask|-1 is outside [0, 4095]|",
		"invalid d_a_high |d.a|2147483648 is outside [-2147483648, 2147483647]|",
		"invalid holder_o_high |h.o|2147483648 is outside [-2147483648, 2147483647]|",
		"invalid node_o_high |head.o|2147483648 is outside [-2147483648, 2147483647]|",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	if strings.Contains(got, "damaged read\n") {
		t.Errorf("a truncated wire was read:\n%s", got)
	}
	if !strings.Contains(got, "damaged wire_damaged\n") {
		t.Errorf("the truncated wire did not refuse as wire_damaged:\n%s", got)
	}
	if out, err := pyrun.Analyze(t, dir, "optional"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "optional/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
}

// TestPythonVtOptionalNegativeControl plants the optional kind's fault in a
// copy of the runtime: its write delegates nothing, so a present optional is
// elided and the probe's read-then-write is not the canonical wire. The test
// must go red by the probe's own name.
func TestPythonVtOptionalNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, optionalSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "optional/_schema/kind_optional.py"
	const anchor = "self._inner(fd).write(w, fd, value, ctx)"
	text := string(sabotaged[path])
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "return", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	inputs := optionalProbeInputs(t, u)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: eliding every present optional left the probe green")
	}
	const named = "optional_roundtrip"
	if !strings.Contains(stderr, named) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", named, stderr)
	}
}
