// schema bench — the Lobster runner: a conforming run.sh leg per bench/README.md's
// runner contract, measuring the GENERATED Lobster type-wire codec over
// bench/corpus/Bench.schema's one measured shape, against the serialize.lobster
// runtime the generated unit carries beside it (docs/PORT-LOBSTER.md, THE
// RUNTIME LAYOUT).
//
// CONTRACT (BENCH-STANDARD.md): the iteration count identical to every other
// leg's rows (--quick's reduced count is the one deliberate exception, and it
// is recorded in the iters column); 1 discarded warmup run then 7 measured runs
// per (bench, path), or exactly one measured run under --round K (§2.4); the 64
// rotating records of the committed variant corpus on both paths;
// median/min/max/spread; CSV v2 rows under --csv.
//
// GOLDEN GATED (§1.5): before any clock, variant 0 is byte-compared against the
// C++-pinned testdata/wire golden, and every variant is decoded and re-encoded
// byte-identically. A runner that mismatches emits no rows.
//
// SHAPE-BLIND: this file names the generated root at its one call site below
// and nothing else about it: no field, no value, no wire size.
//
// WHAT LOBSTER SPELLS DIFFERENTLY. There is no empty-asm clobber: the write
// stream stores its words into the caller's buffer string in place (the
// runtime's own contract), and every message's byte count folds into a sink
// that is printed, so no write can be deleted; the round trip observes its own
// decode through the re-encode, as in every leg. The clock is
// seconds_elapsed(), the toolchain's high-resolution counter.
//
// Run through bench/lobster/run, which runs this file under the pinned
// toolchain's default JIT with --runtime-no-asserts (the release leg: the
// schema's own write contracts are asserts and go dormant, the reader's
// wire-contract checks stay) and routes the tagged output lines. The --cpp
// ahead-of-time row is owed (bench/lobster/run, docs/PORT-LOBSTER.md, THE
// HARNESS): the pinned release carries no dev/ tree to compile its output
// against.

import harness
import Bench

// §1.2/§2.1: the fixed iteration count, identical across every leg's rows
let mixed_iters = 4000000
// --quick only (PROPOSED, BENCH-STANDARD quick-mode scaling)
let quick_mixed_iters = 400000

// CSV v2 (§5.1) per-runner constants: family gen (the rows measure generated
// code); linkage jit (the generated unit and its vendored runtime compiled
// together into one program by the toolchain's TinyCC JIT, no library
// boundary); checks contract (--runtime-no-asserts: caller contracts dormant,
// wire-contract validation unconditional in the reader); opt default (the JIT
// has no level to name); inline unknown (no AOT artifact the §4 verdict pass
// can walk).
let csv_suffix = "gen,jit,contract,default,unknown"

var sink = 0

let opts = bench_options(command_line_arguments())
// write_one encodes value into buf through a fresh write stream (the stream is
// the per-message state every leg initialises per message) and answers the
// bytes it wrote
def write_one(value:bench.BenchMixed, buf:string) -> int:
    let w = serialize_write_stream(buf)
    if not bench.WriteBenchMixed(w, value):
        return -1
    w.flush()
    return w.bytes_processed()

def read_one(value:bench.BenchMixed, padded:string, bytes:int) -> bool:
    let r = serialize_read_stream(padded, bytes)
    return bench.ReadBenchMixed(r, value) and r.ok()

def run_mixed():
    let row = "bench_mixed"
    let golden = bench_load(opts.wire_dir, row + ".bin", row)
    let packed = bench_load(opts.variant_dir, row + ".variants.bin", row)
    if not golden or not packed:
        return
    let v = bench_split_variants(packed, row)
    if not v:
        return
    let record = v.record

    // the write buffer: whole 64-bit words, the record and one word over
    let cap = (record / 8 + 2) * 8
    let buf = ensure_size(string_with_capacity(cap), cap, 0)

    // gate 1 (§1.5): variant 0 IS the pinned instance
    if v.exact[0] != golden:
        bench_fail(row, "variant 0 is not the wire golden " + opts.wire_dir + "/" + row + ".bin")
        return

    // gate 2: every variant decodes, re-encodes, and comes back byte-identical
    // at the same length, into its own instance the write loop rotates over
    let instances:[bench.BenchMixed] = []
    for(bench_num_variants) k:
        let value = bench.NewBenchMixed()
        if not read_one(value, v.padded[k], record):
            bench_fail(row, "decode of variant " + string(k) + " failed")
            return
        let n = write_one(value, buf)
        if n != record or substring(buf, 0, n) != v.exact[k]:
            bench_fail(row, "variant " + string(k) + " round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
            return
        push(instances, value)

    // gate 3: one reused holder decodes every variant twice over, the
    // last-to-first transition included, and re-encodes each byte-identically:
    // the timed round trip reads into exactly this holder
    let out = bench.NewBenchMixed()
    for(2 * bench_num_variants) i:
        let k = i & (bench_num_variants - 1)
        if not read_one(out, v.padded[k], record):
            bench_fail(row, "decode into the reused holder failed")
            return
        let n = write_one(out, buf)
        if n != record or substring(buf, 0, n) != v.exact[k]:
            bench_fail(row, "reused holder round-trip bytes differ")
            return

    if opts.gate:
        return

    let iters = if opts.iterations > 0: opts.iterations elif opts.quick: quick_mixed_iters else: mixed_iters
    let num_runs = if opts.round: 1 elif opts.quick: 3 else: 7

    // WRITE: encode the 64 pre-decoded instances round-robin, so the encoder
    // never sees the same input twice in a row and bytes/op is constant by
    // construction (§2.7)
    let write_rates:[float] = []
    for(num_runs + 1) run:
        let t0 = seconds_elapsed()
        var acc = 0
        for(iters) i:
            let n = write_one(instances[i & (bench_num_variants - 1)], buf)
            if n != record:
                bench_fail(row, "write failed in loop")
                return
            acc += n
        let elapsed = seconds_elapsed() - t0
        sink += acc
        if run > 0:
            push(write_rates, float(iters) / elapsed)

    // ROUND-TRIP: decode a variant into the reused holder, then re-encode what
    // came out; the decode is observed by the encode (§2.7's read-side sink
    // problem dissolved rather than equalized)
    let trip_rates:[float] = []
    for(num_runs + 1) run:
        let t0 = seconds_elapsed()
        var acc = 0
        for(iters) i:
            let k = i & (bench_num_variants - 1)
            if not read_one(out, v.padded[k], record):
                bench_fail(row, "decode failed in loop")
                return
            let n = write_one(out, buf)
            if n != record:
                bench_fail(row, "re-encode failed in loop")
                return
            acc += n
        let elapsed = seconds_elapsed() - t0
        sink += acc
        if run > 0:
            push(trip_rates, float(iters) / elapsed)

    bench_report("lobster", row, "write", iters, record, write_rates)
    bench_report("lobster", row, "round_trip", iters, record, trip_rates)
    bench_derived(row, write_rates, trip_rates)

if opts.bad != "":
    bench_log(opts.bad)
    set_exit_code(1)
else:
    bench_log("schema bench (lobster, generated codec, JIT" + (if opts.quick: ", --quick: iteration instrument, not certification" else: "") + ")")
    run_mixed()
    bench_finish(opts, csv_suffix)
    bench_log("sink: " + string(sink))
