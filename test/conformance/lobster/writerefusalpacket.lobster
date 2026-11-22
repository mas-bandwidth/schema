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
import .PacketWriteRefusal
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

// float_arg is the argument as a float; nan, inf and -inf are spelled out.
def float_arg() -> float:
    if arg == "nan":
        return 0.0 / 0.0
    elif arg == "inf":
        return 1.0 / 0.0
    elif arg == "-inf":
        return -1.0 / 0.0
    let x, ok = string_to_float(arg)
    assert ok
    return x

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
elif op == "ratio":
    v.Ratio = float_arg()
else:
    print("probe: unknown op " + op)
    assert false

let w = serialize_write_stream(schema_rt.zeros(256))
if packetrefusal.WritePacketWriteRefusal(w, v):
    print(name + " wrote")
else:
    print(name + " answered false")
