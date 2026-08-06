# bench/rust — the Rust runner

`src/main.rs` is the Rust port of the C++ reference
(`bench/cpp/bench_main.cpp`): same pinned corpus instances, same `vary_*`
field mappings, same LCG (wrapping mul/add), same batch builder, golden +
round-trip self-checks before any number is produced (a corpus mismatch
REFUSES to bench), warmup + 7 runs + median/min/max/spread, CSV rows with
`lang=rust`. Full contract: `bench/README.md`.

Wiring: `Cargo.toml` path-depends on `../../generated/rust` and the sibling
serialize.rs checkout, exactly like `test/rust`. `run.sh` runs it as
`cargo run --release -- --csv` from this directory (default release
profile: opt-level 3, no LTO).

Escape barrier: `std::hint::black_box` on the written buffer and every
decoded value. Streams borrow their buffers, so per-iteration construction
is free — the C++ shape exactly; the generic driver monomorphizes and
inlines like the C++ template reference. One deliberate API-shape cost:
`read_message` returns the ~2 KB `Message` enum by value, which the batch
read pays per message.
