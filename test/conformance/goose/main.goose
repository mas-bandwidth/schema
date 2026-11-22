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
            at = conformance_load(f[3], data, vals, nodes);
            if at < 0 { report.refused = true; }
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
        if report.refused || schema_rt::load_error(report, root) != schema_rt::LoadRefuseOk {
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
        if root < 0 || report.refused || schema_rt::load_error(report, root) != schema_rt::LoadRefuseOk {
            if !saved {
                answer(f[1], "refused\n");
            }
            continue;
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
    // message-batch <case> <connection> <root> <wire>: load with room for
    // every body and answer the count LoadMessages returned
    // (docs/SPEC-TABLES.md §3.3; FORMAT.md, `message-batch`).
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "message-batch" { continue; }
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
        load_file(f[4], wire);
        let n = conformance_message_count(f[3], ann, wire);
        if n == -1 {
            absent(f[1], "no message form for this root (SPEC-TABLES §3.3)");
            continue;
        }
        if n == -2 {
            write_stderr(str("driver: ", f[1], ": the announcement was refused\n"));
            exit(1);
        }
        answer(f[1], str("count ", n, "\n"));
    }
}



fn wref_loaded_hex(s: const u8[:], out: u8[>..]&) -> bool {
    if (s.len % 2) != 0 { return false; }
    var i: i64 = 0;
    while i < s.len {
        let hi = s[i];
        let lo = s[i + 1];
        let hv: u8 = if hi >= 'a' && hi <= 'f' { hi - 'a' + 10 } else if hi >= 'A' && hi <= 'F' { hi - 'A' + 10 } else { hi - '0' };
        let lv: u8 = if lo >= 'a' && lo <= 'f' { lo - 'a' + 10 } else if lo >= 'A' && lo <= 'F' { lo - 'A' + 10 } else { lo - '0' };
        out.push((hv << 4) | lv);
        i += 2;
    }
    return true;
}

fn wref_loaded_f32_bits(s: const u8[:]) -> u64 {
    if s == "nan" { return 0x7fc00000; }
    if s == "inf" { return 0x7f800000; }
    if s == "-inf" { return 0xff800000; }
    if s == "1.5" { return 0x3fc00000; }
    if s == "2.5" { return 0x40200000; }
    if s == "0" || s == "0.0" { return 0; }
    return schema_rt::fixed_f32_bits(wref_loaded_dec(s) as f32) as u64;
}

fn wref_loaded_f64_bits(s: const u8[:]) -> u64 {
    if s == "nan" { return 0x7ff8000000000000; }
    if s == "inf" { return 0x7ff0000000000000; }
    if s == "-inf" { return 0xfff0000000000000; }
    if s == "1.5" { return 0x3ff8000000000000; }
    if s == "2.5" { return 0x4004000000000000; }
    if s == "0" || s == "0.0" { return 0; }
    return schema_rt::fixed_f64_bits(wref_loaded_dec(s));
}

