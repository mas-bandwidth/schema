# C packet defaults: Studio reproduction, 2026-09-07 UTC

The C and C++ reproduction rows required by `bench/LOCK` passed at schema
`f26c0800`: `bench_mixed` write and round_trip, plus bitpacker write/read,
with seven measured runs per row and corpus `6b213fbfa1a03a99`.

The sitting used the shared Mac Studio M3 Ultra, Darwin 25.6.0, Apple Clang
21, 32 CPU cores and 512 GiB memory, without CPU pinning. C++ began at
00:28:44 UTC with one-minute load 5.01; C followed at 00:29:00 with load
4.87. Both start checks reported zero swap. No other benchmark executable
was observed before the sitting, and no other compiler or benchmark from
this task ran concurrently. Other account activity was possible.

These are diagnostic reproductions, not quiet-box certification or a
before/after performance comparison. The existing generated benchmark
sources are unchanged against main `a0f32edf`. The new defaults and wide
flags paths are exercised by the separate packet-default runtime fixture.

The runtimes are serialize v1.16.2 at `93b8ea2` and serialize.c v1.9.2 at
`ddea231`. The CSVs retain flags, runtime provenance, rates and spreads.

```sh
bench/run.sh --only cpp --out bench/results/2026-09-07-c-defaults-cpp-studio.csv
bench/run.sh --only c --out bench/results/2026-09-07-c-defaults-c-studio.csv
```

Both commands used `GOMAXPROCS=2` and the shared-machine `BENCH_NOISE`
description recorded in the CSVs.

| File | SHA-256 |
|---|---|
| `internal/codegen/c/dispatch.go` | `09f26905ef053f0dc903db6da9bc34a1063720f47995e6209d3338894811f2f7` |
| `internal/codegen/c/fields.go` | `9bc7d7fbc231c8dd8cfb4b2a93cb6040453548afb6818108e3fef35f05c6f8bb` |
| `internal/codegen/cpp/cpp.go` | `b533a0f6ca6a102d1280f7691ee1a1ed4631ea444b7d80f3de84fae4a30ffc7a` |
| `generated/bench/c/BenchWire.h` | `0c34ed6616ff9c8bbe0915c93db41f356f96b72b93a761f0a67b98a8d2712de6` |
| `generated/bench/cpp/BenchWire.h` | `94ee0471aea189c8e6b35a96082e745aab80befca7f8e3df8e7f8925decb0601` |
