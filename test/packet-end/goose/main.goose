// Verify SPEC §5's read termination on the Goose packet target: the public
// one-packet read of test/packet-end/End.schema accepts the exact packet as
// the value 3 and refuses the same packet with a whole trailing byte and the
// same value with the final byte's unused bits set. The make target generates
// End.schema beside this file and runs it with `-- <vector-directory>`.
import os;
import .End;
import .serialize;

var failed = false;
var failures = 0;
var ran = 0;

fn fail(name: const u8[:], detail: const u8[:]) {
    write_stderr(str("FAILED: ", name, ": ", detail, "\n"));
    failed = true;
    failures += 1;
}

var rbuf: u8[4104] = [0; 4104];

// readPacket reads one whole packet through the generated public read and
// reports whether it was accepted and the value it carried.
fn readPacket(wire: const u8[:]) -> bool, u8 {
    var value = packetend::NewEnd();
    var r = serialize_read_stream_init_padded(rbuf, wire);
    let accepted = packetend::ReadEnd(r, value);
    return accepted, value.Value;
}

fn load(dir: const u8[:], name: const u8[:], wire: u8[>..]&) -> bool {
    if !read_file(str(dir, "/", name), wire) {
        fail(name, "read the shared vector");
        return false;
    }
    return true;
}

fn main() {
    let argv = args();
    if argv.len < 2 {
        write_stderr("usage: main.goose -- <vector-directory> <vector .bin file>...\n");
        exit(2);
    }
    let dir = argv[1];

    // The arguments after the directory are its own listing of *.bin (the
    // make recipe passes it): a vector on disk this driver does not read, a
    // vector it reads that is not listed, and an empty listing each fail.
    var listedExact = false;
    var listedTrailing = false;
    var listedPadding = false;
    for i in argv.len {
        if i < 2 { continue; }
        if argv[i] == "exact.bin" { listedExact = true; }
        else if argv[i] == "trailing.bin" { listedTrailing = true; }
        else if argv[i] == "padding.bin" { listedPadding = true; }
        else { fail(argv[i], "a vector in the directory that this driver does not read"); }
    }
    if argv.len == 2 { fail("listing", "the vector directory listing is empty"); }
    if !listedExact { fail("exact.bin", "the vector is not in the directory listing"); }
    if !listedTrailing { fail("trailing.bin", "the vector is not in the directory listing"); }
    if !listedPadding { fail("padding.bin", "the vector is not in the directory listing"); }

    ran += 1;
    var exact: u8[>..] = [];
    if load(dir, "exact.bin", exact) {
        let accepted, value = readPacket(exact[0..exact.len]);
        if !accepted {
            fail("exact", "refused the exact packet");
        } else if value != 3 {
            fail("exact", str("read value ", value, ", want 3"));
        }
    }

    ran += 1;
    var trailing: u8[>..] = [];
    if load(dir, "trailing.bin", trailing) {
        let accepted, value = readPacket(trailing[0..trailing.len]);
        if accepted {
            fail("trailing", str("accepted a whole trailing byte as value ", value));
        }
    }

    ran += 1;
    var padding: u8[>..] = [];
    if load(dir, "padding.bin", padding) {
        let accepted, value = readPacket(padding[0..padding.len]);
        if accepted {
            fail("padding", str("accepted set unused bits as value ", value));
        }
    }

    // The last stdout line is the count tools/mustrun reads.
    print("packet end Goose: ran ", ran, ", failed ", failures);
    if failed || ran == 0 { exit(1); }
}
