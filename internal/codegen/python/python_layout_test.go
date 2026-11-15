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
// Refused with the exact reason, and the read stops at the bit offset of the
// construct the damage or the cut sits in. Each failure prints its own marker,
// naming the case, for the negative control.
//
// The bit offsets beside each case are the Go reference writer's for the same
// schema (the compiler's Go packet code, written and read back through
// serialize.go): the same bytes, and a refusal of the same reason at the same
// construct. The Go reader coalesces const and on into one 9-bit read, so it
// reports a cut or a wrong const there at that group's start; the Python
// reader reads them one by one and stops at the construct's own offset.
//
// No byte prefix ends inside align padding. The padding runs from the read
// position to the next byte boundary, and a byte-built stream holds a whole
// number of bytes, so any prefix that reaches the padding's first bit holds
// all of it: a cut before the padding fails in the construct the cut is in,
// and a cut at or after it fails in the construct after the padding. A stream
// therefore never ends inside the padding, in Go or in Python, and the align
// read's refusal is reached only by set padding bits: the nonzero_padding
// cases below are the align's hostile inputs.
const layoutProbe = pyrun.ImportPrelude + `
import sys

from layoutdemo import InvalidValue, Layout, Nested, Refused, Wide
from layoutdemo._schema.serialize import ReadStream


def fail(message):
    print("FAILED: " + message)
    sys.exit(1)


def refused(name, data, reason, at, kind=Layout):
    # data must be refused for reason, and the reader must stop at bit at: the
    # first bit of the construct the cut or the damage sits in, so a refusal
    # for the right reason in the wrong place is red too.
    try:
        kind.read(data)
    except Refused as exc:
        if exc.reason != reason:
            fail(name + ": refused as " + exc.reason + ", want " + reason)
    else:
        fail(name + ": accepted")
    stream = ReadStream(data)
    if kind()._s_read(stream):
        if stream.serialize_end():
            fail(name + ": the reader accepted it")
    if stream.bits_processed() != at:
        fail(name + ": stopped at bit " + str(stream.bits_processed()) + ", want bit " + str(at))


def truncations(kind, data, where):
    # Every proper prefix of a pinned wire is cut and must be refused past_end.
    # where maps each prefix length to the construct the cut stops inside and
    # that construct's first bit; every length below len(data) is named.
    if sorted(where) != list(range(len(data))):
        fail(kind.__name__ + " truncations name lengths " + repr(sorted(where)) + ", want every prefix below " + str(len(data)))
    for length in range(len(data)):
        construct, at = where[length]
        name = kind.__name__ + " truncated to " + str(length) + " bytes (" + str(8 * length) + " bits), inside " + construct
        refused(name, data[:length], "past_end", at, kind)


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

# Layout, on=True, the Go reference's bits:
#   const(0xAB, 8)  bits 0-7    byte 0
#   on              bit  8      byte 1 bit 0
#   yes_value       bits 9-16   byte 1 bits 1-7, byte 2 bit 0
#   reserved(5)     bits 17-21  byte 2 bits 1-5
#   align padding   bits 22-23  byte 2 bits 6-7
#   tail            bits 24-39  bytes 3-4
pinned_on = bytes.fromhex("ab25005634")

# A wrong const (byte 0 is the whole const): the const's check refuses it
# after its 8 bits are read, so the reader stops at bit 8. The const and
# reserved checks refuse well-formed bits, which the family names rejected.
bad_const = bytearray(pinned_on)
bad_const[0] = 0xAC
refused("a wrong const", bytes(bad_const), "rejected", 8)

# Bit 17 is reserved(5)'s first bit; the check refuses after bits 17-21.
bad_reserved = bytearray(pinned_on)
bad_reserved[2] |= 0x02
refused("a set reserved bit", bytes(bad_reserved), "rejected", 22)

# Bit 22 is the align padding's first bit; the align reads bits 22-23 and
# refuses them nonzero, stopping at the boundary, bit 24, as Go does.
bad_padding = bytearray(pinned_on)
bad_padding[2] |= 0x40
refused("set alignment padding", bytes(bad_padding), "nonzero_padding", 24)

# Each prefix's bits end at 8 * length; the read fails in the first
# construct that runs past them (the Go reference: bits 0, 0, 9, 24, 24).
truncations(Layout, pinned_on, {
    0: ("the const, bits 0-7", 0),
    1: ("on, bit 8", 8),
    2: ("yes_value, bits 9-16", 9),
    3: ("tail, bits 24-39 (the padding, bits 22-23, is whole)", 24),
    4: ("tail, bits 24-39", 24),
})
refused("a trailing byte", pinned_on + b"\x00", "trailing_data", 40)

# Wide, the Go reference's bits:
#   const(0x123456789, 36)  bits 0-35   bytes 0-3, byte 4 bits 0-3
#   reserved(40)            bits 36-75  byte 4 bits 4-7, bytes 5-8, byte 9 bits 0-3
#   align padding           bits 76-79  byte 9 bits 4-7
#   tail                    bits 80-87  byte 10
pinned_wide = bytes.fromhex("896745230100000000005a")
truncations(Wide, pinned_wide, {
    0: ("the const, bits 0-35", 0),
    1: ("the const, bits 0-35", 0),
    2: ("the const, bits 0-35", 0),
    3: ("the const, bits 0-35", 0),
    4: ("the const, bits 0-35", 0),
    5: ("the reserved bits, bits 36-75", 36),
    6: ("the reserved bits, bits 36-75", 36),
    7: ("the reserved bits, bits 36-75", 36),
    8: ("the reserved bits, bits 36-75", 36),
    9: ("the reserved bits, bits 36-75", 36),
    10: ("tail, bits 80-87 (the padding, bits 76-79, is whole)", 80),
})
refused("a Wide trailing byte", pinned_wide + b"\x00", "trailing_data", 88, Wide)
wide_bad = bytearray(pinned_wide)
wide_bad[0] = 0x88
refused("a wrong wide const", bytes(wide_bad), "rejected", 36, Wide)
# Bit 56 (byte 7 bit 0) is inside reserved(40), bits 36-75.
wide_reserved = bytearray(pinned_wide)
wide_reserved[7] = 0x01
refused("a set wide reserved bit", bytes(wide_reserved), "rejected", 76, Wide)
# Bit 76 (byte 9 bit 4) is the padding's first bit, bits 76-79.
wide_padding = bytearray(pinned_wide)
wide_padding[9] = 0x10
refused("set wide alignment padding", bytes(wide_padding), "nonzero_padding", 80, Wide)

on = Layout()
on.yes_value = 0x12
on_data = on.write()
if on_data.hex() != "ab25005634":
    fail("on=True wire: " + on_data.hex())
back = Layout.read(on_data)
if back.on is not True or back.yes_value != 0x12 or back.no_value != 0 or back.inverted != 0 or back.tail != 0x3456:
    fail("on=True read or SPEC §5 untaken zero: " + repr(back))

# Layout, on=False, the Go reference's bits: as on=True with no_value at
# bits 9-16 in yes_value's place, and inverted after tail at bits 40-47.
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
    0: ("the const, bits 0-7", 0),
    1: ("on, bit 8", 8),
    2: ("no_value, bits 9-16", 9),
    3: ("tail, bits 24-39 (the padding, bits 22-23, is whole)", 24),
    4: ("tail, bits 24-39", 24),
    5: ("inverted, bits 40-47", 40),
})

wide = Wide()
wide_data = wide.write()
if wide_data.hex() != "896745230100000000005a":
    fail("const/reserved/align wider than 32 bits: " + wide_data.hex())
if Wide.read(wide_data) != wide:
    fail("Wide round-trip")

# Nested, the Go reference's bits: lead 0-7; the child Layout (on=False)
# from bit 8: const 8-15, on 16, no_value 17-24, reserved 25-29, padding
# 30-31, tail 32-47, inverted 48-55; trail 56-63.
nested = Nested()
nested.lead = 9
nested.child.on = False
nested.child.yes_value = 0
nested.child.no_value = 0x44
nested.child.inverted = 0x55
nested_data = nested.write()
if nested_data.hex() != "09ab880056345502":
    fail("Nested wire: " + nested_data.hex())
if Nested.read(nested_data) != nested:
    fail("Nested round-trip")
truncations(Nested, nested_data, {
    0: ("lead, bits 0-7", 0),
    1: ("the child's const, bits 8-15", 8),
    2: ("the child's on, bit 16", 16),
    3: ("the child's no_value, bits 17-24", 17),
    4: ("the child's tail, bits 32-47 (the padding, bits 30-31, is whole)", 32),
    5: ("the child's tail, bits 32-47", 32),
    6: ("the child's inverted, bits 48-55", 48),
    7: ("trail, bits 56-63", 56),
})
nested_padding = bytearray(nested_data)
nested_padding[3] |= 0x40
refused("set child alignment padding", bytes(nested_padding), "nonzero_padding", 32, Nested)

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
			want: "FAILED: a wrong const: accepted",
		},
		{
			name: "reserved",
			old:  "if _s_ri.value != 0:",
			new:  "if False:",
			want: "FAILED: a set reserved bit: accepted",
		},
		{
			name: "align",
			old:  "        if not _s_serialize_align():\n            return False\n",
			new:  "",
			want: "FAILED: set alignment padding: ",
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
