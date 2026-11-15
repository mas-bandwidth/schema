package pythontable

// THE ID-TABLE WIRE'S ENUM AND ESCAPE KINDS on the Python leg
// (docs/SPEC-TABLES.md §3, §5; docs/PORT-PYTHON.md, THE TABLE FRAME): Go-engine
// constructs that carry an enum field alone and beside the base's kinds (in a
// field, an array element in an unknown field, a nested table and a pointer
// closure), held to the one read on read-then-write. A construct's canonical
// wire is read and written back byte for byte, or the read refuses and nothing
// is returned. An unknown field riding under the ESCAPE (31) or PAYLOAD-FREE
// (32) kind is skipped and counted unknown, and the save writes the value's
// canonical wire; a kind 32 with a nonzero L, a known field riding under the
// escape, and an enum reference past the id table (on a known field, on an
// unknown field, and as an element of an unknown enum array) are damage or a
// kind mismatch and refuse the whole load with the Go engine's own reason,
// which the test reads from tablewire.Decode first.
//
// TestPythonVtIdtableNegativeControl plants one defect at a time in a copy of
// the emitted runtime (each check this file adds) and requires the probe to go
// red by name under each pinned interpreter.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const idtableSchema = `package idtable

enum Hue { Red, Green, Blue }
enum Mood { Calm, Loud }

table Alone
{
    hue Hue
}

table Defaulted
{
    hue Hue = Blue
}

table Nested
{
    mood Mood
    tag  int32
}

table Outer
{
    live  bool
    hue   Hue
    inner Nested
    total uint64
}

table Chain
{
    hue   Hue
    score uint64
    next  *Chain
}

table Wide
{
    hue  Hue
    mask bits(5)
}

