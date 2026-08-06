# bench/rust — STUB: the Rust runner lands with the serialize.rs port

Entry point `run.sh` expects: `bench/rust/Cargo.toml` + `src/main.rs`
(manifest wired like `test/rust/Cargo.toml` — path dependencies on
`../../generated/rust` and the sibling serialize.rs checkout). Run as
`cargo run --release -- --csv`.

Port the C++ reference exactly (`bench/cpp/bench_main.cpp`): the `pin_*`
instances, the `vary_*` field mappings, the LCG (wrapping mul/add), the
batch builder, the golden + round-trip self-checks, warmup + 7 runs +
median/min/max/spread, and the CSV row format with `lang=rust`. Use
`std::hint::black_box` as the escape barrier. Full contract:
`bench/README.md`.
