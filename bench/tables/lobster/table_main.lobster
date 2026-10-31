// the tables bench — the Lobster runner (bench/tables/README.md, "Registering a
// port"; docs/PORT-LOBSTER.md, THE HARNESS).
//
// Measures ONE thing: the representative fixed table written and read on the
// TOLERANT WIRE (docs/SPEC-TABLES.md §3) through the port's form-1 codec — the
// one runtime walk over the generated descriptors (load_tolerant, save) — over
// the committed 64-record variant corpus. It is a port of
// bench/tables/cpp/table_main.cpp's bench_table driver and keeps its contract:
// the golden gate runs before the clock, 1 warmup + 7 measured runs (one under
// --round K), median beside min/max/spread, rows `write` and `round_trip`, and
// READ derived on stderr only (§2.9).
//
// THIS FILE IS SHAPE-BLIND. It names the generated root ONCE, as the string the
// runtime walk is asked to load at; no field, no pinned value, no wire size.
//
// WHAT LOBSTER SPELLS DIFFERENTLY, and the reason at each site:
//
// - Lobster has no reflection, so the port's table codec is ONE runtime walk
//   over the generated descriptors into a schema_rt.Record (docs/PORT-LOBSTER.md,
//   "Lobster has no reflection"). That IS the port's Load and Save, so it is
//   what is timed; there is no typed per-table codec to measure instead.
// - The public load answers a FRESH Record with the declared defaults
//   restored before the wire is overlaid — there is no load-into-a-reused-target
//   form — so gate 3 runs exactly what the timed round trip runs (every
//   variant twice over, last-to-first included) and the allocation stays inside the
//   clock, where the reference keeps its default restoration. A fixed table
//   holds no pointer and so no cycle; reference counting frees each record
//   when the next replaces it, and release() (THE LOAD) has nothing to break.
// - save answers a new string; its length is the write arm's sink and the
//   bytes are compared before the clock. The round trip observes its own load
//   through the re-save, as in every leg.

import harness
import BenchTableTable
import load
import schema_save

// the C++ reference's base count for this row (bench/tables/cpp/table_main.cpp)
let table_iters = 400000

// CSV v2 (§5.1): family table (§1.9); linkage jit (the generated unit and its
// runtime compiled together into one program by the toolchain's TinyCC JIT);
// checks contract (--runtime-no-asserts; the reader's wire-contract checks are
// unconditional); opt default; inline unknown.
let csv_suffix = "table,jit,contract,default,unknown"

var sink = 0

let opts = bench_options(command_line_arguments())

// the root the walk loads at: the generated type, named once (shape-blind)
let root = "TableMixed"

// load_one is the codec's tolerant load, accepted when it answered a record
// and reported no malformation (the C++ leg's LoadTolerant && !malformed)
def load_one(data:string) -> schema_rt.Record?:
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load_tolerant(root, data, rep)
    if rep.malformed or rep.refused:
        return nil
    return rec

def run_table():
    let row = "bench_table"
    let golden = bench_load(opts.wire_dir, row + ".bin", row)
    let packed = bench_load(opts.variant_dir, row + ".variants.bin", row)
    if not golden or not packed:
        return
    let v = bench_split_variants(packed, row)
    if not v:
        return
    let record = v.record

    // gate 1 (§1.5): variant 0 IS the pinned instance
    if v.exact[0] != golden:
        bench_fail(row, "variant 0 is not the wire golden " + opts.wire_dir + "/" + row + ".bin")
        return

    // gate 2: every variant loads, re-saves, and comes back byte-identical at
    // the same length, into the instance the write loop rotates over
    let instances:[schema_rt.Record] = []
    for(bench_num_variants) k:
        let rec = load_one(v.exact[k])
        if not rec:
            bench_fail(row, "load of variant " + string(k) + " failed")
            return
        if schema_rt.save(rec) != v.exact[k]:
            bench_fail(row, "variant " + string(k) + " round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
            return
        push(instances, rec)

    // gate 3: exactly what the timed round trip runs, every variant twice over,
    // the last-to-first transition included
    for(2 * bench_num_variants) i:
        let k = i & (bench_num_variants - 1)
        let rec = load_one(v.exact[k])
        if not rec or schema_rt.save(rec) != v.exact[k]:
            bench_fail(row, "repeated load round-trip bytes differ")
            return

    if opts.gate:
        return

    let iters = if opts.iterations > 0: opts.iterations else: table_iters
    let num_runs = if opts.round: 1 else: 7

    // WRITE: save the 64 pre-loaded records round-robin (§2.7's variation)
    let write_rates:[float] = []
    for(num_runs + 1) run:
        let t0 = seconds_elapsed()
        var acc = 0
        for(iters) i:
            let n = length(schema_rt.save(instances[i & (bench_num_variants - 1)]))
            if n != record:
                bench_fail(row, "save failed in loop")
                return
            acc += n
        let elapsed = seconds_elapsed() - t0
        sink += acc
        if run > 0:
            push(write_rates, float(iters) / elapsed)

    // ROUND-TRIP: load a variant, then re-save what came out
    let trip_rates:[float] = []
    for(num_runs + 1) run:
        let t0 = seconds_elapsed()
        var acc = 0
        for(iters) i:
            let rec = load_one(v.exact[i & (bench_num_variants - 1)])
            if not rec:
                bench_fail(row, "load failed in loop")
                return
            let n = length(schema_rt.save(rec))
            if n != record:
                bench_fail(row, "re-save failed in loop")
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
    bench_log("schema tables bench (lobster, JIT)")
    run_table()
    bench_finish(opts, csv_suffix)
    bench_log("sink: " + string(sink))
