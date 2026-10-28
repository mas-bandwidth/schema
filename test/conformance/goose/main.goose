// THE GOOSE CONFORMANCE DRIVER (test/conformance/README.md; docs/PORT-GOOSE.md,
// THE HARNESS). The shell driver beside this file runs it under the pinned
// compiler, once for `list` and once PER UNIT for a surface:
//
//   main.goose list <manifest>
//   main.goose <surface> <manifest> <unit>
//
// `list` prints the surfaces this leg answers, one per line. A surface run
// prints one line per case of <unit> on stdout: the case name, a space, and
// the answer's bytes as lowercase hex, or the word `absent` followed by an
// optional reason (the shell driver writes `<case>.absent` holding the
// reason). A surface this leg does not answer exits 2, which the harness
// prints as ABSENT. The working directory is the repository root, so every
// path in the manifest is used as it is written.
//
// WHY ONE RUN PER UNIT. Every generated unit carries its own copy of the
// runtime (schema_rt), so two units cannot be one Goose program. The shell
// driver copies this file into the unit's generated directory beside a glue
// module, conformance_unit.goose, that imports the unit's table modules and
// names its registry: `fn conformance_unit() -> schema_rt::LoadUnit`.
//
// THE SURFACES (test/conformance/harness/run.go). The table wire, form 1
// (docs/SPEC-TABLES.md §3, §3.1, §4; docs/PORT-GOOSE.md, THE TABLE WIRE):
//   wire         load every instance and save it back, byte for byte
//   report       load every report row, print §4's counters and verdict
// The other surfaces read forms other cards carry (the message form, JSON,
// the cook, the block form) and are ABSENT here until they land.
import os;
import .conformance_unit;
import .wire_load;
import .wire_save;
import .wire_retain;

fn hex_line(out: u8[>..]&, b: const u8[:]) {
    let digits = "0123456789abcdef";
    for x in b {
        out.push(digits[(x >> 4) as! i64]);
        out.push(digits[(x & 15) as! i64]);
    }
}

fn answer(name: const u8[:], b: const u8[:]) {
    var line: u8[>..] = [];
    line.append(name);
    line.push(' ');
    hex_line(line, b);
    line.push('\n');
    write_stdout(line);
}

fn absent(name: const u8[:], reason: const u8[:]) {
    var line: u8[>..] = [];
    line.append(name);
    line.append(" absent");
    if reason.len > 0 {
        line.push(' ');
        line.append(reason);
    }
    line.push('\n');
    write_stdout(line);
}

fn bool_word(b: bool) -> const u8[:] { if b { "true" } else { "false" } }

// load_file reads a wire, aborting the run on a file the manifest names and
// the tree does not hold (the harness prints the stderr line).
fn load_file(path: const u8[:], out: u8[>..]&) {
    if !read_file(path, out) {
        write_stderr(str("driver: cannot read ", path, "\n"));
        exit(1);
    }
}

// surface_report: §4's counters and verdict, `u,k,w,c,d,malformed,verdict`.
fn surface_report(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "report" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        let text = str(report.unknown, ",", report.kind_mismatch, ",", report.widened, ",", report.clamped, ",",
                       report.duplicate, ",", bool_word(report.malformed), ",",
                       if report.refused { "refused" } else { "read" }, "\n");
        answer(f[1], text);
    }
}

// surface_wire: every instance loaded and saved back (§3). A save the writer
// refuses by name (a blob two slots name, §2.5) is a named absence.
fn surface_wire(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "instance" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        if root < 0 {
            write_stderr(str("driver: ", f[1], " does not load\n"));
            exit(1);
        }
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table(conformance_unit(), f[3], data, vals, nodes, root, out);
        if refusal != 0 {
            absent(f[1], schema_rt::save_refusal_name(refusal));
            continue;
        }
        answer(f[1], out);
    }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: main.goose list <manifest> | <surface> <manifest> <unit>\n");
        exit(1);
    }
    if argv[1] == "list" {
        write_stdout("wire\nreport\n");
        return;
    }
    if argv.len != 4 {
        write_stderr("usage: main.goose <surface> <manifest> <unit>\n");
        exit(1);
    }
    var text: u8[>..] = [];
    load_file(argv[2], text);
    let lines = split(text, '\n');
    let unit = argv[3];
    if argv[1] == "report" { surface_report(lines, unit); return; }
    if argv[1] == "wire" { surface_wire(lines, unit); return; }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}
