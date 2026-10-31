// accel.goose — THE GOOSE LEG'S ACCELERATOR PROGRAM (test/conformance/README.md;
// docs/SPEC-TABLES.md §7, §19; docs/PORT-GOOSE.md, THE HARNESS). The shell
// driver beside this file runs it in place of main.goose for the two
// accelerators' surfaces, ONCE PER UNIT, as main.goose runs:
//
//   accel.goose list <manifest>
//   accel.goose <surface> <manifest> <unit>
//   accel.goose block-lead <manifest> <unit>
//
// It sits in the unit's generated directory beside two glue modules the driver
// writes: conformance_unit.goose (the table wire's registry, which cook-write
// loads an instance through) and conformance_accel.goose (accel_glue in the
// driver: the unit's block tables and cook roots by name, and its records as
// data). The surfaces:
//
//   block, block-foreign     open every block image (the second with its
//                            magic byte-reversed), `open` or `refuse`
//   block-dump               the ROW DUMP of every block image
//   forgery, block-reason    the block forgery battery: verdict, then reason
//   cook, cook-foreign       the NODE DUMP of every cook (the second: refuse)
//   cook-forgery, cook-reason the cook forgery battery: verdict, then reason
//   cook-write               load an instance's wire, cook it from the TYPED
//                            record in both byte orders (§7.6); a root this
//                            leg has no writer for is a named absence
//
// `block-lead` is no harness surface: it is the block lead gate
// (docs/PORTING.md I5), every block image at every lead 0..64, which make
// tables-goose-block-lead runs. Nor are the forgery fuzzer's two halves
// (docs/PORTING.md I3), which make tables-goose-fuzz runs:
//
//   accel.goose block-fuzz <manifest> <unit> <seed> <n>
//   accel.goose cook-fuzz <manifest> <unit> <seed> <n> <fixtures dir>
//
// BOTH DUMPS ARE WRITTEN AGAINST THE DESCRIPTORS AND NOTHING ELSE — the unit's
// BlockRecords and CookRecords, the records as data — so two implementations'
// reads of the same bytes are byte-compared (testdata/conformance/tables/
// FORMAT.md, block/<name>.dump and cook/<case>.dump), exactly as the C leg's
// unit_blockdemo.c and unit_graphdemo.c walk theirs.
//
// THE BUFFER A CALLER HOLDS is the bytes the manifest names, cut or
// zero-extended to the extent the caller claims, at `lead` bytes past an
// aligned base (the pointer column; null is no buffer at all). A Goose slice
// has no address, so the lead travels as the stated fact the generated Open
// checks (runtime/block_form.goose).
import os;
import .conformance_unit;
import .conformance_accel;
import .block_form;
import .cook_form;
import .wire_load;

