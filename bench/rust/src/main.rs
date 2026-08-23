// schema bench — the Rust runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated Rust crate and the serialize.rs runtime: same benchmark set,
// same pinned corpus instances, same LCG (wrapping mul/add) and vary-function
// field mappings, same golden + round-trip self-checks (a mismatch REFUSES to
// bench), same warmup + 7 measured runs + median/min/max/spread, same CSV row
// format with lang=rust. See bench/README.md for the runner contract.
//
// Language-specific discipline:
//   - escape barrier: std::hint::black_box on the written buffer and on every
//     decoded value (the stub's sanctioned equivalent of the C++ empty-asm
//     clobber), plus a sink accumulator observed at exit
//   - streams borrow stack/heap buffers — construction per iteration is free,
//     exactly the C++ shape
//   - the driver is generic; write/read/vary monomorphize and inline like the
//     C++ template reference

#![allow(clippy::field_reassign_with_default)]
#![allow(clippy::too_many_arguments)]

use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::hint::black_box;
use std::time::Instant;

use example::*;
use realworldcorpus as realworld;
use serialize::{ReadStream, Stream, WriteStream};

mod rt;

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
// family is per ROW now (gen | rt | bits — §5.1); linkage/checks/opt/inline
// stay per-runner constants
const CSV_SUFFIX: &str = "crate,always,O3,unknown";

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

    fn report(&self, bench: &str, path: &str, iters: i64, bytes_per_op: i64, s: &RunStats, family: &'static str) {
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
        for (row, family) in self.csv_rows.borrow().iter() {
            println!("{row},{id},{family},{CSV_SUFFIX}");
        }
    }
}

// ------------------------------------------------------------------------------------------
// the per-message benchmark driver
// ------------------------------------------------------------------------------------------

