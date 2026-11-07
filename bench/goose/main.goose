// schema bench — the Goose runner: a conforming run.sh leg per
// bench/README.md's runner contract, measuring the generated Goose packet
// codec over bench/corpus/Bench.schema's BenchMixed.
//
// CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
// other runner's rows for this bench; 1 discarded warmup run then 7 measured
// runs per (bench, path) — or exactly one measured run under --round K, where
// the interleaved driver aggregates across rounds (§2.4); 64 rotating variant
// buffers on both paths; median/min/max/spread over the measured runs; CSV v2
// rows on stdout under --csv, a human table on stderr.
//
// WHAT THE ROWS MEASURE: the generated codec (docs/PORT-GOOSE.md, THE PACKET
// WIRE) over the vendored serialize.goose runtime, compiled with this driver
// into ONE C translation unit by `goose build -O2` (the bench machine's C
// compiler, never TinyCC). family gen. The per-message work is the Go leg's:
// a write is stream init + Write<T> + flush; a round trip is read stream init
// + Read<T> into one hoisted value, then the write.
//
// GOLDEN GATED (§1.5): before any timing, variant 0 of the committed variant
// corpus is byte-compared against the C++-pinned testdata/wire golden, and
// every variant is decoded and re-encoded byte-identically. A runner that
// mismatches REFUSES to bench. corpus_id is FNV-1a-64 over the goldens this
// run actually loaded (§1.6).
//
// run.sh builds it (`bench/run.sh --only goose`): it generates
// bench/corpus/Bench.schema for Goose with build/schema-goose (the target is
// behind the schema_leg_goose tag), copies this file beside the generated
// modules, and runs `goose build -O2`. Run from bench/goose:
//
//   cd bench/goose && ../../build/bench/schema_bench_goose [--csv] [--round K] [--quick]
//
// --quick: 3 measured runs instead of 7 — run.sh's iteration instrument,
// never the certification instrument. bench_mixed is the whole leg either way.
import os;
import .Bench;
import .serialize;

let numVariants: i64 = 64;
let slotBytes: i64 = 512; // a record plus at least the reader's 8 slack bytes

// §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
// language's rows for these benches, recorded in the iters column
let mixedIters: i64 = 4000000;

var csv = false;
var quick = false;
var numRuns: i64 = 7;

// the g_sink of the C bench: every write's byte count flows here, and the
// bench publishes it at exit under an env var the compiler cannot rule out,
// so no loop's work can be deleted
var sink: u64 = 0;

fn gateFail(row: const u8[:], what: const u8[:]) {
    write_stderr(str("GOLDEN GATE FAILED: ", row, " ", what, "\nreporting nothing.\n"));
    exit(1);
}

/* --------------------------------------------------------------------------
   CSV v2 (§5.1): rows collected and flushed with the corpus_id of the goldens
   this run loaded; the per-runner constants — family gen (the rows measure
   generated code), linkage hdr (codec, runtime and driver are one C
   translation unit), checks always (the writer refuses every value the wire
   cannot carry and the reader validates the wire in every build: Goose has no
   build-removable check, docs/PORT-GOOSE.md), opt O2 (goose build -O2),
   inline unknown (the §4 verdict pass has no goose branch).
   -------------------------------------------------------------------------- */

let csvSuffix = "gen,hdr,always,O2,unknown";
var csvRows: u8[>..] = [];

// the goldens this run loaded, in name order (bench_mixed.bin sorts before
// bench_mixed.variants.bin): FNV-1a 64 over name, a zero byte, the bytes
var goldenBytes: u8[>..] = [];
var variantBytes: u8[>..] = [];

fn fnvMix(h: u64&, bytes: const u8[:]) {
    for b in bytes {
        h = (h ^ b) * 0x100000001b3;
    }
}

fn corpusId() -> u8[>..] {
    var h: u64 = 0xcbf29ce484222325;
    fnvMix(h, "bench_mixed.bin");
    fnvMix(h, [0 as u8]);
    fnvMix(h, goldenBytes);
    fnvMix(h, "bench_mixed.variants.bin");
    fnvMix(h, [0 as u8]);
    fnvMix(h, variantBytes);
    var out: u8[>..] = [];
    format_uint(out, h, 16, 16, '0');
    return out;
}

fn padLeft(s: const u8[:], width: i64) -> u8[>..] {
    var out: u8[>..] = [];
    for i in width - s.len { out.push(' '); }
    out.append(s);
    return out;
}