fn wref_loaded_leaf(data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, s: i64, d: schema_rt::FieldDesc, kind: const u8[:], text: const u8[:]) -> i64 {
    vals[s].state = schema_rt::ValPresent;
    if kind == "float" {
        vals[s].bits = schema_rt::Int128 { lo: if d.is_f32 { wref_loaded_f32_bits(text) } else { wref_loaded_f64_bits(text) }, hi: 0 };
        return 1;
    }
    if kind == "int" || kind == "ordinal" || kind == "tag" {
        let n, ok = parse_int(text);
        if !ok { return 0; }
        let hi: u64 = if n < 0 { 0xffffffffffffffff } else { 0 };
        vals[s].bits = schema_rt::Int128 { lo: n as! u64, hi: hi };
        return 1;
    }
    if kind == "length" {
        let n, ok = parse_int(text);
        if !ok || n < 0 { return 2; }
        vals[s].off = data.len;
        vals[s].len = n;
        var k: i64 = 0;
        while k < n { data.push('a'); k += 1; }
        return 1;
    }
    if kind == "bytes" {
        var raw: u8[>..] = [];
        if !wref_loaded_hex(text, raw) { return 0; }
        vals[s].off = data.len;
        vals[s].len = raw.len;
        data.append(raw);
        return 1;
    }
    if kind == "count" {
        let n, ok = parse_int(text);
        if !ok { return 0; }
        if n < 0 {
            // SF-28: the flat value table's array is a count beside its
            // slots, not a native list, so `count -1` is held as it is (a
            // negative count) and handed to the writer, which refuses it in
            // every build
            vals[s].len = n;
            vals[s].made = true;
            return 1;
        }
        wref_loaded_resize(vals, s, n);
        return 1;
    }
    if kind == "dupkey" {
        // FL-22: `dupkey 1` appends a copy of the map's first entry, so one
        // key is held twice; the flat value table holds it, and the writer
        // judges it. `dupkey 0` leaves the map as loaded.
        let n, ok = parse_int(text);
        if !ok || n < 0 || d.shape != schema_rt::ShapeMap { return 0; }
        if n == 0 { return 1; }
        if vals[s].len < 1 { return 0; }
        wref_loaded_resize(vals, s, vals[s].len + n);
        return 1;
    }
    return 0;
}

// wref_loaded_resize sets an array's or a map's element count to n; slots
// past the loaded ones are copies of element 0.
fn wref_loaded_resize(vals: schema_rt::LoadVal[>..]&, s: i64, n: i64) {
    if n > vals[s].count {
        let old_first = vals[s].first;
        let old_count = vals[s].count;
        let new_first = schema_rt::value_reserve(vals, n);
        var k: i64 = 0;
        while k < n {
            if k < old_count { vals[new_first + k] = vals[old_first + k]; }
            else if old_count > 0 { vals[new_first + k] = vals[old_first]; }
            k += 1;
        }
        vals[s].first = new_first;
        vals[s].count = n;
    }
    vals[s].len = n;
    vals[s].made = true;
}

recursive fn wref_loaded_set(unit: schema_rt::LoadUnit, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: const schema_rt::LoadNode[:], rec: i64, path: const u8[:], kind: const u8[:], text: const u8[:]) -> i64 {
    var dot: i64 = 0;
    while dot < path.len && path[dot] != '.' { dot += 1; }
    let seg_raw = path[0..dot];
    let rest = if dot < path.len { path[dot + 1..path.len] } else { "" };
    var br: i64 = 0;
    while br < seg_raw.len && seg_raw[br] != '[' { br += 1; }
    let seg = seg_raw[0..br];
    var idx: i64 = -1;
    if br < seg_raw.len && seg_raw[seg_raw.len - 1] == ']' {
        let parsed, ok = parse_int(seg_raw[br + 1..seg_raw.len - 1]);
        if ok { idx = parsed; }
    }
    let t = unit.tables[vals[rec].tab];
    var fi: i64 = 0;
    while fi < t.fields.len {
        let d = t.fields[fi];
        if d.name == seg {
            let s = vals[rec].first + fi;
            if rest.len == 0 && idx < 0 {
                return wref_loaded_leaf(data, vals, s, d, kind, text);
            }
            if idx >= 0 {
                if d.shape == schema_rt::ShapeMap {
                    let erec = vals[s].first + idx;
                    return wref_loaded_set(unit, data, vals, nodes, erec, rest, kind, text);
                }
                let es = vals[s].first + idx;
                let e = d.elem;
                guard e else { return 0; }
                if rest.len == 0 {
                    if d.ek == 12 && e.shape == schema_rt::ShapeTab {
                        let et = schema_rt::value_table(unit, e.ref);
                        return wref_loaded_leaf(data, vals, vals[es].first, unit.tables[et].fields[0], kind, text);
                    }
                    return wref_loaded_leaf(data, vals, es, e, kind, text);
                }
                if e.shape == schema_rt::ShapeTab {
                    return wref_loaded_set(unit, data, vals, nodes, es, rest, kind, text);
                }
                if e.shape == schema_rt::ShapePtr {
                    let widx = vals[es].bits.lo as! i64;
                    return wref_loaded_set(unit, data, vals, nodes, nodes[widx - 2].val, rest, kind, text);
                }
                return 0;
            }
            if d.shape == schema_rt::ShapePtr {
                let widx = vals[s].bits.lo as! i64;
                return wref_loaded_set(unit, data, vals, nodes, nodes[widx - 2].val, rest, kind, text);
            }
            if d.shape == schema_rt::ShapeTab {
                return wref_loaded_set(unit, data, vals, nodes, s, rest, kind, text);
            }
            if d.shape == schema_rt::ShapeUnion {
                let ui = schema_rt::value_union(unit, d.ref);
                let arms = unit.unions[ui].arms;
                var ai: i64 = 0;
                while ai < arms.len {
                    if arms[ai].name == rest {
                        return wref_loaded_leaf(data, vals, vals[s].first, arms[ai], kind, text);
                    }
                    ai += 1;
                }
                return 0;
            }
            return 0;
        }
        fi += 1;
    }
    return 0;
}

