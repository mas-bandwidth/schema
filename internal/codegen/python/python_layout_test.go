package python

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// layoutSchema exercises the storage-less packet wire constructs and the
// if/else branch beside them: const(Value, Bits), reserved(Bits), align, a
// taken and an untaken side with their declared defaults, and a negated
// branch. The two types pin a wire of one byte and one of more than 32 bits.
const layoutSchema = `package layoutdemo

type Layout
{
    const(0xAB, 8)
    on bool = true
    if on
    {
        yes_value uint8 = 5
    }
    else
    {
        no_value uint8 = 6
    }
    reserved(5)
    align
    tail uint16 = 0x3456
    if !on
    {
        inverted uint8 = 7
    }
}

type Wide
{
    const(0x123456789, 36)
    reserved(40)
    align
    tail uint8 = 0x5a
}

type Nested
{
    lead uint8 = 1
    child Layout
    trail uint8 = 2
}
`

// layoutUnit builds the layout fixture in a temp directory and returns the
// generated tree keyed by path relative to the output directory.
func layoutUnit(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Layout.schema"), []byte(layoutSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	if blocker := rootBlocker(u, "Layout"); blocker != "" {
		t.Fatalf("Layout is still on the golden harness's skip list: %s", blocker)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Layout: %v", err)
	}
	return u.Package, files
}

// layoutProbe is the Python side of the layout gate: every construct reads and
// writes its pinned bytes, a successful read zeroes the untaken side (SPEC
// §5), an out-of-contract write raises InvalidValue, and a wrong const, a set
// reserved bit, set padding, a truncated input and a trailing byte each raise
// Refused. Each failure prints its own marker for the negative control.
const layoutProbe = pyrun.ImportPrelude + `
import sys

from layoutdemo import InvalidValue, Layout, Nested, Refused, Wide


def fail(message):
    print("FAILED: " + message)
    sys.exit(1)


def refused(name, data, kind=Layout):
    try:
        kind.read(data)
    except Refused:
        return
    fail("accepted " + name)


def truncations(kind, data, where):
    # Every proper prefix of a pinned wire must be refused. where maps each
    # prefix length to the construct the cut stops inside, so a failure names
    # it; every length below len(data) is cut, named or not.
    for length in range(len(data)):
        refused(kind.__name__ + " truncated to " + str(length) + " bytes, inside " + where.get(length, "the wire"), data[:length], kind)


def invalid_layout(name, mutate):
    value = Layout()
    mutate(value)
    try:
        value.write()
    except InvalidValue:
        return
    fail("accepted an invalid " + name)


fresh = Layout()
if fresh.on is not True or fresh.yes_value != 5 or fresh.no_value != 6 or fresh.tail != 0x3456 or fresh.inverted != 7:
    fail("the declared defaults are not in the constructor: " + repr(fresh))

pinned_on = bytes.fromhex("ab25005634")

bad_const = bytearray(pinned_on)
bad_const[0] = 0xAC
refused("a wrong const", bytes(bad_const))

bad_reserved = bytearray(pinned_on)
bad_reserved[2] |= 0x02
refused("a set reserved bit", bytes(bad_reserved))

bad_padding = bytearray(pinned_on)
bad_padding[2] |= 0x40
refused("set alignment padding", bytes(bad_padding))

# Layout on=True: byte 0 is the const; byte 1 is on and yes_value's low seven
# bits; byte 2 is yes_value's top bit, reserved(5) and two bits of align
# padding; bytes 3 and 4 are tail. Padding runs to a byte boundary, so a cut
# that stops in the padding is the cut that stops in its byte, which also
# holds the reserved bits.
truncations(Layout, pinned_on, {
    0: "the const",
    1: "yes_value",
    2: "the reserved bits and the align padding",
    3: "tail",
    4: "tail",
})
refused("a trailing byte", pinned_on + b"\x00")

# Wide: const(0x123456789, 36) fills bytes 0-3 and the low half of byte 4;
# reserved(40) runs from the top half of byte 4 to the low half of byte 9;
# the align padding is the top half of byte 9; byte 10 is tail.
pinned_wide = bytes.fromhex("896745230100000000005a")
truncations(Wide, pinned_wide, {
    0: "the const",
    1: "the const",
    2: "the const",
    3: "the const",
    4: "the const",
    5: "the reserved bits",
    6: "the reserved bits",
    7: "the reserved bits",
    8: "the reserved bits",
    9: "the reserved bits and the align padding",
    10: "tail",
})
refused("a Wide trailing byte", pinned_wide + b"\x00", Wide)
wide_bad = bytearray(pinned_wide)
wide_bad[0] = 0x88
refused("a wrong wide const", bytes(wide_bad), Wide)
wide_reserved = bytearray(pinned_wide)
wide_reserved[7] = 0x01
refused("a set wide reserved bit", bytes(wide_reserved), Wide)
wide_padding = bytearray(pinned_wide)
wide_padding[9] = 0x10
refused("set wide alignment padding", bytes(wide_padding), Wide)

on = Layout()
on.yes_value = 0x12
on_data = on.write()
if on_data.hex() != "ab25005634":
    fail("on=True wire: " + on_data.hex())
back = Layout.read(on_data)
if back.on is not True or back.yes_value != 0x12 or back.no_value != 0 or back.inverted != 0 or back.tail != 0x3456:
    fail("on=True read or SPEC §5 untaken zero: " + repr(back))

off = Layout()
off.on = False
off.no_value = 0x34
off.inverted = 0x78
off_data = off.write()
if off_data.hex() != "ab6800563478":
    fail("on=False wire: " + off_data.hex())
back_off = Layout.read(off_data)
if back_off.on is not False or back_off.no_value != 0x34 or back_off.yes_value != 0 or back_off.inverted != 0x78 or back_off.tail != 0x3456:
    fail("on=False read or SPEC §5 untaken zero: " + repr(back_off))
truncations(Layout, off_data, {
    0: "the const",
    1: "no_value",
    2: "the reserved bits and the align padding",
    3: "tail",
    4: "tail",
    5: "inverted",
})

wide = Wide()
wide_data = wide.write()
if wide_data.hex() != "896745230100000000005a":
    fail("const/reserved/align wider than 32 bits: " + wide_data.hex())
if Wide.read(wide_data) != wide:
    fail("Wide round-trip")

nested = Nested()
nested.lead = 9
nested.child.on = False
nested.child.yes_value = 0
nested.child.no_value = 0x44
nested.child.inverted = 0x55
nested_data = nested.write()
if Nested.read(nested_data) != nested:
    fail("Nested round-trip")
truncations(Nested, nested_data, {0: "lead", 1: "the child's const"})

invalid_layout("on=1", lambda value: setattr(value, "on", 1))
invalid_layout("on=0", lambda value: setattr(value, "on", 0))
invalid_layout("yes_value=256", lambda value: setattr(value, "yes_value", 256))
invalid_layout("tail=65536", lambda value: setattr(value, "tail", 65536))

off_no = Layout()
off_no.on = False
off_no.no_value = 256
try:
    off_no.write()
except InvalidValue:
    pass
else:
    fail("accepted an invalid no_value")

off_inverted = Layout()
off_inverted.on = False
off_inverted.inverted = -1
try:
    off_inverted.write()
except InvalidValue:
    pass
else:
    fail("accepted an invalid inverted")

print("ok")
`

