// the tables bench — the Rust runner, THE FIXED FORM ONLY.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated Rust table codec. It measures ONE thing: the paired
// bench's sixty-four logical records written and read in the FIXED FORM, form
// byte 3 (docs/SPEC-TABLES.md §3.4), through the generated codec.
//
// WHY THERE IS NO TOLERANT HALF HERE, and why that is not a gap this runner
// can close: the Rust port has NO FORM-1 TABLE WIRE. The backend emits the
// fixed form and the two accelerators (§19) and nothing else — there is no
// tolerant `Save`/`Load` pair in `generated/bench/paired/rust` for this leg to
// gate or to time, and `TableReport`, the tolerant reader's verdict type, is
// not in the crate at all. The C++ and C legs run the tolerant form's three
// correctness gates without a clock and measure the fixed form; this leg
// measures the same fixed form and cannot run those three. What it CAN do with
// the tolerant corpus it does — see `check_tolerant_corpus` below: THE FRAMING
// is arithmetic over the length index and needs no decoder, and the bytes are
// what the corpus id is made of.
//
// WHY THERE IS NO PACKET LEG HERE, so `bench/paired` runs rust as a TABLE-ONLY
// language and never divides a ratio for it. bench/rust/src/main.rs is a real,
// maintained packet runner, and it is still not this driver's:
//
//   - it has no `--gate` and no `--iterations`, the two flags the paired
//     driver's every invocation passes — `--iterations` is how the driver
//     holds one uniform count across every language and round (§2.1), and
//     `--gate` is the no-clock correctness pass it runs before any sitting;
//   - it reports the median of 7 runs of its own choosing, where the driver
//     requires one measured run per round (`runs` must be 1) and aggregates
//     across rounds itself;
//   - and it measures the packet corpus's own variant set, not this pairing's.
//
// Those are three changes to somebody else's measured leg, each of which moves
// numbers that are already published. That is separate work with its own
// ruling, and filling this driver's second wire with a fabricated packet row
// would be inventing a measurement. So rust rides the table wire alone and
// appears in no ratio, no confirmation pass and no board.
//
// THE RUST COST THIS LEG DOCUMENTS: NONE OF THE READ IS PROJECTION. The
// generated Rust reader lands its plan run in this build's own RECORD IMAGE
// and then SCATTERS that image into the blittable `<Name>Row` — a `#[repr(C)]`
// value that is plain data. There is no per-record allocation, no object
// graph, no string. So unlike the JavaScript leg (bench/tables/js), whose read
// pays a projection into language objects, this leg's read is the plan's own
// copy plus the scatter's straight line, and the derived read line is where
// that shows.
//
// It follows the same contract as every other leg (BENCH-STANDARD.md): the
// committed corpus drives it, the gates run before the clock, a failing gate
// emits NO rows, and the report is 1 warmup + 7 measured runs with the median
// beside min/max/spread — `--round K` drops that to one warmup and one
// measured run so the driver aggregates across rounds itself.
//
// THIS FILE IS SHAPE-BLIND. It names the generated type and its generated
// codec at one import and its storage declarations and nothing else: no field,
// no pinned value, no wire size. `make shape-gate` holds that mechanically and
// bench/tables/rust/SHAPE-GATE.allow carries the exact count.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
//
// Run from the repository root (the paired driver does): the corpus paths are
// the ones it passes.

use std::collections::BTreeMap;
use std::hint::black_box;
use std::time::Instant;

// The measured shape, named once — the generated type, its plan entry, its
// read verdict and its three generated entry points, imported here and named
// nowhere else (bench/tables/rust/SHAPE-GATE.allow).
use benchpaired::{
    FIXED_TABLE_FIXED_BLOCK, FixedTableRow, TableFixedEntry, TableFixedReport,
    fixed_table_fixed_load, fixed_table_fixed_save,
};

const MAX_NUM_RUNS: usize = 7; // median of 7 (N >= 5), after 1 warmup run
const NUM_VARIANTS: usize = 64; // the corpus is 64 records, and an OP is one record
const DEFAULT_ITERATIONS: i64 = 400_000; // the tables bench's standard table count