fn wref_loaded_has_str(hay: const u8[:], needle: const u8[:]) -> bool {
    if needle.len == 0 || hay.len < needle.len { return false; }
    var i: i64 = 0;
    while i + needle.len <= hay.len {
        if hay[i..i + needle.len] == needle { return true; }
        i += 1;
    }
    return false;
}

fn wref_loaded_run(surf: const u8[:], root_name: const u8[:], wire: const u8[:], path: const u8[:], kind: const u8[:], text: const u8[:]) -> const u8[:] {
    let u = conformance_unit();
    if schema_rt::value_table(u, root_name) < 0 { return "!absent"; }
    var data: u8[>..] = [];
    data.append(wire);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    let root = load_instance(root_name, data, vals, nodes);
    if root < 0 { return "!load-failed"; }
    var st: i64 = 0;
    if kind == "point" {
        // FL-28: the pointer at `path` is set to the node the pointer at
        // `text` points at (pointed at an enclosing node it makes a cycle)
        let dst = wref_loaded_slot(u, vals, nodes, root, path);
        let src = wref_loaded_slot(u, vals, nodes, root, text);
        if dst < 0 || src < 0 { return "!set-failed"; }
        vals[dst] = vals[src];
        st = 1;
    } else {
        st = wref_loaded_set(u, data, vals, nodes, root, path, kind, text);
    }
    if st == 0 { return "!set-failed"; }
    if st == 2 { return "unrepresentable\n"; }
    if surf == "write-refusal" {
        var out: u8[>..<] = [];
        if schema_rt::save_table(u, root_name, data, vals, nodes, root, out) != 0 { return "refused\n"; }
        return "wrote\n";
    }
    if surf == "write-refusal-cook" {
        // the cook writer over the loaded value: <Root>CookMeasure, then
        // <Root>Cook into a buffer of that size, little-endian
        let c = conformance_cook_var(root_name, data, vals, nodes, root);
        if c == -1 { return "!absent"; }
        return if c == 0 { "wrote\n" } else { "refused\n" };
    }
    if surf != "write-refusal-json" { return "!absent"; }
    if conformance_json_measure(root_name, data, vals, nodes, root) < 0 { return "refused\n"; }
    var jout: u8[>..] = [];
    if conformance_json_write(root_name, data, vals, nodes, root, jout) < 0 { return "refused\n"; }
    if kind == "bytes" {
        var raw: u8[>..] = [];
        let _ok = wref_loaded_hex(text, raw);
        var has_zero = false;
        for b in raw { if b == 0 { has_zero = true; } }
        if has_zero && !wref_loaded_has_str(jout, "\\u0000") { return "wrote without \\u0000\n"; }
    }
    return "wrote\n";
}