table Pair
{
    first  Hue
    second Hue
}
`

type idtableConstruct struct {
	name, root, json string
}

var idtableConstructs = []idtableConstruct{
	{"alone", "Alone", `{"hue":"Green"}`},
	{"defaulted", "Defaulted", `{"hue":"Red"}`},
	{"defaulted_default", "Defaulted", `{}`},
	{"nested", "Nested", `{"mood":"Loud","tag":-4}`},
	{"outer", "Outer", `{"live":true,"hue":"Blue","inner":{"mood":"Calm","tag":9},"total":18446744073709551615}`},
	{"chain", "Chain", `{"hue":"Blue","score":9,"next":{"hue":"Red","score":0}}`},
	{"wide", "Wide", `{"hue":"Green","mask":21}`},
	{"pair", "Pair", `{"first":"Red","second":"Red"}`},
	{"alone_default", "Alone", `{}`},
}

// idtableEngineWire is the Go engine's canonical form-1 wire of the JSON text
// at root.
func idtableEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("the unit declares no table %s", root)
	}
	inst := m.New(st)
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) || report.Refused || report.Unknown > 0 {
		t.Fatalf("the Go engine does not read %s cleanly: %+v", root, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write %s: %v", root, err)
	}
	if wire[0] != ir.TableWireForm {
		t.Fatalf("%s is written as form %d, not the variable form", root, wire[0])
	}
	return wire
}

func idtableVarint(value int) []byte {
	out := make([]byte, 0, 2)
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

// idtableWire builds one form-1 file from a body and its id table.
func idtableWire(body []byte, ids ...uint64) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	var word [8]byte
	for _, id := range ids {
		binary.LittleEndian.PutUint64(word[:], id)
		out = append(out, word[:]...)
	}
	binary.LittleEndian.PutUint64(word[:], uint64(len(ids)))
	return append(out, word[:]...)
}

func idtableHeader(ref int, kind int, payload []byte) []byte {
	out := append(idtableVarint(ref), byte(kind))
	return append(out, payload...)
}

func idtableL(payload []byte) []byte {
	return append(idtableVarint(len(payload)), payload...)
}

// idtableInputs is every wire the probe reads: the constructs' canonical
// wires and the hand-built escape, payload-free and enum-unknown cases.
func idtableInputs(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	inputs := map[string][]byte{}
	for _, c := range idtableConstructs {
		inputs[c.name+".wire"] = idtableEngineWire(t, u, c.root, c.json)
	}
	unknownID := ir.TableWireId("anonymous_escape_field")
	unknownArrayID := ir.TableWireId("anonymous_enum_array")
	hueID := ir.TableWireId("hue")
	greenID := ir.TableWireId("Green")
	inputs["unknown_escape.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindEscape, idtableL([]byte{1, 2, 3})), 0), unknownID)
	inputs["unknown_nopayload.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindNoPayload, idtableL(nil)), 0), unknownID)
	// an unknown array whose element kind is ENUM (30): skipped and counted
	// unknown, but a skipped value is still validated, as the engine's wire
	// check does: each element's variant reference must name an id-table
	// entry. One element in the table reads; one past it is damage.
	arrayBody := append([]byte{byte(ir.TableKindEnum)}, idtableVarint(1)...)
	arrayBody = append(arrayBody, idtableVarint(2)...)
	inputs["unknown_enum_array.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindArray, idtableL(arrayBody)), 0), unknownArrayID, greenID)
	pastBody := append([]byte{byte(ir.TableKindEnum)}, idtableVarint(2)...)
	pastBody = append(pastBody, idtableVarint(1)...)
	pastBody = append(pastBody, idtableVarint(5)...)
	inputs["unknown_enum_array_past.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindArray, idtableL(pastBody)), 0), unknownArrayID, greenID)
	// a variant reference this enum cannot name is ordinary unknown: the field
	// reads None and the save writes the value's canonical wire.
	inputs["unknown_variant.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindEnum, idtableVarint(2)), 0),
		hueID, ir.TableWireId("retired_variant"))
	// the refusals: a payload-free kind with a nonzero L, a known field riding
	// under the escape, and a variant reference past the id table, on an
	// unknown field (the skip's check) and on the known field hue (the enum
	// codec's check).
	inputs["nopayload_nonzero.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindNoPayload, idtableL([]byte{0xaa})), 0), unknownID)
	inputs["known_escape.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindEscape, idtableL(nil)), 0), hueID)
	inputs["enum_ref_past.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindEnum, idtableVarint(2)), 0), unknownID)
	inputs["known_enum_ref_past.wire"] = idtableWire(
		append(idtableHeader(1, ir.TableKindEnum, idtableVarint(2)), 0), hueID)
	return inputs
}

// idtableAccepted are the hand-built wires the read accepts, each with one
// field skipped and counted unknown, read at Alone.
var idtableAccepted = []string{"unknown_escape", "unknown_nopayload", "unknown_enum_array", "unknown_variant"}

// idtableRefusals are the hand-built wires the read refuses at Alone, with the
// reason the Go engine gives: TestPythonVtIdtable holds the engine to each
// reason before the probe holds Python to it.
var idtableRefusals = []struct{ name, reason string }{
	{"nopayload_nonzero", tablewire.ReasonWireDamaged},
	{"known_escape", tablewire.ReasonKindMismatch},
	{"enum_ref_past", tablewire.ReasonWireDamaged},
	{"known_enum_ref_past", tablewire.ReasonWireDamaged},
	{"unknown_enum_array_past", tablewire.ReasonWireDamaged},
}

// idtableEngineVerdict is the Go engine's read of a wire at root: "read" with
// its unknown count, or the refusal's reason.
func idtableEngineVerdict(t *testing.T, u *ir.Unit, root string, wire []byte) (string, int) {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var report tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &report)
	if ok {
		return "read", report.Unknown
	}
	var refusal *tablewire.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("the Go engine refuses with %v, not a reasoned refusal", err)
	}
	return refusal.Reason, 0
}

// idtableProbeText is the probe with one load_refusal call per refusal, so the
// probe and the engine check share the one list.
func idtableProbeText() string {
	var calls strings.Builder
	for _, r := range idtableRefusals {
		fmt.Fprintf(&calls, "load_refusal(%q, Alone, %q)\n", r.name, r.reason)
	}
	return strings.Replace(idtableProbe, "#REFUSALS#\n", calls.String(), 1)
}

const idtableProbe = `from idtable import Alone, Chain, Defaulted, Hue, InvalidValue, LoadReport, Mood, Nested, Outer, Pair, Refused, Wide

def wire(name):
    with open(name + ".wire", "rb") as fh:
        return fh.read()

def roundtrip(name, cls, detail):
    data = wire(name)
    try:
        value = cls.load(data)
    except Refused as err:
        raise SystemExit("idtable_" + name + ": load refused " + err.reason)
    saved = value.save()
    if saved != data:
        raise SystemExit("idtable_" + name + ": save is not the canonical wire")
    if value.measure() != len(saved):
        raise SystemExit("idtable_" + name + ": measure differs")
    print(name, "same", detail(value))

