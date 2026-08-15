// schema bench — families rt and bits for the Rust runner.
//
// Family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.rs runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the Stream trait, the way a game would write them. The §1.5 oracle gate
// byte-compares the hand-written wire against the goldens the GENERATED code
// pinned (testdata/wire/bench_*.bin) and round-trips before any number. No
// `pub` on the packet types or ops per §3.1. Per §3.2 every benched op
// (each monomorphized Serialize) has EXACTLY two call sites: its untimed
// once-helper and its timed loop (#[inline(never)], so the §4.1 verdict has
// a loop body to count).
//
// Family bits (§1.4): the raw BitWriter/BitReader with the 16-width table
// (227 bits/group) over a 65536-byte buffer. Values vary per pass through
// the LCG (widths are the structure and stay fixed; bytes/pass asserted
// constant); reads rotate 64 pre-written variant buffers, each verified to
// read back exactly what was written before any number is produced.

use std::hint::black_box;
use std::time::Instant;

use serialize::{BitReader, BitWriter, ReadStream, Stream, WriteStream};

use crate::{bench_rng, run_stats, Ctx, BUFFER_SIZE, MAX_NUM_RUNS, NUM_VARIANTS, VARIANT_STRIDE};

// ---- the four shapes, hand-written (Bench.schema §1.3) ----

#[derive(Copy, Clone, Default)]
struct RtBenchPacket {
    a: i32,
    b: i32,
    c: i32,
    bits7: u32,
    bits13: u32,
    bits23: u32,
    flag: bool,
    x: f32,
    y: f32,
    z: f32,
    big: u64,
    blob: [u8; 17],
}

// serialize.rs 2.0.0: the canonical generic signature returns Result<(), S::Error>,
// so the write/measure instantiations are statically infallible and every `?`
// in them compiles to nothing (the read instantiation stays fully checked).
fn serialize_rt_packet<S: Stream>(s: &mut S, p: &mut RtBenchPacket) -> serialize::Result<(), S::Error> {
    s.serialize_int(&mut p.a, -100, 100)?;
    s.serialize_int(&mut p.b, 0, 65535)?;
    s.serialize_int(&mut p.c, -1000000, 1000000)?;
    s.serialize_bits(&mut p.bits7, 7)?;
    s.serialize_bits(&mut p.bits13, 13)?;
    s.serialize_bits(&mut p.bits23, 23)?;
    s.serialize_bool(&mut p.flag)?;
    s.serialize_f32(&mut p.x)?;
    s.serialize_f32(&mut p.y)?;
    s.serialize_f32(&mut p.z)?;
    s.serialize_u64(&mut p.big)?;
    s.serialize_bytes(&mut p.blob)?; // aligns internally — the schema says `align` out loud
    Ok(())
}

#[derive(Copy, Clone, Default)]
struct RtBenchInts {
    f0: i32,
    f1: i32,
    f2: i32,
    f3: i32,
    f4: i32,
    f5: i32,
    f6: i32,
    f7: i32,
    f8: i32,
    f9: i32,
}

fn serialize_rt_ints<S: Stream>(s: &mut S, f: &mut RtBenchInts) -> serialize::Result<(), S::Error> {
    s.serialize_int(&mut f.f0, -100, 100)?;
    s.serialize_int(&mut f.f1, 0, 65535)?;
    s.serialize_int(&mut f.f2, -1000000, 1000000)?;
    s.serialize_int(&mut f.f3, 0, 3)?;
    s.serialize_int(&mut f.f4, -15, 15)?;
    s.serialize_int(&mut f.f5, 0, 1000)?;
    s.serialize_int(&mut f.f6, -2048, 2047)?;
    s.serialize_int(&mut f.f7, 0, 255)?;
    s.serialize_int(&mut f.f8, -600000, 600000)?;
    s.serialize_int(&mut f.f9, 0, 100)?;
    Ok(())
}

#[derive(Copy, Clone, Default)]
struct RtBenchBits {
    b7: u32,
    b13: u32,
    b23: u32,
    b3: u32,
    b32: u32,
    b11: u32,
    b19: u32,
    b48: u64,
}

fn serialize_rt_bits<S: Stream>(s: &mut S, f: &mut RtBenchBits) -> serialize::Result<(), S::Error> {
    s.serialize_bits(&mut f.b7, 7)?;
    s.serialize_bits(&mut f.b13, 13)?;
    s.serialize_bits(&mut f.b23, 23)?;
    s.serialize_bits(&mut f.b3, 3)?;
    s.serialize_bits(&mut f.b32, 32)?;
    s.serialize_bits(&mut f.b11, 11)?;
    s.serialize_bits(&mut f.b19, 19)?;
    s.serialize_bits64(&mut f.b48, 48)?;
    Ok(())
}

