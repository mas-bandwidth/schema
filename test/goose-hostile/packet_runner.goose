// THE GOOSE PORT RUNS THE SHARED HOSTILE CORPUS'S PACKET ROWS
// (docs/HOSTILE-CORPUS.md; make/goose-hostile.sh), the twin of runner.goose
// for a schema of packet types, which has no table unit:
//
//   packet_runner.goose <corpus dir> <schema path>
//
// For every manifest case of that schema whose carrier is `packet`, the
// generated Read<Root> is handed the raw input from bit 0 and the runner
// prints one line: `<name> refuse packet_refused` when the read refuses, and
// otherwise `<name> accept <hex>`, the value written back by the port's own
// Write<Root> (conformance_packet, glued by make/goose-hostile.sh). The make
// target holds the lines to the manifest; an accept by the sha256 of the
// written bytes.
import os;
import .packet_unit;

fn hex(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for x in b {
        out.push(digits[(x >> 4) as! i64]);
        out.push(digits[(x & 15) as! i64]);
    }
}

fn main() {
    let argv = args();
    if argv.len != 3 {
        write_stderr("usage: packet_runner.goose <corpus dir> <schema path>\n");
        exit(1);
    }
    var text: u8[>..] = [];
    if !read_file(str(argv[1], "/manifest.tsv"), text) {
        write_stderr("packet runner: cannot read the corpus manifest\n");
        exit(1);
    }
    var out: u8[>..] = [];
    for line in split(text, '\n') {
        if line.len == 0 || line[0] == '#' { continue; }
        let f = split(line, '\t');
        if f.len < 9 || f[2] != argv[2] { continue; }
        out.append(f[0]);
        if f[1] != "packet" {
            out.append(" absent ");
            out.append(f[1]);
            out.push('\n');
            continue;
        }
        var data: u8[>..] = [];
        if !read_file(str(argv[1], "/", f[4]), data) {
            write_stderr(str("packet runner: cannot read ", f[4], "\n"));
            exit(1);
        }
        var saved: u8[>..] = [];
        let r = conformance_packet(f[3], data, saved);
        if r == 0 {
            out.append(" accept ");
            hex(out, saved);
        } else if r == 1 {
            out.append(" refuse packet_refused");
        } else {
            out.append(" error the port's writer refused the decoded value");
        }
        out.push('\n');
    }
    write_stdout(out);
}