// The plan storage the caller owns, declared by capacity: this codec never
// allocates (§3.4). The identity path never touches it; a stranger's block
// compiles into it, and one that does not fit is a refusal by name.
const PLAN_CAPACITY: usize = 4096;

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9): a different wire over a different corpus, so a tools
// refusal to divide it against a `gen` row is correct and automatic.
//
// linkage `crate` — the generated table modules name NO runtime: they import
// only each other, and they compile into the one crate graph this binary
// monomorphizes over. That is the Rust spelling of the C++ leg's `hdr`. (The
// unit's PACKET module does name serialize.rs, and the crate therefore links
// it; no table path calls it, and nothing in this binary's measured loops
// reaches it.)
//
// checks `contract` — the generated writer's caller-error guards are
// `debug_assert!` (the 2026-09-07 ruling: write-side checks are DEBUG ONLY)
// and the release profile turns them off, while the reader's wire-contract
// validation is unconditional in every build. That is §3.4's word for exactly
// this, and it is what the paired driver requires of every table leg.
//
// opt: READ FROM THE BUILD, never asserted — the same seam the packet Rust leg
// opened. `cargo` will build at opt-level 2 when a pass driver asks it to
// (CARGO_PROFILE_RELEASE_OPT_LEVEL=2), so a literal "O3" here would be a claim
// this binary could not keep, silently, in the one column a cross-level
// comparison is keyed on. The default is "unknown" and never "O3": a build
// nobody told is a build whose level this binary does not know.
//
// inline unknown, and it stays unknown until a §4.2 verdict pass has a branch
// for the generated table codec.
const BENCH_OPT: &str = match option_env!("BENCH_OPT") {
    Some(level) => level,
    None => "unknown",
};

fn csv_suffix() -> String {
    format!("crate,contract,{BENCH_OPT},unknown")
}

struct Args {
    csv: bool,
    gate: bool,
    indexed: bool,
    num_runs: usize,
    iterations: i64,
    wire_dir: String,
    variant_dir: String,
}

struct Ctx {
    args: Args,
    failed: bool,
    csv_rows: Vec<String>,
    // §1.6 wants sorted basename order, and a BTreeMap iterates in it.
    goldens_loaded: BTreeMap<String, Vec<u8>>,
    sink: u64,
}