fn hex_answer(name: const u8[:], answer: const u8[:]) {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    line.append(name);
    line.push(' ');
    for x in answer {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    line.push('\n');
    write_stdout(line);
}

fn absent_answer(name: const u8[:], reason: const u8[:]) {
    write_stdout(str(name, " absent ", reason, "\n"));
}

fn slurp(path: const u8[:]) -> u8[>..] {
    var data: u8[>..] = [];
    if !read_file(path, data) {
        write_stderr(str("driver: cannot read ", path, "\n"));
        exit(1);
    }
    return data;
}

fn number(s: const u8[:]) -> i64 {
    if s == "null" { return -1; }
    if starts_with(s, "0x") {
        let v, ok = parse_int(s[2..], 16);
        if !ok { write_stderr(str("driver: not a number: ", s, "\n")); exit(1); }
        return v;
    }
    let v, ok = parse_int(s);
    if !ok { write_stderr(str("driver: not a number: ", s, "\n")); exit(1); }
    return v;
}

fn verdict(reason: i64) -> const u8[:] {
    if reason == schema_rt::TableRefuseOk { return "open\n"; }
    return "refuse\n";
}

// foreign makes a file foreign to WHOEVER READS IT: the magic's eight bytes
// reversed are what the word looks like to a reader of the other order, so
// every leg on every host refuses by magic (test/conformance/harness/run.go).
fn foreign(data: u8[:]) {
    if data.len < 8 { return; }
    var i: i64 = 0;
    while i < 4 {
        let t = data[i];
        data[i] = data[7 - i];
        data[7 - i] = t;
        i += 1;
    }
}

// buffer is the caller's buffer: the bytes, cut or zero-extended to extent
// (below zero: the bytes' own length); none at all for a lead below zero.
fn buffer(out: u8[>..]&, data: const u8[:], extent: i64, lead: i64) {
    if lead < 0 { return; }
    var n = extent;
    if n < 0 { n = data.len; }
    var i: i64 = 0;
    while i < n {
        if i < data.len { out.push(data[i]); } else { out.push(0); }
        i += 1;
    }
}

// ---- which unit a manifest line belongs to ----

// block_table is the table a block image of the manifest is a block of: the
// manifest names an image, and the C leg's unit_blockdemo.c says which.
fn block_table(name: const u8[:]) -> const u8[:] {
    if starts_with(name, "block_render") { return "RenderFrame"; }
    if starts_with(name, "block_padded") { return "PaddedFrame"; }
    write_stderr(str("driver: no block named ", name, "\n"));
    exit(1);
}

// subject_unit is the unit a forgery's subject lives in: a block image's
// `block` line, or a cook root's `cook` line, names it. "" for none.
fn subject_unit(lines: const u8[:][:], kind: const u8[:], subject: const u8[:]) -> const u8[:] {
    for line in lines {
        let f = words(line);
        if kind == "block" && f.len > 3 && f[0] == "block" && f[1] == subject { return f[2]; }
        if kind == "cook" && f.len > 4 && f[0] == "cook" && f[3] == subject { return f[2]; }
    }
    return "";
}

fn open_reason(kind: const u8[:], subject: const u8[:], b: const u8[:], lead: i64) -> i64 {
    var reason: i64 = 0;
    if kind == "block" {
        reason = accel_block_reason(block_table(subject), b, lead);
    } else {
        reason = accel_cook_reason(subject, b, lead);
    }
    if reason < 0 {
        write_stderr(str("driver: this unit has no ", kind, " named ", subject, "\n"));
        exit(1);
    }
    return reason;
}

// ---- the dumps ----

// dump_text is a string's or a `bytes`' USED bytes, quoted, every byte outside
// printable ASCII (and `"` and `\`) written \xNN, then ` len=<n>`.
fn dump_text(out: u8[>..]&, b: const u8[:], at: i64, used: i64) {
    format(out, "\"");
    var i: i64 = 0;
    while i < used {
        let c = b[at + i];
        if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
            out.push(c);
        } else {
            format(out, "\\x");
            format_uint(out, c, 16, 2, '0');
        }
        i += 1;
    }
    format(out, "\" len=", used);
}

// dump_scalar is one slot at its elem_size: a bool, a float's BIT PATTERN
// (the block's spelling; the cook refuses a float), the four signed integer
// kinds sign-extended, and every other slot unsigned — an enum's ordinal, a
// flags mask.
fn dump_scalar(out: u8[>..]&, b: const u8[:], at: i64, kind: u8, width: i64) {
    if kind == 1 {
        if b[at] != 0 { format(out, "true"); } else { format(out, "false"); }
    } else if kind == 10 {
        format(out, "0x");
        format_uint(out, schema_rt::accel_u32(b, at), 16, 8, '0');
    } else if kind == 11 {
        format(out, "0x");
        format_uint(out, schema_rt::accel_u64(b, at), 16, 16, '0');
    } else if kind >= 2 && kind <= 5 {
        format(out, schema_rt::accel_get_signed(b, at, width));
    } else {
        format_uint(out, schema_rt::accel_get(b, at, width), 10, 0, ' ');
    }
}

// Path is a dotted field path, fixed-size so a recursive walk may hold one
// across its call (§7.8 of the pinned spec); a path past it is cut.
type Path = u8[..512];

fn append_path(dst: Path&, s: const u8[:]) {
    for c in s {
        if dst.len < 512 { dst.push(c); }
    }
}

fn join(dst: Path&, prefix: const u8[:], name: const u8[:]) {
    dst.clear();
    if prefix.len > 0 {
        append_path(dst, prefix);
        append_path(dst, ".");
    }
    append_path(dst, name);
}

fn index_path(dst: Path&, name: const u8[:], s: i64, indexed: bool) {
    dst.clear();
    append_path(dst, name);
    if indexed {
        append_path(dst, "[");
        var digits: u8[>..] = [];
        format(digits, s);
        append_path(dst, digits);
        append_path(dst, "]");
    }
}

