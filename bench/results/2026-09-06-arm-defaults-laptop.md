# Packet arm defaults: laptop reproduction, 2026-09-06

These are the C/C++ reproduction rows required by `bench/LOCK`: `bench_mixed` write and round_trip, plus the raw bitpacker write/read control. Both ordinary Release runs passed their wire oracle with corpus id `6b213fbfa1a03a99`. Each uses seven measured runs; neither uses `--quick`.

The sitting was the shared LA laptop (Apple M2, Apple clang 21, no pinning). One-minute load was 2.51 before C++ and 2.49 before C, with zero swap. Stella ran no other compiler jobs during these runs; Rowan could be working. The CSVs record runtime commits, compiler flags, rates and spread. This is a diagnostic reproduction, not a quiet-box certification or a before/after performance comparison.

Commands, with single-worker runtime environment settings and the sitting description in `BENCH_NOISE`:

```sh
bench/run.sh --only cpp --out bench/results/2026-09-06-arm-defaults-cpp-laptop.csv
bench/run.sh --only c --out bench/results/2026-09-06-arm-defaults-c-laptop.csv
```

The recorded checkout was `c8d0df58-dirty` on `codex/packet-arm-defaults`. These SHA-256 values identify the emitter and measured generated wire sources in that working tree:

| File | SHA-256 |
|---|---|
| `internal/codegen/c/c.go` | `2158a2be6f5a0b1eb47c0efde75c173a925fce2a6685939e37add126e9218aa1` |
| `internal/codegen/cpp/cpp.go` | `72b9245709c92c08a4cc79edcd5b58999c175aaf1e3ed8d41988386591da3541` |
| `internal/codegen/cpp/functions.go` | `af14247379fa7b39e6fa6beecc508f5e467b90c04c37af91074aad7c38716a24` |
| `generated/bench/c/BenchWire.h` | `0c34ed6616ff9c8bbe0915c93db41f356f96b72b93a761f0a67b98a8d2712de6` |
| `generated/bench/cpp/BenchWire.h` | `94ee0471aea189c8e6b35a96082e745aab80befca7f8e3df8e7f8925decb0601` |