roundtrip("alone", Alone, lambda v: "hue=%d" % v.hue)
roundtrip("defaulted", Defaulted, lambda v: "hue=%d" % v.hue)
roundtrip("defaulted_default", Defaulted, lambda v: "hue=%d" % v.hue)
roundtrip("nested", Nested, lambda v: "mood=%d tag=%d" % (v.mood, v.tag))
roundtrip("outer", Outer, lambda v: "live=%s hue=%d total=%d inner_mood=%d inner_tag=%d" % (v.live, v.hue, v.total, v.inner.mood, v.inner.tag))
roundtrip("chain", Chain, lambda v: "hue=%d score=%d next_hue=%d next_score=%d next_next=%s" % (v.hue, v.score, v.next.hue, v.next.score, v.next.next))
roundtrip("wide", Wide, lambda v: "hue=%d mask=%d" % (v.hue, v.mask))
roundtrip("pair", Pair, lambda v: "first=%d second=%d" % (v.first, v.second))

def unknown(name, cls):
    report = LoadReport()
    try:
        value = cls.load(wire(name), report=report)
    except Refused as err:
        raise SystemExit("idtable_" + name + ": load refused " + err.reason)
    if report.unknown != 1:
        raise SystemExit("idtable_" + name + ": unknown is %d, not 1" % report.unknown)
    if value != cls():
        raise SystemExit("idtable_" + name + ": value is not the default")
    if value.save() != wire("alone_default"):
        raise SystemExit("idtable_" + name + ": save is not the default wire")
    print(name, report.unknown, True)

unknown("unknown_escape", Alone)
unknown("unknown_nopayload", Alone)
unknown("unknown_enum_array", Alone)
unknown("unknown_variant", Alone)

def load_refusal(name, cls, reason):
    try:
        cls.load(wire(name))
    except Refused as err:
        if err.reason != reason:
            raise SystemExit("idtable_" + name + ": refused " + err.reason + ", not " + reason)
        print("load", name, "refused", err.reason)
    except Exception as err:
        raise SystemExit("idtable_" + name + ": raised " + type(err).__name__ + ", not Refused")
    else:
        raise SystemExit("idtable_" + name + ": read")

#REFUSALS#

def write_refusal(name, build):
    try:
        build().save()
    except InvalidValue:
        print("write", name, "refused")
    except Exception as err:
        raise SystemExit("idtable_write_" + name + ": raised " + type(err).__name__ + ", not InvalidValue")
    else:
        raise SystemExit("idtable_write_" + name + ": wrote")

def bad_enum():
    value = Alone()
    value.hue = 99
    return value

def bad_bool():
    value = Alone()
    value.hue = True
    return value

def bad_nested():
    value = Nested()
    value.tag = 1
    value.mood = 4
    return value

write_refusal("bad_enum", bad_enum)
write_refusal("bad_bool", bad_bool)
write_refusal("bad_nested", bad_nested)

