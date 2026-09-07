# C packet UTF-8 reads: Studio reproduction, 2026-09-07 UTC

The C reproduction rows required by `bench/LOCK` passed for PR #639 at schema
`383d7affcff2cc0481e73d7fa3d5388096a0433e`: `bench_mixed` write and round_trip,
plus bitpacker write/read, with seven measured runs per row and corpus
`6b213fbfa1a03a99`. The runner's golden self-check passed. Maximum row spread
was 1.51%.

The sitting used the shared Mac Studio M3 Ultra, Darwin 25.6.0, Apple Clang 21,
32 CPU cores and 512 GiB memory, without CPU pinning. Build and run occupied
2026-09-07T04:12:52.407197+00:00 through 2026-09-07T04:13:11.709750+00:00.
Automatic one-second snapshots recorded load starting at { 17.00 12.52 11.55 }
and ending at { 14.38 12.22 11.46 }; maximum one-minute load was 17.00. Every
sampled swap reading was zero. No other benchmark executable was observed before
the sitting or in its snapshots. The two C review sittings ran sequentially, and
this task ran no concurrent compiler outside the benchmark's own build and
provenance checks. Other activity was possible: the snapshot count of
non-benchmark processes over 5% CPU reached 43, including build and provenance
processes.

These are diagnostic reproductions, not quiet-box certification or a
before/after performance comparison. No cross-language ratio is claimed; inline
status remains `unknown`. A release's quiet-box gate remains separate.

The C change moves UTF-8 validation onto reads in the generated benchmark
header. The C benchmark harness, bitpacker source and C++ emitter and generated
benchmark sources are unchanged against the parent 87e5b8ec.

The C runtime is serialize.c v1.9.2 at `ddea231`; the C++ runtime checkout is
serialize v1.16.2 at `93b8ea2`. The CSV records flags and build-verified runtime
provenance. No C++ reproduction is owed by these C-only changes.

```sh
GOMAXPROCS=2 BENCH_NOISE="Shared Mac Studio M3 Ultra; unpinned; no concurrent Stella compiler or benchmark; other account activity possible; diagnostic reproduction only" \
  bench/run.sh --only c --out bench/results/2026-09-07-c-utf8-c-studio.csv
```

The adjacent `2026-09-07-c-utf8-studio-sitting.json` retains the automatic
snapshots across the build and run. Process counts include the benchmark build;
they are not a claim that its timed loop was isolated. Generated benchmark
sources were regenerated with the pinned compiler and left no tracked changes.

| File | SHA-256 |
|---|---|
| `internal/codegen/c/fields.go` | `46b2a4e5578e69bd1b3a84becb4bab336461f3b03751b371074bc02c10b2531f` |
| `internal/codegen/cpp/cpp.go` | `b533a0f6ca6a102d1280f7691ee1a1ed4631ea444b7d80f3de84fae4a30ffc7a` |
| `bench/c/bench_main.c` | `dc034fd59a84eb25361926e4b34698ba78216ae6eb4039e179ce19c8e57161a3` |
| `generated/bench/c/BenchWire.h` | `b3a58b260a18e5cc4d67081f920b86938addb39a72d4a81debd512c3e3a2b566` |
| `generated/bench/cpp/BenchWire.h` | `94ee0471aea189c8e6b35a96082e745aab80befca7f8e3df8e7f8925decb0601` |
| `2026-09-07-c-utf8-c-studio.csv` | `1322a3ca3db10de67419071b149625a6aefadf9654e2d97ca1a524e4f09a4adb` |
| `2026-09-07-c-utf8-studio-sitting.json` | `b689c90a3844d97ce289ec86ddf61bc3b34b5daa1b12b6216d93e2efc4526ed8` |