fn surface_wref_all(lines: const u8[:][:], unit: const u8[:], surf: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 8 || f[0] != surf { continue; }
        var ann: u8[>..] = [];
        if surf == "write-refusal-message" {
            var conn: i64 = -1;
            for i in lines.len {
                let c = words(lines[i]);
                if c.len >= 5 && c[0] == "connection" && c[1] == f[2] && c[2] == unit { conn = i; }
            }
            if conn < 0 { continue; }
            let c = words(lines[conn]);
            load_file(c[4], ann);
        } else {
            if f[2] != unit { continue; }
        }
        // a wire of `-` is the root's fresh value (SF-05, SF-06: a root with
        // no file form)
        var data: u8[>..] = [];
        let fresh = f[4] == "-";
        if !fresh { load_file(f[4], data); }
        let res = conformance_wref(surf, f[3], ann, data, fresh, f[5], f[6], f[7]);
        if res == "!absent" {
            if !fresh && (surf == "write-refusal" || surf == "write-refusal-json" || surf == "write-refusal-cook") {
                let vres = wref_loaded_run(surf, f[3], data, f[5], f[6], f[7]);
                if vres != "!absent" {
                    if vres.len > 0 && vres[0] == '!' {
                        write_stderr(str("driver: ", f[1], " ", vres, "\n"));
                        exit(1);
                    }
                    answer(f[1], vres);
                    continue;
                }
            }
            absent(f[1], "");
            continue;
        }
        if res.len > 0 && res[0] == '!' {
            write_stderr(str("driver: ", f[1], " ", res, "\n"));
            exit(1);
        }
        answer(f[1], res);
    }
}

// wref_loaded_dec reads a row's decimal number (`1.5`, `-0.25`, `1`) for a
// `float` kind.
fn wref_loaded_dec(s: const u8[:]) -> f64 {
    var neg = false;
    var i: i64 = 0;
    if s.len > 0 && s[0] == '-' { neg = true; i = 1; }
    var whole: f64 = 0.0;
    while i < s.len && s[i] != '.' {
        whole = whole * 10.0 + ((s[i] - '0') as f64);
        i += 1;
    }
    var frac: f64 = 0.0;
    var scale: f64 = 0.1;
    if i < s.len { i += 1; }
    while i < s.len {
        frac += ((s[i] - '0') as f64) * scale;
        scale *= 0.1;
        i += 1;
    }
    let r = whole + frac;
    return if neg { -r } else { r };
}

// wref_loaded_slot is the value-table slot a row's path names (a `point`
// row's two pointers): fields by name, `[i]` into an array or a map, and a
// pointer followed to its node. -1 for a path that names nothing.
recursive fn wref_loaded_slot(unit: schema_rt::LoadUnit, vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], rec: i64, path: const u8[:]) -> i64 {
    if rec < 0 || rec >= vals.len { return -1; }
    var dot: i64 = 0;
    while dot < path.len && path[dot] != '.' { dot += 1; }
    let seg_raw = path[0..dot];
    let rest = if dot < path.len { path[dot + 1..path.len] } else { "" };
    var br: i64 = 0;
    while br < seg_raw.len && seg_raw[br] != '[' { br += 1; }
    let seg = seg_raw[0..br];
    var idx: i64 = -1;
    if br < seg_raw.len && seg_raw[seg_raw.len - 1] == ']' {
        let parsed, ok = parse_int(seg_raw[br + 1..seg_raw.len - 1]);
        if !ok { return -1; }
        idx = parsed;
    }
    let t = unit.tables[vals[rec].tab];
    var fi: i64 = 0;
    while fi < t.fields.len {
        let d = t.fields[fi];
        if d.name == seg {
            let s = vals[rec].first + fi;
            var at = s;
            if idx >= 0 {
                if idx >= vals[s].len { return -1; }
                at = vals[s].first + idx;
                if rest.len == 0 { return at; }
                let e = d.elem;
                guard e else { return -1; }
                if e.shape == schema_rt::ShapeTab { return wref_loaded_slot(unit, vals, nodes, at, rest); }
                if e.shape != schema_rt::ShapePtr { return -1; }
            } else {
                if rest.len == 0 { return s; }
                if d.shape == schema_rt::ShapeTab { return wref_loaded_slot(unit, vals, nodes, s, rest); }
                if d.shape != schema_rt::ShapePtr { return -1; }
            }
            let widx = vals[at].bits.lo as! i64;
            if widx < 2 || widx - 2 >= nodes.len { return -1; }
            return wref_loaded_slot(unit, vals, nodes, nodes[widx - 2].val, rest);
        }
        fi += 1;
    }
    return -1;
}

