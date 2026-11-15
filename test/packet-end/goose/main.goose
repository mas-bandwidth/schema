// Verify SPEC §5's read termination on the Goose packet target: the public
// one-packet read of test/packet-end/End.schema accepts the exact packet as
// the value 3 and refuses the same packet with a whole trailing byte and the
// same value with the final byte's unused bits set. The make target generates
// End.schema beside this file and runs it with `-- <vector-directory>`.
import os;
import .End;
import .serialize;

var failed = false;

fn fail(name: const u8[:], detail: const u8[:]) {
    write_stderr(str("FAILED: ", name, ": ", detail, "\n"));
    failed = true;
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
    if argv.len != 2 {
        write_stderr("usage: main.goose -- <vector-directory>\n");
        exit(2);
    }
    let dir = argv[1];

    var exact: u8[>..] = [];
    if load(dir, "exact.bin", exact) {
        let accepted, value = readPacket(exact[0..exact.len]);
        if !accepted {
            fail("exact", "refused the exact packet");
        } else if value != 3 {
            fail("exact", str("read value ", value, ", want 3"));
        }
    }

    var trailing: u8[>..] = [];
    if load(dir, "trailing.bin", trailing) {
        let accepted, value = readPacket(trailing[0..trailing.len]);
        if accepted {
            fail("trailing", str("accepted a whole trailing byte as value ", value));
        }
    }

    var padding: u8[>..] = [];
    if load(dir, "padding.bin", padding) {
        let accepted, value = readPacket(padding[0..padding.len]);
        if accepted {
            fail("padding", str("accepted set unused bits as value ", value));
        }
    }

    if failed { exit(1); }
    print("packet end Goose: exact accepted as 3, trailing and padding refused");
}
