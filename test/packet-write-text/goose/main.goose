// The Goose packet Write text gate (SPEC §5.1 row 8): the writer holds the
// used bytes of string(N) to well-formed UTF-8 and no zero byte, by a
// refusal in every build. One case per stdin line (cases.go turns the shared
// test/packet-write-text/cases.txt into name, verdict and used bytes in hex):
// each refused case must be refused by the writer; each written case must be
// written and read back byte exact. The make target generates the shared
// test/packet-write-text/Text.schema beside this file.
import os;
import .Text;
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
    if fields.len != 3 {
        fail("input", "expected three tab-separated fields");
        return;
    }
    var used: u8[>..] = [];
    hex_decode(fields[2], used);
    var v = packetwritetext::TextPacket { .. };
    if used.len > v.Text.len {
        fail(fields[0], "the case's used bytes exceed the field's bound");
        return;
    }
    for i in used.len { v.Text[i] = used[i]; }
    v.TextLength = used.len as! i32;
    var wbuf: u8[64] = [0; 64];
    var w = serialize_write_stream_init(wbuf);
    let refusal = packetwritetext::WriteTextPacket(w, v);
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
            var out = packetwritetext::TextPacket { .. };
            if !packetwritetext::ReadTextPacket(r, out) {
                fail(fields[0], "reader refused the writer's value");
            } else if out.Text[0..out.TextLength] != used[0..used.len] {
                fail(fields[0], "read back bytes other than the case's used bytes");
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
    print("packet write text Goose: ", cases, " cases OK");
}