fn fnv1a64(mut h: u64, data: &[u8]) -> u64 {
    for &b in data {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

struct RunStats {
    median: f64,
    min: f64,
    max: f64,
    spread: f64,
}

fn run_stats(rates: &mut [f64]) -> RunStats {
    rates.sort_by(|a, b| a.partial_cmp(b).expect("finite rates"));
    let n = rates.len();
    let median = rates[n / 2];
    RunStats {
        median,
        min: rates[0],
        max: rates[n - 1],
        spread: (rates[n - 1] - rates[0]) / median * 100.0,
    }
}

impl Ctx {
    fn fail(&mut self, name: &str, what: &str) {
        eprintln!("FAILED: {name}: {what}");
        self.failed = true;
    }

    /// §1.6: FNV-1a-64 over the goldens THIS RUN LOADED — for each file in
    /// sorted basename order, the basename bytes, a 0x00 byte, then the
    /// contents. The paired driver computes the same hash over the same five
    /// files and refuses a row whose id does not match it.
    fn corpus_id(&self) -> String {
        let mut h: u64 = 0xcbf29ce484222325;
        for (name, contents) in self.goldens_loaded.iter() {
            h = fnv1a64(h, name.as_bytes());
            h = fnv1a64(h, &[0u8]);
            h = fnv1a64(h, contents);
        }
        format!("{h:016x}")
    }

    fn read_corpus(&mut self, dir: &str, basename: &str) -> Option<Vec<u8>> {
        let path = format!("{dir}/{basename}");
        match std::fs::read(&path) {
            Ok(bytes) => {
                self.goldens_loaded
                    .insert(basename.to_string(), bytes.clone());
                Some(bytes)
            }
            Err(_) => {
                eprintln!(
                    "missing corpus {path} — run from the schema repo root (or pass --wire-dir/--variant-dir)"
                );
                None
            }
        }
    }

    fn report(&mut self, bench: &str, path: &str, iters: i64, bytes_per_op: f64, s: &RunStats) {
        let mbps = s.median * bytes_per_op / (1024.0 * 1024.0);
        eprintln!(
            "{:<18} {:<11} {:>10.3} M msg/s {:>10.1} MB/s   (min {:.3}, max {:.3}, spread {:.1}%)",
            bench,
            path,
            s.median / 1e6,
            mbps,
            s.min / 1e6,
            s.max / 1e6,
            s.spread
        );
        if self.args.csv {
            self.csv_rows.push(format!(
                "rust,{},{},{},{},{},{:.0},{:.0},{:.0},{:.2},{:.2}",
                bench,
                path,
                iters,
                bytes_per_op,
                self.args.num_runs,
                s.median,
                s.min,
                s.max,
                mbps,
                s.spread
            ));
        }
    }

    fn flush_csv(&self) {
        if !self.args.csv {
            return;
        }
        if self.failed {
            // §1.5: a failing run emits NO rows. Numbers from a run whose gate
            // refused are not numbers.
            eprintln!("refusing to emit CSV rows from a failing run");
            return;
        }
        let id = self.corpus_id();
        let suffix = csv_suffix();
        println!(
            "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,\
max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline"
        );
        for row in self.csv_rows.iter() {
            println!("{row},{id},table,{suffix}");
        }
    }
}

// THE TOLERANT CORPUS, read for what this leg can honestly say about it.
//
// The corpus is ONE corpus (docs/SPEC-TABLES.md §3.4): the same sixty-four
// logical records ride both forms, and the corpus id is over all five files,
// which is what makes a fixed-form row and a tolerant-form row answerable to
// each other and to one packet row. So this leg loads all five. It has NO
// FORM-1 DECODER to gate the tolerant three with, and it does not pretend to:
// what it checks is the FRAMING, which is arithmetic over the length index and
// needs no codec —
//
//   - the index is 64 little-endian uint32 lengths,
//   - they sum to exactly the concatenation's size, none of them zero,
//   - and variant 0 IS the pinned instance, byte for byte.
//
// A leg that skipped these files would report a corpus id the paired driver
// could not match, which is §1.6 doing its job rather than a workaround.
fn check_tolerant_corpus(ctx: &mut Ctx, name: &str, golden: &str) -> bool {
    let variant_dir = ctx.args.variant_dir.clone();
    let wire_dir = ctx.args.wire_dir.clone();
    let Some(packed) = ctx.read_corpus(&variant_dir, &format!("{name}.variants.bin")) else {
        ctx.failed = true;
        return false;
    };
    let Some(pinned) = ctx.read_corpus(&wire_dir, &format!("{golden}.bin")) else {
        ctx.failed = true;
        return false;
    };
    let mut lengths = [0usize; NUM_VARIANTS];
    if ctx.args.indexed {
        let Some(index) = ctx.read_corpus(&variant_dir, &format!("{name}.lengths")) else {
            ctx.failed = true;
            return false;
        };
        if index.len() != NUM_VARIANTS * 4 {
            ctx.fail(
                name,
                "the length index is not 64 little-endian uint32 lengths",
            );
            return false;
        }
        for k in 0..NUM_VARIANTS {
            lengths[k] = u32::from_le_bytes(index[k * 4..k * 4 + 4].try_into().expect("four bytes"))
                as usize;
        }
    } else {
        // the historical fixed-stride format, retained exactly as the C++ leg
        // retains it
        if packed.is_empty() || !packed.len().is_multiple_of(NUM_VARIANTS) {
            ctx.fail(name, "the unindexed corpus is not 64 equal records");
            return false;
        }
        lengths.fill(packed.len() / NUM_VARIANTS);
    }
    let mut offset = 0usize;
    for (k, &n) in lengths.iter().enumerate() {
        if n == 0 || offset + n > packed.len() {
            ctx.fail(
                name,
                &format!("variant {k}'s length does not fit the corpus"),
            );
            return false;
        }
        offset += n;
    }
    if offset != packed.len() {
        ctx.fail(name, "the lengths do not sum to the corpus size");
        return false;
    }
    if pinned != packed[..lengths[0]] {
        eprintln!(
            "WIRE GOLDEN MISMATCH: {golden} ({} golden vs {} corpus bytes) — refusing to bench \
against a corpus whose variant 0 is not the pinned instance",
            pinned.len(),
            lengths[0]
        );
        ctx.failed = true;
        return false;
    }
    true
}

// ------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ------------------------------------------------------------------------
//
// A FILE is the unit here and not a record: the vocabulary block rides once,
// the records follow it to the end, and every one of them is the same size —
// so the write is one call for all 64 and the read is one call back, and an OP
// is one RECORD of that call.
//
// The gates before the clock are the C++ leg's four, in the same order and for
// the same reasons. The first is the one this form adds: THE BLOCK THIS BUILD
// EMITS IS THE CORPUS'S BLOCK, byte for byte. Every other fact about the form
// — the positions, the ids, the kinds, the record's size, the hash every
// record carries — is settled by those bytes, so it is checked first and by
// itself, and a leg that matches it is speaking the form and not a near miss.
fn bench_fixed(ctx: &mut Ctx, name: &str, base_iters: i64) {
    let iters = if ctx.args.iterations != 0 {
        ctx.args.iterations
    } else {
        base_iters
    };
    let variant_dir = ctx.args.variant_dir.clone();
    let Some(file) = ctx.read_corpus(&variant_dir, "bench_fixed.bin") else {
        ctx.failed = true;
        return;
    };
    let Some(vocab) = ctx.read_corpus(&variant_dir, "bench_fixed.layout") else {
        ctx.failed = true;
        return;
    };
    if file.is_empty() {
        ctx.failed = true;
        return;
    }
    let bytes_per_op = file.len() as f64 / NUM_VARIANTS as f64;
    let mut twin = vec![0u8; file.len()];

    // gate 1: THE BLOCK IS THE CORPUS'S BLOCK.
    if vocab != FIXED_TABLE_FIXED_BLOCK {
        ctx.fail(
            name,
            "this build's vocabulary block is not the corpus's, byte for byte",
        );
        return;
    }

    // The storage: the values, a reused read target, and the caller-owned plan
    // and remap the codec compiles a stranger's block into.
    let mut values = vec![FixedTableRow::default(); NUM_VARIANTS];
    let mut out = vec![FixedTableRow::default(); NUM_VARIANTS];
    let mut plan = vec![TableFixedEntry::default(); PLAN_CAPACITY];
    let mut out_plan = vec![TableFixedEntry::default(); PLAN_CAPACITY];
    let mut remap = vec![0u16; PLAN_CAPACITY];
    let mut out_remap = vec![0u16; PLAN_CAPACITY];
    let mut report = TableFixedReport::default();

    // gate 2: the whole file loads clean.
    if fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        != Some(NUM_VARIANTS)
    {
        ctx.fail(name, "the fixed corpus did not load");
        return;
    }

    // gate 3: writing them back reproduces the file, byte for byte.
    if fixed_table_fixed_save(&values, &mut twin) != Some(file.len()) || twin != file {
        ctx.fail(
            name,
            "round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus",
        );
        return;
    }

    // gate 4: reused storage round-trips too. The load owns the prefill, so
    // nothing is reset between the two passes.
    for _ in 0..2 {
        if fixed_table_fixed_load(&mut out, &file, &mut out_plan, &mut out_remap, &mut report)
            != Some(NUM_VARIANTS)
            || fixed_table_fixed_save(&out, &mut twin) != Some(file.len())
            || twin != file
        {
            ctx.fail(name, "reused target round-trip bytes differ");
            return;
        }
    }

    if ctx.args.gate {
        return;
    }

    let runs = ctx.args.num_runs;
    let mut write_rates = vec![0f64; runs];
    let mut roundtrip_rates = vec![0f64; runs];

    // WRITE: one call lays down all 64 records; an op is one record.
    //
    // The escape barrier is `black_box` on the written buffer — the stub's
    // sanctioned equivalent of the C++ leg's empty-asm memory clobber, and the
    // same barrier the packet Rust leg uses — plus a sink the exit observes.
    for run in 0..=runs {
        let start = Instant::now();
        let mut i = 0i64;
        while i < iters {
            let wrote = fixed_table_fixed_save(&values, &mut twin);
            if wrote != Some(file.len()) {
                ctx.fail(name, "save failed in loop");
                return;
            }
            black_box(&twin);
            ctx.sink = ctx.sink.wrapping_add(wrote.unwrap_or(0) as u64);
            i += NUM_VARIANTS as i64;
        }
        let elapsed = start.elapsed().as_secs_f64();
        if run > 0 {
            write_rates[run - 1] = iters as f64 / elapsed;
        }
    }

    // ROUND-TRIP: read the file back, then write what came out. The read needs
    // no sink discipline of its own — its output IS the write's input, so every
    // decoded field is observed by construction (§2.7's read-side sink problem
    // dissolved rather than equalized).
    for run in 0..=runs {
        let start = Instant::now();
        let mut i = 0i64;
        while i < iters {
            if fixed_table_fixed_load(&mut out, &file, &mut out_plan, &mut out_remap, &mut report)
                != Some(NUM_VARIANTS)
            {
                ctx.fail(name, "load failed in loop");
                return;
            }
            let wrote = fixed_table_fixed_save(&out, &mut twin);
            if wrote != Some(file.len()) {
                ctx.fail(name, "re-save failed in loop");
                return;
            }
            black_box(&twin);
            ctx.sink = ctx.sink.wrapping_add(wrote.unwrap_or(0) as u64);
            i += NUM_VARIANTS as i64;
        }
        let elapsed = start.elapsed().as_secs_f64();
        if run > 0 {
            roundtrip_rates[run - 1] = iters as f64 / elapsed;
        }
    }
    let w = run_stats(&mut write_rates);
    let rt = run_stats(&mut roundtrip_rates);
    ctx.report(name, "write", iters, bytes_per_op, &w);
    ctx.report(name, "round_trip", iters, bytes_per_op, &rt);

    // READ is DERIVED, never measured: round-trip time minus write time. It
    // prints for continuity and is NOT a CSV row — a derived number in the CSV
    // would be divided as if it had been measured (§2.9).
    let read_time = 1.0 / rt.median - 1.0 / w.median;
    if read_time > 0.0 {
        eprintln!(
            "{:<18} {:<11} {:>10.3} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)",
            name,
            "read",
            1e-6 / read_time
        );
    }
}

fn usage(program: &str) -> ! {
    eprintln!(
        "usage: {program} [--indexed] [--gate] [--csv] [--round K] [--iterations N] \
[--wire-dir <dir>] [--variant-dir <dir>]"
    );
    std::process::exit(1);
}

fn parse_args() -> Args {
    let argv: Vec<String> = std::env::args().collect();
    let program = argv
        .first()
        .cloned()
        .unwrap_or_else(|| "table_main".to_string());
    let mut args = Args {
        csv: false,
        gate: false,
        indexed: false,
        num_runs: MAX_NUM_RUNS,
        iterations: 0,
        wire_dir: "testdata/wire".to_string(),
        variant_dir: "bench/corpus/variants".to_string(),
    };
    let mut i = 1;
    while i < argv.len() {
        match argv[i].as_str() {
            "--indexed" => args.indexed = true,
            "--gate" => args.gate = true,
            "--csv" => args.csv = true,
            "--wire-dir" if i + 1 < argv.len() => {
                i += 1;
                args.wire_dir = argv[i].clone();
            }
            "--variant-dir" if i + 1 < argv.len() => {
                i += 1;
                args.variant_dir = argv[i].clone();
            }
            "--iterations" if i + 1 < argv.len() => {
                i += 1;
                match argv[i].parse::<i64>() {
                    Ok(n) if n > 0 && n <= 2147483584 && n % NUM_VARIANTS as i64 == 0 => {
                        args.iterations = n
                    }
                    _ => {
                        eprintln!(
                            "--iterations requires a positive multiple of 64 up to 2147483584"
                        );
                        std::process::exit(1);
                    }
                }
            }
            "--round" if i + 1 < argv.len() => {
                // §2.4: one warmup + one measured run, then exit. K only
                // identifies the round to the interleaved driver, which
                // aggregates across rounds itself.
                i += 1;
                if argv[i].parse::<u32>().is_err() {
                    eprintln!("--round takes a non-negative integer, got '{}'", argv[i]);
                    std::process::exit(1);
                }
                args.num_runs = 1;
            }
            _ => usage(&program),
        }
        i += 1;
    }
    args
}

fn main() {
    let args = parse_args();
    let mut ctx = Ctx {
        args,
        failed: false,
        csv_rows: Vec::new(),
        goldens_loaded: BTreeMap::new(),
        sink: 0,
    };

    if cfg!(debug_assertions) {
        eprintln!("schema tables bench (rust, Debug — only release numbers are meaningful)");
    } else {
        eprintln!("schema tables bench (rust, Release, the FIXED FORM)");
    }

    // The tolerant half's bytes, and every check this leg can make of them
    // without a form-1 decoder it does not have.
    if check_tolerant_corpus(&mut ctx, "bench_table", "bench_table") {
        bench_fixed(&mut ctx, "bench_fixed", DEFAULT_ITERATIONS);
    }

    ctx.flush_csv(); // rows carry the corpus_id of the goldens this run loaded

    if ctx.failed {
        eprintln!("TABLES BENCH FAILED (corpus_id {})", ctx.corpus_id());
        std::process::exit(1);
    }
    black_box(ctx.sink);
    eprintln!(
        "OK (corpus_id {}, sink {})",
        ctx.corpus_id(),
        if ctx.sink == 0 { "0" } else { "nonzero" }
    );
}
