# bench/rust — the Rust runner

`src/main.rs` is the Rust port of the C++ reference
(`bench/cpp/bench_main.cpp`): the same data-driven driver over the same
committed variant corpus, golden + per-variant round-trip self-checks before
any number is produced (a corpus mismatch REFUSES to bench), warmup + 7 runs
+ median/min/max/spread, CSV rows with `lang=rust`. Full contract:
`bench/README.md`.

`src/bits.rs` adds family bits (BENCH-STANDARD.md §1.4): the 16-width
bitpacker workload — timed loops in `#[inline(never)]` symbols for the
§4.1 verdict.

Wiring: `Cargo.toml` path-depends on `../../generated/bench/rust` and the
sibling serialize.rs checkout. One generated crate, the one this runner
measures; `generated/bench/rust-realworld` keeps its own `make test` compile
gate (`cargo build` in the crate directory). `run.sh` runs it as
`cargo run --release -- --csv` from this directory (default release profile:
opt-level 3, no LTO).

The default profile is a measured choice, not an omission: six
`[profile.release]` variants (thin/fat LTO x codegen-units, paired
interleave x3, M2, 2026-08-07) found no coherent win — mixed ±2..4%
cells, the only large positives on the layout-noise batch row, and
fat+cu1 a stable -14.8% on ship_shallow read. The generated crate's
`#[inline]` surface (schema#5) plus the runtime's (serialize.rs#19)
already deliver the cross-crate inlining that matters, so consumers of
`generated/rust` do not need LTO on this crate's account — choose your
profile on your own whole-program evidence. Full table:
`bench/results/2026-08-07-rust-lto-experiment-arm64-m2.csv` and the
`2026-08-07-rust-emit-levers.md` results doc.

Escape barrier: `std::hint::black_box` on the written buffer and every
decoded value. Streams borrow their buffers, so per-iteration construction
is free — the C++ shape exactly. The generic driver monomorphizes, but it
does NOT inline like the C++ template reference, and the difference is
measured: `#[inline(always)]` on a generated spine is honored only into the
`Fn::call` shim standing between the driver and the spine, and LLVM then
prices that shim against its caller and refuses it, so every generated call
in a timed loop is an out-of-line call entered with an unknown stream
position. clang honors `always_inline` unconditionally, so the C and C++
legs never see that regime. Rebuilding this leg with
`RUSTFLAGS="-C llvm-args=--inline-threshold=5000"` — a diagnostic, not a
shipped flag — moves the generated rows 2.3x on the same binary. Equalizing that discipline is a named open item on
issue #170; until it is ruled, every rust row here is measured out of line.