// block_dump_record is one record's leaves, at two spaces, in descriptor
// order; out-of-line arrays are their own sections, not leaves.
recursive fn block_dump_record(out: u8[>..]&, b: const u8[:], base: i64, r: i64, path: const u8[:]) -> bool {
    let rec = accel_block_records()[r];
    for f in rec.fields {
        if f.out_of_line { continue; }
        var name: Path = [];
        join(name, path, f.name);
        if f.counted {
            let used = schema_rt::accel_get_signed(b, base + f.count_offset, 4);
            if used < 0 || used > f.array_bound {
                write_stderr(str("driver: ", rec.name, ".", f.name, " carries a used length outside its bound\n"));
                return false;
            }
            format(out, "  ", name, " = ");
            dump_text(out, b, base + f.offset, used);
            format(out, "\n");
        } else {
            var slots: i64 = 1;
            if f.is_array { slots = f.array_bound; }
            var s: i64 = 0;
            while s < slots {
                let at = base + f.offset + s * f.elem_size;
                var label: Path = [];
                index_path(label, name, s, f.is_array);
                if f.record >= 0 {
                    if !block_dump_record(out, b, at, f.record, label) { return false; }
                } else {
                    format(out, "  ", label, " = ");
                    dump_scalar(out, b, at, f.kind, f.elem_size);
                    format(out, "\n");
                }
                s += 1;
            }
        }
        if f.optional {
            format(out, "  ", name, "#present = ");
            if b[base + f.present_offset] != 0 { format(out, "true\n"); } else { format(out, "false\n"); }
        }
    }
    return true;
}

// block_dump is the whole ROW DUMP of one opened block: the projection's own
// fields, then every out-of-line array in declaration order, row by row, at
// the offset, count and stride read OUT OF THE INSTANCE. Each array's pitch
// constant is held to its row record's size first — the layout contract's
// one pair of independent derivations (docs/SPEC-TABLES.md §19.3, §19.5).
fn block_dump(out: u8[>..]&, name: const u8[:], b: const u8[:]) -> bool {
    let table = block_table(name);
    if accel_block_reason(table, b, 0) != schema_rt::TableRefuseOk {
        write_stderr(str("driver: block ", name, " does not open\n"));
        return false;
    }
    let p = accel_block_record(table);
    let records = accel_block_records();
    let rec = records[p];
    format(out, "projection ", rec.name, " @0\n");
    if !block_dump_record(out, b, 0, p, "") { return false; }
    for f in rec.fields {
        if !f.out_of_line { continue; }
        let row = records[f.record];
        if f.stride != row.size {
            write_stderr(str("driver: ", rec.name, ".", f.name, "'s pitch constant is ", f.stride, ", its row ", row.name, " is ", row.size, " bytes\n"));
            return false;
        }
        let offset_of = schema_rt::accel_u64(b, f.offset_of_offset) as! i64;
        let count = schema_rt::accel_u32(b, f.count_offset) as! i64;
        let stride = schema_rt::accel_u32(b, f.stride_offset) as! i64;
        format(out, "array ", f.name, " ", row.name, " @", offset_of, " count=", count, " stride=", stride, "\n");
        var i: i64 = 0;
        while i < count {
            let at = offset_of + i * stride;
            format(out, "row ", i, " @", at, "\n");
            if !block_dump_record(out, b, at, f.record, "") { return false; }
            i += 1;
        }
    }
    return true;
}

// The node dump's state: whether it has failed. What it has written is `out`,
// and every node it has reached, by offset, in visit order — one node, one
// visit (§6.3) — is `reached`.
struct CookWalk { failed: bool, quiet: bool }

fn cook_fail(w: CookWalk&, why: const u8[:]) {
    if !w.failed && !w.quiet { write_stderr(str("driver: ", why, "\n")); }
    w.failed = true;
}

recursive fn cook_node(out: u8[>..]&, reached: i64[>..]&, w: CookWalk&, region: const u8[:], offset: i64, r: i64, depth: i64) {
    if w.failed { return; }
    if depth > 4096 { cook_fail(w, "the walk nested past any depth a region can hold"); return; }
    if contains(reached, offset) { return; }
    let rec = accel_cook_records()[r];
    if offset > region.len || rec.size > region.len - offset { cook_fail(w, "a node does not fit inside the region"); return; }
    format(out, "node ", reached.len, " ", rec.name, " @", offset, "\n");
    reached.push(offset);
    cook_storage(out, reached, w, region, offset, r, depth, "");
}

