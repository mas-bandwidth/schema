package pythontable

import (
	"maps"
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
	maps.Copy(sabotaged, files)
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

// optionalArraysSchema is the second half of the kind's surface: the optional
// arrays ?[N]T and ?[..N]T (and a ranged count, ?[1..4]T) over the base's
// kinds, and ?T and the by-value ?[N]T over a VARIABLE closure, a by-value
// Guard that holds a *Leaf pointer (docs/SPEC-TABLES.md §2.3, the
// optional-variable-closure construct the compiler registers for python).
const optionalArraysSchema = `package optarrays

table Pair
{
    a int32
    b bool
}

table Inner
{
    n int32
}

table Leaf
{
    value int32
    next  *Leaf
}

table Guard
{
    tag  int32
    leaf *Leaf
}

table Root
{
    whole  ?[3]int32
    flags  ?[..4]bool
    pairs  ?[2]Pair
    inners ?[..3]Inner
    ranged ?[1..4]int16
    cover  ?Guard
    covers ?[2]Guard
    total  int32
}
`

// optionalArraysOlder is an older or foreign writer of the same Root: the same
// table and field names (so the same wire ids), a shorter fixed bound and a
// wider counted one. Its wires are what this reader must pad or refuse.
const optionalArraysOlder = `package optarrays

table Root
{
    whole  ?[2]int32
    flags  ?[..8]bool
    total  int32
}
`

// optionalArraysForeign rides fixed under a different element kind.
const optionalArraysForeign = `package optarrays

table Root
{
    whole  ?[3]int64
    total  int32
}
`

func optionalArraysInputs(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	cases := map[string]string{
		"absent":  `{"total":5}`,
		"present": `{"whole":[1,-2,3],"flags":[true,false,true],"pairs":[{"a":4,"b":true},{"a":0,"b":false}],"inners":[{"n":7}],"ranged":[-3,9],"total":5}`,
		// Present at the empty end: a counted array at count zero (or at its
		// minimum) and a fixed array all at default still ride.
		"empty": `{"whole":[0,0,0],"flags":[],"pairs":[{"a":0,"b":false},{"a":0,"b":false}],"inners":[],"ranged":[0],"total":5}`,
		// One Leaf named twice, from ?Guard and from ?[2]Guard's first slot,
		// with a chain behind it: the node rides once and both read back as
		// the same object.
		"shared": `{"cover":{"tag":1,"leaf":{"&node":1,"value":10,"next":{"value":11}}},"covers":[{"tag":2,"leaf":{"&node":1}},{"tag":3}],"total":5}`,
		"chain":  `{"covers":[{"tag":2},{"tag":3,"leaf":{"value":4,"next":{"value":5}}}],"total":5}`,
		// The variable-closure ?Guard present at its default: it rides, so
		// it is neither the absent wire nor a value with a node.
		"cover_default": `{"cover":{"tag":0},"total":5}`,
	}
	inputs := map[string][]byte{}
	for name, text := range cases {
		inputs[name+".wire"] = optionalEngineWire(t, u, "Root", text)
	}
	if string(inputs["absent.wire"]) == string(inputs["empty.wire"]) || string(inputs["absent.wire"]) == string(inputs["cover_default.wire"]) {
		t.Fatalf("the engine's absent wire is not distinct from its present-empty array or present-default ?Guard wire")
	}
	inputs["older.wire"] = optionalEngineWire(t, loadUnit(t, optionalArraysOlder), "Root", `{"whole":[6,7],"total":5}`)
	inputs["over.wire"] = optionalEngineWire(t, loadUnit(t, optionalArraysOlder), "Root", `{"flags":[true,true,true,true,true],"total":5}`)
	inputs["foreign.wire"] = optionalEngineWire(t, loadUnit(t, optionalArraysForeign), "Root", `{"whole":["1","2","3"],"total":5}`)
	present := inputs["present.wire"]
	inputs["damaged.wire"] = append([]byte(nil), present[:len(present)-3]...)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + optionalArraysProbe)
	return inputs
}

