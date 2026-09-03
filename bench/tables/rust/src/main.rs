// the tables bench — the Rust runner.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated Rust table codec: same corpus, same golden gate, same
// per-variant round-trip gate before any clock, same 1 warmup + 7 measured
// runs with median/min/max/spread, same CSV v2 rows with lang=rust.
//
// The Rust table codec names NO runtime — the generated table modules carry no
// serialize dependency at all — so this leg depends on the generated crate and
// on nothing else. That is the one contract difference from bench/rust, which
// compiles against serialize.rs.
//
// Language-specific discipline, the same choices the type leg made:
//   - escape barriers: std::hint::black_box around the buffer and the observed
//     byte count, plus a sink, so LLVM cannot delete the work
//   - the read path loads into ONE reused instance, RESET FIRST — the tolerant
//     wire elides a field at its default, so resetting is part of a correct
//     read into reused storage and stays inside the clock
//   - the instances are boxed: a table's storage is bounded but not small, and
//     64 of them on the stack would be measuring the stack
//
// THIS FILE IS SHAPE-BLIND: it names the generated type at its call sites and
// nothing else — no field, no pinned value, no wire size.

use std::collections::BTreeMap;
use std::env;
use std::fs;
use std::hint::black_box;
use std::process::exit;
use std::time::Instant;

use benchtable::*;

const MAX_NUM_RUNS: usize = 7; // median of 7 (N >= 5), after 1 warmup run
const NUM_VARIANTS: usize = 64; // read-path variant buffers

// The tolerant wire spends bytes on ids, kinds and lengths, so a table record
// is several times its equivalent type's. The buffer is sized from the corpus
// at run time and this is only the ceiling the runner refuses past.
const BUFFER_SIZE: usize = 65536;
// §2.7's variant stride, for the same reason and by the same arithmetic as the
// reference runner's: a power-of-two stride maps every head line into a
// handful of L1 set groups and a memory-bound read then feels every background
// conflict miss.
const VARIANT_STRIDE: usize = BUFFER_SIZE + 64;

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9) per row: the tolerant table wire is a DIFFERENT wire
// over a different corpus, so a tools refusal to divide it against a `gen` row
// is correct and automatic. linkage `crate` — the generated codec compiles
// into the one crate graph this bench monomorphizes over, and there is no
// runtime crate to cross. checks `always` — Rust bounds-checks every slice
// index in every build, and the reader's wire-contract validation is
// unconditional on top of it. inline unknown until the §4 verdict pass has a
// branch for the generated table codec.
//
// THE OPT COLUMN IS READ FROM THE BUILD rather than asserted, for the reason
// the type leg records at length: cargo will build at opt-level 2 when a
// driver asks it to, and a row that said O3 regardless would name the wrong
// level in the one column a cross-level comparison is keyed on. The default is
// "unknown", never "O3": a build nobody told is a build this binary does not
// know.
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

struct Bench {
    csv: bool,
    num_runs: usize,
    wire_dir: String,
    variant_dir: String,
    csv_rows: Vec<(String, String)>,
    // sorted basename order, which is what §1.6's digest is defined over
    goldens_loaded: BTreeMap<String, Vec<u8>>,
    failed: bool,
    sink: u64,
}

impl Bench {
    fn corpus_id(&self) -> String {
        let mut h: u64 = 0xcbf29ce484222325;
        for (name, bytes) in self.goldens_loaded.iter() {
            h = fnv1a64(h, name.as_bytes());
            h = fnv1a64(h, &[0]);
            h = fnv1a64(h, bytes);
        }
        format!("{h:016x}")
    }

    fn flush_csv(&self) {
        if !self.csv {
            return;
        }
        if self.failed {
            // §1.5: a failing run emits NO rows.
            eprintln!("refusing to emit CSV rows from a failing run");
            return;
        }
        let id = self.corpus_id();
        let suffix = csv_suffix();
        for (row, family) in self.csv_rows.iter() {
            println!("{row},{id},{family},{suffix}");
        }
    }

    fn fail(&mut self, name: &str, what: &str) {
        eprintln!("FAILED: {name}: {what}");
        self.failed = true;
    }
}

struct RunStats {
    median_rate: f64,
    min_rate: f64,
    max_rate: f64,
    spread_pct: f64,
}

fn run_stats(rates: &mut [f64]) -> RunStats {
    rates.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let median_rate = rates[rates.len() / 2];
    let min_rate = rates[0];
    let max_rate = rates[rates.len() - 1];
    RunStats {
        median_rate,
        min_rate,
        max_rate,
        spread_pct: (max_rate - min_rate) / median_rate * 100.0,
    }
}

fn read_file(path: &str) -> Option<Vec<u8>> {
    fs::read(path).ok()
}

