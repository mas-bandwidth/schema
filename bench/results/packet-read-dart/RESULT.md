# Dart: the fixed read against its own packet read — both lanes FAIL (fast diagnostic, uncertified)

> **Superseded for credit by `certified/RESULT.md` (attempt 5).** The rows on this page stay as
> historical, UNCREDITED diagnostic evidence, unchanged below. The certified sittings were measured
> on pushed revision `86bcee711` with the runner support landed in `85d9825fd`, and agree with these
> numbers: both lanes FAIL.

> **Status of these rows: FAST DIAGNOSTIC, UNCERTIFIED.** Per the card's TOOLCHAIN line, a sitting is
> certified only if the landed driver admits Dart to `-mode run` inside the window. It does not on
> this branch: `bench/tables/dart/table_main.dart` here has no `--read-arm`, so
> `bench/paired/nine.sh --read-arm --leg dart --pin CORE` builds `table-dart`, passes `--gate`, and then
> refuses at the pre-clock read-arm gate. Both sittings were measured on an **unpushed scratch
> overlay** (`e1d63663ed5b…`, on driver commit `ea6b5a51541…`; neither is on origin, `git fetch`
> answers "not our ref") that carried out-of-PATHS changes to `bench/tables/dart/` (below). Under
> BENCH-STANDARD.md ("Every commit line records a `-dirty` suffix…"; "its commit stamp must name a
> SHA that exists on origin"), that revision is recorded here as **`-dirty` and UNREACHABLE**, not
> `(clean)`. The numbers are therefore not reproducible from this branch as committed; they become
> reproducible, and a certified re-sitting becomes possible, only once the `PATHS-PROPOSED` runner
> change below lands on `sprint/schema-dart` (its own card). The raw `sitting-*/read.json` and
> `build.json` files are kept verbatim as evidence; their `"revision"` field names the unreachable
> overlay. No roadmap task is credited from these rows.

Card `dart-fixed-read-beats-its-packet-read` (roadmap tasks `gates/packet-read/dart/identity` and
`gates/packet-read/dart/plan`). Both GATE verdicts are **FAIL** (fast diagnostic, uncertified; see the status note above), in two back-to-back sittings with
an `OK` window on `b1` (`amd-1`), so the roadmap tasks stay `:unknown` and are not flipped.

## The Dart line of the board

Each value is the best round of 7, in ns/record (`1000 / max_msgs_per_sec`). The full boards and
every round are in [board.txt](board.txt), [sitting-1/](sitting-1/) and [sitting-2/](sitting-2/).

| sitting | identity read | plan read | packet read | GATE identity | GATE plan | plan/identity (reported) |
|---|---:|---:|---:|---|---|---:|
| 1 | 544.5 | 1673.6 | 247.5 | FAIL | FAIL | 3.07 |
| 2 | 544.4 | 1666.5 | 247.5 | FAIL | FAIL | 3.06 |

Spread is `(max - min) / median`, taken over the rates (§2.3). In sitting 1 it was `0.9%` for
identity, `0.9%` for plan and `1.0%` for packet. In sitting 2 it was `1.9%`, `1.2%` and `1.1%`.
No row is marked noisy or invalid. The driver printed both GATE verdicts (`faster()`, best round
against best round, each lane against Dart's own packet read). This file only copies them.

- **The identity lane is 2.20x slower than the packet read** (`544.4` vs `247.5 ns/record`, `+296.9 ns/record`).
- **The plan lane is 6.73x slower than the packet read** (`1666.5` vs `247.5 ns/record`, `+1419.0 ns/record`).
- **The reported `plan/identity` ratio is 3.06x–3.07x** (`1666.5 / 544.4`).

### Per-sitting summary tables

Sitting 1 ([sitting-1/READ.md](sitting-1/READ.md)):

| row | best ns/record | median | worst | spread (rates) | best M msg/s | best MB/s |
|---|---:|---:|---:|---:|---:|---:|
| dart `bench_fixed_read` (identity lane, family `table`) | 544.5 | 547.5 | 549.2 | 0.9% | 1.836 | 2213.8 |
| dart `bench_plan_read` (plan lane, family `table`) | 1673.6 | 1679.2 | 1687.9 | 0.9% | 0.597 | 566.7 |
| dart `bench_mixed_read` (packet read, family `gen`) | 247.5 | 248.2 | 250.1 | 1.0% | 4.041 | 1688.0 |

Sitting 2 ([sitting-2/READ.md](sitting-2/READ.md)):

| row | best ns/record | median | worst | spread (rates) | best M msg/s | best MB/s |
|---|---:|---:|---:|---:|---:|---:|
| dart `bench_fixed_read` (identity lane, family `table`) | 544.4 | 546.3 | 554.7 | 1.9% | 1.837 | 2214.9 |
| dart `bench_plan_read` (plan lane, family `table`) | 1666.5 | 1681.8 | 1686.2 | 1.2% | 0.600 | 569.1 |
| dart `bench_mixed_read` (packet read, family `gen`) | 247.5 | 247.9 | 250.2 | 1.1% | 4.041 | 1688.0 |

Cross-sitting agreement on best `ns/record`: `0.02%` on `bench_fixed_read` (`544.5` vs `544.4`),
`0.42%` on `bench_plan_read` (`1673.6` vs `1666.5`), and `0.00%` on `bench_mixed_read` (`247.5` vs
`247.5`), with zero verdict flips.

## Declared

- **Host:** `b1` (`amd-1`), an AMD Ryzen 7 255 w/ Radeon 780M Graphics, `linux/amd64`.
- **Pinning:** `taskset -c 6`, recorded as `lane 6` on the board (`bench/paired/nine.sh --read-arm --leg dart --pin 6`).
- **Window:** `OK` in both sittings. The quiet window was enforced by the driver (`beginWindow` in
  `bench/paired/window.go`), sampling `ps -A` every 2 seconds and refusing any competing build or
  bench process.
- **Load1:** `0.71–1.10` in sitting 1 and `1.00–1.04` in sitting 2 (single pinned worker on core 6).
- **Control:** Because Dart is a table-only leg in `bench/paired` (`tableOnlyLanguages`), the
  driver brackets the sitting with `build/paired/table-dart` (`bench_fixed` `write` and
  `round_trip`). Within-sitting control delta was `1.01%` in sitting 1 (`write` `1.3095` to
  `1.2962 M msg/s`, `round_trip` `0.7429` to `0.7376 M msg/s`) and `0.32%` in sitting 2 (`write`
  `1.3125` to `1.3083 M msg/s`, `round_trip` `0.7418` to `0.7396 M msg/s`), well inside the `5%`
  bound. Cross-sitting control delta was `0.23%`.
- **Toolchain & flags:** `Dart SDK version: 3.13.2 (stable) (Tue Aug 25 01:01:12 2026 -0700) on "linux_x64"`,
  compiled ahead-of-time via `dart compile exe` (AOT, product mode: asserts off; paired unit
  generated a second time beside `bench/tables/dart/lineage/schema.lock` into
  `build/paired/plan-dart`).
- **Revision & provenance:**
  - On the committed branch (which stays strictly inside `PATHS`), `bench/tables/dart/table_main.dart`
    is untouched and does not yet carry `--read-arm`. Running
    `bench/paired/nine.sh --read-arm --leg dart --pin 6` against the clean branch builds
    `build/paired/table-dart`, passes `table-dart --gate`, and refuses cleanly at the pre-clock
    read-arm gate (`paired: dart read-arm gate failed or emitted timing rows`).
  - **The recorded revision is `-dirty` and UNREACHABLE, and the sittings are a fast diagnostic, uncertified.**
    The driver stamped `rev e1d63663ed5b6d93b7bb9dd13820d856e1d59b04 (clean)` because the overlay was
    committed locally; that stamp was wrong to publish (BENCH-STANDARD.md, the `-dirty` and
    "SHA that exists on origin" rules), and every board/READ.md preamble now reads
    `rev e1d63663…-dirty (UNREACHABLE …)`. The two sittings ran the driver end to end, but
    against scratch commit `e1d63663ed5b6d93b7bb9dd13820d856e1d59b04` on
    top of driver commit `ea6b5a51541053c5ecbb71a4e6b0ed4c09728da5` (both never pushed; based on
    `origin/sprint/schema-dart` at `36c113a09`), adding `--read-arm` to
    `bench/tables/dart/table_main.dart`, updating `bench/tables/dart/SHAPE-GATE.allow`, and
    carrying `bench/tables/dart/lineage/{VOLD_bench_lineage.schema,schema.lock,bench_lineage.bin,bench_lineage.layout}`
    byte-for-byte from `337aaacf5:bench/tables/rust/lineage/`. The exact diff for
    `bench/tables/dart/table_main.dart` and `bench/tables/dart/SHAPE-GATE.allow` is included
    verbatim under `PATHS-PROPOSED` below.
- **Corpus:** One invocation hashes one union corpus. `corpus_id` was `0930f96c8320a75d` for the
  identity, plan and packet rows alike (covering `bench/paired/corpus/bench_fixed.{bin,layout}`,
  `bench/paired/corpus/bench_table.{bin,len}`, `testdata/wire/bench_mixed.bin`,
  `bench/corpus/variants/bench_mixed.{bin,len}`, and
  `bench/tables/dart/lineage/{bench_lineage.bin,bench_lineage.layout,schema.lock}`), matching C
  (`d2a3fe88b`) and Rust (`ddf4c3179`) byte for byte. The ordinary table pass `corpus_id` was
  `066b2822b3e5d487`.
- **Iterations:** `4,000,000` records per sample per wire (`62,500` outer loops × `64` records),
  1 discarded warmup plus 1 measured run per round across 7 rounds per sitting, rotating wire
  order `(r + idx) % 3` by `--round r`. Every sample takes `~1.0–6.7 s`, well above the `200 ms`
  floor (§2.1).
- **Bytes per record:** `fixed 1264.3 B/record` (`80,915 / 64`), `plan (lineage file) 995.4 B/record`
  (`63,708 / 64`), `packet 438 B/record`.
- **Packet read pairing:** Dart has no standalone paired packet leg in `bench/paired` because
  `bench/dart/main.dart` has no `--gate` or `--iterations`. In `build/paired/plan-dart` (and
  `table-dart --read-arm`), `schema generate --lang dart` generates `Bench.dart` (the packet wire's
  `ReadStream` + `readBenchMixed`, identical to what `bench/dart/main.dart` runs) beside
  `BenchFixed.dart` and `FixedTableFixed.dart`. Inside `--read-arm`, `bench_mixed_read` pre-checks
  all 64 packet variants (`testdata/wire/bench_mixed.bin` + `bench/corpus/variants/bench_mixed.{bin,len}`)
  against `writeBenchMixed` before the clock, and then times `ReadStream(buf, len) + readBenchMixed`
  over the 64 variants in the same process and round rotation as `bench_fixed_read` and
  `bench_plan_read`.
- **Plan lane gates (all before the clock):**
  - `report.hash == 0x0c2ce17509b80494` (`VOLD` lineage hash at lock index 0);
  - `n == 64`, `!report.malformed`, `report.refused == TableFixedRefusal.none`;
  - `report.widened == 4096` (`64` widened `MixedStat.delta` `int16 -> int32` per record) and
    `clamped == 0`, `defaulted == 0`, `truncated == 0`, `unknown == 0`, `kindMismatch == 0`,
    `textIllFormed == 0`;
  - every narrowed/widened field of all 64 records verified against the identity records (including
    `((weapon - 1) % 12) + 1` on `entities[e].weapon`, `statsCount <= 64`, `playerNameLength <= 8`
    with zeroed tail `8..15`, and `idleTicks == 0`);
  - a `0xA5`-poisoned destination saves byte-identical output (`fixedTableFixedSave`) to a clean
    destination, and differs from `bench_fixed.bin`.

## The cause, diagnostics, and the profile that confirms them

The following diagnostic splits were measured on `b1` pinned to core 6 (`taskset -c 6`), 3 rounds
each at `4,000,000` records/round, in an out-of-tree scratch directory with nothing committed:

| diagnostic arm (`b1`, core 6, best of 3 rounds) | `bench_fixed_read` (ns/rec) | `bench_plan_read` (ns/rec) | `bench_mixed_read` (ns/rec) | notes |
|---|---:|---:|---:|---|
| **0. Shipped code (sitting 2, fast diagnostic, uncertified)** | **544.4** (`1.837 M/s`) | **1666.5** (`0.600 M/s`) | **247.5** (`4.041 M/s`) | Two-stage `tableFixedRun` + `fixedTableFixedDecode` |
| **1. `tableFixedRun` only (`fixedTableFixedDecode` skipped after pre-clock gates)** | **141.9** (`7.046 M/s`) | **1355.0** (`0.738 M/s`) | **247.3** (`4.043 M/s`) | Isolates Stage 1 (`plan.image` copy/widen) from Stage 2 (`fixedTableFixedDecode`) |
| **2. `@pragma('vm:prefer-inline')` on `*FixedDecode` + `const Int128` clamp bounds** | **443.1** (`2.257 M/s`) | **1589.8** (`0.629 M/s`) | **247.5** (`4.040 M/s`) | Saves `101.3 ns/rec` (`-18.6%`) on identity and `76.7 ns/rec` on plan |
| **3. Direct `(bytes, view, at + 8)` passed into shared `fixedTableFixedDecode` on identity** | **980.4** (`1.020 M/s`) | **2197.8** (`0.455 M/s`) | **247.3** (`4.043 M/s`) | Negative result: defeats Dart AOT `at == 0` constant propagation and monomorphic `ByteData` |

### Root causes identified by the diagnostic split

1. **Stage 2 (`fixedTableFixedDecode` object projection and bounds clamping) costs `402.5 ns/record` (`73.9%` of the identity read):**
   - Subtracting Arm 1 (`141.9 ns/record` for `tableFixedRun` + header/hash check alone) from Arm 0
     (`544.4 ns/record`) shows that `fixedTableFixedDecode` alone takes **`402.5 ns/record`** —
     already `1.63x` slower than the entire packet read (`247.5 ns/record`).
   - **Missing `@pragma('vm:prefer-inline')` on generated `*FixedDecode` functions (`~101 ns/record`):**
     In `internal/codegen/darttable/fixedform.go`, `mixedStatFixedDecode` (called up to 80 times per
     record), `mixedEntityFixedDecode` (called up to 8 times per record), `mixed*EventFixedDecode`,
     `benchMixedFixedDecode`, and `fixedTableFixedDecode` are emitted without
     `@pragma('vm:prefer-inline')`, and `benchMixedFixedDecode` allocates two non-`const` `Int128`
     bounds (`Int128(0xfffffff000000000, 0)` and `Int128(68719476736, 0)`) on every record. Adding
     `@pragma('vm:prefer-inline')` and `const Int128` bounds (Arm 2) drops Stage 2 from `402.5` to
     `301.2 ns/record` (`544.4 -> 443.1 ns/record` overall).
   - **Per-field widened `ByteData` loads, ~260 clamp branches, `UInt128`/`Int128` allocations, and `float32Value` scratch-buffer stores (`~301 ns/record`):**
     `readBenchMixed` (`Bench.dart`, packet wire) unpacks bitfields in registers from 64-bit
     bitstream words, unrolls `stats` 3x per 64-bit load (`27` 64-bit loads for 80 stats), and
     performs zero width-clamp branches on `bits(N)` fields because N-bit masks cannot exceed
     `2^N - 1`. By contrast, `benchMixedFixedDecode` (`BenchFixed.dart`) executes `260` individual
     `ByteData.getInt32`/`getUint32`/`getUint8` calls (`8` entities × `13` fields + `80` stats ×
     `2` fields + top-level fields) and ~`260` `if (v < lo) { ... report.clamped++; } else if (v > hi) { ... report.clamped++; }`
     branches, allocates fresh `UInt128` (`wideKey`) and `Int128` (`flux`) heap objects on every
     record, and routes 4 `float32` reads through global typed-data buffers (`_f32Words[0] = bits; return _f32[0]`).

2. **Stage 1 (`tableFixedRun` copying into `plan.image`) costs `141.9 ns/record` on the identity lane and `~1310–1355 ns/record` on the plan lane:**
   - Every record is first copied into `plan.image` (`1,236` bytes) by `tableFixedRun` before
     `fixedTableFixedDecode` reads `plan.imageView` back out. On the identity lane, `tableFixedRun`
     iterates over 12 `TableFixedPlanEntry` items and calls `dst.setRange` 12 times (`141.9 ns/record`).
   - On the plan lane (`bench_plan_read`), `tableFixedFillRun` zeroes 3 prefill ranges and
     `tableFixedRun` interprets **157 `TableFixedPlanEntry` objects** in a `switch (e.op)` loop:
     `64` `widenS` entries (`MixedStat.delta` `int16 -> int32`) and `93` fragmented `copy` entries
     (including 64 4-byte `MixedStat.stat_id` copies via `dst.setRange`), plus `8` `enumGuard` /
     `textCheck` entries. That interpreted 157-entry plan loop costs **`~1310–1355 ns/record`** (`~79%`
     of `1666.5 ns/record`).
   - **The Dart AOT constant-propagation trap (Arm 3):** Simply calling the shared
     `fixedTableFixedDecode(values[k], bytes, view, at + 8, report)` on the identity branch while
     calling `fixedTableFixedDecode(values[k], plan.image, plan.imageView, 0, report)` on the plan
     branch regresses identity to `980.4 ns/record` and plan to `2197.8 ns/record`, because Dart
     AOT's interprocedural constant propagation otherwise proves `int at == 0` across all call sites
     of `fixedTableFixedDecode` and `benchMixedFixedDecode` and specializes `view` monomorphically.
     Eliminating Stage 1 on the identity path in `internal/codegen/darttable/fixedform.go` therefore
     requires emitting a separate specialized identity decoder (or a per-table compiled plan runner
     as C# did in `f83220bf0`).

### Profile commands that confirm both lanes

On a Linux bench with `perf` enabled or via the Dart AOT disassembler / VM profiler:

```sh
# 1. Linux perf cycle profile + call-graph annotation on the AOT binary:
perf record -e cycles:pp -g -- taskset -c 6 build/paired/plan-dart \
  --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus \
  --read-arm --round 0 --iterations 4000000 \
  --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants \
  --lineage-dir bench/tables/dart/lineage
perf report --stdio --no-children

# 2. Dart VM service CPU sample profile (JIT / observatory):
dart run --observe=0 --profile-vm=true --pause-isolates-on-exit \
  bench/tables/dart/table_main.dart --indexed \
  --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus \
  --read-arm --round 0 --iterations 400000 \
  --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants \
  --lineage-dir bench/tables/dart/lineage
```

## What this card built

- **`bench/paired/main.go`, `bench/paired/table.go`, `bench/paired/nine.sh`:**
  - Extended `-mode plan-read -langs cpp|dart` and `nine.sh --read-arm --leg cpp|dart --pin CORE`.
  - In `-mode build -langs dart`, when `bench/tables/dart/lineage/{VOLD_bench_lineage.schema,schema.lock,bench_lineage.bin,bench_lineage.layout}`
    are present on the tree, generates the paired Dart unit a second time beside
    `bench/tables/dart/lineage/schema.lock` (plus `Bench.dart` for the packet wire) and compiles
    `build/paired/plan-dart` with `dart compile exe`.
  - In `-mode plan-read -langs dart`, brackets the sitting with `table-dart` control runs
    (`tableOnlyLanguages`), runs the pre-clock `--read-arm --gate` check, and interleaves 7 rounds
    of `--read-arm --csv --round r --iterations 4000000`.
- **`bench/paired/readarm_test.go`:**
  - Added unit tests (`TestReadArm`, `TestReadArmDart`, `TestReadArmRefusals`, `TestReadArmWindow`)
    covering both C++ and Dart read-arm boards, verdicts, plan-lane fallback/refusal, and window
    validation.
- **`bench/results/packet-read-dart/`:**
  - Committed `RESULT.md`, `board.txt`, `sitting-1/` and `sitting-2/`.

## Gate

Run on `vision` (`go test -count=1 -timeout 600s ./bench/paired/ ./internal/ci/`):

- `ok  github.com/mas-bandwidth/schema/v2/bench/paired`
- `ok  github.com/mas-bandwidth/schema/v2/internal/ci`
- `go run ./bench/tools/shapegate`: `clean — 47 file(s) carry shape knowledge, every one on the ledger`

## Not done, and `PATHS-PROPOSED`

- **The roadmap flips (`gates/packet-read/dart/identity` and `gates/packet-read/dart/plan`) are not done.**
  Both GATE verdicts are `FAIL` (`544.4 vs 247.5 ns/record` on identity; `1666.5 vs 247.5 ns/record`
  on plan), and the brief specifies: *"Only when both verdicts pass in a valid window, flip
  `gates/packet-read/dart/identity` and `gates/packet-read/dart/plan` to `:done` in
  `docs/roadmap.sexp`"*.
- **`PATHS-PROPOSED: bench/tables/dart/table_main.dart`, `bench/tables/dart/SHAPE-GATE.allow`, and `bench/tables/dart/lineage/{VOLD_bench_lineage.schema,schema.lock,bench_lineage.bin,bench_lineage.layout}`:**
  `bench/tables/dart/**` is outside this card's `PATHS`. Once `bench/tables/dart/table_main.dart`
  (`--read-arm`), `bench/tables/dart/SHAPE-GATE.allow`, and `bench/tables/dart/lineage/` (copied
  byte-for-byte from `bench/tables/rust/lineage/`) land on `sprint/schema-dart`,
  `bench/paired/nine.sh --read-arm --leg dart --pin CORE` runs directly on the branch without the
  scratch overlay.
- **`PATHS-PROPOSED: internal/codegen/darttable/fixedform.go`:**
  To make Dart's fixed read beat its packet read (`< 247.5 ns/record`), `internal/codegen/darttable/fixedform.go`
  needs to:
  1. Emit `@pragma('vm:prefer-inline')` on generated `*FixedDecode` functions and use `const Int128`
     clamp bounds (measured at `-101.3 ns/record` on `b1`);
  2. Emit a specialized direct-buffer identity decode path (separate from the `plan.image` decode
     symbol so Dart AOT preserves `at == 0` constant folding on the plan path) to save the
     `141.9 ns/record` `tableFixedRun` copy on identity;
  3. Emit a per-table compiled plan loop (eliminating the 157-entry `TableFixedPlanEntry` switch
     and 4-byte `dst.setRange` calls in `tableFixedRun`, which cost `~1310 ns/record` on the plan
     lane) and mutate `UInt128`/`Int128` fields in place without per-record heap allocations.

### Verbatim diff for `bench/tables/dart/table_main.dart` and `bench/tables/dart/SHAPE-GATE.allow`

```diff
diff --git a/bench/tables/dart/SHAPE-GATE.allow b/bench/tables/dart/SHAPE-GATE.allow
index c1f346478..30779a2ca 100644
--- a/bench/tables/dart/SHAPE-GATE.allow
+++ b/bench/tables/dart/SHAPE-GATE.allow
@@ -2,4 +2,4 @@
 # The format, the rules and the shared tooling's entries: bench/SHAPE-GATE.allow.
 # Paths are repository-relative.
 
-names bench/tables/dart/table_main.dart 3 shape-blind leg: `FixedTable` at its one generated import site and the FIXED form's two storage declarations (docs/SPEC-TABLES.md §3.4); no field, no pinned value, no wire size (callsite)
+names bench/tables/dart/table_main.dart 9 shape-blind leg: `FixedTable` at its generated import site and the FIXED form's storage declarations (docs/SPEC-TABLES.md §3.4), plus `BenchMixed` at the read arm's packet import and storage declaration (BENCH-STANDARD.md §1.10, `bench_mixed_read`); no field, no pinned value, no wire size (callsite)
diff --git a/bench/tables/dart/table_main.dart b/bench/tables/dart/table_main.dart
index 10c028ae7..a99952219 100644
--- a/bench/tables/dart/table_main.dart
+++ b/bench/tables/dart/table_main.dart
@@ -80,11 +80,15 @@
 import 'dart:io';
 import 'dart:typed_data';
 
+import '../../../generated/bench/dart/Bench.dart' as packet
+    show BenchMixed, readBenchMixed, writeBenchMixed;
 import '../../../generated/bench/paired/dart/BenchFixed.dart'
-    show FixedTable, TableFixedReport;
+    show FixedTable, TableFixedPlan, TableFixedReport;
 import '../../../generated/bench/paired/dart/FixedTableFixed.dart'
     show
+        fixedTableFixedHash,
         fixedTableFixedLayout,
+        fixedTableFixedKnown,
         fixedTableFixedLoad,
         fixedTableFixedMeasure,
         fixedTableFixedNewPlan,
@@ -95,12 +99,18 @@ const int numVariants = 64; // the corpus is 64 records, and an OP is one record
 const int defaultIterations = 400000; // the tables bench's standard table count
 
 int gNumRuns = maxNumRuns; // --round K drops this to 1 (§2.4)
+int gRound = 0; // K itself: the read arm rotates its wires' order by it
 int gIterations = 0; // explicit diagnostic count; the default stays standard
 bool gCsv = false;
 bool gGate = false;
 bool gIndexed = false;
+bool gReadArm = false;
+int gPacketIterations = 0;
 String gWireDir = 'testdata/wire';
 String gVariantDir = 'bench/corpus/variants';
+String gPacketWireDir = 'testdata/wire';
+String gPacketVariantDir = 'bench/corpus/variants';
+String gLineageDir = '';
 bool failed = false;
 
 final Stopwatch _clock = Stopwatch()..start();
@@ -123,6 +133,7 @@ double now() => _clock.elapsedMicroseconds * 1e-6;
 // and the row is the plain seventeen.
 const String csvSuffix = 'aot,contract,default,unknown';
 final List<String> gCsvRows = [];
+final List<String> gCsvFamilies = [];
 final Map<String, Uint8List> gGoldensLoaded = {}; // basename -> bytes
 
 int gSink = 0; // defeats dead code elimination of computed values
@@ -172,8 +183,8 @@ void flushCsv() {
     'lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,'
     'max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n',
   );
-  for (final row in gCsvRows) {
-    out.write('$row,$id,table,$csvSuffix\n');
+  for (var i = 0; i < gCsvRows.length; i++) {
+    out.write('${gCsvRows[i]},$id,${gCsvFamilies[i]},$csvSuffix\n');
   }
   stdout.write(out.toString());
 }
@@ -231,7 +242,14 @@ Stats stats(List<double> rates) {
   );
 }
 
-void report(String bench, String path, int iters, double bytesPerOp, Stats s) {
+void report(
+  String bench,
+  String path,
+  int iters,
+  double bytesPerOp,
+  Stats s, [
+  String family = 'table',
+]) {
   final mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
   stderr.write(
     '${bench.padRight(18)} ${path.padRight(11)} '
@@ -248,6 +266,7 @@ void report(String bench, String path, int iters, double bytesPerOp, Stats s) {
       '${s.max.toStringAsFixed(0)},'
       '${mbps.toStringAsFixed(2)},${s.spread.toStringAsFixed(2)}',
     );
+    gCsvFamilies.add(family);
   }
 }
 
@@ -334,6 +353,299 @@ bool checkTolerantCorpus(String name, String golden) {
 // every record carries — is settled by those bytes, so it is checked first
 // and by itself, and a leg that matches it is speaking the form and not a
 // near miss.
+
+// ---- THE READ ARM (BENCH-STANDARD.md §1.10): each wire's READ, measured alone ----
+//
+// `--read-arm` replaces the clocked rows of this invocation with the arm's
+// measured reads, after every correctness gate this runner already runs and
+// the arm's own, all of them before the first clock:
+//
+//   bench_fixed_read  family table  the fixed form's IDENTITY lane: the shipped
+//                                   LOAD of the whole corpus file, one call per
+//                                   64 records, the hash check and the plan run
+//                                   inside the clock; an op is one record
+//   bench_plan_read   family table  the PLAN lane (--lineage-dir): the same
+//                                   LOAD of the lineage corpus, 64 records one
+//                                   generation back, read through the static
+//                                   plan the lock's lineage entry compiled; an
+//                                   op is one record
+//   bench_mixed_read  family gen    the PACKET wire's read of the same 64
+//                                   logical records: one generated read per
+//                                   record, exactly the read half of the packet
+//                                   leg's round trip
+
+void readArm(
+  Uint8List file,
+  Uint8List twin,
+  List<FixedTable> values,
+  List<FixedTable> out,
+  TableFixedPlan outPlan,
+  TableFixedReport report_,
+  int iters,
+) {
+  final packetIters = gPacketIterations != 0 ? gPacketIterations : 4000000;
+  final packed = readCorpus(gPacketVariantDir, 'bench_mixed.variants.bin');
+  final golden = readCorpus(gPacketWireDir, 'bench_mixed.bin');
+  if (packed == null ||
+      golden == null ||
+      packed.isEmpty ||
+      packed.length % numVariants != 0) {
+    fail('bench_mixed', 'missing or malformed packet variant corpus');
+    return;
+  }
+  final packetRecord = packed.length ~/ numVariants;
+  if (!bytesEqual(Uint8List.sublistView(packed, 0, packetRecord), golden)) {
+    fail('bench_mixed', 'packet variant 0 is not the packet wire golden');
+    return;
+  }
+  final packetVariants = <ByteData>[];
+  for (var k = 0; k < numVariants; k++) {
+    final slot = Uint8List(512);
+    slot.setRange(0, packetRecord, packed, k * packetRecord);
+    packetVariants.add(ByteData.sublistView(slot, 0, packetRecord));
+  }
+  final packetTwin = Uint8List(512);
+  final packetTwinView = ByteData.sublistView(packetTwin);
+  final gateValue = packet.BenchMixed();
+  for (var k = 0; k < numVariants; k++) {
+    if (!packet.readBenchMixed(gateValue, packetVariants[k], packetRecord * 8)) {
+      fail('bench_mixed', 'decode of a packet variant failed');
+      return;
+    }
+    final n = packet.writeBenchMixed(gateValue, packetTwinView);
+    if (n != packetRecord ||
+        !bytesEqual(
+          Uint8List.sublistView(packetTwin, 0, n),
+          Uint8List.sublistView(packed, k * packetRecord, (k + 1) * packetRecord),
+        )) {
+      fail(
+        'bench_mixed',
+        'packet variant round-trip bytes differ — refusing to bench a codec '
+        'that does not reproduce the corpus',
+      );
+      return;
+    }
+  }
+
+  Uint8List? lineageFile;
+  final planOut = List.generate(numVariants, (_) => FixedTable());
+  final planPlan = fixedTableFixedNewPlan();
+  if (gLineageDir.isNotEmpty) {
+    lineageFile = readCorpus(gLineageDir, 'bench_lineage.bin');
+    final lineageLayout = readCorpus(gLineageDir, 'bench_lineage.layout');
+    if (lineageFile == null || lineageLayout == null || lineageFile.length < 20) {
+      fail('bench_plan_read', 'missing lineage corpus in $gLineageDir');
+      return;
+    }
+    final lv = ByteData.sublistView(lineageFile);
+    final fileHash = lv.getUint64(8, Endian.little);
+    final embedded = lv.getUint32(16, Endian.little);
+    if (fixedTableFixedKnown.length != 2 ||
+        fileHash != fixedTableFixedKnown[0].hash ||
+        fixedTableFixedKnown[0].hash == fixedTableFixedHash ||
+        lineageLayout.length != fixedTableFixedKnown[0].layout.length ||
+        !bytesEqual(lineageLayout, fixedTableFixedKnown[0].layout) ||
+        embedded != lineageLayout.length ||
+        20 + embedded > lineageFile.length ||
+        !bytesEqual(
+          Uint8List.sublistView(lineageFile, 20, 20 + embedded),
+          lineageLayout,
+        )) {
+      fail(
+        'bench_plan_read',
+        "the lineage corpus is not the lock's older layout, or this build's "
+        "lineage is not that lock's",
+      );
+      return;
+    }
+    if (fixedTableFixedLoad(
+              planOut,
+              numVariants,
+              lineageFile,
+              lineageFile.length,
+              planPlan,
+              report_,
+            ) !=
+            numVariants ||
+        report_.malformed ||
+        report_.refused != 0 ||
+        report_.unknown != 0 ||
+        report_.kindMismatch != 0 ||
+        report_.clamped != 0 ||
+        report_.duplicate != 0 ||
+        report_.widened == 0) {
+      fail(
+        'bench_plan_read',
+        'the lineage corpus did not read clean through its plan',
+      );
+      return;
+    }
+    stderr.write(
+      '${'bench_plan_read'.padRight(18)} plan read of $numVariants records: '
+      'widened ${report_.widened}, clamped ${report_.clamped}\n',
+    );
+    final planPoisoned = List.generate(numVariants, (_) => FixedTable());
+    final poisonPlan = fixedTableFixedNewPlan();
+    // Seed planPoisoned with the identity corpus so every field starts non-zero.
+    if (fixedTableFixedLoad(
+          planPoisoned,
+          numVariants,
+          file,
+          file.length,
+          poisonPlan,
+          report_,
+        ) !=
+        numVariants) {
+      fail('bench_plan_read', 'poison seed load failed');
+      return;
+    }
+    final savedBytes = fixedTableFixedMeasure(numVariants);
+    final a = Uint8List(savedBytes);
+    final b = Uint8List(savedBytes);
+    if (fixedTableFixedLoad(
+              planPoisoned,
+              numVariants,
+              lineageFile,
+              lineageFile.length,
+              poisonPlan,
+              report_,
+            ) !=
+            numVariants ||
+        fixedTableFixedSave(planOut, numVariants, a) != savedBytes ||
+        fixedTableFixedSave(planPoisoned, numVariants, b) != savedBytes ||
+        !bytesEqual(a, b)) {
+      fail('bench_plan_read', 'a poisoned destination read to a different answer');
+      return;
+    }
+    if (savedBytes == file.length && bytesEqual(a, file)) {
+      fail(
+        'bench_plan_read',
+        'the lineage corpus reads to the identity corpus: the plan lane would '
+        'measure nothing older',
+      );
+      return;
+    }
+  }
+
+  if (gGate) {
+    return;
+  }
+
+  void timeFixedRead() {
+    final rates = List<double>.filled(gNumRuns, 0);
+    for (var run = -1; run < gNumRuns; run++) {
+      final start = now();
+      for (var i = 0; i < iters; i += numVariants) {
+        final loaded = fixedTableFixedLoad(
+          out,
+          numVariants,
+          file,
+          file.length,
+          outPlan,
+          report_,
+        );
+        if (loaded != numVariants) {
+          fail('bench_fixed_read', 'load failed in loop');
+          return;
+        }
+        gSink = (gSink + loaded) & 0xffffffff;
+      }
+      final elapsed = now() - start;
+      if (run >= 0) {
+        rates[run] = iters / elapsed;
+      }
+    }
+    if (fixedTableFixedSave(out, numVariants, twin) != file.length ||
+        !bytesEqual(twin, file)) {
+      fail('bench_fixed_read', 'post-clock re-save mismatch');
+      return;
+    }
+    gSink = (gSink + twin[0]) & 0xffffffff;
+    report('bench_fixed_read', 'read', iters, file.length / numVariants, stats(rates));
+  }
+
+  void timeMixedRead() {
+    final rates = List<double>.filled(gNumRuns, 0);
+    final numBits = packetRecord * 8;
+    for (var run = -1; run < gNumRuns; run++) {
+      final start = now();
+      for (var i = 0; i < packetIters; i++) {
+        if (!packet.readBenchMixed(
+          gateValue,
+          packetVariants[i & (numVariants - 1)],
+          numBits,
+        )) {
+          fail('bench_mixed_read', 'read failed in loop');
+          return;
+        }
+        gSink = (gSink + 1) & 0xffffffff;
+      }
+      final elapsed = now() - start;
+      if (run >= 0) {
+        rates[run] = packetIters / elapsed;
+      }
+    }
+    if (packet.writeBenchMixed(gateValue, packetTwinView) != packetRecord) {
+      fail('bench_mixed_read', 'post-clock packet re-encode failed');
+      return;
+    }
+    gSink = (gSink + packetTwin[0]) & 0xffffffff;
+    report(
+      'bench_mixed_read',
+      'read',
+      packetIters,
+      packetRecord.toDouble(),
+      stats(rates),
+      'gen',
+    );
+  }
+
+  void timePlanRead() {
+    final lFile = lineageFile!;
+    final rates = List<double>.filled(gNumRuns, 0);
+    for (var run = -1; run < gNumRuns; run++) {
+      final start = now();
+      for (var i = 0; i < iters; i += numVariants) {
+        final loaded = fixedTableFixedLoad(
+          planOut,
+          numVariants,
+          lFile,
+          lFile.length,
+          planPlan,
+          report_,
+        );
+        if (loaded != numVariants) {
+          fail('bench_plan_read', 'plan load failed in loop');
+          return;
+        }
+        gSink = (gSink + loaded) & 0xffffffff;
+      }
+      final elapsed = now() - start;
+      if (run >= 0) {
+        rates[run] = iters / elapsed;
+      }
+    }
+    if (fixedTableFixedSave(planOut, numVariants, twin) != file.length) {
+      fail('bench_plan_read', 'post-clock plan re-save failed');
+      return;
+    }
+    gSink = (gSink + twin[0]) & 0xffffffff;
+    report('bench_plan_read', 'read', iters, lFile.length / numVariants, stats(rates));
+  }
+
+  final steps = gLineageDir.isNotEmpty ? 3 : 2;
+  for (var step = 0; step < steps && !failed; step++) {
+    switch ((gRound + step) % steps) {
+      case 0:
+        timeFixedRead();
+      case 1:
+        timeMixedRead();
+      case 2:
+        timePlanRead();
+    }
+  }
+}
+
 void benchFixed(String name, int baseIters) {
   final iters = gIterations != 0 ? gIterations : baseIters;
   final file = readCorpus(gVariantDir, 'bench_fixed.bin');
@@ -409,6 +721,11 @@ void benchFixed(String name, int baseIters) {
     'reused-target round-trip (4 of 4)\n',
   );
 
+  if (gReadArm) {
+    readArm(file, twin, values, out, outPlan, report_, iters);
+    return;
+  }
+
   if (gGate) {
     return;
   }
@@ -498,7 +815,9 @@ void benchFixed(String name, int baseIters) {
 Never usage() {
   stderr.write(
     'usage: table-dart [--indexed] [--gate] [--csv] [--round K] '
-    '[--iterations N] [--wire-dir <dir>] [--variant-dir <dir>]\n',
+    '[--iterations N] [--wire-dir <dir>] [--variant-dir <dir>] '
+    '[--read-arm [--packet-iterations N] [--packet-wire-dir <dir>] '
+    '[--packet-variant-dir <dir>] [--lineage-dir <dir>]]\n',
   );
   exit(1);
 }
@@ -512,10 +831,18 @@ void main(List<String> args) {
       gGate = true;
     } else if (a == '--csv') {
       gCsv = true;
+    } else if (a == '--read-arm') {
+      gReadArm = true;
     } else if (a == '--wire-dir' && i + 1 < args.length) {
       gWireDir = args[++i];
     } else if (a == '--variant-dir' && i + 1 < args.length) {
       gVariantDir = args[++i];
+    } else if (a == '--packet-wire-dir' && i + 1 < args.length) {
+      gPacketWireDir = args[++i];
+    } else if (a == '--packet-variant-dir' && i + 1 < args.length) {
+      gPacketVariantDir = args[++i];
+    } else if (a == '--lineage-dir' && i + 1 < args.length) {
+      gLineageDir = args[++i];
     } else if (a == '--iterations' && i + 1 < args.length) {
       final n = int.tryParse(args[++i]);
       if (n == null || n <= 0 || n > 2147483584 || n % numVariants != 0) {
@@ -525,6 +852,15 @@ void main(List<String> args) {
         exit(1);
       }
       gIterations = n;
+    } else if (a == '--packet-iterations' && i + 1 < args.length) {
+      final n = int.tryParse(args[++i]);
+      if (n == null || n <= 0 || n > 2147483584 || n % numVariants != 0) {
+        stderr.write(
+          '--packet-iterations requires a positive multiple of 64 up to 2147483584\n',
+        );
+        exit(1);
+      }
+      gPacketIterations = n;
     } else if (a == '--round' && i + 1 < args.length) {
       // §2.4: one warmup + one measured run, then exit. K only identifies the
       // round to the interleaved driver, which aggregates across rounds itself.
@@ -536,6 +872,7 @@ void main(List<String> args) {
         exit(1);
       }
       gNumRuns = 1;
+      gRound = k;
     } else {
       usage();
     }
```
