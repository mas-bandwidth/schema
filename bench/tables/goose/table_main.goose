// the tables bench — the Goose Form-1 runner (bench/tables/README.md).
//
// Shape-blind: names the generated root once at `rootName` and nowhere else.
import os;
import .benchtable_unit;
import .schema_rt;
import .wire_load;
import .wire_save;

const numVariants = 64;
const defaultIters = 12800;
const maxRuns = 7;
const rootName = "TableMixed";

var csv = false;
var gateOnly = false;
var numRuns = maxRuns;
var itersOverride = 0;
var sink: u64 = 0;

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len {
        if a[i] != b[i] { return false; }
    }
    return true;
}

fn parse_i64(s: const u8[:]) -> i64, bool {
    if s.len == 0 { return 0, false; }
    var v: i64 = 0;
    for b in s {
        if b < '0' || b > '9' { return 0, false; }
        v = v * 10 + (b - '0');
    }
    return v, true;
}

fn fnv_bytes(h_in: u64, b: const u8[:]) -> u64 {
    var h = h_in;
    for x in b {
        h = (h ^ x) * 0x100000001b3;
    }
    return h;
}

fn hex_u64(v: u64) -> u8[>..] {
    let digits = "0123456789abcdef";
    var out: u8[>..] = [];
    for i in 16 {
        let shift = ((15 - i) * 4) as! u64;
        out.push(digits[((v >> shift) & 15) as! i64]);
    }
    return out;
}

fn sort_f64(a: f64[>..]&) {
    var i = 1;
    while i < a.len {
        let x = a[i];
        var j = i - 1;
        while j >= 0 && a[j] > x {
            a[j + 1] = a[j];
            j -= 1;
        }
        a[j + 1] = x;
        i += 1;
    }
}

fn fmt_int(v_in: i64) -> u8[>..] {
    var out: u8[>..] = [];
    if v_in == 0 {
        out.push('0');
        return out;
    }
    var v = v_in;
    var rev: u8[>..] = [];
    while v > 0 {
        rev.push((48 + (v % 10)) as! u8);
        v /= 10;
    }
    var i = rev.len - 1;
    while i >= 0 {
        out.push(rev[i]);
        i -= 1;
    }
    return out;
}

fn fmt_f2(v: f64) -> u8[>..] {
    let scaled = (v * 100.0 + 0.5) as! i64;
    var out = fmt_int(scaled / 100);
    let frac = scaled % 100;
    out.push('.');
    out.push((48 + (frac / 10)) as! u8);
    out.push((48 + (frac % 10)) as! u8);
    return out;
}

fn fail(what: const u8[:]) {
    write_stderr("FAILED: bench_table: ");
    write_stderr(what);
    write_stderr("\n");
    exit(1);
}