fn padRight(s: const u8[:], width: i64) -> u8[>..] {
    var out: u8[>..] = [];
    out.append(s);
    for i in width - s.len { out.push(' '); }
    return out;
}

fn fmtf(v: f64, decimals: i64) -> u8[>..] {
    var out: u8[>..] = [];
    format_flt(out, v, decimals);
    return out;
}

fn report(bench: const u8[:], path: const u8[:], iters: i64, bytesPerOp: i64, rates: f64[:]) {
    sort(rates);
    let median = rates[rates.len / 2];
    let lo = rates[0];
    let hi = rates[rates.len - 1];
    let spread = (hi - lo) / median * 100.0;
    let mbps = median * bytesPerOp / (1024.0 * 1024.0);
    write_stderr(str(padRight(bench, 18), " ", padRight(path, 5), " ",
                     padLeft(fmtf(median / 1e6, 2), 10), " M msg/s ",
                     padLeft(fmtf(mbps, 1), 10), " MB/s   (min ", fmtf(lo / 1e6, 2),
                     ", max ", fmtf(hi / 1e6, 2), ", spread ", fmtf(spread, 1), "%)\n"));
    if csv {
        csvRows.append(str("goose,", bench, ",", path, ",", iters, ",", bytesPerOp, ",", rates.len, ",",
                           fmtf(median, 0), ",", fmtf(lo, 0), ",", fmtf(hi, 0), ",",
                           fmtf(mbps, 2), ",", fmtf(spread, 2), "\n"));
    }
}

fn flushCsv() {
    if !csv { return; }
    let id = corpusId();
    var out: u8[>..] = [];
    out.append("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,");
    out.append("max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
    each_split(csvRows, '\n') {
        if it.len > 0 {
            out.append(it);
            out.append(str(",", id, ",", csvSuffix, "\n"));
        }
    };
    write_stdout(out);
    flush_stdout();
}

/* --------------------------------------------------------------------------
   the DATA-DRIVEN driver for bench_mixed (issue #191).

   THE PROPERTY: nothing below names a field of the shape it measures. Shape
   knowledge lives in the committed variant DATA (bench/corpus/variants,
   emitted by bench/tools/variantgen) and in the generated codec, and nowhere
   else. The generated type and its two codec entries are named once each,
   in the three declarations directly below.
   -------------------------------------------------------------------------- */

var instances: bench::BenchMixed[64];
var decoded: bench::BenchMixed;

fn writeOne(v: bench::BenchMixed&, buffer: u8[:]) -> i64 {
    var w = serialize_write_stream_init(buffer);
    if bench::WriteBenchMixed(w, v) != SERIALIZE_ERROR_NONE { return -1; }
    serialize_flush(w);
    return serialize_bytes_processed(w);
}

fn readOne(v: bench::BenchMixed&, slot: const u8[:], bytes: i64) -> bool {
    var r = serialize_read_stream_init(slot, bytes);
    return bench::ReadBenchMixed(r, v);
}

// the 64 variant records, one slot each, the bytes past a record zero (the
// reader's slack contract); the write buffer and the gate's twin
var slots: u8[32768] = [0; 32768];
var buffer: u8[512] = [0; 512];
var twin: u8[512] = [0; 512];

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len {
        if a[i] != b[i] { return false; }
    }
    return true;
}

fn gate(row: const u8[:]) -> i64 {
    // The records are fixed-width by construction (§2.7 pins every structure
    // field), so the file needs no index: the record size IS file size /
    // numVariants, and a file that does not divide evenly is a refusal.
    let path = str("../../bench/corpus/variants/", row, ".variants.bin");
    if !read_file(path, variantBytes) {
        write_stderr(str("missing variant data ", path, " — run `make bench-variants`, and run the bench from bench/goose\n"));
        exit(1);
    }
    if variantBytes.len == 0 || variantBytes.len % numVariants != 0 {
        gateFail(row, str("variant data ", path, " is ", variantBytes.len, " bytes, not a multiple of ",
                          numVariants, " records — refusing to bench data whose stride is not the record size"));
    }
    let record = variantBytes.len / numVariants;
    if record > slotBytes - 8 {
        gateFail(row, str("a ", record, "-byte record leaves no reader slack in a ", slotBytes, "-byte slot"));
    }
    for k in numVariants {
        for i in record { slots[k * slotBytes + i] = variantBytes[k * record + i]; }
    }

    // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
    // file is bound to the wire golden by one byte-compare.
    let golden = str("../../testdata/wire/", row, ".bin");
    if !read_file(golden, goldenBytes) {
        write_stderr(str("missing wire golden testdata/wire/", row, ".bin — run from bench/goose\n"));
        exit(1);
    }
    if !same(variantBytes[0..record], goldenBytes) {
        gateFail(row, str("variant 0 vs testdata/wire/", row, ".bin"));
    }

    // gate 2: every variant decodes, re-encodes, and comes back
    // byte-identical at the same length.
    for v in numVariants {
        if !readOne(instances[v], slots[v * slotBytes..(v + 1) * slotBytes], record) {
            gateFail(row, str("decode of variant ", v, " failed"));
        }
        let n = writeOne(instances[v], twin);
        if n != record || !same(twin[0..n], variantBytes[v * record..(v + 1) * record]) {
            gateFail(row, str("variant ", v, " round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus"));
        }
    }
    return record;
}

