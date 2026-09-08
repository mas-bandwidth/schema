# C# vocabulary index evidence

The vocabulary index reduces median Fixed Table round-trip cost from
38.559 to 22.642 microseconds: **58.72% of before**, a 41.28% cost reduction.
The seven pairs all come from the accepted September 8 sitting, 22:42:11 to
22:51:35 UTC. No earlier attempted pass contributes a row.

| Operation | Before median µs | Indexed median µs | Indexed cost | Before spread | Indexed spread |
|---|---:|---:|---:|---:|---:|
| Write | 27.186 | 11.338 | 41.70% | 2.56% | 2.94% |
| Round trip | 38.559 | 22.642 | 58.72% | 0.89% | 2.55% |

For each operation and implementation, cost is `1,000,000 / msgs_per_sec`.
The table uses the median of seven costs; spread is
`100 × (maximum cost − minimum cost) / median cost`. Indexed cost is
`100 × indexed median / before median`. The CSV rates are rounded to whole
messages per second, so derived costs have that precision limit.
The [summary](summary.json) also retains the best cost per operation.

## Problem and prediction

Collecting, measuring and writing the table repeatedly resolves vocabulary
references. Each resolution previously scanned the collected IDs linearly.
The implementation adds a hash index into the existing, ordered vocabulary;
it preserves first-use order and therefore the wire bytes. Extra stack
storage is bounded at 8 KiB. Caller vocabulary capacities above 1,024 keep
the existing linear lookup path without a per-save heap allocation.

A diagnostic profile before the change attributed 42.64% of exclusive sampled
time to `BodySize`, 15.23% to `WriteBody`, and 13.70% to `Collect`; `Save`
accounted for 82.94% cumulatively. These are sampled managed thread stacks,
including inlined work, not isolated lookup timings.

The prediction recorded at **2026-09-08 21:52:21 UTC**, before edits and
measurement, was unchanged bytes and allocation behavior, and materially
lower `BodySize`/`Collect` cost if inlined reference scans explained the
attribution. Acceptance required a paired gain exceeding noise. No numerical
gain was predicted. The end-to-end cost reduction exceeds the observed
spreads; no new profile was captured to establish the post-change attribution
inside `BodySize` or `Collect`.

## Workload and sitting

- Before: `46052fc9d6b7ba8fcc2abf4279c54414667f7e4a`.
- Indexed implementation: `eb3a9f7be2989ebb1fe05ee17bc975e7342a0c5d`.
- Original `BenchTable.schema`, 64 distinct records of 2,147 bytes each;
  corpus ID `b51387f36d9b59c4`. The first record is the wire golden.
  Schema, both corpus files, runner and project are byte-identical between
  the two revisions.
- Seven paired rounds, alternating before/indexed then indexed/before.
  Each invocation starts a fresh process, checks all 64 records, and performs
  one warmup plus one measured run of 400,000 operations for each path.
  The shortest measured sample is 4.493 seconds, above the 200 ms floor.
- Write saves preloaded records. Round trip resets reused storage, loads a
  record and re-saves it. Reset and the resulting save remain inside the
  measured work. No read-only timing is claimed.
- Release, `net10.0`, .NET SDK 10.0.400, runtime-default tiering; no affinity.
  Apple M3 Ultra, arm64, Darwin 25.6.0. The team coordinated one benchmark
  at a time. [Load observations](load.json) are retained; load averages are
  context, not proof of an idle machine.

The accepted driver recorded DLL SHA-256 values before the first round.
Both frozen DLLs still match those values. Companion dependency and runtime
configuration files also match each other; their hashes were added during
evidence export, not captured by the timing driver. The exact runtime patch
was not recorded during measurement. CPU and macOS product/build details
were checked during evidence export on the same host.

## Correctness and reproduction

The implementation's existing checks passed: C# table tests in Debug and
Release; compiler and generated golden checks; an independent vocabulary
fixture at capacities 141, 1,024 and 1,025, including unchanged first-use bytes
and insufficient-capacity refusal; and 162,842 wire-fuzz mutants with zero
divergences. A separate warmed diagnostic measured zero managed bytes per
operation for write and reset/load/re-save. Each of the 14 accepted runner
invocations also passed its 64-record golden gate before timing.

The 14 `round-*.csv` files are unchanged raw output: 28 data rows, covering
both operations in every pair. [Metadata](metadata.json) records the sitting;
[provenance](provenance.json) records the input and binary hashes, prediction,
and private gate-receipt hashes. The original metadata and invalidated earlier
attempt remain in ignored local build output. The published metadata replaces
only the local `DOTNET_CLI_HOME` path with `<TASK_CACHE>/dotnet`, explicitly
marked as normalization; it does not change a measurement or runtime policy.

From this repository, verify the artifact without running a benchmark:

```sh
python3 bench/tables/results/2026-09-08-csharp-vocabulary-index-arm64/verify.py
```

Where the two original `build/csharp-before` and `build/csharp-hash` directories
are retained, add `--binary-root build` to verify their DLL and companion
hashes too. The verifier checks every row, pair, identity field, duration,
spread, aggregate and corpus byte hash. It reconstructs the corpus ID from
**both** `bench_table.bin` and the complete `bench_table.variants.bin`.

This is one local C# before/after result on the retained Fixed Table corpus.
It does not update the cross-language board, establish packet-relative cost,
measure the matched packet/table corpus, or establish performance for
message, variable-table, block or cooked forms.
