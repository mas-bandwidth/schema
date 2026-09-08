# Fixed-table baseline — September 8, 2026

All four languages passed the shared corpus gate and seven interleaved rounds.
Every measured row's spread is below 9%, within the normal 15% threshold.
This is a baseline for finding each language's fastest correct implementation;
no implementation is certified fastest by this comparison.

| Language | Path | Best µs/op | Median µs/op | Best MiB/s | Median MiB/s | Spread |
|---|---|---:|---:|---:|---:|---:|
| C | write | 3.736 | 3.760 | 548.10 | 544.57 | 6.18% |
| C | round_trip | 5.868 | 5.939 | 348.95 | 344.79 | 2.72% |
| C++ | write | 0.980 | 0.987 | 2089.32 | 2075.44 | 3.96% |
| C++ | round_trip | 3.045 | 3.178 | 672.34 | 644.19 | 6.16% |
| C# | write | 27.117 | 27.261 | 75.51 | 75.11 | 1.46% |
| C# | round_trip | 38.536 | 38.760 | 53.13 | 52.83 | 1.50% |
| Go | write | 6.508 | 6.689 | 314.64 | 306.11 | 8.96% |
| Go | round_trip | 10.875 | 10.946 | 188.29 | 187.06 | 6.96% |

Lower time is better. `round_trip` is explicit reset, load, then re-save of the
loaded value; it is not a direct read measurement. MiB/s counts one 2,147-byte
record per operation, including for the round trip. It does not count that
record twice. The headline is the best rate over seven samples; medians remain
beside it. There are no cross-machine, cross-corpus or cross-language ratios.

[Original CSV](../2026-09-08-fixed-initial-arm64-studio.csv), individual
`round-0.csv` through `round-6.csv`, and [load snapshots](load.json) preserve the
measurement and its variation. Reaggregating the round files reproduces the
original eight rows exactly.

## Workload and sitting

- Schema: `bench/corpus/BenchTable.schema`, generated fixed TableMixed file-form
  codecs for C, C++, C# and Go. 64 rotating records; 2,147 bytes each; corpus ID
  `b51387f36d9b59c4`. The producer reconstructs and byte-compares all records.
- TableMixed is the existing representative counterpart of packet BenchMixed:
  eight entities, eighty statistic records, event union, bounded text/payload,
  and mixed scalars. It retains historical fixed-point-to-compressed-float and
  128-to-64-bit substitutions and framing/default choices. It is not an exact
  copy of every packet scalar. See [corpus documentation](../../README.md).
- 400,000 operations per measured path, per language, per round; one discarded
  warmup per path each round. Round is outermost, then C/C++/C#/Go. Every
  runner verifies the golden and all variant round trips before its clocks.
  C# uses managed authoring values and reused output storage.
- Measured revision: `9b7a05cf971f53b58b8fb568d08dcea6b9b9b364`, including C#
  repair #752. The driver subsequently merged in #754 as `e04920be`. C message
  repair #753 merged during the sitting and is not silently included in the
  measured revision. The four fixed-file golden gates passed before timing.
- Apple M3 Ultra, 32 CPU cores, arm64; Darwin 25.6.0. No affinity. Other local
  heavy work was coordinated to stop; OS and desktop activity remained.
  Observed 1-minute load range: 6.52–8.57. The load file contains boundary
  observations, not continuous monitoring, and counts processes above 5% CPU
  without treating those counts as precise core occupancy.
- Clang 21.0.0, `-O3 -DNDEBUG -ffp-contract=off`, C99/C++17; C++ `-fno-rtti`.
  Go 1.27.1, normal optimized build. .NET SDK 10.0.400, default runtime tiering.
  Complete flags are in the CSV. No optimization setting changed mid-pass.
- Timing pass: 21:13:05–21:22:20 UTC. Each measured sample exceeds 200 ms.
  C/C++ runtime-independent table codecs; Go/C# table paths likewise do not
  call the packet runtime bundled beside their generated packet types.
- Build dependency tags: serialize v1.16.2 (`93b8ea2a`), serialize.go v1.15.1
  (`963f6dfe`), serialize.cs v1.9.1 (`1bf2b19f`). The raw CSV labels serialize.cs
  dirty. It is preserved as recorded. A subsequent status/diff was clean and
  hashes of both compiled runtime sources matched HEAD; the driver's stat-based
  `diff-index` detector is replaced by content-aware `git diff` in this follow-up.
  This does not retroactively rewrite the recorded marker.

This local table pass implements the table protocol's golden, interleaving and
spread checks. It is not a full packet benchmark window certificate: packet
control/twin legs and an inline-codegen verdict were not collected. `inline`
remains `unknown`. Broader wire acceptance and merged-main CI are separate
receipts. No server or production-runtime claim follows from these local values.

## Separate diagnostics

These ran after the headline pass; their timings do not contribute to the table.
The same generated corpus was checked. They suggest where to investigate, and
do not establish an optimization gain.

| Language | Evidence | Next investigation |
|---|---|---|
| C | Two-second, 1 ms `sample` of the release binary's write phase: leading top-of-stack counts were `table_writer_id` 721, nested stats walk 406 and entities walk 393. | ID handling and repeated nested size/write work; inspect codegen before proposing changes. |
| C++ | Separate two-second sample crosses save/load: inlined root save 696, root load 544, entity measurement 179 and LEB read 178. | A longer path-specific profile before choosing an optimization; inlining limits this sample's attribution. |
| C# | 10,000 calls per path after 1,000 warmup calls: **0 managed B/op** via `GC.GetAllocatedBytesForCurrentThread`, both paths. Source has collect/measure/write passes, descriptor callbacks and linear ID lookup. | Capture a managed CPU profile, then evaluate those source candidates. No managed CPU sample was collected in this pass. |
| Go | 10,000 calls via `testing.AllocsPerRun`: **0 allocations/op**, both paths (the helper sets GOMAXPROCS=1). Separate 14.22-second CPU profile, 12.82 seconds sampled: `Advance` 23.01% flat, `TableIds.Ref` 10.14%, `PutLeb` 10.14%; root save 74.88% cumulative. | Writer cursor/varint/ID work, including the speculative measure path. Heap allocation is not the leading candidate for this workload. |

Go diagnostics used a separate copy of the shape-blind runner with a Go test
wrapper and `go test -cpuprofile`, leaving the measured binary unchanged. C#
diagnostics used the same generated sources and generic callbacks in a separate
release program, with thread allocation counters outside 10,000-call loops.
No native allocation counter was collected for C or C++. Allocation findings
cover these successful warmed-up paths, not every API, input or lifecycle stage.

Any candidate optimization must preserve the shared golden and relevant
conformance checks and earn its gain in paired same-sitting measurements.
The representative variable-table corpus is the next workload; message, block,
and cooked save/load are later profiles with their own corpus identities.

## Reproduce

From the repository root with the pinned sibling runtime checkouts:

```sh
make -j4 bench-table-check generated/bench/tables/c/.stamp generated/bench/tables/cs/.stamp generated/bench/tables/go/.stamp
bench/tables/run.sh --only c,cpp,cs,go --gate --bare
BENCH_NOISE='describe this machine and its actual load' bench/tables/run.sh --only c,cpp,cs,go --rounds 7 --tag fixed
```

Reconstruct the stored statistics without running codecs or clocks:

```sh
go run ./bench/tools aggregate bench/tables/results/2026-09-08-fixed-initial/round-*.csv
```