#[derive(Copy, Clone, Default)]
struct RtBenchMixed {
    sequence: i32,
    ack_bits: u32,
    entity_id: u32,
    pos_x: i32,
    pos_y: i32,
    pos_z: i32,
    yaw: u32,
    moving: bool,
    firing: bool,
    timestamp: u64,
    weapon: i32,
}

fn serialize_rt_mixed<S: Stream>(s: &mut S, f: &mut RtBenchMixed) -> serialize::Result<(), S::Error> {
    s.serialize_int(&mut f.sequence, 0, 65535)?;
    s.serialize_bits(&mut f.ack_bits, 32)?;
    s.serialize_bits(&mut f.entity_id, 12)?;
    s.serialize_int(&mut f.pos_x, -16384, 16383)?;
    s.serialize_int(&mut f.pos_y, -16384, 16383)?;
    s.serialize_int(&mut f.pos_z, -16384, 16383)?;
    s.serialize_bits(&mut f.yaw, 9)?;
    s.serialize_bool(&mut f.moving)?;
    s.serialize_bool(&mut f.firing)?;
    s.serialize_bits64(&mut f.timestamp, 48)?;
    s.serialize_int(&mut f.weapon, 0, 15)?;
    Ok(())
}

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

fn pin_rt_packet() -> RtBenchPacket {
    let mut p = RtBenchPacket {
        a: -37,
        b: 12345,
        c: 987654,
        bits7: 97,
        bits13: 5000,
        bits23: 1234567,
        flag: true,
        x: 1.5,
        y: -3.25,
        z: 100.125,
        big: 0x123456789ABCDEF0,
        blob: [0; 17],
    };
    for (i, byte) in p.blob.iter_mut().enumerate() {
        *byte = (i * 31) as u8;
    }
    p
}

fn pin_rt_ints() -> RtBenchInts {
    RtBenchInts {
        f0: -37,
        f1: 12345,
        f2: 987654,
        f3: 2,
        f4: -15,
        f5: 777,
        f6: -2048,
        f7: 200,
        f8: -543210,
        f9: 99,
    }
}

fn pin_rt_bits() -> RtBenchBits {
    RtBenchBits {
        b7: 97,
        b13: 5000,
        b23: 1234567,
        b3: 5,
        b32: 0xDEADBEEF,
        b11: 1024,
        b19: 333333,
        b48: 0xFEDCBA987654,
    }
}

fn pin_rt_mixed() -> RtBenchMixed {
    RtBenchMixed {
        sequence: 52428,
        ack_bits: 0xA5A5A5A5,
        entity_id: 2049,
        pos_x: -16384,
        pos_y: 16383,
        pos_z: -1,
        yaw: 511,
        moving: true,
        firing: false,
        timestamp: 0x123456789ABC,
        weapon: 15,
    }
}

// ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

fn vary_rt_packet(p: &mut RtBenchPacket, rng: u64) {
    p.a = (((rng >> 8) & 63) as i32) - 32;
    p.b = ((rng >> 16) as u32 & 65535) as i32;
    p.c = (((rng >> 24) & 0xFFFFF) as i32) - 500000;
    p.bits7 = rng as u32 & 127;
    p.bits13 = (rng >> 3) as u32 & 8191;
    p.bits23 = (rng >> 5) as u32 & 8388607;
    p.flag = (rng & 1) != 0;
    p.x = (rng as u32 & 0xFFFF) as f32;
    p.big = rng;
    p.blob[0] = (rng >> 32) as u8;
}

fn vary_rt_ints(f: &mut RtBenchInts, rng: u64) {
    f.f0 = (((rng >> 8) & 63) as i32) - 32;
    f.f1 = ((rng >> 16) as u32 & 65535) as i32;
    f.f2 = (((rng >> 24) & 0xFFFFF) as i32) - 500000;
    f.f3 = ((rng >> 2) as u32 & 3) as i32;
    f.f4 = (((rng >> 11) & 15) as i32) - 8;
    f.f5 = ((rng >> 22) as u32 & 511) as i32;
    f.f6 = (((rng >> 33) & 2047) as i32) - 1024;
    f.f7 = ((rng >> 40) as u32 & 255) as i32;
    f.f8 = (((rng >> 30) & 0xFFFFF) as i32) - 500000;
    f.f9 = ((rng >> 57) as u32 & 63) as i32;
}