fn measure(row: const u8[:], iters: i64, record: i64) {
    var writeRates: f64[>..] = [];
    var roundTripRates: f64[>..] = [];

    // WRITE: encode the 64 pre-decoded instances round-robin, so the encoder
    // never sees the same input twice in a row.
    for run in -1..numRuns {
        let start = clock_ns();
        for i in iters {
            let n = writeOne(instances[i & (numVariants - 1)], buffer);
            if n < 0 { gateFail(row, "write refused in the timed loop"); }
            sink += n as! u64;
        }
        let elapsed = (clock_ns() - start) / 1e9;
        if run >= 0 { writeRates.push(iters / elapsed); }
    }

    // ROUND-TRIP: decode a variant buffer, then re-encode what came out. The
    // decode's output IS the encode's input, so every decoded field is
    // observed by construction (§2.7's read-side sink dissolved).
    for run in -1..numRuns {
        let start = clock_ns();
        for i in iters {
            let k = i & (numVariants - 1);
            if !readOne(decoded, slots[k * slotBytes..(k + 1) * slotBytes], record) {
                gateFail(row, "read refused in the timed loop");
            }
            let n = writeOne(decoded, buffer);
            if n < 0 { gateFail(row, "re-write refused in the timed loop"); }
            sink += n as! u64;
        }
        let elapsed = (clock_ns() - start) / 1e9;
        if run >= 0 { roundTripRates.push(iters / elapsed); }
    }

    report(row, "write", iters, record, writeRates);
    report(row, "round_trip", iters, record, roundTripRates);

    // READ is DERIVED, never measured: round-trip time minus write time. It
    // prints for continuity and is NOT a CSV row.
    let w = writeRates[writeRates.len / 2];
    let rt = roundTripRates[roundTripRates.len / 2];
    let readTime = 1.0 / rt - 1.0 / w;
    if readTime > 0.0 {
        write_stderr(str(padRight(row, 18), " ", padRight("read", 5), " ", padLeft(fmtf(1e-6 / readTime, 2), 10),
                         " M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n"));
    }
}

fn main() {
    let argv = args();
    var i = 1;
    while i < argv.len {
        if argv[i] == "--csv" {
            csv = true;
        } else if argv[i] == "--quick" {
            quick = true;
        } else if argv[i] == "--round" {
            if i + 1 >= argv.len {
                write_stderr("--round takes a round number\n");
                exit(1);
            }
            i += 1; // K only identifies the round to the driver
            numRuns = 1;
        } else {
            write_stderr("usage: schema_bench_goose [--csv] [--round K] [--quick]\n");
            exit(1);
        }
        i += 1;
    }
    if quick && numRuns == 7 { numRuns = 3; }

    var banner: u8[>..] = [];
    banner.append("schema bench (goose, generated codecs");
    if quick { banner.append(", --quick: iteration instrument, not certification"); }
    banner.append(")\n");
    write_stderr(banner);

    // every measured row's golden gate runs before any row is timed: a runner
    // that fails its goldens reports nothing at all (§1.5)
    let record = gate("bench_mixed");
    measure("bench_mixed", mixedIters, record);

    flushCsv();
    write_stderr(str("OK (corpus_id ", corpusId(), ")\n"));

    // the g_sink escape: the compiler cannot prove the env var absent, so the
    // accumulated sink is observable and no loop's work can be deleted
    var envOut: u8[>..] = [];
    if env("SERIALIZE_BENCH_SINK", envOut) {
        write_stderr(str("sink: ", sink, "\n"));
    }
}
