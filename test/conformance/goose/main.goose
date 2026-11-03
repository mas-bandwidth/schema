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
//   report       load every report row, print §4's counters and verdict,
//                then the §16 text of the value when the read reads
//   framing      load every framing row, print §4's counters and verdict and,
//                for a clean read the row owes a value, the §16 text after it
//   retain       load with retention (§6.6), print the load's three counters
//                and the save's retain_lost
//   retain-save  the bytes that retaining save writes back
//   write-refusal  load the wire, set the ONE field a row names, save: the
//                word `refused` (the save's named refusal) or `wrote`
// The message form, form 2 (docs/SPEC-TABLES.md §3.3):
//   message      AnnounceRead the connection's announcement, LoadMessages the
//                batch against it, SaveMessages what landed: the bytes
// The JSON text form (docs/SPEC-TABLES.md §16.2, §16.3):
//   json-read    a tolerant text read -> form-1 save, against the pinned wire:
//                json-read and json-hostile call one entry point, the
//                tolerant read (test/conformance/README.md)
//   json-write   form-1 load -> the canonical text, against the pinned text
//   json-hostile a tolerant read of <tree>/<root>.json, its report and the
//                §16 text of the value it left: through
//                the unit's text glue for a root it names (unit.goose, which
//                the shell driver copies from units/<unit>.goose, or the empty
//                unit.goose beside this file), and through the flat value
//                table for every other root
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

// surface_report: §4's counters and verdict, `u,k,w,c,d,malformed,verdict`,
// then the §16 text of the value when the read reads. A root the glue writes
// no text for answers the line alone beside its absence: the verdict is held
// and the value is the missing feature (test/conformance/README.md).
fn surface_report(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "report" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let at = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        let refused = report.refused || report.non_finite;
        var text: u8[>..] = [];
        text.append(str(report.unknown, ",", report.kind_mismatch, ",", report.widened, ",", report.clamped, ",",
                        report.duplicate, ",", bool_word(report.malformed), ",",
                        if refused { "refused" } else { "read" }, "\n"));
        if !refused && !report.malformed && at >= 0 {
            var json: u8[>..] = [];
            if conformance_json_write(f[3], data, vals, nodes, at, json) < 0 {
                answer(f[1], text);
                absent(f[1], "");
                continue;
            }
            text.append(json);
        }
        answer(f[1], text);
    }
}

// surface_framing: the framing vectors (docs/LAYERS/1-framing.md): §4's
// counters and verdict, then the §16 text of the value a clean read leaves,
// unless the row is marked `no-value`. Damaged input owes the report line
// alone, because damage discards the whole load (§4).
fn surface_framing(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "framing" || f[2] != unit { continue; }
        var no_value = false;
        for mark in f { if mark == "no-value" { no_value = true; } }
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
        var out: u8[>..] = [];
        out.append(str(report.unknown, ",", report.kind_mismatch, ",", report.widened, ",", report.clamped, ",",
                       report.duplicate, ",", bool_word(report.malformed), ",",
                       if report.refused || report.non_finite { "refused" } else { "read" }, "\n"));
        if !no_value && root >= 0 && !report.malformed && !report.refused && !report.non_finite {
            var json: u8[>..] = [];
            if conformance_json_write(f[3], data, vals, nodes, root, json) < 0 {
                write_stderr(str("driver: ", f[1], " JSON writer refused\n"));
                exit(1);
            }
            out.append(json);
        }
        answer(f[1], out);
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
            var report = schema_rt::LoadReport { .. };
            let at = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
            if at < 0 { write_stderr(str("driver: ", f[1], " does not load\n")); exit(1); }
            var json: u8[>..] = [];
            result = conformance_json_write(f[3], data, vals, nodes, at, json);
            out.append(json);
        } else {
            var text: u8[>..] = [];
            load_file(str("testdata/conformance/tables/json/", f[1], ".json"), text);
            result = conformance_json_read(f[3], text, data, vals, nodes);
            if result >= 0 && schema_rt::save_table(conformance_unit(), f[3], data, vals, nodes, result, out) != 0 { result = -2; }
        }
        if result < 0 { write_stderr(str("driver: ", f[1], " JSON bridge refused\n")); exit(1); }
        answer(name, out);
    }
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

