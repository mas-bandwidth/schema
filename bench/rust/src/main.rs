// schema bench — the Rust runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated Rust crate and the serialize.rs runtime: same benchmark set,
// same variant corpus, same golden + per-variant round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=rust. See
// bench/README.md for the runner contract.
//
// Language-specific discipline:
//   - escape barrier: std::hint::black_box on the written buffer and on every
//     decoded value (the stub's sanctioned equivalent of the C++ empty-asm
//     clobber), plus a sink accumulator observed at exit
//   - streams borrow stack/heap buffers — construction per iteration is free,
//     exactly the C++ shape
//   - the driver is generic; write/read monomorphize and inline like the
//     C++ template reference

use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::hint::black_box;
use std::time::Instant;

use benchcorpus as benchgen;
use serialize::{ReadStream, Stream, WriteStream};

mod bits;

const MAX_NUM_RUNS: usize = 7; // median of 7 (N >= 5), after 1 warmup run
const NUM_VARIANTS: usize = 64; // read-path variant buffers

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// Rows are buffered and emitted at exit so every row carries the corpus_id
// (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each file
// in sorted basename order, the basename bytes, a 0x00 byte, the contents.
// The per-runner constants: family gen (these are the generated-code
// benchmarks), linkage crate (the serialize.rs runtime compiles into the one
// crate graph the bench monomorphizes over), checks always (the runtime is
// unsafe_code = "forbid" — every load is bounds-checked in every build, plus
// range validation and the sticky error check by contract), opt O3 (the cargo
// release profile, opt-level 3), inline unknown until the verdict pass (§4.2)
// backfills it.
// family is per ROW now (gen | bits — §5.1); linkage/checks/inline stay
// per-runner constants, and OPT IS READ FROM THE BUILD rather than asserted.
//
// THE OPT COLUMN USED TO BE THE LITERAL "O3" AND WAS THEREFORE A CLAIM THIS
// BINARY COULD NOT KEEP. `cargo` will happily build at opt-level 2 when the
// pass driver asks it to (CARGO_PROFILE_RELEASE_OPT_LEVEL=2), and every row
// still said O3 — so an O2 pass produced a CSV that named the wrong level,
// silently, in the one column a cross-level comparison is keyed on. That is
// the named harness gap keeping Rust out of the O2 ranking.
//
// The C and C++ runners already do this correctly: run.sh passes
// -DBENCH_OPT="$OPT_LEVEL" and the runner stamps what it was told. This is the
// same seam for cargo, through option_env! (compile time, like -D).
//
// AND THE DEFAULT IS "unknown", NEVER "O3": a build nobody told is a build
// whose level this binary does not know, and a guess that happens to be right
// most of the time is exactly how the old constant survived. An honest
// "unknown" fails a verdict pass loudly; a confident "O3" fails it silently.
const BENCH_OPT: &str = match option_env!("BENCH_OPT") {
    Some(level) => level,
    None => "unknown",
};

fn csv_suffix() -> String {
    format!("crate,always,{BENCH_OPT},unknown")
}

