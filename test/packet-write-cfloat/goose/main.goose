// Test the packet Write of a ranged float32 against the shared cases.
import os;
import .CFloat;
import .driverlib;
import .serialize;

var failed = false;
var cases = 0;

fn fail(name: const u8[:], detail: const u8[:]) {
    write_stderr(str("FAILED: ", name, ": ", detail, "\n"));
    failed = true;
}

fn process(line: const u8[:]) {
    let fields = split(line, '\t');
    if fields.len != 4 {
        fail("input", "expected four tab-separated fields");
        return;
    }
    var inputBits: u8[>..] = [];
    var expectedBits: u8[>..] = [];
    hex_decode(fields[2], inputBits);
    hex_decode(fields[3], expectedBits);
    if inputBits.len != 4 || expectedBits.len != 4 {
        fail(fields[0], "expected four bytes of float32 bits");
        return;
    }
    let bits: u32 = (inputBits[0] as! u32) | ((inputBits[1] as! u32) << 8)
        | ((inputBits[2] as! u32) << 16) | ((inputBits[3] as! u32) << 24);
    let want: u32 = (expectedBits[0] as! u32) | ((expectedBits[1] as! u32) << 8)
        | ((expectedBits[2] as! u32) << 16) | ((expectedBits[3] as! u32) << 24);
    let value = serialize_f32_from_bits(bits);
    var v = cfloat::CFloat { X: value };
    var wbuf: u8[64] = [0; 64];
    var w = serialize_write_stream_init(wbuf);
    let refusal = cfloat::WriteCFloat(w, v);
    cases += 1;

    if fields[1] == "refused" {
        if refusal == SERIALIZE_ERROR_NONE { fail(fields[0], "writer accepted a refused value"); }
    } else if fields[1] == "written" {
        if refusal != SERIALIZE_ERROR_NONE {
            fail(fields[0], "writer refused a lawful value");
        } else {
            serialize_flush(w);
            let n = serialize_bytes_processed(w);
            var rbuf: u8[72] = [0; 72];
            var r = serialize_read_stream_init_padded(rbuf, wbuf[0..n]);
            var out = cfloat::CFloat { .. };
            if !cfloat::ReadCFloat(r, out) {
                fail(fields[0], "reader refused the writer's value");
            } else if serialize_f32_bits(out.X) != want {
                fail(fields[0], "read back a value other than the case's expected value");
            }
        }
    } else {
        fail(fields[0], "unknown verdict");
    }
}

fn main() {
    var line: u8[>..] = [];
    while read_line(line) {
        process(line);
        line.clear();
    }
    if cases == 0 { fail("input", "the shared case list was empty"); }
    if failed { exit(1); }
    print("packet compressed float Goose: ", cases, " cases OK");
}
