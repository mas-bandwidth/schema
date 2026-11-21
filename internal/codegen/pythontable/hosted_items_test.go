package pythontable

import (
	"maps"
	"math/big"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// hostedItemsSchema is a unit with a table, so the table frame hosts its
// packet classes, holding the three storage-less packet wire items (SPEC §4.3)
// and the string, bytes and flags defaults (SPEC §4.2). Framed is reached by
// the table, so it also carries a table descriptor and rides the table wire as
// its two stored fields; Loose is reached by nothing. The frame used to refuse
// the unit ("uses the packet kind const, which the Python packet wire does not
// carry yet") although the packet core it hosts carries all of them.
//
// Framed's wire, bit by bit: const 0xC7 at 0-7, version at 8-9, reserved(5) at
// 10-14, one bit of align padding at 15, sequence at 16-31.
const hostedItemsSchema = `package hostedframing

flags Caps { Jump, Crouch, Fly }

type Framed
{
    const(0xC7, 8)
    version bits(2)
    reserved(5)
    align
    sequence uint16
}

type Loose
{
    const(0x1F, 5)
    reserved(3)
    tail uint8
}

type Defaulted
{
    name string(6) = "hi"
    token bytes(4) = "ab"
    caps Caps = { Jump, Fly }
}

table Holder
{
    header Framed
}
`

const (
	hostedVersion  = 2
	hostedSequence = 0x1234
	hostedTail     = 0x5A
)

const hostedHolderText = `{"header":{"version":2,"sequence":4660}}`

// hostedItemsReference is the packet wire built in Go from the declaration by
// the packet wire's own rules (docs/PACKET-WIRE-ALGORITHM.md §2), not by the
// emitter: Framed at the probe's values, and Loose at its tail.
func hostedItemsReference(t *testing.T, u *ir.Unit) (framed, loose []byte) {
	t.Helper()
	if u.Structs["Framed"] == nil || u.Structs["Loose"] == nil {
		t.Fatal("the unit lacks Framed or Loose")
	}
	var f refBits
	f.put(big.NewInt(0xC7), 8)
	f.put(big.NewInt(hostedVersion), 2)
	f.put(big.NewInt(0), 5)
	for f.n%8 != 0 {
		f.put(big.NewInt(0), 1)
	}
	f.put(big.NewInt(hostedSequence), 16)
	var l refBits
	l.put(big.NewInt(0x1F), 5)
	l.put(big.NewInt(0), 3)
	l.put(big.NewInt(hostedTail), 8)
	return f.bytes(), l.bytes()
}

func hostedHolderWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Holder"))
	var report tabletext.Report
	if !m.Read(inst, []byte(hostedHolderText), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not place the value: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode the value: %v", err)
	}
	return wire
}

// hostedItemsProbe: each packet write is the Go reference's bytes; a wrong
// const, a set reserved bit and set align padding are each refused with the
// exact reason (SPEC §4.3, §6); an out-of-range field raises InvalidValue
// (SPEC §5.1); the defaults are in the constructor and the §5 zero form is
// all-zero; the table round-trips the Go engine's wire.
const hostedItemsProbe = pyrun.ImportPrelude + `import sys

import hostedframing
from hostedframing.Core import Defaulted, Framed, Holder, Loose


def fail(message):
    print("FAILED: " + message)
    sys.exit(1)


def refused(name, kind, data, reason):
    try:
        kind.read(data)
    except hostedframing.Refused as exc:
        if exc.reason != reason:
            fail(name + ": refused as " + exc.reason + ", want " + reason)
        return
    fail(name + ": accepted")


with open("framed.bin", "rb") as handle:
    framed_data = handle.read()
with open("loose.bin", "rb") as handle:
    loose_data = handle.read()
with open("holder.bin", "rb") as handle:
    holder_data = handle.read()

framed = Framed()
framed.version = 2
framed.sequence = 0x1234
if framed.write() != framed_data:
    fail("framed wire differs: " + framed.write().hex() + ", want " + framed_data.hex())
if Framed.read(framed_data) != framed:
    fail("framed read: " + repr(Framed.read(framed_data)))

bad = bytearray(framed_data)
bad[0] ^= 0x01
refused("a wrong const", Framed, bytes(bad), "rejected")
bad = bytearray(framed_data)
bad[1] |= 0x04
refused("a set reserved bit", Framed, bytes(bad), "rejected")
bad = bytearray(framed_data)
bad[1] |= 0x80
refused("set alignment padding", Framed, bytes(bad), "nonzero_padding")
refused("a short framed read", Framed, framed_data[:-1], "past_end")

out_of_range = Framed()
out_of_range.version = 4
try:
    out_of_range.write()
except hostedframing.InvalidValue:
    pass
else:
    fail("accepted version=4 in bits(2)")

loose = Loose()
loose.tail = 0x5A
if loose.write() != loose_data:
    fail("loose wire differs: " + loose.write().hex() + ", want " + loose_data.hex())
bad = bytearray(loose_data)
bad[0] ^= 0x01
refused("a wrong loose const", Loose, bytes(bad), "rejected")
bad = bytearray(loose_data)
bad[0] |= 0x20
refused("a set loose reserved bit", Loose, bytes(bad), "rejected")

fresh = Defaulted()
if fresh.name != "hi" or fresh.token != b"ab" or fresh.caps != 5:
    fail("the declared defaults are not in the constructor: " + repr(fresh))
if Defaulted.read(fresh.write()) != fresh:
    fail("defaulted round trip")
fresh._s_zero()
if fresh.name != "" or fresh.token != b"" or fresh.caps != 0:
    fail("the zero form carries a default: " + repr(fresh))

holder = Holder.load(holder_data)
if holder.header.version != 2 or holder.header.sequence != 0x1234:
    fail("holder load: " + repr(holder.header))
if holder.save() != holder_data:
    fail("holder wire differs: " + holder.save().hex())
print("OK")
`

