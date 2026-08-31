// schema bench — family bits for the Rust runner.
//
// Family bits (BENCH-STANDARD.md §1.4): the raw BitWriter/BitReader with the
// 16-width table (227 bits/group) over a 65536-byte buffer. Values vary per
// pass through the LCG (widths are the structure and stay fixed; bytes/pass
// asserted constant); reads rotate 64 pre-written variant buffers, each
// verified to read back exactly what was written before any number is
// produced. No `pub` beyond the driver entry point per §3.1; the timed loops
// are #[inline(never)] so the §4.1 verdict has a loop body to count.

use std::hint::black_box;
use std::time::Instant;

use serialize::{BitReader, BitWriter};

use crate::{Ctx, MAX_NUM_RUNS, NUM_VARIANTS, bench_rng, run_stats};

const BITS_NUM_WIDTHS: usize = 16;
const BITS_WIDTHS: [u32; BITS_NUM_WIDTHS] =
    [1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22]; // 227 bits/group
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
    let mut variants: Vec<Vec<u8>> = (0..NUM_VARIANTS)
        .map(|_| vec![0u8; BITS_BUFFER_SIZE])
        .collect();

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
        if !bitpacker_write_loop(
            ctx,
            passes,
            bytes_per_pass,
            &mut rng,
            &mut values,
            &mut buffer,
        ) {
            ctx.fail(
                "bitpacker",
                "bytes/pass changed in the timed loop (§2.7 assertion)",
            );
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