// cook_storage walks one record's fields in descriptor order, DEPTH FIRST AND
// EMITTING AS IT GOES: a pointer emits its line and descends at once, so a
// subtree's nodes land before the parent's remaining leaves (FORMAT.md).
recursive fn cook_storage(out: u8[>..]&, reached: i64[>..]&, w: CookWalk&, region: const u8[:], base: i64, r: i64, depth: i64, path: const u8[:]) {
    let rec = accel_cook_records()[r];
    for f in rec.fields {
        if w.failed { return; }
        var name: Path = [];
        join(name, path, f.name);
        var used: i64 = -1;
        if f.counted && f.count_offset >= 0 {
            used = schema_rt::accel_get_signed(region, base + f.count_offset, 4);
            if used < 0 || used > f.array_bound { cook_fail(w, "a count companion is outside its declared bound"); return; }
        }
        if f.is_union { cook_fail(w, "the dump met a union, which this corpus does not carry"); return; }
        if f.is_pointer {
            var slots: i64 = 1;
            if f.is_array { slots = f.array_bound; }
            var s: i64 = 0;
            while s < slots {
                var label: Path = [];
                index_path(label, name, s, f.is_array);
                let slot = base + f.offset + s * 8;
                let delta = schema_rt::accel_u64(region, slot);
                if delta == 0 {
                    format(out, "  ", label, " = null\n");
                } else {
                    let target = schema_rt::cook_deref(region, slot);
                    if target < 0 { cook_fail(w, "a reference resolves outside the region"); return; }
                    if f.record < 0 { cook_fail(w, "a pointer's descriptor names no table"); return; }
                    format(out, "  ", label, " = -> @", target, "\n");
                    cook_node(out, reached, w, region, target, f.record, depth + 1);
                    if w.failed { return; }
                }
                s += 1;
            }
        } else if f.is_text || f.is_bytes {
            format(out, "  ", name, " = ");
            var n = used;
            if n < 0 { n = 0; }
            dump_text(out, region, base + f.offset, n);
            format(out, "\n");
        } else if f.record >= 0 {
            var slots: i64 = 1;
            if f.is_array { slots = f.array_bound; }
            var s: i64 = 0;
            while s < slots {
                var label: Path = [];
                index_path(label, name, s, f.is_array);
                cook_storage(out, reached, w, region, base + f.offset + s * f.elem_size, f.record, depth, label);
                if w.failed { return; }
                s += 1;
            }
        } else {
            if f.kind == 10 || f.kind == 11 { cook_fail(w, "the dump met a float, whose canonical cross-language spelling this gate does not fix"); return; }
            var slots: i64 = 1;
            if f.is_array { slots = f.array_bound; }
            var s: i64 = 0;
            while s < slots {
                format(out, "  ", name);
                if f.is_array { format(out, "[", s, "]"); }
                format(out, " = ");
                dump_scalar(out, region, base + f.offset + s * f.elem_size, f.kind, f.elem_size);
                format(out, "\n");
                s += 1;
            }
        }
        if f.counted && f.count_offset >= 0 && !f.is_text && !f.is_bytes {
            format(out, "  ", name, "#count = ", used, "\n");
        }
        if f.optional && f.present_offset >= 0 {
            format(out, "  ", name, "#present = ");
            if region[base + f.present_offset] != 0 { format(out, "true\n"); } else { format(out, "false\n"); }
        }
    }
}

// cook_dump opens a cook at its root and writes the canonical NODE DUMP.
fn cook_dump(out: u8[>..]&, root: const u8[:], b: const u8[:]) -> bool {
    if open_reason("cook", root, b, 0) != schema_rt::TableRefuseOk {
        write_stderr(str("driver: the cook at root ", root, " does not open\n"));
        return false;
    }
    let region = schema_rt::cook_region(b);
    var reached: i64[>..] = [];
    var w = CookWalk { failed: false, quiet: false };
    cook_node(out, reached, w, region, 0, accel_cook_record(root), 0);
    return !w.failed;
}

// ---- the surfaces ----

fn surface_open(lines: const u8[:][:], unit: const u8[:], kind: const u8[:], foreign_order: bool) {
    for line in lines {
        let l = words(line);
        if kind == "block" {
            if l.len < 4 || l[0] != "block" || l[2] != unit { continue; }
            var data = slurp(l[3]);
            if foreign_order { foreign(data); }
            hex_answer(l[1], verdict(open_reason("block", l[1], data, 0)));
        } else {
            if l.len < 5 || l[0] != "cook" || l[2] != unit { continue; }
            var data = slurp(l[4]);
            if foreign_order {
                foreign(data);
                hex_answer(l[1], verdict(open_reason("cook", l[3], data, 0)));
                continue;
            }
            var out: u8[>..] = [];
            if !cook_dump(out, l[3], data) { exit(1); }
            hex_answer(l[1], out);
        }
    }
}

fn surface_block_dump(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let l = words(line);
        if l.len < 4 || l[0] != "block" || l[2] != unit { continue; }
        let data = slurp(l[3]);
        var out: u8[>..] = [];
        if !block_dump(out, l[1], data) { exit(1); }
        hex_answer(l[1], out);
    }
}

// forgery_reason opens one forgery line's fixture as its caller holds it.
fn forgery_reason(f: const u8[:][:]) -> i64 {
    let data = slurp(f[4]);
    let lead = number(f[6]);
    var buf: u8[>..] = [];
    buffer(buf, data, number(f[5]), lead);
    return open_reason(f[2], f[3], buf, lead);
}

