// The Lobster packet-end driver (SPEC §5, "Read termination";
// testdata/wire/packet-end/README.md). It reads each shared vector with the
// generated one-packet read, packetend.ReadEnd, and holds the verdict: the
// exact packet 03 is accepted as 3; the same packet with one whole byte after
// it (03 ff) is refused; the same value with the final byte's unused bits set
// (fb) is refused. A failed vector prints "FAILED: <vector>"; the last line
// is the verdict line the test requires.
import .End
import .serialize

var failures = 0
def ck(ok:bool, what:string):
    if not ok:
        failures += 1
        print("FAILED: " + what)

// read_vector reads one vector through the public read and answers whether
// it was accepted and the value it read.
def read_vector(name:string) -> bool, int:
    let data = read_file(name + ".bin")
    if not data:
        print("FAILED: " + name + " (the vector is missing)")
        failures += 1
        return false, -1
    let v = packetend.NewEnd()
    let r = serialize_read_stream_padded(data)
    let ok = packetend.ReadEnd(r, v)
    return ok, v.Value

let exact_ok, exact_value = read_vector("exact")
ck(exact_ok and exact_value == 3, "exact")
let trailing_ok, trailing_value = read_vector("trailing")
ck(not trailing_ok, "trailing (read as " + string(trailing_value) + ")")
let padding_ok, padding_value = read_vector("padding")
ck(not padding_ok, "padding (read as " + string(padding_value) + ")")
if failures == 0:
    print("packet end Lobster: exact accepted, trailing byte and set padding refused")