// TestPythonPacketLayout is the packet layout gate: const, reserved and align
// on the wire, beside if/else, over every pinned interpreter; every field's
// declared default in the constructor; the untaken side zeroed by a read
// (SPEC §5); InvalidValue for every out-of-contract write; Refused for every
// truncated or hostile input (every proper prefix of each pinned wire); mypy
// --strict over the generated package.
//
// The gate proves the constructs on this fixture only. The goldens that use
// them (probe_header, bench_packet, bench_mixed, testdata) also use kinds
// other packet cards carry, so they stay on the golden harness's skip list,
// naming that kind, and their round-trip is recorded by the packet close.
func TestPythonPacketLayout(t *testing.T) {
	t.Parallel()
	pkg, files := layoutUnit(t)
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(layoutProbe)})))
			if got != "ok" {
				t.Fatalf("packet layout under %s:\n%s", run.name, got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, err := pyrun.Analyze(t, dir, pkg)
	if err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("packet layout mypy --strict: %v\n%s", err, out)
	}
}

// TestPythonPacketLayoutNegativeControl plants one fault at a time in a copy
// of the generated module: the const comparison, the reserved comparison, the
// alignment's refusal, and the untaken side's zeroing. Each must turn the
// probe red on its own marker.
func TestPythonPacketLayoutNegativeControl(t *testing.T) {
	t.Parallel()
	pkg, files := layoutUnit(t)
	module := pkg + "/Layout.py"
	original := string(files[module])
	cases := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "const",
			old:  "if _s_ri.value != 171:",
			new:  "if False:",
			want: "FAILED: accepted a wrong const",
		},
		{
			name: "reserved",
			old:  "if _s_ri.value != 0:",
			new:  "if False:",
			want: "FAILED: accepted a set reserved bit",
		},
		{
			name: "align",
			old:  "        if not _s_serialize_align():\n            return False\n",
			new:  "",
			want: "FAILED: accepted set alignment padding",
		},
		{
			name: "untaken zero",
			old:  "            self.yes_value = 0\n",
			new:  "            pass\n",
			want: "FAILED: on=False read or SPEC §5 untaken zero",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(original, c.old) {
				t.Fatalf("the generated Layout reader has no %q:\n%s", c.old, original)
			}
			copied := make(map[string][]byte, len(files))
			for rel, data := range files {
				copied[rel] = data
			}
			copied[module] = []byte(strings.Replace(original, c.old, c.new, 1))
			dir := t.TempDir()
			writeTree(t, dir, copied)
			out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(layoutProbe)})
			if err == nil || !strings.Contains(out, c.want) {
				t.Fatalf("the planted %s fault did not go red on %q (err %v):\n%s", c.name, c.want, err, out)
			}
		})
	}
}