// surface_forgery: the verdict (reasons false) or the first failing clause's
// name (reasons true) of every forgery of one kind whose subject is this
// unit's. The reason surfaces answer the forgeries a `refusal` line names.
fn surface_forgery(lines: const u8[:][:], unit: const u8[:], kind: const u8[:], reasons: bool) {
    for line in lines {
        let l = words(line);
        if reasons {
            if l.len < 3 || l[0] != "refusal" || l[2] != kind { continue; }
            var found = false;
            for other in lines {
                let f = words(other);
                if f.len < 7 || f[0] != "forgery" || f[1] != l[1] { continue; }
                found = true;
                if subject_unit(lines, kind, f[3]) != unit { break; }
                hex_answer(l[1], str(schema_rt::table_refuse_name(forgery_reason(f)), "\n"));
                break;
            }
            if !found { write_stderr(str("driver: no forgery named ", l[1], "\n")); exit(1); }
            continue;
        }
        if l.len < 7 || l[0] != "forgery" || l[2] != kind { continue; }
        if subject_unit(lines, kind, l[3]) != unit { continue; }
        hex_answer(l[1], verdict(forgery_reason(l)));
    }
}

// surface_block_lead is THE BLOCK LEAD GATE (docs/PORTING.md I5): every block
// image of this unit at every lead 0..64 past an aligned base — 0 and 64
// open, 1..63 refuse by name, unaligned_base. A base-alignment check no gate
// exercises is dead code, and unaligned reads succeed on every host.
fn surface_block_lead(lines: const u8[:][:], unit: const u8[:]) {
    var failed = false;
    var images: i64 = 0;
    for line in lines {
        let l = words(line);
        if l.len < 4 || l[0] != "block" || l[2] != unit { continue; }
        images += 1;
        let data = slurp(l[3]);
        var lead: i64 = 0;
        while lead <= 64 {
            let reason = open_reason("block", l[1], data, lead);
            var want = schema_rt::TableRefuseUnalignedBase;
            if lead == 0 || lead == 64 { want = schema_rt::TableRefuseOk; }
            if reason != want {
                write_stdout(str(l[1], " at lead ", lead, " answered ", schema_rt::table_refuse_name(reason), ", wanted ", schema_rt::table_refuse_name(want), "\n"));
                failed = true;
            }
            lead += 1;
        }
    }
    if images == 0 { write_stdout(str("block-lead: unit ", unit, " has no block image\n")); exit(1); }
    if failed { exit(1); }
    write_stdout(str("block-lead: ", unit, ": ", images, " block images x 65 leads — 0 and 64 open, 1..63 refuse unaligned_base\n"));
}

// surface_cook_write: THE COOK WRITE SIDE (docs/SPEC-TABLES.md §7.6). Each
// instance of this unit is loaded from its wire, copied into its TYPED root
// record, measured and cooked in both byte orders; the harness compares the
// bytes against `schema cook`'s. A capacity one byte short must write
// nothing. A root this unit has no typed writer for answers a named absence.
fn surface_cook_write(lines: const u8[:][:], unit: const u8[:]) {
    for line in lines {
        let c = words(line);
        if c.len < 2 || c[0] != "cook-write" { continue; }
        for other in lines {
            let f = words(other);
            if f.len < 5 || f[0] != "instance" || f[1] != c[1] { continue; }
            if f[2] != unit { break; }
            let data = slurp(f[4]);
            var vals: schema_rt::LoadVal[>..] = [];
            var nodes: schema_rt::LoadNode[>..] = [];
            var report = schema_rt::LoadReport { .. };
            let root = schema_rt::load_table(conformance_unit(), f[3], data, vals, nodes, report);
            if root < 0 || report.malformed || report.refused {
                write_stderr(str("driver: ", c[1], ": the cook input does not load\n"));
                exit(1);
            }
            let need = accel_cook_measure(f[3]);
            if need < 0 {
                absent_answer(c[1], "no-typed-cook-writer");
                absent_answer(str(c[1], "-be"), "no-typed-cook-writer");
                break;
            }
            var big = false;
            while true {
                var name: u8[>..] = [];
                name.append(c[1]);
                if big { name.append("-be"); }
                var file: u8[>..] = [];
                var i: i64 = 0;
                while i < need { file.push(0xa5); i += 1; }
                if accel_cook_write(f[3], data, vals, root, file[0..need - 1], big) {
                    write_stderr(str("driver: ", name, ": a capacity one byte short cooked\n"));
                    exit(1);
                }
                for x in file {
                    if x != 0xa5 { write_stderr(str("driver: ", name, ": a capacity one byte short wrote bytes\n")); exit(1); }
                }
                if !accel_cook_write(f[3], data, vals, root, file, big) {
                    write_stderr(str("driver: ", name, ": the cook write refused\n"));
                    exit(1);
                }
                hex_answer(name, file);
                if big { break; }
                big = true;
            }
            break;
        }
    }
}