fn vary_rt_bits(f: &mut RtBenchBits, rng: u64) {
    f.b7 = rng as u32 & 127;
    f.b13 = (rng >> 3) as u32 & 8191;
    f.b23 = (rng >> 5) as u32 & 8388607;
    f.b3 = (rng >> 29) as u32 & 7;
    f.b32 = (rng >> 16) as u32;
    f.b11 = (rng >> 37) as u32 & 2047;
    f.b19 = (rng >> 44) as u32 & 524287;
    f.b48 = rng & 0xFFFFFFFFFFFF;
}

fn vary_rt_mixed(f: &mut RtBenchMixed, rng: u64) {
    f.sequence = ((rng >> 8) as u32 & 65535) as i32;
    f.ack_bits = (rng >> 16) as u32;
    f.entity_id = rng as u32 & 4095;
    f.pos_x = (((rng >> 20) & 32767) as i32) - 16384;
    f.pos_y = (((rng >> 25) & 32767) as i32) - 16384;
    f.pos_z = (((rng >> 30) & 32767) as i32) - 16384;
    f.yaw = (rng >> 3) as u32 & 511;
    f.moving = (rng & 1) != 0;
    f.firing = (rng & 2) != 0;
    f.timestamp = rng & 0xFFFFFFFFFFFF;
    f.weapon = ((rng >> 60) as u32 & 15) as i32;
}

// ---- per-shape once-helpers (the single untimed call site per op, §3.2)
// and timed loops (#[inline(never)], one symbol per shape+path) ----

macro_rules! rt_family {
    ($shape:ty, $serialize:ident, $vary:ident,
     $once_write:ident, $once_read:ident, $write_loop:ident, $read_loop:ident) => {
        fn $once_write(msg: &mut $shape, buffer: &mut [u8]) -> i64 {
            let mut ws = WriteStream::new(buffer);
            if $serialize(&mut ws, msg).is_err() {
                return -1;
            }
            ws.flush();
            ws.bytes_processed() as i64
        }

        fn $once_read(out: &mut $shape, buffer: &[u8], bytes: usize) -> bool {
            let mut rs = ReadStream::new(buffer, bytes);
            $serialize(&mut rs, out).is_ok()
        }

        #[inline(never)]
        fn $write_loop(
            ctx: &Ctx,
            base: &mut $shape,
            iters: i64,
            rng: &mut u64,
            buffer: &mut [u8],
        ) -> bool {
            for _ in 0..iters {
                *rng = bench_rng(*rng);
                $vary(base, *rng);
                let n;
                {
                    let mut ws = WriteStream::new(buffer);
                    if $serialize(&mut ws, base).is_err() {
                        return false;
                    }
                    ws.flush();
                    n = ws.bytes_processed();
                }
                black_box(&*buffer);
                ctx.sink.set(ctx.sink.get().wrapping_add(n));
            }
            true
        }

        #[inline(never)]
        fn $read_loop(
            ctx: &Ctx,
            out: &mut $shape,
            iters: i64,
            bytes_per_op: usize,
            variants: &[[u8; VARIANT_STRIDE]],
        ) -> bool {
            for i in 0..iters {
                let mut rs =
                    ReadStream::new(&variants[(i as usize) & (NUM_VARIANTS - 1)], bytes_per_op);
                if $serialize(&mut rs, out).is_err() {
                    return false;
                }
                black_box(&*out);
                ctx.sink.set(ctx.sink.get().wrapping_add(1));
            }
            true
        }
    };
}

rt_family!(RtBenchPacket, serialize_rt_packet, vary_rt_packet,
    rt_once_write_packet, rt_once_read_packet, rt_bench_packet_write_loop, rt_bench_packet_read_loop);
rt_family!(RtBenchInts, serialize_rt_ints, vary_rt_ints,
    rt_once_write_ints, rt_once_read_ints, rt_bench_ints_write_loop, rt_bench_ints_read_loop);
rt_family!(RtBenchBits, serialize_rt_bits, vary_rt_bits,
    rt_once_write_bits, rt_once_read_bits, rt_bench_bits_write_loop, rt_bench_bits_read_loop);
rt_family!(RtBenchMixed, serialize_rt_mixed, vary_rt_mixed,
    rt_once_write_mixed, rt_once_read_mixed, rt_bench_mixed_write_loop, rt_bench_mixed_read_loop);

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

