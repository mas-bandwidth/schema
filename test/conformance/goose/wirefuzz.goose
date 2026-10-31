// THE GOOSE WIRE-FUZZ LEG (test/conformance/README.md, "The wire-fuzz
// driver"; docs/SPEC-TABLES.md §4.2; docs/PORTING.md I15). The harness holds
// the mutators and the oracle (the compiler's own engine, internal/tablewire);
// this leg loads each mutant through the generated tolerant read, saves what
// it decoded, and answers what happened. It decides nothing.
//
//   wirefuzz <unit> builder   the variable-class file roots, driven as the
//                             harness's --builder arm: Goose's load grows its
//                             own value table, so it has no caller region and
//                             no LoadMeasure, which is that arm's shape
//   wirefuzz <unit> fixed     the value-only roots on the region arm, where a
//                             fixed root has no region and measures -1
//
// ONE UNIT PER PROCESS. Every generated unit carries its own copy of the
// runtime, so two units cannot be one Goose program; make/goose-fuzz.mk builds
// this file once per unit, beside a glue module, conformance_unit.goose, that
// names the unit's registry, and runs the harness once per unit. The roster
// entries of every other unit are a `0` and are never sent a mutant.
//
// The stream is little-endian throughout. In: the roster, then per mutant a
// u32 roster index, a u32 length and the bytes, until end of input. Out: one
// byte per roster entry, then per mutant `u8 loaded; i32 unknown,
// kind_mismatch, widened, clamped, duplicate; u8 malformed; u8 refused; i64
// measure; i32 retained, retain_lost; i64 saved` and the saved bytes, flushed
// before the next mutant is read.
import os;
import .conformance_unit;
import .wire_load;
import .wire_save;

extern "schema_goose_wirefuzz_read" fn read_exact(out: u8[>..]&, n: i64) -> i64;

fn le(b: const u8[:], at: i64, w: i64) -> u64 {
    var v: u64 = 0;
    var i = w - 1;
    while i >= 0 {
        v = (v << 8) | b[at + i];
        i -= 1;
    }
    return v;
}

fn put_le(out: u8[>..]&, v: u64, w: i64) {
    for i in w { out.push((v >> (8 * i)) as! u8); }
}

fn put_i32(out: u8[>..]&, v: i64) { put_le(out, v as! u64, 4); }

fn flag(b: bool) -> u64 { if b { 1 } else { 0 } }

// take reads exactly n bytes of stdin into a fresh builder; end of input in
// the middle of a frame ends the run.
fn take(n: i64) -> u8[>..] {
    var b: u8[>..] = [];
    if read_exact(b, n) != n {
        write_stderr("wirefuzz: the stream ended inside a frame\n");
        exit(1);
    }
    return b;
}

fn main() {
    let argv = args();
    if argv.len != 3 || (argv[2] != "builder" && argv[2] != "fixed") {
        write_stderr("usage: wirefuzz <unit> builder|fixed\n");
        exit(1);
    }
    let unit = conformance_unit();
    let builder = argv[2] == "builder";
    var dbg: u8[>..] = [];
    let debug = env("SCHEMA_GOOSE_WIREFUZZ_DEBUG", dbg);

    // THE ROSTER: answer 1 for a root of this unit this arm drives
    let head = take(4);
    let count = le(head, 0, 4) as! i64;
    var roots: i64[>..] = [];
    var reply: u8[>..] = [];
    for _ in count {
        let kn = take(2);
        let key = take(le(kn, 0, 2) as! i64);
        let rn = take(2);
        let root = take(le(rn, 0, 2) as! i64);
        let fr = take(2);
        var t: i64 = -1;
        if key == argv[1] && fr[0] == 1 && fr[1] == 0 {
            for d, i in unit.tables {
                if d.name == root && d.variable == builder { t = i; break; }
            }
        }
        roots.push(t);
        reply.push(if t >= 0 { 1 } else { 0 });
    }
    write_stdout(reply);
    flush_stdout();

    // THE MUTANTS, until end of input
    loop {
        var frame: u8[>..] = [];
        let got = read_exact(frame, 8);
        if got == 0 { return; }
        if got != 8 {
            write_stderr("wirefuzz: the stream ended inside a frame header\n");
            exit(1);
        }
        let idx = le(frame, 0, 4) as! i64;
        let data = take(le(frame, 4, 4) as! i64);
        if idx >= roots.len || roots[idx] < 0 {
            write_stderr("wirefuzz: a mutant for a root this leg answered 0\n");
            exit(1);
        }
        let name = unit.tables[roots[idx]].name;
        var vals: schema_rt::LoadVal[>..] = [];
        var nodes: schema_rt::LoadNode[>..] = [];
        var report = schema_rt::LoadReport { .. };
        let root = schema_rt::load_table(unit, name, data, vals, nodes, report);
        var saved: u8[>..<] = [];
        var save_failed = true;
        if root >= 0 && builder && report.stopped {
            // a builder read that stopped saves nothing: the partial value is
            // discarded (test/conformance/README.md, the --builder arm)
        } else if root >= 0 {
            let refusal = schema_rt::save_table(unit, name, data, vals, nodes, root, saved);
            save_failed = refusal != 0;
            if save_failed && debug {
                write_stderr(str("wirefuzz: save refused: ", schema_rt::save_refusal_name(refusal), "\n"));
            }
        }
        var out: u8[>..] = [];
        // Whole-load discard applies to both value-only and variable roots.
        out.push(if root >= 0 && !report.stopped { 1 } else { 0 });
        put_i32(out, report.unknown);
        put_i32(out, report.kind_mismatch);
        put_i32(out, report.widened);
        put_i32(out, report.clamped);
        put_i32(out, report.duplicate);
        out.push(flag(report.malformed) as! u8);
        out.push(flag(report.refused || report.non_finite) as! u8);
        put_le(out, 0xFFFFFFFFFFFFFFFF, 8); // measure: no region (-1)
        put_i32(out, 0);                    // retained: this leg drives no retention arm
        put_i32(out, 0);                    // retain_lost
        if save_failed {
            put_le(out, 0xFFFFFFFFFFFFFFFF, 8);
        } else {
            put_le(out, saved.len as! u64, 8);
            out.append(saved);
        }
        write_stdout(out);
        flush_stdout();
    }
}
