// The bare-integer packet Write driver for the Lobster leg (SPEC §5.1 row 11,
// docs/PACKET-WIRE-ALGORITHM.md §2.4). It reads the shared case list beside it,
// takes one case name and the case's storage value as its arguments, writes
// that value through the generated writer, then reads it back. A refused case
// is an assert failure from the generated writer (packets: --runtime-no-asserts
// removes it); a written case prints the wire's bit count and the value the
// generated reader returns.
import .BareInt
import .serialize
import .schema_runtime

def write_one(w:serialize_write_stream, typ:string, value:int):
    if typ == "I8":
        let v = bareint.NewI8()
        v.Value = value
        assert bareint.WriteI8(w, v)
    elif typ == "I16":
        let v = bareint.NewI16()
        v.Value = value
        assert bareint.WriteI16(w, v)
    elif typ == "I32":
        let v = bareint.NewI32()
        v.Value = value
        assert bareint.WriteI32(w, v)
    elif typ == "I64":
        let v = bareint.NewI64()
        v.Value = value
        assert bareint.WriteI64(w, v)
    elif typ == "U8":
        let v = bareint.NewU8()
        v.Value = value
        assert bareint.WriteU8(w, v)
    elif typ == "U16":
        let v = bareint.NewU16()
        v.Value = value
        assert bareint.WriteU16(w, v)
    elif typ == "U32":
        let v = bareint.NewU32()
        v.Value = value
        assert bareint.WriteU32(w, v)
    elif typ == "U64":
        let v = bareint.NewU64()
        v.Value = value
        assert bareint.WriteU64(w, v)
    else:
        assert false

def read_one(r:serialize_read_stream, typ:string) -> int:
    if typ == "I8":
        let v = bareint.NewI8()
        assert bareint.ReadI8(r, v)
        return v.Value
    elif typ == "I16":
        let v = bareint.NewI16()
        assert bareint.ReadI16(r, v)
        return v.Value
    elif typ == "I32":
        let v = bareint.NewI32()
        assert bareint.ReadI32(r, v)
        return v.Value
    elif typ == "I64":
        let v = bareint.NewI64()
        assert bareint.ReadI64(r, v)
        return v.Value
    elif typ == "U8":
        let v = bareint.NewU8()
        assert bareint.ReadU8(r, v)
        return v.Value
    elif typ == "U16":
        let v = bareint.NewU16()
        assert bareint.ReadU16(r, v)
        return v.Value
    elif typ == "U32":
        let v = bareint.NewU32()
        assert bareint.ReadU32(r, v)
        return v.Value
    elif typ == "U64":
        let v = bareint.NewU64()
        assert bareint.ReadU64(r, v)
        return v.Value
    assert false
    return 0

let args = command_line_arguments()
assert length(args) == 2
let wanted = args[0]
let value, value_ok = string_to_int(args[1])
assert value_ok

def words(line:string) -> [string]:
    let out:[string] = []
    for(tokenize(line, " ", " \t\r")) w:
        if length(w) > 0:
            push(out, w)
    return out

let text = read_file("cases.txt")
assert text
var typ = ""
var found = false
for(tokenize(text, "\n", " \t\r")) line:
    if not found and line != "" and substring(line, 0, 1) != "#":
        let f = words(line)
        if f[0] == wanted:
            typ = f[1]
            found = true
assert found

let w = serialize_write_stream(ensure_size("", 64, 0))
write_one(w, typ, value)
let bits = w.bits_processed()
w.flush()
let r = serialize_read_stream_padded(substring(w.buffer, 0, w.bytes_processed()))
let back = read_one(r, typ)
assert r.serialize_end()
print(string(bits) + " " + string(back))