#[allow(clippy::too_many_arguments)]
fn bench_rt<T: Copy + Default>(
    ctx: &Ctx,
    name: &str,
    iters: i64,
    pinned: T,
    once_write: fn(&mut T, &mut [u8]) -> i64,
    once_read: fn(&mut T, &[u8], usize) -> bool,
    write_loop: fn(&Ctx, &mut T, i64, &mut u64, &mut [u8]) -> bool,
    read_loop: fn(&Ctx, &mut T, i64, usize, &[[u8; VARIANT_STRIDE]]) -> bool,
    vary: fn(&mut T, u64),
) {
    let mut buffer = vec![0u8; BUFFER_SIZE];
    let mut twin = vec![0u8; BUFFER_SIZE];
    let mut variants = vec![[0u8; VARIANT_STRIDE]; NUM_VARIANTS];

    // oracle 1: the hand-written wire must equal the generated-code golden
    let mut base = pinned;
    let wrote = once_write(&mut base, &mut buffer);
    if wrote < 0 {
        ctx.fail(name, "write of pinned instance failed");
        return;
    }
    let bytes_per_op = wrote as usize;
    if !ctx.check_golden(name, &buffer[..bytes_per_op]) {
        ctx.failed.set(true);
        return;
    }

    // oracle 2: round-trip write -> read -> re-write -> identical bytes
    let mut out = T::default();
    if !once_read(&mut out, &buffer, bytes_per_op) {
        ctx.fail(name, "read of pinned instance failed");
        return;
    }
    if once_write(&mut out, &mut twin) != bytes_per_op as i64
        || buffer[..bytes_per_op] != twin[..bytes_per_op]
    {
        ctx.fail(name, "round-trip bytes differ");
        return;
    }

    // variant buffers (and proof that variation keeps bytes/op constant)
    let mut rng: u64 = 1;
    for k in 0..NUM_VARIANTS {
        rng = bench_rng(rng);
        vary(&mut base, rng);
        if once_write(&mut base, &mut variants[k][..BUFFER_SIZE]) != bytes_per_op as i64 {
            ctx.fail(
                name,
                "variation changed bytes/op — vary must keep structure fields fixed",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !write_loop(ctx, &mut base, iters, &mut rng, &mut buffer) {
            ctx.fail(name, "write failed in loop");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = iters as f64 / time;
        }
    }

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !read_loop(ctx, &mut out, iters, bytes_per_op, &variants) {
            ctx.fail(name, "read failed in loop");
            return;
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
        "rt",
    );
    ctx.report(
        name,
        "read",
        iters,
        bytes_per_op as i64,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "rt",
    );
}

// iteration counts fixed and identical across all five runners (§2.1; sized
// in the C++ reference)
pub fn bench_rt_all(ctx: &Ctx) {
    bench_rt(ctx, "bench_packet", 32000000, pin_rt_packet(),
        rt_once_write_packet, rt_once_read_packet, rt_bench_packet_write_loop, rt_bench_packet_read_loop, vary_rt_packet);
    bench_rt(ctx, "bench_ints", 40000000, pin_rt_ints(),
        rt_once_write_ints, rt_once_read_ints, rt_bench_ints_write_loop, rt_bench_ints_read_loop, vary_rt_ints);
    bench_rt(ctx, "bench_bits", 48000000, pin_rt_bits(),
        rt_once_write_bits, rt_once_read_bits, rt_bench_bits_write_loop, rt_bench_bits_read_loop, vary_rt_bits);
    bench_rt(ctx, "bench_mixed", 40000000, pin_rt_mixed(),
        rt_once_write_mixed, rt_once_read_mixed, rt_bench_mixed_write_loop, rt_bench_mixed_read_loop, vary_rt_mixed);
}

// ------------------------------------------------------------------------------------------
// family bits (§1.4)
// ------------------------------------------------------------------------------------------

const BITS_NUM_WIDTHS: usize = 16;
const BITS_WIDTHS: [u32; BITS_NUM_WIDTHS] = [1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22]; // 227 bits/group
const BITS_BUFFER_SIZE: usize = 65536;

fn bits_mask(width: u32) -> u32 {
    if width == 32 {
        0xFFFFFFFF
    } else {
        (1u32 << width) - 1
    }
}

// the per-pass value variation: one LCG step per pass, values from its bits
fn vary_bits_values(values: &mut [u32; BITS_NUM_WIDTHS], rng: u64) {
    for i in 0..BITS_NUM_WIDTHS {
        values[i] = (rng >> i) as u32 & bits_mask(BITS_WIDTHS[i]);
    }
}

// the single untimed write_bits call site (§3.2)
fn bits_write_pass(buffer: &mut [u8], values: &[u32; BITS_NUM_WIDTHS]) -> i64 {
    let mut w = BitWriter::new(buffer);
    while w.bits_available() >= 256 {
        for i in 0..BITS_NUM_WIDTHS {
            w.write_bits(values[i], BITS_WIDTHS[i]);
        }
    }
    w.flush_bits();
    w.bytes_written() as i64
}

// the single untimed read_bits call site (§3.2): the buffer must read back
// exactly the values written — the bits family's refusal gate
fn bits_read_verify(buffer: &[u8], values: &[u32; BITS_NUM_WIDTHS]) -> bool {
    let mut r = BitReader::new(buffer, BITS_BUFFER_SIZE);
    while r.bits_remaining() >= 256 {
        for i in 0..BITS_NUM_WIDTHS {
            if r.read_bits(BITS_WIDTHS[i]) != values[i] {
                return false;
            }
        }
    }
    true
}

#[inline(never)]
fn bitpacker_write_loop(
    ctx: &Ctx,
    passes: i64,
    bytes_per_pass: i64,
    rng: &mut u64,
    values: &mut [u32; BITS_NUM_WIDTHS],
    buffer: &mut [u8],
) -> bool {
    for _ in 0..passes {
        *rng = bench_rng(*rng);
        vary_bits_values(values, *rng);
        let wrote;
        {
            let mut w = BitWriter::new(buffer);
            while w.bits_available() >= 256 {
                for i in 0..BITS_NUM_WIDTHS {
                    w.write_bits(values[i], BITS_WIDTHS[i]);
                }
            }
            w.flush_bits();
            wrote = w.bytes_written() as i64;
        }
        if wrote != bytes_per_pass {
            return false; // the bytes_per_op assertion (§2.7)
        }
        black_box(&*buffer);
        ctx.sink.set(ctx.sink.get().wrapping_add(wrote as u64));
    }
    true
}

#[inline(never)]
fn bitpacker_read_loop(ctx: &Ctx, passes: i64, variants: &[Vec<u8>]) -> bool {
    for pass in 0..passes {
        let mut r = BitReader::new(
            &variants[(pass as usize) & (NUM_VARIANTS - 1)],
            BITS_BUFFER_SIZE,
        );
        let mut sum: u64 = 0;
        while r.bits_remaining() >= 256 {
            for i in 0..BITS_NUM_WIDTHS {
                sum += r.read_bits(BITS_WIDTHS[i]) as u64;
            }
        }
        ctx.sink.set(ctx.sink.get().wrapping_add(sum));
    }
    true
}

pub fn bench_bitpacker(ctx: &Ctx, passes: i64) {
    let mut values = [0u32; BITS_NUM_WIDTHS];
    let mut buffer = vec![0u8; BITS_BUFFER_SIZE];
    let mut variants: Vec<Vec<u8>> = (0..NUM_VARIANTS).map(|_| vec![0u8; BITS_BUFFER_SIZE]).collect();

    let mut rng: u64 = 1;
    let mut bytes_per_pass: i64 = -1;
    for k in 0..NUM_VARIANTS {
        rng = bench_rng(rng);
        vary_bits_values(&mut values, rng);
        let wrote = bits_write_pass(&mut variants[k], &values);
        if bytes_per_pass < 0 {
            bytes_per_pass = wrote;
        }
        if wrote != bytes_per_pass {
            ctx.fail(
                "bitpacker",
                "variation changed bytes/pass — widths are the structure and must stay fixed",
            );
            return;
        }
        if !bits_read_verify(&variants[k], &values) {
            ctx.fail(
                "bitpacker",
                "read-back disagrees with written values — refusing to bench",
            );
            return;
        }
    }

    let mut write_rates = [0.0f64; MAX_NUM_RUNS];
    let mut read_rates = [0.0f64; MAX_NUM_RUNS];

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !bitpacker_write_loop(ctx, passes, bytes_per_pass, &mut rng, &mut values, &mut buffer) {
            ctx.fail("bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            write_rates[run - 1] = passes as f64 / time;
        }
    }

    for run in 0..(ctx.num_runs + 1) {
        let start = Instant::now();
        if !bitpacker_read_loop(ctx, passes, &variants) {
            ctx.fail("bitpacker", "read loop failed");
            return;
        }
        let time = start.elapsed().as_secs_f64();
        if run >= 1 {
            read_rates[run - 1] = passes as f64 / time;
        }
    }

    ctx.report(
        "bitpacker",
        "write",
        passes,
        bytes_per_pass,
        &run_stats(&mut write_rates[..ctx.num_runs]),
        "bits",
    );
    ctx.report(
        "bitpacker",
        "read",
        passes,
        bytes_per_pass,
        &run_stats(&mut read_rates[..ctx.num_runs]),
        "bits",
    );
}
