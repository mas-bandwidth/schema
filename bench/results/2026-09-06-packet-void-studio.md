# Payload-free packet arms: Studio reproduction, 2026-09-06

These are the C/C++ reproduction rows required by `bench/LOCK` for #503:
`bench_mixed` write and round_trip, plus bitpacker write/read. Both Release
runs passed the wire oracle with corpus id `6b213fbfa1a03a99`. Each has seven
measured runs and uses the ordinary instrument, without `--quick`.

The sitting was the shared Mac Studio M3 Ultra, Darwin 25.6.0, Apple Clang 21,
32 CPU cores and 512 GiB memory, without CPU pinning. C++ began at 19:20:04 EDT
with one-minute load 10.48; C followed at 19:20:21 with load 9.57. Both start
checks reported zero swap. No other benchmark process was observed before the
sitting, and no other compiler or benchmark from this task ran concurrently.
Other account activity was possible. This is a diagnostic reproduction, not
quiet-box certification or a before/after performance comparison.

The measured schema revision is `3c5ba2be`, integrating main `3d511f5a` into
`codex/packet-void-arms`. The C++ runtime is v1.16.2 at `93b8ea2`; C is v1.9.2
at `ddea231`. The CSVs retain build flags, runtime path verification, rates
and spreads. The existing generated benchmark wire sources are unchanged
against main; this feature is exercised by the separate packet-void runtime
oracles and direction-specific negative controls.

```sh
bench/run.sh --only cpp --out bench/results/2026-09-06-packet-void-cpp-studio.csv
bench/run.sh --only c --out bench/results/2026-09-06-packet-void-c-studio.csv
```

Both commands used `GOMAXPROCS=2` and the shared-machine description recorded
in each CSV's `BENCH_NOISE` preamble.

| File | SHA-256 |
|---|---|
| `internal/codegen/c/c.go` | `88fcd394beb2fe8360f0be1f04ff11062519ff451561480f820f39453b35e383` |
| `internal/codegen/cpp/cpp.go` | `cfd202a994885f533ca540c003aa93af5e815491c6ff3ddfe66eca738f8132be` |
| `internal/codegen/cpp/functions.go` | `446bda812d065ce5d86a9bc540ed328cf5982fe1485b6d0b8208489ab57d64c6` |
| `generated/bench/c/BenchWire.h` | `0c34ed6616ff9c8bbe0915c93db41f356f96b72b93a761f0a67b98a8d2712de6` |
| `generated/bench/cpp/BenchWire.h` | `94ee0471aea189c8e6b35a96082e745aab80befca7f8e3df8e7f8925decb0601` |
