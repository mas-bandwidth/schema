# C packet wide strings: Studio reproduction, 2026-09-07 UTC

The C reproduction rows required by `bench/LOCK` passed for PR #647 at schema
`d8e80c0907f80bc9a7b90bb9b15ba8ba89d3c1a3`: `bench_mixed` write and round_trip,
plus bitpacker write/read, with seven measured runs per row and corpus
`6b213fbfa1a03a99`. The runner's golden self-check passed. Maximum row spread
was 1.59%.

The sitting used the shared Mac Studio M3 Ultra, Darwin 25.6.0, Apple Clang 21,
32 CPU cores and 512 GiB memory, without CPU pinning. Build and run occupied
2026-09-07T04:13:11.795183+00:00 through 2026-09-07T04:13:30.140912+00:00.
Automatic one-second snapshots recorded load starting at { 14.38 12.22 11.46 }
and ending at { 12.96 12.05 11.42 }; maximum one-minute load was 14.38. Every
sampled swap reading was zero. No other benchmark executable was observed before
the sitting or in its snapshots. The two C review sittings ran sequentially, and
this task ran no concurrent compiler outside the benchmark's own build and
provenance checks. Other activity was possible: the snapshot count of
non-benchmark processes over 5% CPU reached 32, including build and provenance
processes.

These are diagnostic reproductions, not quiet-box certification or a
before/after performance comparison. No cross-language ratio is claimed; inline
status remains `unknown`. A release's quiet-box gate remains separate.

The wide-string emitter changes are additive for this benchmark:
generated/bench/c is byte-identical to the parent 5ebc1405. The C benchmark
harness, bitpacker source and C++ emitter and generated benchmark sources are
also unchanged. The separate packet-wide fixture exercises the new wire path.

The C runtime is serialize.c v1.9.2 at `ddea231`; the C++ runtime checkout is
serialize v1.16.2 at `93b8ea2`. The CSV records flags and build-verified runtime
provenance. No C++ reproduction is owed by these C-only changes.

```sh
GOMAXPROCS=2 BENCH_NOISE="Shared Mac Studio M3 Ultra; unpinned; no concurrent Stella compiler or benchmark; other account activity possible; diagnostic reproduction only" \
  bench/run.sh --only c --out bench/results/2026-09-07-c-wide-c-studio.csv
```

The adjacent `2026-09-07-c-wide-studio-sitting.json` retains the automatic
snapshots across the build and run. Process counts include the benchmark build;
they are not a claim that its timed loop was isolated. Generated benchmark
sources were regenerated with the pinned compiler and left no tracked changes.

| File | SHA-256 |
|---|---|
| `internal/codegen/c/fields.go` | `7656da040dd8b28c76990c24992bbe2e6f3ab8866bc5ca06fe6a52610fdc06a9` |
| `internal/codegen/c/c.go` | `e74a3008858cca950823e2ecdc625a0f2fb27b538f6d4308f2fb8e0a0728cd6a` |
| `internal/codegen/c/wstring.go` | `40178d33dea338d62afd0f5265f0f0fbfd51a4e0fd7d1018c92d13ceb534f195` |
| `internal/codegen/cpp/cpp.go` | `b533a0f6ca6a102d1280f7691ee1a1ed4631ea444b7d80f3de84fae4a30ffc7a` |
| `bench/c/bench_main.c` | `dc034fd59a84eb25361926e4b34698ba78216ae6eb4039e179ce19c8e57161a3` |
| `generated/bench/c/BenchWire.h` | `b3a58b260a18e5cc4d67081f920b86938addb39a72d4a81debd512c3e3a2b566` |
| `generated/bench/cpp/BenchWire.h` | `94ee0471aea189c8e6b35a96082e745aab80befca7f8e3df8e7f8925decb0601` |
| `2026-09-07-c-wide-c-studio.csv` | `805a995f8e370a81460c8646e4cf36729a521fab20fa47fc50e5603e442bed81` |
| `2026-09-07-c-wide-studio-sitting.json` | `e4358e07646b11e8c8801d112f1e83f2d99d0fa858d106b7275c05c8948ad34b` |