// ---- THE FORGERY FUZZER (docs/PORTING.md I3; docs/SPEC-TABLES.md §19.2, §7) ----
//
// The forgery battery above is a fixed list, one row per clause Open checks.
// This is the standing gate beside it, the Go leg's TestBlockForgeryFuzz and
// TestCookForgeryFuzz in Goose: valid images from the corpus, mutated by the
// passes below, and ONE ORACLE over every mutant —
//
//   REFUSE, or OPEN and be WHOLE. A mutant either makes Open refuse, or it
//   opens, and then every bound the oracle RE-DERIVES from the descriptors
//   (the unit's BlockRecords and CookRecords, the records as data) and from
//   the words the instance carries holds — never Open's own arithmetic, which
//   is the only reason the two can disagree — and a READ of every byte of
//   every row (the block) or of the region and every node a walk reaches
//   (the cook) stays inside the extent the caller passed.
//
// WHAT GOOSE GIVES AND WHAT IT DOES NOT. Every Goose slice index is checked:
// a read past the view aborts the program (goose_spec.md, the abort list), so
// the walk cannot read a neighbour, which the sanitized twin is for on the
// C++ side; the oracle's own arithmetic names the forgery before the walk
// would. Open takes a `const u8[:]`, so a write past the extent is not
// expressible, and the C++ fuzzer's poison check has nothing to watch here.
//
// Every mutant is a pure function of (seed, image, index): the passes draw
// from the standard library's splitmix64, seeded with the seed and the
// image's name, so a failure names the one case that reproduces it.

fn fuzz_seed(seed: i64, name: const u8[:]) -> rng {
    var h: u64 = 0xcbf29ce484222325;
    for c in name {
        h = (h ^ c) * 0x100000001b3;
    }
    return rng { (seed as! u64) ^ h };
}

// fuzz_mutate applies ONE deterministic mutation and names it: a word of the
// prologue or header, a word of a triple, a bit anywhere, and the two facts no
// file can carry — the caller's CLAIMED EXTENT (a short claim is what a
// truncation is; `buffer` cuts or zero-extends to it, so nothing clamps the
// draw back to the file's own length) and the BASE's lead past an aligned
// address.
fn fuzz_mutate(r: rng&, m: u8[:], claim: i64&, lead: i64&, why: u8[>..]&) {
    let pass = r.rand_int(5);
    if pass == 0 {
        let at = r.rand_int(m.len / 8) * 8;
        schema_rt::accel_put(m, at, r.rand_u64(), 8);
        format(why, "u64 at ", at);
    } else if pass == 1 {
        let at = r.rand_int(m.len / 4) * 4;
        schema_rt::accel_put(m, at, r.rand_u64(), 4);
        format(why, "u32 at ", at);
    } else if pass == 2 {
        let at = r.rand_int(m.len);
        let bit = r.rand_int(8) as u64;
        m[at] = m[at] ^ ((1 as u64) << bit) as! u8;
        format(why, "bit at ", at);
    } else if pass == 3 {
        claim = r.rand_int(m.len * 2);
        format(why, "claimed extent ", claim);
    } else {
        lead = r.rand_int(64);
        format(why, "base +", lead);
    }
}

// fuzz_fail names the case and stops: the first mutant the oracle rejects is
// the finding.
fn fuzz_fail(what: const u8[:], i: i64, why: const u8[:], finding: const u8[:]) {
    write_stdout(str("FUZZ FAILED: ", what, " case ", i, " (", why, "): ", finding, "\n"));
    exit(1);
}

