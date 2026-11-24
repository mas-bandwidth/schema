// The write-refusal-packet probe for the Lobster leg (docs/SPEC.md §5.1,
// test/conformance/README.md). It takes ONE row of rows.txt as its arguments
// (<name> <op> <arg>), builds a fresh lawful PacketWriteRefusal, applies the
// op, and runs the generated packet Write into a 256-byte buffer. Lobster's
// packet writer refuses by assert (--runtime-no-asserts removes it) or, for a
// value wider than its bits, by the runtime's write trap (serialize_abort);
// either ends this program, so the probe prints `<name> wrote` only when the
// write returned, and writerefusalpacket_test.go reads a refusal off the exit
// status and the VM's own line, which names the generated file. The probe's
// own failures name this file, never the generated one.
import .RefusalPacket
import .serialize
import .schema_runtime

let args = command_line_arguments()
assert length(args) == 3
let name = args[0]
let op = args[1]
let arg = args[2]

def int_arg() -> int:
    let n, ok = string_to_int(arg)
    assert ok
    return n

// bytes_of is the row's hex argument as a byte string, two digits a byte
def bytes_of(text:string) -> string:
    var out = schema_rt.zeros(length(text) / 2)
    var i = 0
    while i + 1 < length(text):
        let b, ok = string_to_int(substring(text, i, 2), 16)
        assert ok
        out = write_int8_le(out, i / 2, b)
        i += 2
    return out

// dec128 is a decimal argument as the int128 storage: the two's complement
// halves, each a wrapping 64-bit int. acc = acc * 10 + d runs on 32-bit limbs
// of lo so the carry into hi is exact.
def dec128(text:string) -> schema_rt.Int128:
    var neg = false
    var i = 0
    if length(text) > 0 and text[0] == '-':
        neg = true
        i = 1
    assert i < length(text)
    var lo = 0
    var hi = 0
    while i < length(text):
        let d = text[i] - '0'
        assert d >= 0 and d <= 9
        let p0 = (lo & 0xffffffff) * 10 + d
        let p1 = ((lo >> 32) & 0xffffffff) * 10 + (p0 >> 32)
        lo = (p0 & 0xffffffff) | ((p1 & 0xffffffff) << 32)
        hi = hi * 10 + (p1 >> 32)
        i += 1
    if neg:
        lo = ~lo + 1
        hi = ~hi
        if lo == 0:
            hi += 1
    return schema_rt.Int128 { lo: lo, hi: hi }

let v = packetrefusal.NewPacketWriteRefusal()
if op == "scalar":
    v.Scalar = int_arg()
elif op == "narrow":
    v.Narrow = int_arg()
elif op == "grade":
    v.Grade = packetrefusal.Grade(int_arg())
elif op == "shot_tag":
    v.Shot.Type = packetrefusal.ShotType(int_arg())
elif op == "window_count":
    v.WindowCount = int_arg()
elif op == "text_length":
    v.TextLength = int_arg()
elif op == "text_bytes":
    let b = bytes_of(arg)
    v.Text = schema_rt.put(schema_rt.zeros(8), b)
    v.TextLength = length(b)
elif op == "text_fill":
    let n = int_arg()
    var s = ""
    for(n): s += "a"
    v.Text = schema_rt.put(schema_rt.zeros(8), s)
    v.TextLength = n
elif op == "fixed_constant":
    v.FixedConstant = int_arg()
elif op == "reserved_zero":
    v.ReservedZero = int_arg()
elif op == "trio_len":
    let t:[int] = []
    for(int_arg()): push(t, 0)
    v.Trio = t
elif op == "trio_elem":
    v.Trio[0] = int_arg()
elif op == "ints_elem":
    v.Ints[0] = int_arg()
    v.IntsCount = 1
elif op == "pick_small":
    v.Pick.Type = packetrefusal.PickType(1) // the arm `small`
    v.Pick.Small.V = int_arg()
elif op == "huge":
    v.Huge = dec128(arg)
elif op == "fx":
    v.Fx = int_arg() // fixed(16, 16) storage is the raw scaled integer
elif op == "ufx":
    v.Ufx = dec128(arg).lo // ufixed(32, 32) raw value as a uint64 bit pattern: the low lane
else:
    print("probe: unknown op " + op)
    assert false

let w = serialize_write_stream(schema_rt.zeros(256))
if packetrefusal.WritePacketWriteRefusal(w, v):
    print(name + " wrote")
else:
    print(name + " answered false")
