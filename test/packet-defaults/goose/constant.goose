// The constant-default pins on the Goose packet target: a fresh Value (its
// defaults come from constants) writes exactly the pinned bytes and bit
// count, and the pin reads back over zeroed storage to a fresh Value. Each
// schema of test/conformance/packet/testdata/constant-defaults declares
// package arithmetic, so each is generated into its own directory and this
// file runs once per directory:
//   goose run constant.goose -- <golden-directory> <pin name>
// The last stdout line is the count tools/mustrun reads.
import os;
import .Defaults;
import .serialize;

var failures = 0;
var ran = 0;

fn check(ok: bool, name: const u8[:]) {
    ran += 1;
    if !ok {
        write_stderr(str("FAILED: ", name, "\n"));
        failures += 1;
    }
}

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}

fn parseBits(text: const u8[:]) -> i64, bool {
    var n: i64 = 0;
    var any = false;
    for c in text {
        if c >= '0' && c <= '9' { n = n * 10 + (c - '0'); any = true; }
        else if c != '\n' && c != ' ' && c != '\r' { return 0, false; }
    }
    return n, any;
}

fn main() {
    let argv = args();
    if argv.len != 3 {
        write_stderr("usage: constant.goose -- <golden-directory> <pin name>\n");
        exit(2);
    }
    let dir = argv[1];
    let name = argv[2];
    var expected: u8[>..] = [];
    check(read_file(str(dir, "/", name, ".bin"), expected), str(name, ": read C++ byte golden"));
    var bitText: u8[>..] = [];
    check(read_file(str(dir, "/", name, ".bits"), bitText), str(name, ": read C++ bit golden"));
    let expectedBits, parsed = parseBits(bitText);
    check(parsed, str(name, ": parse C++ bit golden"));

    var wbuf: u8[4096] = [0; 4096];
    var w = serialize_write_stream_init(wbuf);
    var fresh = arithmetic::NewValue();
    let refusal = arithmetic::WriteValue(w, fresh);
    check(refusal == SERIALIZE_ERROR_NONE, str(name, ": write: ", serialize_refusal_name(refusal)));
    let wbits = serialize_bits_processed(w);
    serialize_flush(w);
    let wlen = serialize_bytes_processed(w);
    check(same(wbuf[0..wlen], expected), str(name, ": C++ golden bytes"));
    check(wbits == expectedBits, str(name, ": C++ golden bits"));

    // decode the oracle's bytes over zeroed storage
    var rbuf: u8[4104] = [0; 4104];
    var r = serialize_read_stream_init_padded(rbuf, expected);
    var got = arithmetic::NewValue();
    arithmetic::ZeroValue(got);
    check(arithmetic::ReadValue(r, got), str(name, ": read: ", serialize_refusal_name(serialize_error(r))));
    check(serialize_bits_processed(r) == (expectedBits + 7) / 8 * 8, str(name, ": read consumed bits"));
    check(got == arithmetic::NewValue(), str(name, ": read values"));
    print("packet defaults Goose constant ", name, ": ran ", ran, ", failed ", failures);
    if failures > 0 || ran == 0 { exit(1); }
}
