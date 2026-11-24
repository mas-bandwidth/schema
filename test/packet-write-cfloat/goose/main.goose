// Test the packet Write of a ranged float32 against the shared cases.
import os;
import .PacketCFloat;
import .driverlib;
import .serialize;

var failed = false;
var failures = 0;
var cases = 0;

fn fail(name: const u8[:], detail: const u8[:]) {
    write_stderr(str("FAILED: ", name, ": ", detail, "\n"));
    failed = true;
    failures += 1;
}

fn process(line: const u8[:]) {
    let fields = split(line, '\t');
    if fields.len != 7 {
        fail("input", "expected seven tab-separated fields");
        return;
    }
    var inputBits: u8[>..] = [];
    var expectedBits: u8[>..] = [];
    var wantWire: u8[>..] = [];
    var wantBits: u8[>..] = [];
    hex_decode(fields[2], inputBits);
    hex_decode(fields[3], expectedBits);
    hex_decode(fields[5], wantWire);
    // the bits the write emits before the flush, one byte; empty when unpinned
    hex_decode(fields[6], wantBits);
    if inputBits.len != 4 || expectedBits.len != 4 {
        fail(fields[0], "expected four bytes of float32 bits");
        return;
    }
    let ratio = fields[4] == "RatioEndpoint";
    if !ratio && fields[4] != "CFloat" {
        fail(fields[0], "unknown type");
        return;
    }
    let bits: u32 = (inputBits[0] as! u32) | ((inputBits[1] as! u32) << 8)
        | ((inputBits[2] as! u32) << 16) | ((inputBits[3] as! u32) << 24);
    let want: u32 = (expectedBits[0] as! u32) | ((expectedBits[1] as! u32) << 8)
        | ((expectedBits[2] as! u32) << 16) | ((expectedBits[3] as! u32) << 24);
    let value = serialize_f32_from_bits(bits);
    var v = cfloat::CFloat { X: value };
    var rv = cfloat::RatioEndpoint { Ratio: value };
    var wbuf: u8[64] = [0; 64];
    var w = serialize_write_stream_init(wbuf);
    var refusal = SERIALIZE_ERROR_NONE;
    if ratio {
        refusal = cfloat::WriteRatioEndpoint(w, rv);
    } else {
        refusal = cfloat::WriteCFloat(w, v);
    }
    cases += 1;

    if fields[1] == "refused" {
        if refusal == SERIALIZE_ERROR_NONE { fail(fields[0], "writer accepted a refused value"); }
    } else if fields[1] == "written" {
        if refusal != SERIALIZE_ERROR_NONE {
            fail(fields[0], "writer refused a lawful value");
        } else if wantBits.len != 0 && serialize_bits_processed(w) != (wantBits[0] as! i64) {
            fail(fields[0], "the write emitted other than the case's bits before the flush");
        } else {
            serialize_flush(w);
            let n = serialize_bytes_processed(w);
            if wantWire.len != 0 {
                var same = n == wantWire.len;
                var i: i64 = 0;
                while same && i < n {
                    if wbuf[i] != wantWire[i] { same = false; }
                    i += 1;
                }
                if !same { fail(fields[0], "the written bytes are not the case's wire"); }
            }
            var rbuf: u8[72] = [0; 72];
            var r = serialize_read_stream_init_padded(rbuf, wbuf[0..n]);
            var got: u32 = 0;
            var read = false;
            if ratio {
                var out = cfloat::RatioEndpoint { .. };
                read = cfloat::ReadRatioEndpoint(r, out);
                got = serialize_f32_bits(out.Ratio);
            } else {
                var out = cfloat::CFloat { .. };
                read = cfloat::ReadCFloat(r, out);
                got = serialize_f32_bits(out.X);
            }
            if !read {
                fail(fields[0], "reader refused the writer's value");
            } else if got != want {
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
        let before = failures;
        process(line);
        // the case's answer tools/casegate reads (the FAILED line says why)
        let name = split(line, '\t')[0];
        if failures == before { print("case ", name, " ok"); }
        else { print("case ", name, " FAILED: see the FAILED line above"); }
        line.clear();
    }
    if cases == 0 { fail("input", "the shared case list was empty"); }
    // A summary; tools/casegate holds every case of the file to an answer.
    print("packet compressed float Goose: ran ", cases, ", failed ", failures);
    if failed { exit(1); }
}