// block_whole is the BLOCK ORACLE over one opened mutant: the projection
// inside the extent, then per out-of-line array of the projection's record
// the triple read out of the instance — the pitch against the ROW RECORD'S
// size (this build's own), the count against the descriptor's declared
// maximum, the offset_of and the rows inside the extent — and the used
// extent the generated <T>BlockBytes answers inside it too. Then it READS
// every byte of the projection and of every row. It answers the finding, ""
// for whole.
fn block_whole(table: const u8[:], b: const u8[:], sink: u64&, finding: u8[>..]&) -> bool {
    let records = accel_block_records();
    let rec = records[accel_block_record(table)];
    let extent = b.len as u64;
    if (rec.size as u64) > extent { format(finding, "the projection of ", rec.size, " bytes leaves an extent of ", extent); return false; }
    for f in rec.fields {
        if !f.out_of_line { continue; }
        let row = records[f.record];
        let offset_of = schema_rt::accel_u64(b, f.offset_of_offset);
        let count = schema_rt::accel_u32(b, f.count_offset);
        let pitch = schema_rt::accel_u32(b, f.stride_offset);
        if pitch != row.size as u64 { format(finding, f.name, " opened at a pitch of ", pitch, ", and this build's row is ", row.size); return false; }
        if count > f.array_bound as u64 { format(finding, f.name, " opened with ", count, " rows, past the declared maximum of ", f.array_bound); return false; }
        if offset_of > extent { format(finding, f.name, " starts at ", offset_of, ", outside an extent of ", extent); return false; }
        let rows = count * pitch; // both bounded above: this cannot carry
        if rows > extent - offset_of { format(finding, f.name, "'s ", count, " rows at ", offset_of, " leave an extent of ", extent); return false; }
        var i = offset_of as! i64;
        let end = (offset_of + rows) as! i64;
        while i < end {
            sink = sink ^ b[i];
            i += 1;
        }
    }
    let used = accel_block_bytes(table, b);
    if used < 0 || used > b.len { format(finding, "Open reported ", used, " used bytes inside an extent of ", b.len); return false; }
    var i: i64 = 0;
    while i < rec.size {
        sink = sink ^ b[i];
        i += 1;
    }
    return true;
}

// fuzz_block is the block half: every block image of this unit, the clean
// image opened and held whole first (or the fuzzer mutates nothing), then N
// mutants.
fn fuzz_block(lines: const u8[:][:], unit: const u8[:], seed: i64, n: i64) {
    var images: i64 = 0;
    var sink: u64 = 0;
    for line in lines {
        let l = words(line);
        if l.len < 4 || l[0] != "block" || l[2] != unit { continue; }
        images += 1;
        let table = block_table(l[1]);
        let image = slurp(l[3]);
        if accel_block_reason(table, image, 0) != schema_rt::TableRefuseOk { fuzz_fail(l[1], -1, "clean", "the corpus's own image does not open"); }
        var clean: u8[>..] = [];
        if !block_whole(table, image, sink, clean) { fuzz_fail(l[1], -1, "clean", clean); }
        var r = fuzz_seed(seed, l[1]);
        var refused: i64 = 0;
        var opened: i64 = 0;
        var i: i64 = 0;
        while i < n {
            var m: u8[>..] = [];
            m.append(image);
            var claim = image.len;
            var lead: i64 = 0;
            var why: u8[>..] = [];
            fuzz_mutate(r, m, claim, lead, why);
            var buf: u8[>..] = [];
            buffer(buf, m, claim, lead);
            if accel_block_reason(table, buf, lead) != schema_rt::TableRefuseOk {
                refused += 1;
            } else {
                opened += 1;
                var finding: u8[>..] = [];
                if !block_whole(table, buf, sink, finding) { fuzz_fail(l[1], i, why, finding); }
            }
            i += 1;
        }
        write_stdout(str("block-fuzz: ", l[1], ": seed ", seed, ", ", n, " mutants: ", refused, " refused, ", opened, " opened and walked whole (", sink & 1, ")\n"));
    }
    if images == 0 { write_stdout(str("block-fuzz: unit ", unit, " has no block image\n")); exit(1); }
}

// cook_whole is the COOK ORACLE over one opened mutant: the data part
// re-derived from the header words — align_up(64, alignment), the data
// length — inside the extent and at least the root record's size, every byte
// of it READ, and then the node walk the cook surface takes, from the root,
// every pointer through cook_deref: a walk that meets a forged count or a
// reference outside the region stops by name (that is a refusal of the read,
// not an escape); a walk that would read outside the region aborts.
fn cook_whole(root: const u8[:], b: const u8[:], sink: u64&, walks: i64&, finding: u8[>..]&) -> bool {
    let rec = accel_cook_records()[accel_cook_record(root)];
    let extent = b.len as u64;
    if extent < schema_rt::TableCookHeaderBytes as u64 { format(finding, "a cook of ", extent, " bytes opened, shorter than its header"); return false; }
    let alignment = schema_rt::accel_u64(b, 40);
    if alignment == 0 || alignment > 4096 { format(finding, "a cook opened naming a region alignment of ", alignment); return false; }
    let data_offset = ((schema_rt::TableCookHeaderBytes as u64) + alignment - 1) / alignment * alignment;
    let data_length = schema_rt::accel_u64(b, 24);
    if data_offset > extent || data_length > extent - data_offset { format(finding, "the region of ", data_length, " bytes at ", data_offset, " leaves a file of ", extent); return false; }
    if data_length < rec.size as u64 { format(finding, "a region of ", data_length, " bytes opened for a root of ", rec.size); return false; }
    var i = data_offset as! i64;
    let end = (data_offset + data_length) as! i64;
    while i < end {
        sink = sink ^ b[i];
        i += 1;
    }
    let region = schema_rt::cook_region(b);
    if region.len as u64 != data_length { format(finding, "cook_region answered ", region.len, " bytes, the header ", data_length); return false; }
    var out: u8[>..] = [];
    var reached: i64[>..] = [];
    var w = CookWalk { failed: false, quiet: true };
    cook_node(out, reached, w, region, 0, accel_cook_record(root), 0);
    if !w.failed { walks += 1; }
    return true;
}

