# Dart: the fixed read against its own packet read — certified, both lanes FAIL

Card `dart-fixed-read-beats-its-packet-read` (attempt 5), roadmap tasks
`gates/packet-read/dart/identity` and `gates/packet-read/dart/plan`. These are the CERTIFIED rows:
two quiet-window sittings of the landed driver's read arm (`-mode plan-read`, reached through
`bench/paired/nine.sh --read-arm --leg dart --pin 5`) on a revision pushed to origin. Both GATE
verdicts are **FAIL** in both sittings, so neither roadmap task is flipped; they stay `:unknown`.

The older rows one directory up (`../RESULT.md`, `../board.txt`, `../sitting-1/`, `../sitting-2/`)
are kept unchanged as historical, UNCREDITED evidence: a fast diagnostic of attempt 3 measured on an
unpushed scratch overlay (`e1d63663…`, unreachable) and relabelled by attempt 4 (`80ff4e10`). Nothing
here relies on them, and they credit nothing.

## The Dart line of the board

| sitting | identity read | plan read | packet read | GATE identity | GATE plan | plan/identity (reported) | window | control delta |
|---|---:|---:|---:|---|---|---:|---|---:|
| 1 | 546.9 | 1688.7 | 247.3 | FAIL | FAIL | 3.09 | OK | 0.56% |
| 2 | 547.8 | 1693.7 | 247.4 | FAIL | FAIL | 3.09 | OK | 0.83% |

ns/record, each row's BEST round of 7 (BENCH-STANDARD §2.2); GATE compares best against best, per
lane, as the driver printed it. Spread of rates (best/median/worst in each `sitting-N/READ.md`):

| row | sitting 1 spread | sitting 2 spread |
|---|---:|---:|
| `bench_fixed_read` (identity lane, family table) | 1.7% | 2.3% |
| `bench_plan_read` (plan lane, lineage file) | 3.5% | 3.0% |
| `bench_mixed_read` (packet read, family gen) | 2.9% | 5.0% |

Identity is 2.21x the packet read and plan is 6.83x it; the two sittings agree within 0.3%.

## Declared conditions

- **Revision:** `86bcee7116cf0eea4e6b8667bf3181c52467ac22` on
  `sprint/dart-fixed-read-beats-its-packet-read.w5.g1.e21`, pushed to origin before either sitting.
  Its source tree was clean; the board names the files the build itself regenerated under
  `generated/` (the committed copies on `sprint/schema-dart` lag the emitter):
  `generated/bench/dart/Bench.dart`, `generated/bench/paired/cpp/BenchTable.h`,
  `generated/bench/paired/dart/{BenchFixed,FixedTableFixed,BenchWire}.dart`. The runner the
  binaries were compiled from is this revision's `bench/tables/dart/table_main.dart` (landed in
  `85d9825fd`); `sitting-N/build.json` records each binary's and module's hash.
- **Host:** `b1` (`amd-1`), linux/amd64, AMD Ryzen 7 255 w/ Radeon 780M Graphics; every runner pinned
  to CPU 5 (`taskset -c 5`); sittings back to back, windows 18:56:41–18:59:05Z and 18:59:30–19:01:55Z, 2026-10-16.
- **Toolchain:** Dart SDK 3.13.2 (stable), `dart compile exe` (AOT, product mode, asserts off); the
  plan binary compiled against the paired unit generated beside `bench/tables/dart/lineage/schema.lock`.
  Go 1.26.6 for the driver.
- **Corpus:** one invocation, one id for all three lanes: `0930f96c8320a75d`
  (`bench/paired/corpus/bench_fixed.{bin,layout}`, `bench_table.{bin,len}`,
  `testdata/wire/bench_mixed.bin`, `bench/corpus/variants/bench_mixed.{bin,len}`,
  `bench/tables/dart/lineage/{bench_lineage.bin,bench_lineage.layout}`); the same sixty-four logical
  records on both wires (`paired corpus verified: 64 identical logical records`).
- **Iterations:** 4,000,000 records per sample per wire, 1 discarded warmup + 1 measured run per
  round, 7 rounds, wire order rotated per round; every sample is above the 200 ms floor.
- **Window:** `OK` both sittings (`sitting-N/window.json.gz`, the driver's own 2 s process/load
  samples, gzip-compressed for size; load1 0.34–0.95 and 0.73–1.08), bracketed by the `table-dart`
  control (`control-{start,end}-table.csv`), delta 0.56% and 0.83% against the 5% bound.
- **Pairing:** done by the driver: one `--read-arm` invocation per round times identity, plan and
  `bench_mixed_read` (bench/dart's generated packet read of the same records) in the same process
  and pin. No row is derived from a round trip or a write.

## Why FAIL, and the profile that would find the cause

The cause analysis in `../RESULT.md` (the uncredited diagnostic) still matches these numbers within
1%: identity pays the `tableFixedRun` copy and Int128 clamp work per record, and the plan lane pays
the per-entry `TableFixedPlanEntry` dispatch. A fix belongs to the Dart table emitter
(`internal/codegen/darttable/fixedform.go`), outside this measurement card. The profile that confirms
it on this revision:

```sh
perf record -e cycles:pp -g -- taskset -c 5 build/paired/plan-dart \
  --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus \
  --read-arm --round 0 --iterations 4000000 \
  --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants \
  --lineage-dir bench/tables/dart/lineage
perf report --stdio --no-children
```

or a Dart VM service CPU profile (`dart run --observe … bench/tables/dart/table_main.dart …`) for
symbolised frames.

## Files

`board.txt` (both boards), `sitting-{1,2}/READ.md`, `read.json`, `build.json`,
`round-{0..6}-dart-read.csv`, `control-{start,end}-table.csv`, `window.json.gz`.