// surface_announce: THE ANNOUNCEMENT (docs/SPEC-TABLES.md §3.3;
// test/conformance/README.md, `announce`). `write`: the unit's announcement
// writer; `strict` and `lenient`: a fresh vocabulary of the unit reads the
// announcement file, its strict flag set or left at its default; `send`:
// SaveMessages of one value with no announcement read or written. The glue
// (conformance_announce_*) appends the answer; -1 is a unit with no message
// form.
fn surface_announce(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 6 || f[0] != "announce" || f[2] != unit { continue; }
        var out: u8[>..] = [];
        var r: i64 = -1;
        if f[4] == "write" {
            r = conformance_announce_write(out);
        } else if f[4] == "strict" || f[4] == "lenient" {
            var ann: u8[>..] = [];
            load_file(f[5], ann);
            r = conformance_announce_read(f[4] == "strict", ann, out);
        } else if f[4] == "send" {
            var data: u8[>..] = [];
            let fresh = f[5] == "-";
            if !fresh { load_file(f[5], data); }
            r = conformance_announce_send(f[3], data, fresh, out);
        } else {
            write_stderr(str("driver: ", f[1], ": announce mode ", f[4], " is not one this driver knows\n"));
            exit(1);
        }
        if r == -1 { absent(f[1], "no message form for this unit or root (SPEC-TABLES §3.3)"); continue; }
        if r < 0 { write_stderr(str("driver: ", f[1], ": the row's input does not load (", r, ")\n")); exit(1); }
        answer(f[1], out);
    }
}

// read_into_variable: THE VARIABLE FORM's read-into row. Its read,
// schema_rt::load_table, takes the caller's stores and ONE report, and
// RETURNS the root's index into them: the value is returned, not read into
// caller values (shared ruling 3). So the driver holds the value it was last
// handed back — the index and the bytes it points into — and a read that
// returns one replaces it. A refused read returns none (load_table puts the
// stores back as they were), and the value printed after it is the one the
// driver holds then. No value is reset here to make the row pass.
fn read_into_variable(root: const u8[:], lawful: const u8[:], damaged: const u8[:], out: u8[>..]&) -> i64 {
    let u = conformance_unit();
    if schema_rt::value_table(u, root) < 0 { return -1; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    var held: u8[>..] = [];
    var at: i64 = -1;
    for step in 3 {
        var input: u8[>..] = [];
        if step == 1 { input.append(damaged); } else { input.append(lawful); }
        let r = schema_rt::load_table(u, root, input, vals, nodes, report);
        conformance_ri_verdict(out, r >= 0 && !report.refused && schema_rt::load_error(report, r) == schema_rt::LoadRefuseOk);
        if r >= 0 {
            at = r;
            held.clear();
            held.append(input);
        }
        if step == 1 {
            var t: u8[>..] = [];
            if at < 0 {
                if conformance_fresh_json(root, t) < 0 { return -2; }
            } else if conformance_json_write(root, held, vals, nodes, at, t) < 0 {
                return -2;
            }
            out.append(t);
        }
    }
    return 0;
}

// surface_read_into: ONE report and ONE set of caller values through a
// lawful read, a refused read and the lawful read again (docs/SPEC-TABLES.md
// §6.5; test/conformance/README.md, `read-into`). The glue's
// conformance_read_into calls the fixed load, LoadMessages and the text read,
// each of which takes the caller's values and report.
fn surface_read_into(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 9 || f[0] != "read-into" || f[2] != unit { continue; }
        if f[4] == "fixed-nocap" {
            // FL-17: <Root>FixedLoad's capacity is the length of the values
            // slice the caller hands it; no call omits it (shared ruling 5)
            answer(f[1], "unrepresentable\n");
            continue;
        }
        var lawful: u8[>..] = [];
        load_file(f[5], lawful);
        var damaged: u8[>..] = [];
        load_file(f[6], damaged);
        let count, cok = parse_int(f[7]);
        if !cok || count < 1 { write_stderr(str("driver: ", f[1], ": count ", f[7], "\n")); exit(1); }
        var ann: u8[>..] = [];
        if f[4] == "message" { load_file(f[8], ann); }
        var out: u8[>..] = [];
        var r: i64 = -1;
        if f[4] == "variable" {
            r = read_into_variable(f[3], lawful, damaged, out);
        } else {
            r = conformance_read_into(f[3], f[4], lawful, damaged, count, ann, out);
        }
        if r == -1 { absent(f[1], str("no ", f[4], " read for ", f[3])); continue; }
        if r < 0 { write_stderr(str("driver: ", f[1], ": read-into failed (", r, ")\n")); exit(1); }
        answer(f[1], out);
    }
}

