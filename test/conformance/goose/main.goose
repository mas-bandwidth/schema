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
//   report       load every report row, print the verdict, the §16 text of
//                the value, and the canonical write-back wire in `<case>.wire`
//   framing      load every framing row, print the verdict and the §16 text
//                of the value a clean read owes, and the write-back wire
//   retain       load with retention (§6.6), print the verdict and the
//                complete known §16 value
//   retain-save  the bytes that retaining save writes back
// The message form, form 2 (docs/SPEC-TABLES.md §3.3):
//   message      AnnounceRead the connection's announcement, LoadMessages the
//                batch against it, SaveMessages what landed: the bytes
// The JSON text form (docs/SPEC-TABLES.md §16.2, §16.3):
//   json-read    a tolerant text read -> form-1 save, against the pinned wire:
//                json-read and json-hostile call one entry point, the
//                tolerant read (test/conformance/README.md)
//   json-write   form-1 load -> the canonical text, against the pinned text
//   json-hostile a tolerant read of <tree>/<root>.json, the verdict, the §16
//                text of the value it left, and the write-back wire in
//                `<case>.wire`: through the unit's text glue for a root it
//                names (unit.goose, which the shell driver copies from
//                units/<unit>.goose, or the empty unit.goose beside this file),
//                and through the flat value table for every other root
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

// emit_read writes one accepted read's two owed forms (test/conformance/
// README.md): the verdict line and the complete value in name, and the
// canonical write-back wire in name.wire, after the JSON text `json` has been
// produced. If the write-back is refused or absent, the verdict is held beside
// an absent marker: the read did not fail, the value is the missing feature.
fn load_instance(root: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {
    let at = conformance_json_read(root, "", data, vals, nodes);
    if at != -2 { return at; }
    var report = schema_rt::LoadReport { .. };
    return schema_rt::load_table(conformance_unit(), root, data[0..data.len], vals, nodes, report);
}

fn save_instance(root: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, at: i64, out: u8[>..<]&) -> i64 {
    var fixed: u8[>..] = [];
    let r = conformance_json_write(root, data, vals, nodes, -1 - at, fixed);
    if r >= 0 {
        out.append(fixed);
        return r;
    }
    return schema_rt::save_table(conformance_unit(), root, data, vals, nodes, at, out);
}

fn emit_read(name: const u8[:], root: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, at: i64, json: const u8[:]) {
    var wire: u8[>..<] = [];
    let refusal = save_instance(root, data, vals, nodes, at, wire);
    if refusal != 0 {
        answer(name, "read\n");
        absent(name, "");
        return;
    }
    var line: u8[>..] = [];
    line.append("read\n");
    line.append(json);
    answer(name, line);
    answer(str(name, ".wire"), wire);
}

// load_file reads a wire, aborting the run on a file the manifest names and
// the tree does not hold (the harness prints the stderr line).
fn load_file(path: const u8[:], out: u8[>..]&) {
    if !read_file(path, out) {
        write_stderr(str("driver: cannot read ", path, "\n"));
        exit(1);
    }
}

// surface_report: §4's verdict, the complete §16 text of the value the read
// leaves, and the canonical write-back wire in `<case>.wire`. A leg the glue
// cannot write the value for answers the verdict line alone beside its absence:
// the verdict is held and the value is the missing feature
// (test/conformance/README.md).
fn surface_report(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "report" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var fixed_root = false;
        for d in conformance_unit().tables {
            if d.name == f[3] && d.fixed { fixed_root = true; }
        }
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        var at: i64 = -1;
        if fixed_root {
            report.refused = true;
        } else {
            at = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        }
        if report.refused || schema_rt::load_error(report, at) != schema_rt::LoadRefuseOk {
            answer(f[1], "refused\n");
            continue;
        }
        var json: u8[>..] = [];
        if conformance_json_write(f[3], data, vals, nodes, at, json) < 0 {
            answer(f[1], "read\n");
            absent(f[1], "");
            continue;
        }
        emit_read(f[1], f[3], data, vals, nodes, at, json);
    }
}

// surface_framing: the framing vectors (docs/LAYERS/1-framing.md): §4's
// verdict, then the §16 text of the value a clean read leaves and the write-back
// wire in `<case>.wire`. Damaged or refused input owes `refused\n` alone,
// because damage discards the whole load (§4).
fn surface_framing(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "framing" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        if report.refused || schema_rt::load_error(report, root) != schema_rt::LoadRefuseOk || (f.len >= 6 && f[5] == "refused") {
            answer(f[1], "refused\n");
            continue;
        }
        var json: u8[>..] = [];
        if f.len < 7 || f[6] != "-" {
            if conformance_json_write(f[3], data, vals, nodes, root, json) < 0 {
                answer(f[1], "read\n");
                absent(f[1], "");
                continue;
            }
        }
        emit_read(f[1], f[3], data, vals, nodes, root, json);
    }
}