func hostedItemsFiles(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	u := loadUnit(t, hostedItemsSchema)
	closure := ir.TableClosure(u)
	if !closure["Framed"] || closure["Loose"] || closure["Defaulted"] {
		t.Fatal("the construct is wrong: the table must reach Framed and nothing else")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("the table frame refuses hosted const, reserved, align or value defaults: %v", err)
	}
	return u, files
}

// TestPythonTableHostedPacketItemsGenerate is the fast half: the table frame
// generates the unit, and the hosted classes write and check the items and
// construct the defaults. Text only; no interpreter.
func TestPythonTableHostedPacketItemsGenerate(t *testing.T) {
	t.Parallel()
	_, files := hostedItemsFiles(t)
	module := string(files["hostedframing/Core.py"])
	for _, want := range []string{
		"        _s_ri.value = 199\n        _s_serialize_bits(_s_ri, 8)\n",
		"        if _s_ri.value != 199:\n            return False\n",
		"        _s_ri.value = 0\n        _s_serialize_bits(_s_ri, 5)\n",
		"        if not _s_serialize_align():\n            return False\n",
		"        _s_ri.value = 31\n        _s_serialize_bits(_s_ri, 5)\n",
		"        if _s_ri.value != 31:\n            return False\n",
		"        self.name = \"hi\"\n        self.token = b\"ab\"\n        self.caps = 5\n",
		"Framed.descriptor = ",
	} {
		if !strings.Contains(module, want) {
			t.Errorf("the hosted module lacks\n%s", want)
		}
	}
	if strings.Contains(module, "Loose.descriptor = ") || strings.Contains(module, "Defaulted.descriptor = ") {
		t.Error("a type no table reaches carries a table descriptor")
	}
}

// TestPythonTableHostedPacketItems runs the probe under both pinned
// interpreters and mypy --strict over the package.
func TestPythonTableHostedPacketItems(t *testing.T) {
	t.Parallel()
	u, files := hostedItemsFiles(t)
	framed, loose := hostedItemsReference(t, u)
	inputs := map[string][]byte{
		"main.py":    []byte(hostedItemsProbe),
		"framed.bin": framed,
		"loose.bin":  loose,
		"holder.bin": hostedHolderWire(t, u),
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := strings.TrimSpace(string(run(t, dir, "main.py", inputs))); got != "OK" {
			t.Fatalf("hosted packet items probe:\n%s", got)
		}
	}
	for _, path := range []string{"hostedframing", "hostedframing/_schema"} {
		if out, err := pyrun.Analyze(t, dir, path); err != nil {
			t.Fatalf("mypy --strict over %s:\n%s", path, out)
		}
	}
}

// TestPythonTableHostedPacketItemsNegativeControl plants one fault at a time
// in a COPY of the hosted module, the const check, the reserved check, the
// align read and the const write, and each must turn the probe red by name.
// The align padding's own refusal is the runtime's serialize_align, held by
// the "set alignment padding" case of the positive probe.
func TestPythonTableHostedPacketItemsNegativeControl(t *testing.T) {
	t.Parallel()
	u, files := hostedItemsFiles(t)
	framed, loose := hostedItemsReference(t, u)
	inputs := map[string][]byte{
		"main.py":    []byte(hostedItemsProbe),
		"framed.bin": framed,
		"loose.bin":  loose,
		"holder.bin": hostedHolderWire(t, u),
	}
	const path = "hostedframing/Core.py"
	original := string(files[path])
	for _, c := range []struct{ name, old, new, want string }{
		{"const check", "        if _s_ri.value != 199:\n", "        if False:\n", "FAILED: a wrong const: accepted"},
		{"reserved check", "        if not _s_serialize_bits(_s_ri, 5):\n            return False\n        if _s_ri.value != 0:\n", "        if not _s_serialize_bits(_s_ri, 5):\n            return False\n        if False:\n", "FAILED: a set reserved bit: accepted"},
		// without the align read the sequence is read one bit early, so the
		// first read of the pinned bytes goes red before any padding case
		{"align read", "        if not _s_serialize_align():\n            return False\n", "", "FAILED: framed read"},
		{"const write", "        _s_ri.value = 199\n", "        _s_ri.value = 198\n", "FAILED: framed wire differs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if strings.Count(original, c.old) != 1 {
				t.Fatalf("the anchor %q is not exactly once in %s", c.old, path)
			}
			sabotaged := make(map[string][]byte, len(files))
			maps.Copy(sabotaged, files)
			sabotaged[path] = []byte(strings.Replace(original, c.old, c.new, 1))
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)
			out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
			if err == nil || !strings.Contains(out, c.want) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the planted %s fault did not go red by %q (err %v):\n%s", c.name, c.want, err, out)
			}
		})
	}
}
