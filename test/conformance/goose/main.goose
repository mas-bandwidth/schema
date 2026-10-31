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
//   retain       load with retention (§6.6), print the load's three counters
//                and the save's retain_lost
//   retain-save  the bytes that retaining save writes back
// The message form, form 2 (docs/SPEC-TABLES.md §3.3):
//   message      AnnounceRead the connection's announcement, LoadMessages the
//                batch against it, SaveMessages what landed: the bytes
// The JSON text form (docs/SPEC-TABLES.md §16.2, §16.3):
//   json-hostile FromJsonTolerant of <tree>/<root>.json and its report, through
//                the unit's text glue: unit.goose, which the shell driver
//                copies from units/<unit>.goose, or the empty unit.goose beside
//                this file for a unit that has none (every row ABSENT)
// The cook's and the block form's surfaces are accel.goose's: the shell
// driver runs that program for them.
import os;
import .conformance_unit;
import .unit;
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
                       if report.refused || report.non_finite { "refused" } else { "read" }, "\n");
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

// surface_retain: the RETAIN-UNKNOWN round trip (docs/SPEC-TABLES.md §6.6).
// `full` is a buffer the whole load fits in; `short` is one byte short of the
// LAST record, found by loading once at full and reading what it used. The
// id capacity is `full` or a count. The form-2 row (`retain-message`) names a
// variable table root (RT1's Node), which carries no message verbs (§3.3: only
// a fixed table rides the message form): a named absence, as the Go leg
// answers it.
fn surface_retain(lines: const u8[:][:], unit: const u8[:], saved: bool) {
    for line in lines {
        let f = words(line);
        if f.len < 1 { continue; }
        if f[0] == "retain-message" {
            if f.len > 3 && f[3] == unit { absent(f[1], "variable table: §3.3"); }
            continue;
        }
        // the derived manifest carries no expectation: the harness keeps the
        // counters and the saves, and asks retain-save only of rows that save
        if f.len < 7 || f[0] != "retain" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var ids_cap: i64 = -1;
        if f[6] != "full" {
            let n, ok = parse_int(f[6]);
            if !ok { write_stderr(str("driver: ", f[1], ": ids ", f[6], "\n")); exit(1); }
            ids_cap = n;
        }
        var cap: i64 = -1;
        if f[5] == "short" {
            // a roomy load first, to read what the whole load used
            var vals: schema_rt::LoadVal[>..] = [];
            var nodes: schema_rt::LoadNode[>..] = [];
            var report = schema_rt::LoadReport { .. };
            var rbytes: u8[>..<] = [];
            var rrecs: schema_rt::RetainRec[>..<] = [];
            var rids: u64[>..<] = [];
            var store = schema_rt::RetainStore { capacity: -1, id_capacity: ids_cap, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
            schema_rt::load_table_retain(conformance_unit(), f[3], data, vals, nodes, report, store);
            cap = schema_rt::retain_last_short(store);
        }
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        var rbytes: u8[>..<] = [];
        var rrecs: schema_rt::RetainRec[>..<] = [];
        var rids: u64[>..<] = [];
        var store = schema_rt::RetainStore { capacity: cap, id_capacity: ids_cap, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
        let root = schema_rt::load_table_retain(conformance_unit(), f[3], data, vals, nodes, report, store);
        if root < 0 {
            write_stderr(str("driver: ", f[1], " does not load\n"));
            exit(1);
        }
        let load_retained = report.retained;
        let load_lost = report.retain_lost;
        let load_unknown = report.unknown;
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table_retain(conformance_unit(), f[3], data, vals, nodes, root, store, report, out);
        if refusal != 0 {
            absent(f[1], schema_rt::save_refusal_name(refusal));
            continue;
        }
        if saved {
            answer(f[1], out);
        } else {
            answer(f[1], str(load_retained, ",", load_lost, ",", load_unknown, " ", report.retain_lost - load_lost, "\n"));
        }
    }
}

// surface_json_hostile: the verdict of FromJsonTolerant over <tree>/<root>.json
// for every json-hostile row of the unit, `u,k,w,c,d,false,read` or `refused`
// (test/conformance/README.md). A root the unit's text glue does not name is
// ABSENT.
fn surface_json_hostile(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "json-hostile" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(str(f[4], "/", f[3], ".json"), data);
        var verdict: u8[>..] = [];
        if !unit_json_verdict(f[3], data, verdict) {
            absent(f[1], "");
            continue;
        }
        answer(f[1], verdict);
    }
}

// surface_message: the MESSAGE FORM's round trip (docs/SPEC-TABLES.md §3.3).
// A message row names its CONNECTION, whose announcement is the peer's
// vocabulary and whose unit is the unit under test; the glue's
// conformance_message carries every root the unit has the message form for,
// which is a fixed table's (§3.3: only a fixed table rides the message form):
// a variable table's row is a named absence, as the Go leg answers it.
fn surface_message(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 6 || f[0] != "message" { continue; }
        var conn: i64 = -1;
        for i in lines.len {
            let c = words(lines[i]);
            if c.len >= 5 && c[0] == "connection" && c[1] == f[2] && c[2] == unit { conn = i; }
        }
        if conn < 0 { continue; }
        let c = words(lines[conn]);
        var ann: u8[>..] = [];
        load_file(c[4], ann);
        var wire: u8[>..] = [];
        load_file(f[5], wire);
        var out: u8[>..] = [];
        let n = conformance_message(f[3], ann, wire, out);
        if n == -1 {
            absent(f[1], "variable table: §3.3");
            continue;
        }
        if n < 0 {
            write_stderr(str("driver: ", f[1], " does not round-trip (", n, ")\n"));
            exit(1);
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
        write_stdout("wire\nmessage\nreport\nretain\nretain-save\njson-hostile\n");
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
    if argv[1] == "message" { surface_message(lines, unit); return; }
    if argv[1] == "retain" { surface_retain(lines, unit, false); return; }
    if argv[1] == "retain-save" { surface_retain(lines, unit, true); return; }
    if argv[1] == "json-hostile" { surface_json_hostile(lines, unit); return; }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}