// fuzz_cook is the cook half: every cook case of this unit, its fixture the
// Go reference's (the harness materialises them under <fixtures>), the clean
// cook opened, held whole and walked first, then N mutants.
fn fuzz_cook(lines: const u8[:][:], unit: const u8[:], seed: i64, n: i64, fixtures: const u8[:]) {
    var cases: i64 = 0;
    var sink: u64 = 0;
    for line in lines {
        let l = words(line);
        if l.len < 5 || l[0] != "cook" || l[2] != unit { continue; }
        cases += 1;
        let root = l[3];
        let image = slurp(str(fixtures, "/", l[1], ".cook"));
        if accel_cook_reason(root, image, 0) != schema_rt::TableRefuseOk { fuzz_fail(l[1], -1, "clean", "the corpus's own cook does not open"); }
        var walks: i64 = 0;
        var clean: u8[>..] = [];
        if !cook_whole(root, image, sink, walks, clean) { fuzz_fail(l[1], -1, "clean", clean); }
        if walks != 1 { fuzz_fail(l[1], -1, "clean", "the corpus's own cook does not walk"); }
        walks = 0;
        var r = fuzz_seed(seed, l[1]);
        var refused: i64 = 0;
        var opened: i64 = 0;
        var i: i64 = 0;
        while i < n {
            var m: u8[>..] = [];
            m.append(image);
            var claim = image.len;
            var lead: i64 = 0;
            var why: u8[>..] = [];
            fuzz_mutate(r, m, claim, lead, why);
            var buf: u8[>..] = [];
            buffer(buf, m, claim, lead);
            if accel_cook_reason(root, buf, lead) != schema_rt::TableRefuseOk {
                refused += 1;
            } else {
                opened += 1;
                var finding: u8[>..] = [];
                if !cook_whole(root, buf, sink, walks, finding) { fuzz_fail(l[1], i, why, finding); }
            }
            i += 1;
        }
        write_stdout(str("cook-fuzz: ", l[1], ": seed ", seed, ", ", n, " mutants: ", refused, " refused, ", opened, " opened and held whole, ", walks, " walked to the end (", sink & 1, ")\n"));
    }
    if cases == 0 { write_stdout(str("cook-fuzz: unit ", unit, " has no cook\n")); exit(1); }
}

fn main() {
    let argv = args();
    if argv.len < 3 {
        write_stderr("usage: accel.goose list <manifest> | <surface> <manifest> <unit>\n");
        exit(1);
    }
    let surface = argv[1];
    if surface == "list" {
        write_stdout("cook\ncook-write\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\ncook-reason\nblock-reason\n");
        return;
    }
    if argv.len < 4 {
        write_stderr("usage: accel.goose <surface> <manifest> <unit>\n");
        exit(1);
    }
    let text = slurp(argv[2]);
    let lines = split(text, '\n');
    let unit = argv[3];
    if surface == "block" { surface_open(lines, unit, "block", false); return; }
    if surface == "block-foreign" { surface_open(lines, unit, "block", true); return; }
    if surface == "cook" { surface_open(lines, unit, "cook", false); return; }
    if surface == "cook-foreign" { surface_open(lines, unit, "cook", true); return; }
    if surface == "block-dump" { surface_block_dump(lines, unit); return; }
    if surface == "forgery" { surface_forgery(lines, unit, "block", false); return; }
    if surface == "block-reason" { surface_forgery(lines, unit, "block", true); return; }
    if surface == "cook-forgery" { surface_forgery(lines, unit, "cook", false); return; }
    if surface == "cook-reason" { surface_forgery(lines, unit, "cook", true); return; }
    if surface == "cook-write" { surface_cook_write(lines, unit); return; }
    if surface == "block-lead" { surface_block_lead(lines, unit); return; }
    if surface == "block-fuzz" && argv.len == 6 { fuzz_block(lines, unit, number(argv[4]), number(argv[5])); return; }
    if surface == "cook-fuzz" && argv.len == 7 { fuzz_cook(lines, unit, number(argv[4]), number(argv[5]), argv[6]); return; }
    // a surface this program does not answer: ABSENT, never a guessed answer
    exit(2);
}