impl Bench {
    fn check_golden(&mut self, name: &str, data: &[u8]) -> bool {
        let path = format!("{}/{name}.bin", self.wire_dir);
        let expected = match read_file(&path) {
            Some(bytes) => bytes,
            None => {
                eprintln!(
                    "missing wire golden {path} — run from the schema repo root (or pass --wire-dir)"
                );
                return false;
            }
        };
        self.goldens_loaded
            .insert(format!("{name}.bin"), expected.clone());
        if expected.len() != data.len() || expected != data {
            eprintln!(
                "WIRE GOLDEN MISMATCH: {name} ({} golden vs {} actual bytes) — refusing to bench code that does not match the corpus",
                expected.len(),
                data.len()
            );
            return false;
        }
        true
    }

    // Loads <variant-dir>/<name>.variants.bin into the NUM_VARIANTS staggered
    // slots and returns the record size. Records are fixed-width by
    // construction — test/bench/table_main.cpp refuses to emit a corpus whose
    // records differ — so the record size IS file size / NUM_VARIANTS.
    fn load_variants(&mut self, name: &str, variants: &mut Vec<u8>) -> Option<usize> {
        let path = format!("{}/{name}.variants.bin", self.variant_dir);
        let packed = match read_file(&path) {
            Some(bytes) => bytes,
            None => {
                eprintln!(
                    "missing variant data {path} — run `make bench-table-corpus`, and run from the schema repo root (or pass --variant-dir)"
                );
                return None;
            }
        };
        if packed.is_empty() || packed.len() % NUM_VARIANTS != 0 {
            eprintln!(
                "variant data {path} is {} bytes, not a multiple of {NUM_VARIANTS} records — refusing to bench data whose stride is not the record size",
                packed.len()
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
        variants.clear();
        variants.resize(NUM_VARIANTS * VARIANT_STRIDE, 0);
        for k in 0..NUM_VARIANTS {
            variants[k * VARIANT_STRIDE..k * VARIANT_STRIDE + record]
                .copy_from_slice(&packed[k * record..(k + 1) * record]);
        }
        let base = path.rsplit('/').next().unwrap_or(&path).to_string();
        self.goldens_loaded.insert(base, packed);
        Some(record)
    }

    fn report(&mut self, bench: &str, path: &str, iters: u64, bytes_per_op: usize, s: &RunStats) {
        let mbps = s.median_rate * bytes_per_op as f64 / (1024.0 * 1024.0);
        eprintln!(
            "{bench:<18} {path:<11} {:10.3} M msg/s {mbps:10.1} MB/s   (min {:.3}, max {:.3}, spread {:.1}%)",
            s.median_rate / 1e6,
            s.min_rate / 1e6,
            s.max_rate / 1e6,
            s.spread_pct
        );
        if self.csv {
            let row = format!(
                "rust,{bench},{path},{iters},{bytes_per_op},{},{:.0},{:.0},{:.0},{mbps:.2},{:.2}",
                self.num_runs, s.median_rate, s.min_rate, s.max_rate, s.spread_pct
            );
            self.csv_rows.push((row, "table".to_string()));
        }
    }
}

// ------------------------------------------------------------------------------------------
// the data-driven table driver
// ------------------------------------------------------------------------------------------
//
// THE READ ARM RESETS BEFORE IT LOADS, and that is not overhead the runner
// added: the tolerant wire ELIDES a field at its default (§3), so `load` fills
// only what actually rode and a reused instance would otherwise keep the
// previous record's values in the elided fields. Resetting is part of a
// correct read into reused storage, in every language, so it is inside the
// clock rather than hidden outside it.
#[allow(clippy::too_many_arguments)]
fn bench_table<T: Default>(
    bench: &mut Bench,
    name: &str,
    golden: &str,
    base_iters: u64,
    reset: fn(&mut T),
    save: fn(&T, &mut [u8]) -> i64,
    load: fn(&mut T, &[u8], &mut TableReport) -> bool,
) {
    let iters = base_iters;

    let mut variants: Vec<u8> = Vec::new();
    let bytes_per_op = match bench.load_variants(name, &mut variants) {
        Some(n) => n,
        None => {
            bench.failed = true;
            return;
        }
    };
    let record = |k: usize| -> &[u8] {
        let at = k * VARIANT_STRIDE;
        &variants[at..at + bytes_per_op]
    };

    // gate 1 (§1.5): variant 0 IS the pinned instance.
    if !bench.check_golden(golden, record(0)) {
        bench.failed = true;
        return;
    }

    // gate 2: every variant loads, re-saves, and comes back byte-identical at
    // the same length — before any clock starts.
    let mut instances: Vec<Box<T>> = Vec::with_capacity(NUM_VARIANTS);
    let mut twin = vec![0u8; BUFFER_SIZE];
    for k in 0..NUM_VARIANTS {
        let mut instance: Box<T> = Box::default();
        reset(&mut instance);
        let mut report = TableReport::default();
        if !load(&mut instance, record(k), &mut report) || report.malformed {
            bench.fail(name, "load of a variant failed");
            return;
        }
        let wrote = save(&instance, &mut twin);
        if wrote != bytes_per_op as i64 || twin[..bytes_per_op] != *record(k) {
            bench.fail(
                name,
                "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus",
            );
            return;
        }
        instances.push(instance);
    }

    let mut buffer = vec![0u8; BUFFER_SIZE];
    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut roundtrip_rates = [0.0f64; MAX_NUM_RUNS];

    // WRITE: save the 64 pre-loaded instances round-robin. Rotating the
    // instances is the §2.7 variation: the encoder never sees the same input
    // twice in a row, and bytes/op is constant by construction rather than by
    // assertion. black_box is the byte fold's escape barrier.
    for run in 0..bench.num_runs + 1 {
        let start = Instant::now();
        for i in 0..iters {
            let wrote = save(
                &instances[i as usize & (NUM_VARIANTS - 1)],
                black_box(&mut buffer),
            );
            if wrote != bytes_per_op as i64 {
                bench.fail(name, "save failed in loop");
                return;
            }
            bench.sink = bench.sink.wrapping_add(black_box(wrote) as u64);
        }
        let time = start.elapsed().as_secs_f64();
        if run > 0 {
            write_rates[run - 1] = iters as f64 / time;
        }
    }

    // ROUND-TRIP: reset, load a variant buffer, then re-save what came out.
    // The load needs no sink discipline of its own — its output IS the save's
    // input, so every loaded field is observed by construction (§2.7's
    // read-side sink problem dissolved rather than equalized).
    let mut out: Box<T> = Box::default();
    for run in 0..bench.num_runs + 1 {
        let start = Instant::now();
        for i in 0..iters {
            reset(&mut out);
            let at = (i as usize & (NUM_VARIANTS - 1)) * VARIANT_STRIDE;
            let mut report = TableReport::default();
            if !load(
                &mut out,
                black_box(&variants[at..at + bytes_per_op]),
                &mut report,
            ) || report.malformed
            {
                bench.fail(name, "load failed in loop");
                return;
            }
            let wrote = save(&out, black_box(&mut buffer));
            if wrote != bytes_per_op as i64 {
                bench.fail(name, "re-save failed in loop");
                return;
            }
            bench.sink = bench.sink.wrapping_add(black_box(wrote) as u64);
        }
        let time = start.elapsed().as_secs_f64();
        if run > 0 {
            roundtrip_rates[run - 1] = iters as f64 / time;
        }
    }

    let w = run_stats(&mut write_rates[..bench.num_runs]);
    let rt = run_stats(&mut roundtrip_rates[..bench.num_runs]);
    bench.report(name, "write", iters, bytes_per_op, &w);
    bench.report(name, "round_trip", iters, bytes_per_op, &rt);

    // READ is DERIVED, never measured: round-trip time minus write time. It
    // prints for continuity and is NOT a CSV row — a derived number in the CSV
    // would be divided as if it had been measured (§2.9).
    let read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if read_time > 0.0 {
        eprintln!(
            "{name:<18} {:<11} {:10.3} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)",
            "read",
            1e-6 / read_time
        );
    }
}

fn main() {
    let mut bench = Bench {
        csv: false,
        num_runs: MAX_NUM_RUNS,
        wire_dir: "testdata/wire".to_string(),
        variant_dir: "bench/corpus/variants".to_string(),
        csv_rows: Vec::new(),
        goldens_loaded: BTreeMap::new(),
        failed: false,
        sink: 0,
    };

    let args: Vec<String> = env::args().collect();
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--csv" => bench.csv = true,
            "--wire-dir" if i + 1 < args.len() => {
                bench.wire_dir = args[i + 1].clone();
                i += 1;
            }
            "--variant-dir" if i + 1 < args.len() => {
                bench.variant_dir = args[i + 1].clone();
                i += 1;
            }
            "--round" if i + 1 < args.len() => {
                if args[i + 1].parse::<u64>().is_err() {
                    eprintln!("--round takes a non-negative integer, got '{}'", args[i + 1]);
                    exit(1);
                }
                bench.num_runs = 1;
                i += 1;
            }
            _ => {
                eprintln!(
                    "usage: {} [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>]",
                    args[0]
                );
                exit(1);
            }
        }
        i += 1;
    }

    if cfg!(debug_assertions) {
        eprintln!("schema tables bench (rust, Debug — only release numbers are meaningful)");
    } else {
        eprintln!("schema tables bench (rust, Release)");
    }

    if bench.csv {
        println!(
            "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline"
        );
    }

    // The one measured shape, named once — the generated type at the call site
    // and nothing else about it (bench/SHAPE-GATE.allow).
    bench_table::<TableMixed>(
        &mut bench,
        "bench_table",
        "bench_table",
        400000,
        table_mixed_reset,
        table_mixed_save,
        table_mixed_load,
    );

    bench.flush_csv();

    if bench.failed {
        eprintln!("TABLES BENCH FAILED (corpus_id {})", bench.corpus_id());
        exit(1);
    }

    eprintln!("OK (corpus_id {})", bench.corpus_id());
    black_box(bench.sink);
}