const optionalArraysProbe = `from optarrays import Guard, Inner, InvalidValue, Leaf, Pair, Refused, Root

def wire(name):
    with open(name, "rb") as f:
        return f.read()

def roundtrip(name):
    data = wire(name + ".wire")
    value = Root.load(data)
    saved = value.save()
    if saved != data:
        raise SystemExit("optional_roundtrip arrays_" + name)
    if value.measure() != len(saved):
        raise SystemExit("optional_measure arrays_" + name)
    return value, data

def pairs(items):
    return None if items is None else [(p.a, p.b) for p in items]

def inners(items):
    return None if items is None else [i.n for i in items]

absent, absent_wire = roundtrip("absent")
present, _ = roundtrip("present")
empty, _ = roundtrip("empty")
print("arrays_absent", absent.whole, absent.flags, absent.pairs, absent.inners, absent.ranged, absent.total)
print("arrays_present", present.whole, present.flags, pairs(present.pairs), inners(present.inners), present.ranged)
print("arrays_empty", empty.whole, empty.flags, pairs(empty.pairs), inners(empty.inners), empty.ranged)

for name in ("whole", "flags", "pairs", "inners", "ranged"):
    setattr(present, name, None)
print("arrays_cleared", present.save() == absent_wire)

shared, _ = roundtrip("shared")
print("shared", shared.cover.tag, shared.cover.leaf.value, shared.cover.leaf.next.value,
      shared.covers[0].tag, shared.covers[1].tag, shared.covers[1].leaf is None)
print("shared_once", shared.cover.leaf is shared.covers[0].leaf)
fresh = Root()
leaf = Leaf()
leaf.value = 10
leaf.next = Leaf()
leaf.next.value = 11
fresh.cover = Guard()
fresh.cover.tag = 1
fresh.cover.leaf = leaf
fresh.covers = [Guard(), Guard()]
fresh.covers[0].tag = 2
fresh.covers[0].leaf = leaf
fresh.covers[1].tag = 3
fresh.total = 5
print("shared_built", fresh.save() == wire("shared.wire"))

cover_default, _ = roundtrip("cover_default")
print("cover_default", cover_default.cover is None, cover_default.cover.tag, cover_default.cover.leaf is None, cover_default.covers)

chain, _ = roundtrip("chain")
print("chain", chain.cover is None, chain.covers[0].leaf is None,
      chain.covers[1].leaf.value, chain.covers[1].leaf.next.value)
# Presence gates the edges: a Guard made absent while it still holds its
# Leaf writes no stale node.
shared.cover = None
shared.covers = None
print("closure_cleared", shared.save() == absent_wire)

older = Root.load(wire("older.wire"))
print("older", older.whole)

def refused(name):
    try:
        Root.load(wire(name + ".wire"))
        print("refused", name, "read")
    except Refused as err:
        print("refused", name, err.reason)

refused("over")
refused("foreign")
refused("damaged")

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

def bad_pair():
    value = Root()
    first = Pair()
    first.a = 2**31
    value.pairs = [first, Pair()]
    return value

def bad_guard():
    value = Root()
    value.covers = [Guard(), Guard()]
    value.covers[1].tag = -2**31 - 1
    return value

refuse("fixed_list", with_root(whole=(1, 2, 3)))
refuse("fixed_short", with_root(whole=[1, 2]))
refuse("fixed_elem", with_root(whole=[1, 2**31, 3]))
refuse("flags_long", with_root(flags=[True] * 5))
refuse("flags_elem", with_root(flags=[1]))
refuse("ranged_short", with_root(ranged=[]))
refuse("pairs_kind", with_root(pairs=[Pair(), Inner()]))
refuse("pairs_field", bad_pair)
refuse("guards_field", bad_guard)

print("OK")
`

// TestPythonVtOptionalArrays holds the optional arrays and ?T over a variable
// closure to the Go engine's wire under both pinned interpreters: absent,
// present and present-at-the-empty-end are three values; a shared node behind
// the optional closure rides once; a value made absent writes no stale node;
// a shorter older fixed bound pads, and a count past the bound, a foreign
// element kind and a truncated file refuse by name.
//
// Functional (python pins): run with SCHEMA_SLOW=1; make/test-python does.
func TestPythonVtOptionalArrays(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, optionalArraysSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the table emitter refused the optional arrays: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := optionalArraysInputs(t, u)
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	for _, line := range []string{
		"arrays_absent None None None None None 5",
		"arrays_present [1, -2, 3] [True, False, True] [(4, True), (0, False)] [7] [-3, 9]",
		"arrays_empty [0, 0, 0] [] [(0, False), (0, False)] [] [0]",
		"arrays_cleared True",
		"shared 1 10 11 2 3 True",
		"shared_once True",
		"shared_built True",
		"cover_default False 0 True None",
		"chain True True 4 5",
		"closure_cleared True",
		"older [6, 7, 0]",
		"refused over count_over_bound",
		"refused foreign kind_mismatch",
		"refused damaged wire_damaged",
		"invalid fixed_list |whole|not a list|",
		"invalid fixed_short |whole|a fixed array holds exactly 3 elements, not 2|",
		"invalid fixed_elem |whole[1]|2147483648 is outside [-2147483648, 2147483647]|",
		"invalid flags_long |flags|5 elements is outside [0, 4]|",
		"invalid flags_elem |flags[0]|not a bool|",
		"invalid ranged_short |ranged|0 elements is outside [1, 4]|",
		"invalid pairs_kind |pairs[1]|not a Pair|",
		"invalid pairs_field |pairs[0].a|2147483648 is outside [-2147483648, 2147483647]|",
		"invalid guards_field |covers[1].tag|-2147483649 is outside [-2147483648, 2147483647]|",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "optarrays"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "optarrays/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
}

// TestPythonVtOptionalArraysNegativeControl plants the optional array's fault
// in a copy of the runtime: a present EMPTY array is written as absent, the
// one thing the presence bit buys (docs/SPEC-TABLES.md §2.3). The probe's
// read-then-write of the present-empty wire must go red by its own name.
func TestPythonVtOptionalArraysNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, optionalArraysSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	maps.Copy(sabotaged, files)
	const path = "optarrays/_schema/kind_optional.py"
	const anchor = "            _write_array(w, fd, value, ctx)\n"
	text := string(sabotaged[path])
	if strings.Count(text, anchor) != 1 {
		t.Fatalf("the negative control's anchor %q is not once in %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "            if value:\n                _write_array(w, fd, value, ctx)\n", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	inputs := optionalArraysInputs(t, u)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: writing a present empty array as absent left the probe green")
	}
	const named = "optional_roundtrip arrays_empty"
	if !strings.Contains(stderr, named) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", named, stderr)
	}
}
