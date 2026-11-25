// THE ANNOUNCEMENT WRITER ALONE (test/conformance/goose/driver, "THE
// ANNOUNCEMENT-ONLY BATCH"). A unit whose every row is an `announce ... write`
// row needs nothing of the unit but its announcement. main.goose's glue
// imports every module of the unit, and for the 4096-entry announcement units
// that is about twenty-five seconds of typecheck per unit (the 3.3 MB
// AnnounceTable), all of it for one constant. This program imports only the
// module that holds the announcement, through conformance_announce.goose,
// whose conformance_announce_write the shell driver writes with the very
// lines main.goose's glue uses (announce_write_glue), and answers the
// surface's write rows exactly as main.goose's surface_announce does:
//
//   announce.goose batch <manifest> <unit> announce
//
// It prints the section between "#surface announce" and "#done announce",
// as main.goose's batch does. A row of any other mode is not this program's:
// it stops before "#done", so the driver answers the surface by main.goose's
// single run and names the fallback.
import os;
import .conformance_announce;

fn hex_line(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for x in b {
        out.push(digits[(x >> 4) as! i64]);
        out.push(digits[(x & 15) as! i64]);
    }
}

// answer is main.goose's: the case name, a space, the bytes as lowercase hex
fn answer(name: const u8[:], b: const u8[:]) {
    var line: u8[>..] = [];
    line.append(name);
    line.push(' ');
    hex_line(line, b);
    line.push('\n');
    write_stdout(line);
}

fn main() {
    let argv = args();
    if argv.len != 5 || argv[1] != "batch" || argv[4] != "announce" {
        write_stderr("usage: announce.goose batch <manifest> <unit> announce\n");
        exit(1);
    }
    var text: u8[>..] = [];
    if !read_file(argv[2], text) {
        write_stderr(str("driver: cannot read ", argv[2], "\n"));
        exit(1);
    }
    let unit = argv[3];
    write_stdout("#surface announce\n");
    for line in split(text, '\n') {
        let f = words(line);
        if f.len < 6 || f[0] != "announce" || f[2] != unit { continue; }
        if f[4] != "write" {
            write_stderr(str("announce.goose: ", f[1], ": mode ", f[4], " is main.goose's, not this program's\n"));
            exit(1);
        }
        var out: u8[>..] = [];
        if conformance_announce_write(out) < 0 {
            write_stderr(str("announce.goose: ", f[1], ": the unit has no announcement\n"));
            exit(1);
        }
        answer(f[1], out);
    }
    write_stdout("#done announce\n");
}