fn main() {
    var wireDir: const u8[:] = "testdata/wire";
    var variantDir: const u8[:] = "bench/corpus/variants";

    let argv = args();
    var i = 1;
    while i < argv.len {
        if argv[i] == "--csv" {
            csv = true;
        } else if argv[i] == "--gate" {
            gateOnly = true;
        } else if argv[i] == "--wire-dir" && i + 1 < argv.len {
            wireDir = argv[i + 1];
            i += 1;
        } else if argv[i] == "--variant-dir" && i + 1 < argv.len {
            variantDir = argv[i + 1];
            i += 1;
        } else if argv[i] == "--round" && i + 1 < argv.len {
            let _, ok = parse_i64(argv[i + 1]);
            if !ok { fail("--round expects non-negative integer"); }
            numRuns = 1;
            i += 1;
        } else if argv[i] == "--iterations" && i + 1 < argv.len {
            let n, ok = parse_i64(argv[i + 1]);
            if !ok || n <= 0 || n % numVariants != 0 {
                fail("--iterations expects positive multiple of 64");
            }
            itersOverride = n;
            i += 1;
        } else {
            fail("unknown argument");
        }
        i += 1;
    }

    var goldenPath: u8[>..] = [];
    goldenPath.append(wireDir);
    goldenPath.append("/bench_table.bin");
    var goldenBytes: u8[>..] = [];
    if !read_file(goldenPath, goldenBytes) {
        fail("missing wire golden bench_table.bin");
    }

    var varPath: u8[>..] = [];
    varPath.append(variantDir);
    varPath.append("/bench_table.variants.bin");
    var variantBytes: u8[>..] = [];
    if !read_file(varPath, variantBytes) {
        fail("missing variant corpus bench_table.variants.bin");
    }
    if variantBytes.len == 0 || variantBytes.len % numVariants != 0 {
        fail("variant file size is not a multiple of 64");
    }
    let record = variantBytes.len / numVariants;

    if !same(variantBytes[0..record], goldenBytes) {
        write_stderr("WIRE GOLDEN MISMATCH: bench_table\n");
        exit(1);
    }

    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var outBuf: u8[>..<] = [];

    for k in numVariants {
        let slice = variantBytes[k * record..(k + 1) * record];
        vals.clear();
        nodes.clear();
        var rep = schema_rt::LoadReport { .. };
        let rv = schema_rt::load_table(benchtable::TableUnit, rootName, slice, vals, nodes, rep);
        if rv < 0 || rep.refused || rep.damaged {
            fail("load of variant failed");
        }
        outBuf.clear();
        let refusal = schema_rt::save_table(benchtable::TableUnit, rootName, slice, vals, nodes, rv, outBuf);
        if refusal != 0 || !same(outBuf, slice) {
            fail("variant round-trip bytes differ");
        }
    }

    for step in 2 * numVariants {
        let k = step % numVariants;
        let slice = variantBytes[k * record..(k + 1) * record];
        vals.clear();
        nodes.clear();
        var rep = schema_rt::LoadReport { .. };
        let rv = schema_rt::load_table(benchtable::TableUnit, rootName, slice, vals, nodes, rep);
        if rv < 0 || rep.refused || rep.damaged {
            fail("repeated load failed");
        }
        outBuf.clear();
        let refusal = schema_rt::save_table(benchtable::TableUnit, rootName, slice, vals, nodes, rv, outBuf);
        if refusal != 0 || !same(outBuf, slice) {
            fail("repeated round-trip bytes differ");
        }
    }

    var h: u64 = 0xcbf29ce484222325;
    let z: u8[1] = [0];
    h = fnv_bytes(h, "bench_table.bin");
    h = fnv_bytes(h, z);
    h = fnv_bytes(h, goldenBytes);
    h = fnv_bytes(h, "bench_table.variants.bin");
    h = fnv_bytes(h, z);
    h = fnv_bytes(h, variantBytes);
    let cid = hex_u64(h);

    if gateOnly {
        write_stderr("OK (gate only, corpus_id ");
        write_stderr(cid);
        write_stderr(")\n");
        return;
    }

    let iters = if itersOverride > 0 { itersOverride } else { defaultIters };
    var tripRates: f64[>..] = [];
    for run in -1..numRuns {
        let t0 = clock_ns();
        for idx in iters {
            let k = idx & (numVariants - 1);
            let slice = variantBytes[k * record..(k + 1) * record];
            vals.clear();
            nodes.clear();
            var rep = schema_rt::LoadReport { .. };
            let rv = schema_rt::load_table(benchtable::TableUnit, rootName, slice, vals, nodes, rep);
            if rv < 0 { fail("load failed in loop"); }
            outBuf.clear();
            let refusal = schema_rt::save_table(benchtable::TableUnit, rootName, slice, vals, nodes, rv, outBuf);
            if refusal != 0 || outBuf.len != record { fail("re-save failed in loop"); }
            sink += outBuf.len as! u64;
        }
        let elapsed = (clock_ns() - t0) / 1e9;
        if run >= 0 { tripRates.push(iters / elapsed); }
    }
    sort_f64(tripRates);
    let med = tripRates[tripRates.len / 2];
    let lo = tripRates[0];
    let hi = tripRates[tripRates.len - 1];
    let mbps = med * record / 1048576.0;
    let spread = (hi - lo) / med * 100.0;

    var row: u8[>..] = [];
    row.append("goose,bench_table,round_trip,");
    row.append(fmt_int(iters));
    row.push(',');
    row.append(fmt_int(record));
    row.push(',');
    row.append(fmt_int(numRuns));
    row.push(',');
    row.append(fmt_int((med + 0.5) as! i64));
    row.push(',');
    row.append(fmt_int((lo + 0.5) as! i64));
    row.push(',');
    row.append(fmt_int((hi + 0.5) as! i64));
    row.push(',');
    row.append(fmt_f2(mbps));
    row.push(',');
    row.append(fmt_f2(spread));
    row.push(',');
    row.append(cid);
    row.append(",table,exe,always,O2,default\n");

    if csv {
        write_stdout("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
        write_stdout(row);
    } else {
        write_stderr(row);
    }
    write_stderr("OK (corpus_id ");
    write_stderr(cid);
    write_stderr(")\n");
}