// surface_json_hostile: the verdict of a tolerant read of <tree>/<root>.json
// for every json-hostile row of the unit, `u,k,w,c,d,false,read` and the §16
// text of the value it left, or `refused`
// (test/conformance/README.md): through the unit's text glue for a root it
// names (its typed <Root>FromJsonTolerant), and through the flat value table
// for every other root.
fn surface_json_hostile(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let f = words(line);
        if f.len < 5 || f[0] != "json-hostile" || f[2] != unit { continue; }
        var data: u8[>..] = [];
        load_file(str(f[4], "/", f[3], ".json"), data);
        var verdict: u8[>..] = [];
        if !unit_json_verdict(f[3], data, verdict) { conformance_json_verdict(f[3], data, verdict); }
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

// ---- THE WRITE CONTRACT (testdata/conformance/tables/FORMAT.md,
// `write-refusal`; docs/SPEC.md §5.1) ----
//
// A derived line is `write-refusal <case> <unit> <root> <wire> <path> <kind>
// <value>`: load the wire as surface_wire does, set the ONE field at <path> in
// the flat value table, save through the same save_table, and answer
// `refused\n` or `wrote\n`.

fn wr_fail(name: const u8[:], why: const u8[:]) {
    write_stderr(str("driver: ", name, ": ", why, "\n"));
    exit(1);
}

fn wr_int(name: const u8[:], text: const u8[:]) -> i64 {
    let n, ok = parse_int(text);
    if !ok { wr_fail(name, str("not an integer: ", text)); }
    return n;
}

// wr_deepen gives slot s children of its own: copies of the ones it names,
// at the end of vals, each deepened in turn.
recursive fn wr_deepen(vals: schema_rt::LoadVal[>..]&, s: i64) {
    let v = vals[s];
    if !v.made || v.count <= 0 { return; }
    let base = vals.len;
    for k in v.count {
        let kid = vals[v.first + k];
        vals.push(kid);
    }
    vals[s].first = base;
    for k in v.count { wr_deepen(vals, base + k); }
}

// wr_clone copies slot s and every slot under it to the end of vals, so a
// new array element shares no slot with the one it copies.
fn wr_clone(vals: schema_rt::LoadVal[>..]&, s: i64) -> i64 {
    let at = vals.len;
    let v = vals[s];
    vals.push(v);
    wr_deepen(vals, at);
    return at;
}

// one path segment: the field name and its element index, or -1
fn wr_segment(seg: const u8[:]) -> const u8[:], i64 {
    for b, i in seg {
        if b == '[' {
            let n, ok = parse_int(seg[i + 1 .. seg.len - 1]);
            if !ok { return seg, -1; }
            return seg[0 .. i], n;
        }
    }
    return seg, -1;
}

fn wr_field_index(unit: schema_rt::LoadUnit, tab: i64, name: const u8[:]) -> i64 {
    for d, i in unit.tables[tab].fields { if d.name == name { return i; } }
    return -1;
}

fn surface_write_refusal(lines: const u8[:][:], unit: const u8[:]) {
    let u = conformance_unit();
    for line in lines {
        let f = words(line);
        if f.len < 8 || f[0] != "write-refusal" || f[2] != unit { continue; }
        let name = f[1];
        var data: u8[>..] = [];
        load_file(f[4], data);
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(u, f[3], data, vals, nodes, report);
        if root < 0 { wr_fail(name, "does not load"); }
        // walk to the field: its slot, and its descriptor
        let segs = split(f[5], '.');
        var rec = root;
        var slot: i64 = -1;
        var index: i64 = -1;
        var desc = u.tables[vals[root].tab].fields[0];
        for seg, k in segs {
            let field, at = wr_segment(seg);
            if !vals[rec].made { wr_fail(name, str(field, " sits in a record the load did not make")); }
            let i = wr_field_index(u, vals[rec].tab, field);
            if i < 0 { wr_fail(name, str("no field ", field)); }
            desc = u.tables[vals[rec].tab].fields[i];
            slot = vals[rec].first + i;
            index = at;
            if k + 1 < segs.len {
                if at < 0 { rec = slot; continue; }
                if !vals[slot].made || at >= vals[slot].count { wr_fail(name, str(field, " has no element ", at)); }
                rec = vals[slot].first + at;
            }
        }
        let kind = f[6];
        let value = f[7];
        if index >= 0 {
            // the last segment names an element: its slot, at its element
            // descriptor
            if !vals[slot].made || index >= vals[slot].count { wr_fail(name, "no such element"); }
            let e = desc.elem;
            guard e else { wr_fail(name, "an index into no array"); return; }
            desc = e;
            slot = vals[slot].first + index;
        }
        if kind == "int" || kind == "ordinal" || kind == "tag" {
            vals[slot].bits = schema_rt::i128_of(wr_int(name, value));
            vals[slot].state = schema_rt::ValPresent;
            // a lawful tag takes its arm at the arm's declared defaults
            if kind == "tag" { vals[slot].made = false; }
        } else if kind == "float" {
            var bits: u64 = 0;
            if desc.is_f32 {
                bits = if value == "nan" { 0x7FC00000 } else if value == "inf" { 0x7F800000 } else { 0xFF800000 };
            } else {
                bits = if value == "nan" { 0x7FF8000000000000 } else if value == "inf" { 0x7FF0000000000000 } else { 0xFFF0000000000000 };
            }
            vals[slot].bits.lo = bits;
            vals[slot].bits.hi = 0;
            vals[slot].state = schema_rt::ValPresent;
        } else if kind == "length" {
            let n = wr_int(name, value);
            vals[slot].off = data.len;
            for i in n {
                data.push('a');
                if desc.wide { data.push(0); }
            }
            vals[slot].len = if desc.wide { n * 2 } else { n };
            vals[slot].state = schema_rt::ValPresent;
        } else if kind == "count" {
            // the count beside the elements, even past the bound: the slots
            // hold at most the bound, each new one a copy of element 0
            let n = wr_int(name, value);
            if !vals[slot].made || vals[slot].count < 1 { wr_fail(name, "no element 0 to copy"); }
            let have = vals[slot].count;
            let keep = if n < desc.bound { n } else { desc.bound };
            var srcs: i64[>..] = [];
            for k in keep {
                srcs.push(if k < have { vals[slot].first + k } else { wr_clone(vals, vals[slot].first) });
            }
            let base = vals.len;
            for k in keep {
                let v = vals[srcs[k]];
                vals.push(v);
            }
            vals[slot].first = base;
            vals[slot].count = keep;
            vals[slot].len = n;
            vals[slot].state = schema_rt::ValPresent;
        } else {
            wr_fail(name, str("no write-refusal kind ", kind));
        }
        var out: u8[>..<] = [];
        let refusal = schema_rt::save_table(u, f[3], data, vals, nodes, root, out);
        if refusal != 0 {
            if schema_rt::save_refusal_name(refusal).len == 0 { wr_fail(name, "the save refused and named no reason"); }
            answer(name, "refused\n");
            continue;
        }
        answer(name, "wrote\n");
    }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: main.goose list <manifest> | <surface> <manifest> <unit>\n");
        exit(1);
    }
    if argv[1] == "list" {
        write_stdout("wire\nmessage\nreport\nframing\nretain\nretain-save\njson-read\njson-write\njson-hostile\nwrite-refusal\n");
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
    if argv[1] == "json-write" { surface_json(lines, unit, true); return; }
    if argv[1] == "json-hostile" { surface_json_hostile(lines, unit); return; }
    if argv[1] == "write-refusal" { surface_write_refusal(lines, unit); return; }
    // a surface this leg does not answer: ABSENT, never a guessed answer
    exit(2);
}