// surface_json_write_into: the text writer under a comma-decimal process
// locale into a caller buffer (FL-14, FL-15; test/conformance/README.md,
// `json-write-into`).
//
// THE LOCALE: Goose has no process-locale call. Its standard library binds
// no setlocale, and an `extern fn` cannot hand C the NUL-terminated string
// setlocale takes (docs/goose_spec.md §7.10: a slice crosses as {data, len}),
// so this driver cannot make SCHEMA_COMMA_LOCALE the process's locale; it
// says so here and runs the rest (shared ruling 1). The harness's locale
// name must still arrive: a run without it is an error.
//
// THE BUFFER: the text writer (<Root>ToJson, json_save_table) appends to the
// caller's growable buffer and returns the length it wrote; there is no
// fixed-length caller buffer for it to write past, so the answer is the text
// it wrote (shared ruling 1: no tail to check).
fn surface_json_write_into(lines: const u8[:][:], unit: const u8[:]) {
    var loc: u8[>..] = [];
    if !env("SCHEMA_COMMA_LOCALE", loc) || loc.len == 0 {
        write_stderr("driver: json-write-into: SCHEMA_COMMA_LOCALE is not set (the harness names the comma-decimal locale)\n");
        exit(1);
    }
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "json-write-into" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        let at = load_instance(f[3], data, vals, nodes);
        if at < 0 { write_stderr(str("driver: ", f[1], " does not load\n")); exit(1); }
        var json: u8[>..] = [];
        if conformance_json_write(f[3], data, vals, nodes, at, json) < 0 {
            answer(f[1], "refused\n");
            continue;
        }
        answer(f[1], json);
    }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: main.goose list <manifest> | <surface> <manifest> <unit>\n");
        exit(1);
    }
    if argv[1] == "list" {
        write_stdout("wire\nmessage\nreport\nframing\nretain\nretain-save\njson-read\njson-write\njson-hostile\nwrite-refusal\nwrite-refusal-fixed\nwrite-refusal-message\nwrite-refusal-json\nwrite-refusal-cook\nwrite-refusal-block\nannounce\nread-into\njson-write-into\n");
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
    if argv[1] == "announce" { surface_announce(lines, unit); return; }
    if argv[1] == "read-into" { surface_read_into(lines, unit); return; }
    if argv[1] == "json-write-into" { surface_json_write_into(lines, unit); return; }
    if argv[1] == "write-refusal" || argv[1] == "write-refusal-fixed" || argv[1] == "write-refusal-message" || argv[1] == "write-refusal-json" || argv[1] == "write-refusal-cook" || argv[1] == "write-refusal-block" {
        surface_wref_all(lines, unit, argv[1]);
        return;
    }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}