fn fnv1a64(mut h: u64, data: &[u8]) -> u64 {
    for &b in data {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

// buffers: write buffers must be a multiple of 8 bytes (qword-flush contract);
// variant buffers keep slack past the packet for the reader's window loads.
// 4096 covers MESSAGE_MAX_BYTES (2008) with slack on both contracts.
const BUFFER_SIZE: usize = 4096;

// §2.7 variant-buffer stride: the 64 rotating read buffers are allocated at
// BUFFER_SIZE + 64 per slot, NOT packed at exact 4096. At stride 4096 every
// head line maps into one of 4 L1 set-groups on the M2 (set bits [13:6]:
// 4096 >> 6 = 64 sets per step, 64k mod 256 cycles {0,64,128,192}), and a
// fully-inlined memory-bound read feels every background conflict miss in
// those sets. At 4160 the step is 65 and gcd(65,256) = 1: 64 head lines,
// 64 distinct sets. Identical in all five runners. The slice handed to the
// write streams stays [..BUFFER_SIZE]; the pad is address spacing only.
pub(crate) const VARIANT_STRIDE: usize = BUFFER_SIZE + 64;

// the LCG every runner must use (Knuth MMIX, as in serialize bench.cpp)
fn bench_rng(rng: u64) -> u64 {
    rng.wrapping_mul(6364136223846793005)
        .wrapping_add(1442695040888963407)
}

struct Ctx {
    csv: bool,
    wire_dir: String,
    variant_dir: String,
    num_runs: usize, // MAX_NUM_RUNS, or 1 under --round K (§2.4: one warmup +
    // one measured run per round; the driver aggregates across rounds)
    failed: Cell<bool>,
    sink: Cell<u64>,
    csv_rows: RefCell<Vec<(String, &'static str)>>, // (first 11 columns, family)
    goldens_loaded: RefCell<BTreeMap<String, Vec<u8>>>,
}

struct RunStats {
    median: f64, // ops/sec
    min: f64,
    max: f64,
    spread: f64, // (max - min) / median * 100
}

fn run_stats(rates: &mut [f64]) -> RunStats {
    rates.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let n = rates.len();
    RunStats {
        median: rates[n / 2],
        min: rates[0],
        max: rates[n - 1],
        spread: (rates[n - 1] - rates[0]) / rates[n / 2] * 100.0,
    }
}

impl Ctx {
    fn fail(&self, name: &str, what: &str) {
        eprintln!("FAILED: {name}: {what}");
        self.failed.set(true);
    }

    fn check_golden(&self, name: &str, data: &[u8]) -> bool {
        let path = format!("{}/{}.bin", self.wire_dir, name);
        match std::fs::read(&path) {
            Ok(expected) => {
                self.goldens_loaded
                    .borrow_mut()
                    .insert(format!("{name}.bin"), expected.clone());
                if expected != data {
                    eprintln!(
                        "WIRE GOLDEN MISMATCH: {} ({} golden vs {} actual bytes) — refusing to bench code that does not match the corpus",
                        name,
                        expected.len(),
                        data.len()
                    );
                    return false;
                }
                true
            }
            Err(_) => {
                eprintln!("missing wire golden {path} — run from bench/rust (or pass --wire-dir)");
                false
            }
        }
    }

    // Loads <variant-dir>/<name>.variants.bin into the NUM_VARIANTS
    // §2.7-staggered slots and returns the record size, or None. The records
    // are fixed-width by construction (§2.7 pins every structure field), so
    // the file needs no index: the record size IS file size / NUM_VARIANTS,
    // and a file that does not divide evenly is a refusal.
    fn load_variants(&self, name: &str, variants: &mut [[u8; VARIANT_STRIDE]]) -> Option<usize> {
        let path = format!("{}/{}.variants.bin", self.variant_dir, name);
        let packed = match std::fs::read(&path) {
            Ok(p) => p,
            Err(_) => {
                eprintln!(
                    "missing variant data {path} — run `make bench-variants`, and run the bench from bench/rust (or pass --variant-dir)"
                );
                return None;
            }
        };
        if packed.is_empty() || packed.len() % NUM_VARIANTS != 0 {
            eprintln!(
                "variant data {} is {} bytes, not a multiple of {} records — refusing to bench data whose stride is not the record size",
                path,
                packed.len(),
                NUM_VARIANTS
            );
            return None;
        }
        let record = packed.len() / NUM_VARIANTS;
        if record > BUFFER_SIZE {
            eprintln!(
                "variant data {path} has {record}-byte records, over the {BUFFER_SIZE}-byte buffer"
            );
            return None;
        }
        for (k, slot) in variants.iter_mut().enumerate().take(NUM_VARIANTS) {
            slot[..record].copy_from_slice(&packed[k * record..(k + 1) * record]);
        }
        // The variant data is corpus (§1.6): it defines the work inside the
        // timed loops, so it rides in corpus_id exactly as the wire goldens
        // do. A run against drifted variant data reports a different id and
        // the tools refuse the ratio, instead of publishing a number for
        // different work.
        self.goldens_loaded
            .borrow_mut()
            .insert(format!("{name}.variants.bin"), packed);
        Some(record)
    }

    fn report(
        &self,
        bench: &str,
        path: &str,
        iters: i64,
        bytes_per_op: i64,
        s: &RunStats,
        family: &'static str,
    ) {
        let mbps = s.median * bytes_per_op as f64 / (1024.0 * 1024.0);
        eprintln!(
            "{:<18} {:<5} {:>10.2} M msg/s {:>10.1} MB/s   (min {:.2}, max {:.2}, spread {:.1}%)",
            bench,
            path,
            s.median / 1e6,
            mbps,
            s.min / 1e6,
            s.max / 1e6,
            s.spread
        );
        if self.csv {
            self.csv_rows.borrow_mut().push((
                format!(
                    "rust,{},{},{},{},{},{:.0},{:.0},{:.0},{:.2},{:.2}",
                    bench,
                    path,
                    iters,
                    bytes_per_op,
                    self.num_runs,
                    s.median,
                    s.min,
                    s.max,
                    mbps,
                    s.spread
                ),
                family,
            ));
        }
    }

    // corpus_id (§1.6) over the goldens this run actually loaded
    fn corpus_id(&self) -> String {
        let mut h: u64 = 0xcbf29ce484222325;
        for (name, contents) in self.goldens_loaded.borrow().iter() {
            h = fnv1a64(h, name.as_bytes());
            h = fnv1a64(h, &[0u8]);
            h = fnv1a64(h, contents);
        }
        format!("{h:016x}")
    }

    // the buffered rows — the BTreeMap iterates in sorted basename order.
    fn flush_csv(&self) {
        if !self.csv {
            return;
        }
        if self.failed.get() {
            // §1.5: a failing run emits NO rows — the exit code and stderr
            // are the whole output. Numbers from a run whose gate refused
            // are not numbers.
            eprintln!("refusing to emit CSV rows from a failing run");
            return;
        }
        let id = self.corpus_id();
        let suffix = csv_suffix();
        for (row, family) in self.csv_rows.borrow().iter() {
            println!("{row},{id},{family},{suffix}");
        }
    }
}

// ------------------------------------------------------------------------------------------
// the DATA-DRIVEN benchmark driver (issue #191)
// ------------------------------------------------------------------------------------------
//
// THE PROPERTY: nothing below names a field of the shape it measures. Shape
// knowledge lives in the committed variant DATA (bench/corpus/variants,
// emitted by bench/tools/variantgen) and in the generated codec, and nowhere
// else — so this driver cannot drift from another language's driver in what
// it measures, which is the whole reason the design exists. If a change here
// ever needs a field name, the design has failed and that is the finding.
//
// T — the generated message type — is named explicitly at the call site (a
// turbofish), as in the C++ reference. A TYPE name is not a field name; the
// driver still knows nothing about the shape's contents.
fn bench_datadriven<T, W, R, EW, ER>(
    ctx: &Ctx,
    name: &str,
    golden: &str,
    iters: i64,
    write_fn: W,
    read_fn: R,
) where
    T: Default + Clone,
    W: Fn(&mut WriteStream<'_>, &T) -> core::result::Result<(), EW>,
    R: Fn(&mut ReadStream<'_>, &mut T) -> core::result::Result<(), ER>,
{
    let mut buffer = vec![0u8; BUFFER_SIZE];
    let mut twin = vec![0u8; BUFFER_SIZE];
    let mut variants = vec![[0u8; VARIANT_STRIDE]; NUM_VARIANTS];

    let bytes_per_op = match ctx.load_variants(name, &mut variants) {
        Some(n) => n,
        None => {
            ctx.failed.set(true);
            return;
        }
    };

    // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
    // file is bound to the wire golden by one byte-compare.
    if !ctx.check_golden(golden, &variants[0][..bytes_per_op]) {
        ctx.failed.set(true);
        return;
    }

    // gate 2: every variant decodes, re-encodes, and comes back byte-identical
    // at the same length. This is stronger than the pinned-instance-only gate
    // bench_message applies — §1.5's named residual (the 64 varied buffers
    // length-checked but never value-checked) closes here, for every variant.
    let mut instances = vec![T::default(); NUM_VARIANTS];
    for k in 0..NUM_VARIANTS {
        let mut rs = ReadStream::new(&variants[k], bytes_per_op);
        if read_fn(&mut rs, &mut instances[k]).is_err() {
            ctx.fail(name, "decode of a variant failed");
            return;
        }
        let twin_bytes;
        {
            let mut ws = WriteStream::new(&mut twin);
            if write_fn(&mut ws, &instances[k]).is_err() {
                ctx.fail(name, "re-encode of a decoded variant failed");
                return;
            }
            ws.flush();
            twin_bytes = ws.bytes_processed() as usize;
        }
        if twin_bytes != bytes_per_op || twin[..bytes_per_op] != variants[k][..bytes_per_op] {
            ctx.fail(
                name,
                "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut roundtrip_rates = [0.0f64; MAX_NUM_RUNS];

    // WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
    // instances is what §2.7's per-iteration LCG mutation bought — the encoder
    // never sees the same input twice in a row and cannot precompute scratch
    // words — with none of the per-language mutation code, and with bytes/op
    // constant by construction rather than by assertion. The sink is the byte
    // fold: every iteration's result is a value the loop cannot drop.
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for i in 0..iters {
            let n;
            {
                let mut ws = WriteStream::new(&mut buffer);
                if write_fn(&mut ws, &instances[(i as usize) & (NUM_VARIANTS - 1)]).is_err() {
                    ctx.fail(name, "write failed in loop");
                    return;
                }
                ws.flush();
                n = ws.bytes_processed();
            }
            black_box(&buffer);
            ctx.sink.set(ctx.sink.get().wrapping_add(n));
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = iters as f64 / time;
        }
    }

    // ROUND-TRIP: decode a variant buffer, then re-encode what came out. The
    // decode needs no sink discipline of its own — its output IS the encode's
    // input, so every decoded field is observed by construction, in every
    // language, with no per-language fold to audit (§2.7's read-side sink
    // problem dissolved rather than equalized). The decode target is hoisted
    // and reused, as everywhere else.
    let mut out = T::default();
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for i in 0..iters {
            let mut rs =
                ReadStream::new(&variants[(i as usize) & (NUM_VARIANTS - 1)], bytes_per_op);
            if read_fn(&mut rs, &mut out).is_err() {
                ctx.fail(name, "read failed in loop");
                return;
            }
            let n;
            {
                let mut ws = WriteStream::new(&mut buffer);
                if write_fn(&mut ws, &out).is_err() {
                    ctx.fail(name, "re-write failed in loop");
                    return;
                }
                ws.flush();
                n = ws.bytes_processed();
            }
            black_box(&buffer);
            ctx.sink.set(ctx.sink.get().wrapping_add(n));
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            roundtrip_rates[run - 1] = iters as f64 / time;
        }
    }

    let w = run_stats(&mut write_rates[..ctx.num_runs]);
    let rt = run_stats(&mut roundtrip_rates[..ctx.num_runs]);
    ctx.report(name, "write", iters, bytes_per_op as i64, &w, "gen");
    ctx.report(name, "round_trip", iters, bytes_per_op as i64, &rt, "gen");

    // READ is DERIVED, never measured: round-trip time minus write time. It
    // prints for continuity with the read rows the rest of the corpus still
    // reports and is NOT a CSV row — a derived number in the CSV would be
    // divided as if it had been measured.
    let read_time = 1.0 / rt.median - 1.0 / w.median;
    if read_time > 0.0 {
        eprintln!(
            "{:<18} {:<5} {:>10.2} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)",
            name,
            "read",
            1e-6 / read_time
        );
    }
}

// ------------------------------------------------------------------------------------------

fn main() {
    let mut csv = false;
    let mut quick = false;
    let mut wire_dir = String::from("../../testdata/wire");
    let mut variant_dir = String::from("../../bench/corpus/variants");
    let mut num_runs = MAX_NUM_RUNS;
    let args: Vec<String> = std::env::args().collect();
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--csv" => csv = true,
            "--wire-dir" if i + 1 < args.len() => {
                i += 1;
                wire_dir = args[i].clone();
            }
            "--variant-dir" if i + 1 < args.len() => {
                i += 1;
                variant_dir = args[i].clone();
            }
            "--round" if i + 1 < args.len() => {
                // §2.4: one warmup + one measured run of every benchmark,
                // then exit. K only identifies the round to the interleaved
                // driver, which aggregates across rounds itself.
                i += 1;
                if args[i].parse::<u32>().is_err() {
                    eprintln!("--round takes a non-negative integer, got '{}'", args[i]);
                    std::process::exit(1);
                }
                num_runs = 1;
            }
            "--quick" => quick = true,
            _ => {
                eprintln!(
                    "usage: {} [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>]",
                    args[0]
                );
                std::process::exit(1);
            }
        }
        i += 1;
    }
    if quick && num_runs == MAX_NUM_RUNS {
        num_runs = 3;
    }

    let ctx = Ctx {
        csv,
        wire_dir,
        variant_dir,
        num_runs,
        failed: Cell::new(false),
        sink: Cell::new(0),
        csv_rows: RefCell::new(Vec::new()),
        goldens_loaded: RefCell::new(BTreeMap::new()),
    };

    if quick {
        // --quick: the iteration instrument, never the certification
        // instrument — bench_mixed only, 3 measured runs.
        eprintln!("schema bench (rust, --quick: iteration instrument, not certification)");
    } else {
        eprintln!("schema bench (rust)");
    }

    // family gen over the Bench corpus: BenchMixed through the generated code,
    // fed by the committed variant corpus — same goldens, same iteration count
    // in every runner (§2.1). No hand-written pin, vary or sink code
    // participates in this leg.
    bench_datadriven::<benchgen::BenchMixed, _, _, _, _>(
        &ctx,
        "bench_mixed",
        "bench_mixed",
        4000000,
        benchgen::write_bench_mixed,
        benchgen::read_bench_mixed,
    );

    // family bits (§1.4): the one bitpacker workload in the estate
    if !quick {
        bits::bench_bitpacker(&ctx, 24576);
    }

    ctx.flush_csv(); // rows carry the corpus_id of the goldens this run loaded

    if ctx.failed.get() {
        eprintln!("BENCH FAILED (corpus_id {})", ctx.corpus_id());
        std::process::exit(1);
    }

    eprintln!("OK (corpus_id {})", ctx.corpus_id());
    black_box(ctx.sink.get());
}