print("OK")
`

func idtableRun(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := idtableInputs(t, loadUnit(t, idtableSchema))
	inputs["main.py"] = []byte(pyrun.ImportPrelude + idtableProbeText())
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	if out, err := pyrun.Analyze(t, dir, "idtable"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "idtable/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
	return got
}

// Functional (pythonBinary): run with SCHEMA_SLOW=1.
func TestPythonVtIdtable(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	u := loadUnit(t, idtableSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	// THE ENGINE'S VERDICT FIRST: every hand-built wire the probe reads is
	// read or refused by the Go engine exactly as the probe requires of Python.
	inputs := idtableInputs(t, u)
	for _, name := range idtableAccepted {
		if verdict, unknown := idtableEngineVerdict(t, u, "Alone", inputs[name+".wire"]); verdict != "read" || unknown != 1 {
			t.Errorf("the Go engine answers %s with %s (unknown %d), not a read with one unknown", name, verdict, unknown)
		}
	}
	for _, r := range idtableRefusals {
		if verdict, _ := idtableEngineVerdict(t, u, "Alone", inputs[r.name+".wire"]); verdict != r.reason {
			t.Errorf("the Go engine answers %s with %s, not %s", r.name, verdict, r.reason)
		}
	}
	got := idtableRun(t, files)
	want := []string{
		"alone same hue=2",
		"defaulted same hue=1",
		"defaulted_default same hue=3",
		"nested same mood=2 tag=-4",
		"outer same live=True hue=3 total=18446744073709551615 inner_mood=1 inner_tag=9",
		"chain same hue=3 score=9 next_hue=1 next_score=0 next_next=None",
		"wide same hue=2 mask=21",
		"pair same first=1 second=1",
		"unknown_escape 1 True",
		"unknown_nopayload 1 True",
		"unknown_enum_array 1 True",
		"unknown_variant 1 True",
		"write bad_enum refused",
		"write bad_bool refused",
		"write bad_nested refused",
		"OK",
	}
	for _, r := range idtableRefusals {
		want = append(want, "load "+r.name+" refused "+r.reason)
	}
	for _, line := range want {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("the probe output lacks %q:\n%s", line, got)
		}
	}
}

// TestPythonVtIdtableNegativeControl plants one fault at a time in a copy of
// the emitted runtime: the enum codec's variant reference, its read-side
// past-table check and its write validation, and the reader's payload-free
// zero-length check, unknown-enum past-table check and unknown enum array
// walk. Each must turn the probe red by name under each pinned interpreter.
//
// Functional (pythonBinary): run with SCHEMA_SLOW=1.
func TestPythonVtIdtableNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	u := loadUnit(t, idtableSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	inputs := idtableInputs(t, u)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + idtableProbeText())
	for _, control := range []struct {
		name, file, from, to, marker string
	}{
		// the enum codec references the wrong variant id: no saved enum field
		// is the canonical wire's.
		{"enum-variant", "idtable/_schema/kind_enum.py", "w.varint(ctx.idref(wire_id))", "w.varint(ctx.idref(wire_id) + 1)", "idtable_alone"},
		// the reader drops kind 32's zero-length rule and steps past the
		// payload anyway: a payload-free field with a nonzero L reads where the
		// wire is damage.
		{"escape-nonzero", "idtable/_schema/load.py", "        if length != 0:\n            raise Refused(\"wire_damaged\", \"kind 32 with a nonzero L\")\n        return", "        reader.take(length)\n        return", "idtable_nopayload_nonzero"},
		// the enum codec drops its past-table check: a known enum field whose
		// reference is past the id table indexes off the table instead of
		// refusing wire_damaged.
		{"enum-read-past", "idtable/_schema/kind_enum.py", "        if reference > len(ctx.ids):\n            raise Refused(\"wire_damaged\", \"a variant reference is past the id table\")\n", "", "idtable_known_enum_ref_past"},
		// the reader's kind-30 skip drops its past-table check: an unknown
		// enum field whose reference is past the id table reads.
		{"skip-read-past", "idtable/_schema/load.py", "        if reference > len(reader.ids):\n            raise Refused(\"wire_damaged\", \"a variant reference is past the id table\")\n", "", "idtable_enum_ref_past"},
		// the reader skips an unknown enum array opaquely again: an element
		// reference past the id table reads where the engine refuses.
		{"array-read-past", "idtable/_schema/load.py", "            _skip_enum_array(Reader(content, 1, length, reader.ids))\n", "            pass\n", "idtable_unknown_enum_array_past"},
		// the enum codec validates nothing: an out-of-range enum value reaches
		// the write instead of raising InvalidValue.
		{"enum-validate", "idtable/_schema/kind_enum.py", "    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:\n", "    def validate(self, fd: FieldDesc, value: Any, path: str) -> None:\n        return\n", "idtable_write_bad_enum"},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			sabotaged := make(map[string][]byte, len(files))
			for name, data := range files {
				sabotaged[name] = data
			}
			src := string(sabotaged[control.file])
			if strings.Count(src, control.from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", control.file, control.from, strings.Count(src, control.from))
			}
			sabotaged[control.file] = []byte(strings.Replace(src, control.from, control.to, 1))
			for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := t.TempDir()
				writeTree(t, dir, sabotaged)
				_, stderr, runErr := pyrun.Try(t, python, dir, "main.py", inputs)
				if runErr == nil {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s left the probe green under %s", control.name, python)
				}
				if !strings.Contains(stderr, control.marker) {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s went red under %s, but not by name %q:\n%s", control.name, python, control.marker, stderr)
				}
			}
		})
	}
}