// EW/ER: each generated unit carries its own Error type (example and
// realworldcorpus alike expose Result<T = ()> over their own enum); the
// driver only ever asks is_err(), so it is generic over both.
fn bench_message<T, W, R, V, EW, ER>(
    ctx: &Ctx,
    name: &str,
    golden: Option<&str>,
    iters: i64,
    pinned: T,
    write_fn: W,
    read_fn: R,
    mut vary_fn: V,
) where
    T: Copy + Default,
    W: Fn(&mut WriteStream<'_>, &T) -> core::result::Result<(), EW>,
    R: Fn(&mut ReadStream<'_>, &mut T) -> core::result::Result<(), ER>,
    V: FnMut(&mut T, u64),
{
    let mut buffer = vec![0u8; BUFFER_SIZE];
    let mut twin = vec![0u8; BUFFER_SIZE];
    let mut variants = vec![[0u8; VARIANT_STRIDE]; NUM_VARIANTS];

    // self-check 1: the pinned instance matches its wire golden byte-for-byte
    let mut base = pinned;
    let bytes_per_op;
    {
        let mut ws = WriteStream::new(&mut buffer);
        if write_fn(&mut ws, &base).is_err() {
            ctx.fail(name, "write of pinned instance failed");
            return;
        }
        ws.flush();
        bytes_per_op = ws.bytes_processed() as usize;
    }
    if let Some(golden) = golden {
        if !ctx.check_golden(golden, &buffer[..bytes_per_op]) {
            ctx.failed.set(true);
            return;
        }
    }

    // self-check 2: round-trip write -> read -> re-write -> identical bytes
    {
        let mut out = T::default();
        let mut rs = ReadStream::new(&buffer, bytes_per_op);
        if read_fn(&mut rs, &mut out).is_err() {
            ctx.fail(name, "read of pinned instance failed");
            return;
        }
        let twin_bytes;
        {
            let mut tws = WriteStream::new(&mut twin);
            if write_fn(&mut tws, &out).is_err() {
                ctx.fail(name, "re-write of decoded instance failed");
                return;
            }
            tws.flush();
            twin_bytes = tws.bytes_processed() as usize;
        }
        if twin_bytes != bytes_per_op || buffer[..bytes_per_op] != twin[..bytes_per_op] {
            ctx.fail(name, "round-trip bytes differ");
            return;
        }
    }

    // variant buffers for the read path (and proof that variation keeps bytes/op constant)
    let mut rng: u64 = 1;
    for k in 0..NUM_VARIANTS {
        rng = bench_rng(rng);
        vary_fn(&mut base, rng);
        let mut vs = WriteStream::new(&mut variants[k][..BUFFER_SIZE]);
        if write_fn(&mut vs, &base).is_err() {
            ctx.fail(name, "write of varied instance failed");
            return;
        }
        vs.flush();
        if vs.bytes_processed() as usize != bytes_per_op {
            ctx.fail(
                name,
                "variation changed bytes/op — vary must keep structure fields fixed",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];

    // write path: 1 warmup + NUM_RUNS measured
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for _ in 0..iters {
            rng = bench_rng(rng);
            vary_fn(&mut base, rng);
            let n;
            {
                let mut ws = WriteStream::new(&mut buffer);
                if write_fn(&mut ws, &base).is_err() {
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

    // read path: 1 warmup + NUM_RUNS measured; ONE decode instance hoisted
    // out of the loop and reused, matching the write loop's hoisted base — a
    // fresh T::default() per iteration is stack-zeroing harness overhead, not
    // serialize work (no heap either way; every field a read decodes is
    // overwritten every iteration, structure fields fixed across variants).
    // No per-iteration clone: read_fn takes &mut out, black_box takes &out.
    let mut out = T::default();
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for i in 0..iters {
            let mut rs = ReadStream::new(
                &variants[(i as usize) & (NUM_VARIANTS - 1)],
                bytes_per_op,
            );
            if read_fn(&mut rs, &mut out).is_err() {
                ctx.fail(name, "read failed in loop");
                return;
            }
            black_box(&out); // every decoded field is observed
            ctx.sink.set(ctx.sink.get().wrapping_add(1));
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            read_rates[run - 1] = iters as f64 / time;
        }
    }

    ctx.report(
        name,
        "write",
        iters,
        bytes_per_op as i64,
        &run_stats(&mut write_rates[..ctx.num_runs]),
        "gen",
    );
    ctx.report(
        name,
        "read",
        iters,
        bytes_per_op as i64,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "gen",
    );
}

// ------------------------------------------------------------------------------------------
// pinned corpus instances — the C++ reference's pin_* functions exactly
// (the same instances test/rust/src/main.rs pins to the wire goldens)
// ------------------------------------------------------------------------------------------

fn pin_rigidbody_moving() -> RigidBody {
    let mut input = RigidBody::default();
    input.position = Vec3 {
        x: 1.5,
        y: -2.5,
        z: 3.25,
    };
    input.orientation = Quat {
        x: 0.1,
        y: 0.2,
        z: 0.3,
        w: 0.9,
    };
    input.at_rest = false;
    input.linear_velocity = Vec3 {
        x: 10.0,
        y: 20.0,
        z: -3.0,
    };
    input.angular_velocity = Vec3 {
        x: 0.25,
        y: 0.5,
        z: 0.75,
    };
    input
}

fn pin_chat() -> Chat {
    let mut input = Chat::default();
    input.text[..11].copy_from_slice(b"wire parity");
    input.text_length = 11;
    input
}

fn pin_inputpacket() -> InputPacket {
    let mut input = InputPacket::default();
    input.synchronize_sequence = 7;
    input.current_frame = 123456789;
    input.start_frame = 123456780;
    input.inputs_count = 2;
    input.inputs[0].throttle = 0.5;
    input.inputs[0].fire = true;
    input.inputs[1].stick_x = -0.25;
    input.inputs[1].boost = true;
    input
}

fn pin_shipcreate() -> ShipCreate {
    let mut input = ShipCreate::default();
    input.ship_type = ShipType::BOMBER;
    input.position = QuantizedPosition {
        x: 1000,
        y: -2000,
        z: 3000,
    };
    input.has_flags = true;
    input.flags = SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING;
    input.team = Team::BLUE;
    input.health = 750;
    input.thrust = 55;
    input
}

fn pin_ship_shallow() -> ShipData_Shallow {
    let mut interp = ShipData_Interpolate::default();
    interp.ship_type = ShipType::CORVETTE;
    interp.position = Vec3 {
        x: 1.5,
        y: -2.25,
        z: 100.0,
    };
    interp.rotation = Quat {
        x: 0.0,
        y: 0.0,
        z: 0.0,
        w: 1.0,
    };
    interp.linear_velocity = Vec3 {
        x: 3.0,
        y: 0.0,
        z: -1.0,
    };
    interp.flags = SHIP_FLAGS_BOOSTING;
    interp.team = Team::RED;
    interp.health = 750;
    interp.thrust = 55;
    let mut q = ShipData_Shallow::default();
    quantize_ship(&interp, &mut q);
    q
}

fn pin_probe_header() -> ProbeHeader {
    ProbeHeader {
        version: 5,
        probe_id: 0x1122334455667788,
    }
}

fn pin_probebits() -> ProbeBits {
    let mut input = ProbeBits::default();
    input.small = 0x1FF;
    input.boundary = 0x1FFFFFFFF;
    input.wide = 0xFEDCBA9876543210;
    input.sensor = 4294967295;
    input.nonce = 18446744073709551615;
    input
}

fn pin_probearray() -> ProbeArray {
    let mut input = ProbeArray::new(); // defaults: samples active, retries -1 — as the C++ default ctor
    input.samples[0].orientation = 90.0;
    input.samples[0].raw_delta = -5;
    input.samples[0].big_delta = -1234567890123;
    input.samples[0].weapon = Weapon::LASER;
    input.samples[0].has_target = true;
    input.samples[0].target_id = 777;
    input.samples[0].samples_count = 1;
    input.samples[0].samples[0] = 42;
    input.samples[1].active = false;
    input.samples[1].orientation = -45.5;
    input.samples[1].raw_delta = 7;
    input.samples[1].big_delta = 99;
    input.samples[1].idle_ticks = 1000;
    input.samples[1].samples_count = 2;
    input.samples[1].samples[0] = 7;
    input.samples[1].samples[1] = 8;
    input.config.retries = 3;
    input.config.preferred = Weapon::MISSILE;
    input
}

#[allow(clippy::approx_constant)]
fn pin_testdata() -> TestData {
    let mut input = TestData::default();
    input.a = -100;
    input.b = 100;
    input.c = 149;
    input.d = 0x11;
    input.e = 0x22;
    input.f = 0x33;
    input.g = true;
    input.items_count = 3;
    input.items[0] = 0;
    input.items[1] = 128;
    input.items[2] = 255;
    input.float_value = 3.1415926;
    input.compressed_float_value = 2.5;
    input.double_value = 1.0 / 3.0;
    input.int8_value = -128;
    input.int16_value = -32768;
    input.uint8_value = 255;
    input.uint16_value = 65535;
    input.uint32_value = 4294967295;
    input.uint64_value = 18446744073709551615;
    input.int64_full = i64::MIN;
    input.int64_range = -999999999999;
    for i in 0..17 {
        input.fixed_bytes[i] = (i * 3) as u8;
    }
    input.text[..19].copy_from_slice(b"the quick brown fox");
    input.text_length = 19;
    input
}

// ------------------------------------------------------------------------------------------
// vary functions — the C++ reference's vary_* field mappings exactly: VALUE
// fields mutate within wire ranges through the LCG; structure fields (counts,
// lengths, branch bools) stay fixed so bytes/op is constant.
// ------------------------------------------------------------------------------------------

fn vary_rigidbody(m: &mut RigidBody, rng: u64) {
    m.position.x = ((rng >> 8) & 0xFFFF) as f64 * 0.25;
    m.position.y = ((rng >> 16) & 0xFFFF) as f64 * 0.5;
    m.position.z = ((rng >> 24) & 0xFFFF) as f64 * 0.125;
    m.orientation.x = (rng & 0xFF) as f64 * 0.001;
    m.linear_velocity.x = ((rng >> 32) & 0xFFF) as f64 * 0.25;
    m.angular_velocity.z = ((rng >> 40) & 0xFFF) as f64 * 0.125;
}

fn vary_rigidbody_at_rest(m: &mut RigidBody, rng: u64) {
    m.position.x = ((rng >> 8) & 0xFFFF) as f64 * 0.25;
    m.position.y = ((rng >> 16) & 0xFFFF) as f64 * 0.5;
    m.orientation.x = (rng & 0xFF) as f64 * 0.001;
}

fn vary_chat(m: &mut Chat, rng: u64) {
    for i in 0..m.text_length as usize {
        m.text[i] = b'a' + ((rng >> (i & 7)) & 15) as u8; // never zero
    }
}

fn vary_test(m: &mut Test, rng: u64) {
    m.test_a = rng as u16;
    m.test_b = ((rng >> 16) & 511) as i16; // within [0, 1000]
    m.test_c = ((rng >> 25) & 511) as i16;
    m.test_d = ((rng >> 34) & 511) as i16;
}

fn vary_inputpacket(m: &mut InputPacket, rng: u64) {
    m.synchronize_sequence = rng as u16;
    m.current_frame = rng;
    m.start_frame = rng >> 1;
    m.inputs[0].throttle = ((rng as u32) & 0xFF) as f32 / 256.0;
    m.inputs[0].fire = (rng & 1) != 0;
    m.inputs[1].stick_x = (((rng >> 8) as u32) & 0xFF) as f32 / 256.0 - 0.5;
    m.inputs[1].boost = (rng & 2) != 0;
}

fn vary_shipcreate(m: &mut ShipCreate, rng: u64) {
    m.position.x = (((rng >> 8) & 0xFFFFF) as i32) - 0x80000; // within [-8388608, 8388608]
    m.position.y = (((rng >> 16) & 0xFFFFF) as i32) - 0x80000;
    m.position.z = (((rng >> 24) & 0xFFFFF) as i32) - 0x80000;
    m.rotation.x = ((rng & 0x7FF) as i32 - 1024) as i16; // within [-1024, 1024]
    m.linear_velocity.x = (((rng >> 32) & 0x3FFFFF) as i32) - 2097152;
    m.flags = rng & 15; // 4 wire bits, has_flags stays true
    m.health = ((rng >> 5) & 511) as i16; // within [0, 1000]
    m.thrust = ((rng >> 14) & 63) as i8; // within [0, 100]
}

fn vary_ship_shallow(m: &mut ShipData_Shallow, rng: u64) {
    m.position_x = (((rng >> 8) & 0xFFFFF) as i32) - 0x80000;
    m.position_y = (((rng >> 16) & 0xFFFFF) as i32) - 0x80000;
    m.position_z = (((rng >> 24) & 0xFFFFF) as i32) - 0x80000;
    m.rotation_x = ((rng & 0x7FF) as i32 - 1024) as i16;
    m.rotation_w = (((rng >> 11) & 0x7FF) as i32 - 1024) as i16;
    m.linear_velocity_x = (((rng >> 32) & 0x3FFFFF) as i32) - 2097152;
    m.flags = rng & 15;
    m.health = ((rng >> 5) & 511) as u16;
    m.thrust = ((rng >> 14) & 63) as u8;
}

fn vary_probe_header(m: &mut ProbeHeader, rng: u64) {
    m.version = (rng as u32) & 7; // 3 wire bits
    m.probe_id = rng;
}

fn vary_probebits(m: &mut ProbeBits, rng: u64) {
    m.small = (rng as u32) & 511; // 9 bits
    m.boundary = rng & ((1u64 << 33) - 1); // 33 bits
    m.wide = rng.wrapping_mul(3);
    m.sensor = (rng >> 16) as u32;
    m.nonce = rng ^ 0x5555555555555555;
}

fn vary_probearray(m: &mut ProbeArray, rng: u64) {
    m.samples[0].orientation = -180.0 + ((rng as u32) & 0x3FFF) as f32 * 0.02;
    m.samples[0].raw_delta = (rng >> 8) as u32 as i32;
    m.samples[0].big_delta = rng.wrapping_mul(5) as i64;
    m.samples[0].target_id = (rng >> 24) as u16;
    m.samples[0].samples[0] = (rng >> 40) as u16;
    m.samples[1].orientation = -180.0 + (((rng >> 3) as u32) & 0x3FFF) as f32 * 0.02;
    m.samples[1].idle_ticks = (rng >> 32) as u32;
    m.samples[1].samples[0] = (rng >> 4) as u16;
    m.samples[1].samples[1] = (rng >> 12) as u16;
    m.config.retries = (rng >> 20) as u32 as i32;
}

fn vary_testdata(m: &mut TestData, rng: u64) {
    m.a = (rng & 127) as i32 - 64; // within [-100, 100]
    m.b = ((rng >> 7) & 127) as i32 - 64;
    m.c = ((rng >> 14) & 127) as i32 - 64; // within [-100, 150]
    m.d = (rng as u32) & 255;
    m.e = ((rng >> 8) as u32) & 255;
    m.f = ((rng >> 16) as u32) & 255;
    m.items[0] = (rng & 255) as i32; // items_count stays 3
    m.items[1] = ((rng >> 8) & 255) as i32;
    m.items[2] = ((rng >> 16) & 255) as i32;
    m.float_value = ((rng as u32) & 0xFFFF) as f32;
    m.compressed_float_value = ((rng as u32) & 1023) as f32 * 0.005; // within [0, 10] (max 5.115)
    m.double_value = (((rng >> 16) as i64) & 0xFFFFFF) as f64 * 0.5;
    m.int8_value = rng as i8;
    m.int16_value = (rng >> 8) as i16;
    m.uint8_value = (rng >> 16) as u8;
    m.uint16_value = (rng >> 24) as u16;
    m.uint32_value = (rng >> 32) as u32;
    m.uint64_value = rng.wrapping_mul(7);
    m.int64_full = rng.wrapping_mul(11) as i64;
    m.int64_range = (((rng >> 24) & ((1u64 << 37) - 1)) as i64) - (1i64 << 36); // within +/- 1e12
    m.fixed_bytes[0] = rng as u8;
    m.fixed_bytes[16] = (rng >> 8) as u8;
    for i in 0..m.text_length as usize {
        m.text[i] = b'a' + ((rng >> (i & 7)) & 15) as u8; // never zero
    }
}

// real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured through
// the GENERATED code (bench/corpus/RealWorld.schema ->
// generated/bench/rust-realworld). The pinned instance is the ALL-DEFAULTS
// instance: realworld::RealPacket::new() serialized unmodified, 1629 bits =
// 204 wire bytes, pinned to testdata/wire/real_packet.bin by
// test/bench/main.cpp. The four branch gates (f012 true, f043 false, f050
// true, f074 false) are STRUCTURE (§2.7): they keep their schema defaults
// here, so the same branch bodies ride every iteration and bytes/op is
// constant. The field mappings are bench/cpp/bench_main.cpp's
// vary_real_packet exactly — fields under the false gates do not ride and
// are not varied; every mapping keeps its field inside its declared wire
// range (comments give the bound it stays within).
fn vary_real_packet(m: &mut realworld::RealPacket, rng: u64) {
    // ranged ints, assorted widths, signed and unsigned
    m.f001_int = (((rng >> 8) & 0xFFFFF) as i32) - 0x80000; // +/-2^19 within +/-805495
    m.f003_int = (((rng >> 12) & 0xFFFFF) as i32) - 0x80000; // within +/-835897
    m.f005_uint = ((rng >> 20) & 0xFFF) as u16; // <=4095 within [0, 7316]
    m.f006_int = ((((rng >> 26) & 0x7FF) as i32) - 1024) as i16; // +/-1024 within +/-1513
    m.f009_int = ((((rng >> 33) & 31) as i32) - 16) as i8; // +/-16 within +/-22
    m.f033_uint = ((rng >> 37) & 0x1FFFF) as u32; // <=131071 within [0, 142780]
    m.f041_int = ((((rng >> 42) & 63) as i32) - 32) as i8; // +/-32 within +/-55
    m.f062_uint = ((rng >> 47) & 255) as u16; // <=255 within [0, 503]
    m.f088_int = ((((rng >> 52) & 0x3FF) as i32) - 512) as i16; // +/-512 within +/-694
    m.f090_uint = ((rng >> 57) & 127) as u8; // <=127 within [0, 214]
    // bits(N), narrow and wide
    m.f011_bits = (rng as u32) & 0x3FF; // 10 bits
    m.f023_bits = ((rng >> 5) as u32) & 0x1FFFFFF; // 25 bits
    m.f042_bits = ((rng >> 3) as u32) & 0x3FFFFFFF; // 30 bits
    m.f081_bits = ((rng >> 7) as u32) & 0x1FFFFFFF; // 29 bits
    m.f089_bits = rng & 0xFFFF_FFFF_FFFF; // 48 bits
    m.f093_bits = rng ^ 0x5555555555555555; // 64 bits
    m.f097_bits = ((rng >> 11) as u32) & 0xFFF; // 12 bits
    // bools (NEVER the four branch gates — those are structure, §2.7)
    m.f037_bool = (rng & 1) != 0;
    m.f055_bool = (rng & 2) != 0;
    m.f092_bool = (rng & 4) != 0;
    // float32 / float64
    m.f007_f32 = ((rng as u32) & 0xFFFF) as f32;
    m.f020_f32 = (((rng >> 16) as u32) & 0xFFFF) as f32 * 0.5;
    m.f058_f32 = (((rng >> 24) as u32) & 0xFFFF) as f32 * 0.25;
    m.f002_f64 = (((rng >> 8) as i64) & 0xFFFFFF) as f64 * 0.5;
    m.f059_f64 = (((rng >> 16) as i64) & 0xFFFFFF) as f64 * 0.25;
    m.f087_f64 = (((rng >> 24) as i64) & 0xFFFFFF) as f64 * 0.125;
    // compressed floats (in range by construction)
    m.f004_cf32 = ((rng as u32) & 0x3FFF) as f32 * 0.1; // <=1638.3 within [0, 2000]
    m.f061_cf32 = -90.0 + (((rng >> 9) as u32) & 255) as f32 * 0.5; // within [-90, 90] (max 37.5)
    m.f067_cf32 = -100.0 + (((rng >> 18) as u32) & 511) as f32 * 0.25; // within [-100, 100] (max 27.75)
    m.f072_cf32 = (((rng >> 27) as u32) & 8191) as f32 * 0.01; // <=81.91 within [0, 100]
    // fixed / ufixed (raw storage scaled by 2^F; bounds are whole units)
    m.f016_fixed = (((rng >> 10) & 0x3FFFFFF) as i32) - 0x2000000; // +/-2^25 within +/-36*2^20
    m.f025_fixed = ((((rng >> 18) & 0x7FFF) as i32) - 0x4000) as i16; // +/-2^14 within +/-119*2^8
    m.f095_fixed = (((rng >> 22) & 0x7FFFFFF) as i32) - 0x4000000; // +/-2^26 within +/-1577*2^16
    m.f021_ufixed = ((rng >> 30) as u32) & 0x3FFFFFF; // <=2^26-1 within 25141*2^12
    m.f049_ufixed = ((rng >> 36) & 0x7FFF) as u16; // <=32767 within 3*2^14
    m.f084_ufixed = ((rng >> 44) & 0x7F) as u8; // <=127 within 1*2^7
    // enum / flags (wire-valid by construction)
    m.f036_enum = realworld::PacketMode((((rng >> 30) as u32) & 3) as u8); // within wire range [0, 5]
    m.f083_enum = realworld::PacketMode((((rng >> 34) as u32) & 3) as u8);
    m.f091_flags = rng & 31; // 5 wire bits
    // full-width 64-bit
    m.f008_u64 = rng;
    m.f029_i64 = rng.wrapping_mul(3) as i64;
    m.f063_i64 = rng.wrapping_mul(5) as i64;
    // fields riding inside the TAKEN branches (f012 true, f050 true)
    m.f013_f32 = (((rng >> 4) as u32) & 0xFFFF) as f32;
    m.f014_uint = ((rng >> 21) & 511) as u16; // <=511 within [0, 775]
    m.f015_int = ((((rng >> 40) & 31) as i32) - 16) as i8; // +/-16 within +/-21
    m.f017_uint = ((rng >> 29) & 0xFFF) as u16; // <=4095 within [0, 4606]
    m.f051_bool = (rng & 8) != 0;
    m.f052_int = ((((rng >> 38) & 63) as i32) - 32) as i8; // +/-32 within +/-57
    m.f053_f32 = (((rng >> 40) as u32) & 0xFFFF) as f32 * 0.125;
    m.f054_int = ((((rng >> 45) & 63) as i32) - 32) as i8; // +/-32 within +/-35
}

// ------------------------------------------------------------------------------------------
// the synthetic steady-state batch: NUM_BATCH_MESSAGES messages through the
// Message dispatch surface (write_message/read_message) plus the None
// terminator, mixed types driven by the LCG — the C++ batch builder exactly.
// ------------------------------------------------------------------------------------------

const NUM_BATCH_MESSAGES: usize = 4096;
const BATCH_PASSES: usize = 25600; // rescaled 2026-08-23 with the mix rebalance: §2.1's floor wins (§1.2)

fn build_batch(ctx: &Ctx, messages: &mut Vec<Message>, batch_buffer: &mut [u8]) -> Option<i64> {
    messages.clear();

    let mut rng: u64 = 12345;
    for _ in 0..NUM_BATCH_MESSAGES {
        rng = bench_rng(rng);
        let pick = ((rng >> 32) % 20) as i32;
        let m = if pick < 1 {
            // 5% Chat
            let mut c = Chat::default();
            c.text_length = 8 + (rng & 7) as i32;
            for i in 0..c.text_length as usize {
                c.text[i] = b'a' + ((rng >> (i & 7)) & 15) as u8;
            }
            Message::Chat(c)
        } else if pick < 7 {
            // 30% Test
            let mut t = Test::default();
            t.test_a = rng as u16;
            t.test_b = ((rng >> 16) & 511) as i16;
            t.test_c = ((rng >> 25) & 511) as i16;
            t.test_d = ((rng >> 34) & 511) as i16;
            Message::Test(t)
        } else if pick < 12 {
            // 25% Synchronize
            let mut s = Synchronize::default();
            s.sync_frame = rng;
            s.sync_sequence = (rng >> 8) as u16;
            Message::Synchronize(s)
        } else if pick < 17 {
            // 25% Timescale
            let mut t = Timescale::default();
            t.scale = ((rng as u32) & 0xFFFF) as f64 / 65536.0;
            t.frame_a = (rng >> 16) as u32;
            t.frame_b = (rng >> 24) as u32;
            Message::Timescale(t)
        } else if pick < 19 {
            // 10% Heartbeat
            Message::Heartbeat(Heartbeat::default())
        } else {
            // 5% Block
            let mut b = Block::default();
            b.data_length = 8 + (rng & 15) as i32;
            for i in 0..b.data_length as usize {
                b.data[i] = (rng >> (i & 31)) as u8;
            }
            Message::Block(b)
        };
        messages.push(m);
    }

    let mut ws = WriteStream::new(batch_buffer);
    for m in messages.iter() {
        if write_message(&mut ws, m).is_err() {
            ctx.fail("message_batch", "batch write failed during setup");
            return None;
        }
    }
    if write_message(&mut ws, &Message::None).is_err() {
        ctx.fail("message_batch", "terminator write failed during setup");
        return None;
    }
    ws.flush();
    Some(ws.bytes_processed() as i64)
}

fn bench_batch(ctx: &Ctx) {
    // worst case is NUM_BATCH_MESSAGES * MESSAGE_MAX_BYTES; actual is far
    // less. + 8 read slack, and the size is a multiple of 8 (write contract).
    let batch_buffer_size = (NUM_BATCH_MESSAGES + 1) * MESSAGE_MAX_BYTES + 8;
    let mut batch_buffer = vec![0u8; batch_buffer_size];

    let mut messages: Vec<Message> = Vec::with_capacity(NUM_BATCH_MESSAGES);
    let Some(batch_bytes) = build_batch(ctx, &mut messages, &mut batch_buffer) else {
        return;
    };

    let passes = BATCH_PASSES;
    let total_msgs = (passes * NUM_BATCH_MESSAGES) as i64;

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];
    let mut rng: u64 = 999;

    // write path: whole batch per pass; one message mutates per pass so the
    // batch is never loop-invariant
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for _ in 0..passes {
            rng = bench_rng(rng);
            match &mut messages[((rng >> 16) as usize) % NUM_BATCH_MESSAGES] {
                Message::Synchronize(s) => s.sync_frame = rng,
                Message::Test(t) => t.test_a = rng as u16,
                _ => {}
            }
            let n;
            {
                let mut ws = WriteStream::new(&mut batch_buffer);
                for m in messages.iter() {
                    if write_message(&mut ws, m).is_err() {
                        ctx.fail("message_batch", "write failed in loop");
                        return;
                    }
                }
                let _ = write_message(&mut ws, &Message::None);
                ws.flush();
                n = ws.bytes_processed();
            }
            black_box(&batch_buffer);
            ctx.sink.set(ctx.sink.get().wrapping_add(n));
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = total_msgs as f64 / time;
        }
    }

    // the read buffer: rebuild once from the deterministic batch state so bytes match
    if build_batch(ctx, &mut messages, &mut batch_buffer).is_none() {
        return;
    }

    // read path: read messages until the None terminator, whole batch per
    // pass; ONE reused Message hoisted out of the loop, filled through
    // read_message_into (the Rust shape of the go/cs runners' hoisted
    // MessageStorage and the C++ runner's reused Message) — read_message's
    // by-value return copies the ~2 KB union out of the call per message,
    // which is harness shape, not serialize work. The into-path is
    // byte-verified against the corpus golden in check_message_stream_golden
    // before any number is produced.
    let mut storage = Message::None;
    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        for _ in 0..passes {
            let mut rs = ReadStream::new(&batch_buffer, batch_bytes as usize);
            let mut count: i64 = 0;
            loop {
                if read_message_into(&mut rs, &mut storage).is_err() {
                    ctx.fail("message_batch", "read failed in loop");
                    return;
                }
                if matches!(storage, Message::None) {
                    break;
                }
                black_box(&storage); // every decoded field is observed
                count += 1;
            }
            if count != NUM_BATCH_MESSAGES as i64 {
                ctx.fail("message_batch", "batch message count mismatch on read");
                return;
            }
            ctx.sink.set(ctx.sink.get().wrapping_add(count as u64));
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            read_rates[run - 1] = total_msgs as f64 / time;
        }
    }

    let bytes_per_msg = batch_bytes / NUM_BATCH_MESSAGES as i64;
    ctx.report(
        "message_batch",
        "write",
        total_msgs,
        bytes_per_msg,
        &run_stats(&mut write_rates[..ctx.num_runs]),
        "gen",
    );
    ctx.report(
        "message_batch",
        "read",
        total_msgs,
        bytes_per_msg,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "gen",
    );
}

// ------------------------------------------------------------------------------------------

// the message_stream golden: dispatch wire self-check (not a benchmark).
// Both dispatch read surfaces are held to the golden bytes — read_message
// (by-value) and read_message_into (the reused-storage surface the batch
// read loop runs on) must decode the golden to equal messages, and the
// into-path decode must re-write to the golden byte-for-byte.
fn check_message_stream_golden(ctx: &Ctx) {
    let mut chat = Chat::default();
    chat.text[..8].copy_from_slice(b"dispatch");
    chat.text_length = 8;
    let mut test = Test::default();
    test.test_b = 42;

    let mut buffer = vec![0u8; BUFFER_SIZE];
    let n;
    {
        let mut ws = WriteStream::new(&mut buffer);
        if write_message(&mut ws, &Message::Chat(chat)).is_err()
            || write_message(&mut ws, &Message::Test(test)).is_err()
            || write_message(&mut ws, &Message::None).is_err()
        {
            ctx.fail("message_stream", "dispatch write failed");
            return;
        }
        ws.flush();
        n = ws.bytes_processed() as usize;
    }
    if !ctx.check_golden("message_stream", &buffer[..n]) {
        ctx.failed.set(true);
        return;
    }

    // by-value decode of the golden bytes
    let mut by_value = Vec::new();
    {
        let mut rs = ReadStream::new(&buffer, n);
        loop {
            match read_message(&mut rs) {
                Ok(Message::None) => break,
                Ok(m) => by_value.push(m),
                Err(_) => {
                    ctx.fail("message_stream", "by-value dispatch read failed");
                    return;
                }
            }
        }
    }

    // into-path decode into ONE reused storage, pre-poisoned with a full
    // Block so the zero-form reset (SPEC §5) is what equality proves
    let mut storage = {
        let mut b = Block::default();
        b.data_length = b.data.len() as i32;
        for byte in b.data.iter_mut() {
            *byte = 0xFF;
        }
        Message::Block(b)
    };
    let mut into_msgs = Vec::new();
    {
        let mut rs = ReadStream::new(&buffer, n);
        loop {
            if read_message_into(&mut rs, &mut storage).is_err() {
                ctx.fail("message_stream", "read_message_into failed on the golden bytes");
                return;
            }
            if matches!(storage, Message::None) {
                break;
            }
            into_msgs.push(storage);
        }
    }
    if by_value.len() != 2 || into_msgs != by_value {
        ctx.fail(
            "message_stream",
            "read_message_into decode differs from read_message on the golden bytes",
        );
        return;
    }

    // and the into-path decode re-writes to the golden bytes exactly
    let mut twin = vec![0u8; BUFFER_SIZE];
    let twin_n;
    {
        let mut tws = WriteStream::new(&mut twin);
        for m in &into_msgs {
            if write_message(&mut tws, m).is_err() {
                ctx.fail("message_stream", "re-write of into-path decode failed");
                return;
            }
        }
        if write_message(&mut tws, &Message::None).is_err() {
            ctx.fail("message_stream", "re-write of into-path terminator failed");
            return;
        }
        tws.flush();
        twin_n = tws.bytes_processed() as usize;
    }
    if twin_n != n || twin[..twin_n] != buffer[..n] {
        ctx.fail(
            "message_stream",
            "into-path round-trip bytes differ from the golden",
        );
    }
}

fn main() {
    let mut csv = false;
    let mut wire_dir = String::from("../../testdata/wire");
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
            _ => {
                eprintln!("usage: {} [--csv] [--round K] [--wire-dir <dir>]", args[0]);
                std::process::exit(1);
            }
        }
        i += 1;
    }

    let ctx = Ctx {
        csv,
        wire_dir,
        num_runs,
        failed: Cell::new(false),
        sink: Cell::new(0),
        csv_rows: RefCell::new(Vec::new()),
        goldens_loaded: RefCell::new(BTreeMap::new()),
    };

    eprintln!("schema bench (rust)");

    check_message_stream_golden(&ctx);

    // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
    let mut at_rest = pin_rigidbody_moving();
    at_rest.at_rest = true;

    bench_message(&ctx, "rigidbody_moving", Some("rigidbody_moving"), 24000000, pin_rigidbody_moving(), write_rigid_body, read_rigid_body, vary_rigidbody);
    bench_message(&ctx, "rigidbody_at_rest", Some("rigidbody_at_rest"), 32000000, at_rest, write_rigid_body, read_rigid_body, vary_rigidbody_at_rest);
    bench_message(&ctx, "chat", Some("chat"), 48000000, pin_chat(), write_chat, read_chat, vary_chat);
    bench_message(&ctx, "test", None, 192000000, Test::default(), write_test, read_test, vary_test);
    bench_message(&ctx, "inputpacket", Some("inputpacket"), 16000000, pin_inputpacket(), write_input_packet, read_input_packet, vary_inputpacket);
    bench_message(&ctx, "shipcreate", Some("shipcreate_flags"), 32000000, pin_shipcreate(), write_ship_create, read_ship_create, vary_shipcreate);
    bench_message(&ctx, "ship_shallow", Some("ship_shallow"), 32000000, pin_ship_shallow(), write_ship_data_shallow, read_ship_data_shallow, vary_ship_shallow);
    bench_message(&ctx, "probe_header", Some("probe_header"), 256000000, pin_probe_header(), write_probe_header, read_probe_header, vary_probe_header);
    bench_message(&ctx, "probebits", Some("probebits"), 128000000, pin_probebits(), write_probe_bits, read_probe_bits, vary_probebits);
    bench_message(&ctx, "probearray", Some("probearray"), 20000000, pin_probearray(), write_probe_array, read_probe_array, vary_probearray);
    bench_message(&ctx, "testdata", Some("testdata"), 8000000, pin_testdata(), write_test_data, read_test_data, vary_testdata);

    // real_packet (§1.7): the realistic snapshot — ~93 riding individually
    // serialized small fields, 204 wire bytes, 0% bulk share by bits. The pin
    // is the ALL-DEFAULTS instance (RealPacket::new() — the C++ RealPacket{}),
    // golden-gated like every row above. Iteration count sized in the C++
    // reference (§2.1).
    bench_message(&ctx, "real_packet", Some("real_packet"), 8000000, realworld::RealPacket::new(), realworld::write_real_packet, realworld::read_real_packet, vary_real_packet);

    bench_batch(&ctx);

    // family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
    // the goldens the generated code pinned. Iteration counts are fixed and
    // identical across all five runners (§2.1; sized in the C++ reference).
    rt::bench_rt_all(&ctx);

    // family bits (§1.4): the one bitpacker workload in the estate
    rt::bench_bitpacker(&ctx, 24576);

    ctx.flush_csv(); // rows carry the corpus_id of the goldens this run loaded

    if ctx.failed.get() {
        eprintln!("BENCH FAILED (corpus_id {})", ctx.corpus_id());
        std::process::exit(1);
    }

    eprintln!("OK (corpus_id {})", ctx.corpus_id());
    black_box(ctx.sink.get());
}