// surface_wire: every instance loaded and saved back (§3). A blob two slots
// name (§2.5) is refused by the strict read itself: that refusal is the row's
// answer (the corpus marks it wire-refused), a named absence, and any other
// refusal of a corpus instance fails the run. A save the writer refuses by
// name is a named absence.
fn surface_wire(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "instance" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var out: u8[>..<] = [];
        let fw = conformance_wire(f[3], data, out);
        if fw == 0 {
            answer(f[1], out);
            continue;
        }
        if fw < -1 {
            write_stderr(str("driver: ", f[1], " does not load\n"));
            exit(1);
        }
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        if root < 0 && report.reason == schema_rt::LoadRefuseBlobShared {
            absent(f[1], "blob_shared");
            continue;
        }
        if root < 0 {
            write_stderr(str("driver: ", f[1], " does not load\n"));
            exit(1);
        }
        let refusal = schema_rt::save_table(conformance_unit(), f[3], data, vals, nodes, root, out);
        if refusal != 0 {
            absent(f[1], schema_rt::save_refusal_name(refusal));
            continue;
        }
        answer(f[1], out);
    }
}

// Read canonical JSON and save form 1; or load form 1 and write canonical
// JSON: through a root's typed record where it has one, and through the flat
// value table for every other root (the glue's conformance_json_read and
// conformance_json_write).
fn surface_json(lines: const u8[:][:], unit: const u8[:], writing: bool) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "instance" || f[2] != unit { continue; }
        var no_text = false;
        for mark in f { if mark == "no-text" { no_text = true; } }
        if no_text { continue; }
        let name = if writing { str(f[1], ".json") } else { str(f[1]) };
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var out: u8[>..<] = [];
        var result: i64 = -1;
        if writing {
            load_file(f[4], data);
            let at = load_instance(f[3], data, vals, nodes);
            if at < 0 { write_stderr(str("driver: ", f[1], " does not load\n")); exit(1); }
            var json: u8[>..] = [];
            result = conformance_json_write(f[3], data, vals, nodes, at, json);
            out.append(json);
        } else {
            var text: u8[>..] = [];
            load_file(str("testdata/conformance/tables/json/", f[1], ".json"), text);
            result = conformance_json_read(f[3], text, data, vals, nodes);
            if result >= 0 && save_instance(f[3], data, vals, nodes, result, out) != 0 { result = -2; }
        }
        if result < 0 { write_stderr(str("driver: ", f[1], " JSON bridge refused\n")); exit(1); }
        answer(name, out);
    }
}

// surface_write_refusal: THE VALUES THE WRITER REFUSES (docs/SPEC-TABLES.md
// §16.14, W0). The wire is loaded into the flat value table, the line's
// damage is put into storage in code (damage_text), then the glue's
// conformance_json_measure, which must refuse, and conformance_json_write,
// which must refuse too: `refused`, or the text written when it does not.
fn surface_write_refusal(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 6 || f[0] != "json-write-refusal" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        load_file(f[4], data);
        let at = load_instance(f[3], data, vals, nodes);
        if at < 0 { write_stderr(str("driver: ", f[1], " does not load\n")); exit(1); }
        if !damage_text(data, vals, at, f[5]) { write_stderr(str("driver: ", f[1], ": damage ", f[5], " names no text field\n")); exit(1); }
        let name = str(f[1], ".json");
        var json: u8[>..] = [];
        if conformance_json_measure(f[3], data, vals, nodes, at) >= 0 {
            if conformance_json_write(f[3], data, vals, nodes, at, json) < 0 { write_stderr(str("driver: ", f[1], ": the measure wrote and the write refused\n")); exit(1); }
            answer(name, json);
            continue;
        }
        if conformance_json_write(f[3], data, vals, nodes, at, json) >= 0 { write_stderr(str("driver: ", f[1], ": the measure refused and the write wrote\n")); exit(1); }
        answer(name, "refused\n");
    }
}

// damage_text puts into the root record's top-level text field what no read
// delivers (testdata/conformance/tables/FORMAT.md, `json-write-refusal`): the bytes
// 61 FF 62 in a narrow field, the UTF-16LE units 0061 D800 0062 in a wide one.
// The bytes are appended to the data store and the field's slot (the record's
// fields sit in descriptor order) is pointed at them.
fn damage_text(data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, at: i64, damage: const u8[:]) -> bool {
    if damage == "none" { return true; }
    if damage.len > 7 && damage[..7] == "narrow:" { return damage_field(data, vals, at, damage[7..], "a\xFFb"); }
    if damage.len > 5 && damage[..5] == "wide:" { return damage_field(data, vals, at, damage[5..], "a\x00\x00\xD8b\x00"); }
    return false;
}

