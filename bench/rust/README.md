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
inlines like the C++ template reference. The batch read hoists ONE reused
`Message` and fills it through `read_message_into` — the Rust shape of the
go/cs runners' hoisted `MessageStorage` and the C++ runner's reused
`Message` (`read_message`'s by-value return copies the ~2 KB enum out of
the call per message; measured 2.6x apart on the M2, schema#5). The
self-check byte-verifies BOTH dispatch read surfaces against the
`message_stream` golden before any number is produced.