fn damage_field(data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, at: i64, field: const u8[:], bytes: const u8[:]) -> bool {
    let t = conformance_unit().tables[vals[at].tab];
    var i: i64 = 0;
    while i < t.fields.len {
        if t.fields[i].name == field {
            let s = vals[at].first + i;
            vals[s].off = data.len;
            vals[s].len = bytes.len;
            vals[s].state = schema_rt::ValPresent;
            data.append(bytes);
            return true;
        }
        i += 1;
    }
    return false;
}

// surface_retain: the RETAIN-UNKNOWN round trip (docs/SPEC-TABLES.md §6.6).
// `full` is a buffer the whole load fits in; `short` is one byte short of the
// LAST record, found by loading once at full and reading what it used. The
// id capacity is `full` or a count.
fn surface_retain(lines: const u8[:][:], unit: const u8[:], saved: bool) {
    for line in lines {
        let f = words(line);
        if f.len < 1 { continue; }
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
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table_retain(conformance_unit(), f[3], data, vals, nodes, root, store, report, out);
        if saved {
            if refusal != 0 {
                absent(f[1], schema_rt::save_refusal_name(refusal));
                continue;
            }
            answer(f[1], out);
        } else {
            // the complete known value, one read: acceptance or refusal
            var json: u8[>..] = [];
            if conformance_json_write(f[3], data, vals, nodes, root, json) < 0 {
                answer(f[1], "read\n");
                absent(f[1], "");
                continue;
            }
            var line: u8[>..] = [];
            line.append("read\n");
            line.append(json);
            answer(f[1], line);
        }
    }
}

// json_strict_refused asks the glue's verdict arm (conformance_json_verdict)
// whether the strict read refuses this text: damage, a kind mismatch or an
// out-of-range count in its seven-field line (`u,k,w,c,d,malformed,verdict`)
// refuses the whole read, however much a tolerant read would keep. A glue
// that answers nothing (a gate that drives no text arm) holds no verdict, and
// the tolerant read's own answer stands.
fn json_strict_refused(root: const u8[:], text: const u8[:]) -> bool {
    var verdict: u8[>..] = [];
    conformance_json_verdict(root, text, verdict);
    if verdict.len == 0 { return false; }
    if verdict.len >= 7 && verdict[..7] == "refused" { return true; }
    let lines = split(verdict, '\n');
    if lines.len == 0 { return false; }
    let f = split(lines[0], ',');
    if f.len < 7 { return false; }
    let mismatch, mok = parse_int(f[1]);
    let oor, ook = parse_int(f[3]);
    return (mok && mismatch > 0) || (ook && oor > 0) || f[5] == "true";
}

// surface_json_hostile: the verdict of a tolerant read of <tree>/<root>.json
// for every json-hostile row of the unit, `u,k,w,c,d,false,read` and the §16
// text of the value it left, or `refused`
// (test/conformance/README.md): through the unit's text glue for a root it
// names (its typed <Root>FromJson), and through the flat value table
// for every other root.
fn surface_json_hostile(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "json-hostile" || f[2] != unit { continue; }
        var text: u8[>..] = [];
        load_file(str(f[4], "/", f[3], ".json"), text);
        if json_strict_refused(f[3], text) {
            answer(f[1], "refused\n");
            continue;
        }
        var data: u8[>..] = [];
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var at = conformance_json_read(f[3], text, data, vals, nodes);
        if at < 0 {
            answer(f[1], "refused\n");
            continue;
        }
        var json: u8[>..] = [];
        if conformance_json_write(f[3], data, vals, nodes, at, json) < 0 {
            answer(f[1], "read\n");
            absent(f[1], "");
            continue;
        }
        emit_read(f[1], f[3], data, vals, nodes, at, json);
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
        if f[5] == "-" {
            // a named refusal row: the root is a variable table (§3.3)
            absent(f[1], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
            continue;
        }
        let c = words(lines[conn]);
        var ann: u8[>..] = [];
        load_file(c[4], ann);
        var wire: u8[>..] = [];
        load_file(f[5], wire);
        var out: u8[>..] = [];
        let n = conformance_message(f[3], ann, wire, out);
        if n == -1 {
            absent(f[1], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
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
        write_stdout("wire\nmessage\nreport\nframing\nretain\nretain-save\njson-read\njson-write\njson-hostile\n");
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
    if argv[1] == "framing" { surface_framing(lines, unit); return; }
    if argv[1] == "wire" { surface_wire(lines, unit); return; }
    if argv[1] == "message" { surface_message(lines, unit); return; }
    if argv[1] == "retain" { surface_retain(lines, unit, false); return; }
    if argv[1] == "retain-save" { surface_retain(lines, unit, true); return; }
    if argv[1] == "json-read" { surface_json(lines, unit, false); return; }
    if argv[1] == "json-write" {
        surface_json(lines, unit, true);
        surface_write_refusal(lines, unit);
        return;
    }
    if argv[1] == "json-hostile" { surface_json_hostile(lines, unit); return; }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}
