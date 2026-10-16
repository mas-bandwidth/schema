# 2026-10-16, hetzner1: the C++ fixed-form clamp runs only on a record its check fails

The card's earlier attempt (`2026-10-16-x86_64-hetzner1-cpp-clamp-restrict`) put the
clamp counters behind `TABLE_CLAMP_RESTRICT` and measured neither compiler's identity
read beating its packet read (g++ 215 vs 148, clang 150 vs 130 ns/record). `perf` is
not permitted on the benches (`perf_event_paranoid = 4`, left unchanged), so this attempt
diagnosed the identity read with the compilers' own reports and stage-by-stage builds.
The diagnosis gave an emitter change, and with it both compilers' identity reads beat
their packet reads in every sitting.

## Verdict

| compiler | identity read, ns/record (sitting 1 / 2) | packet read (1 / 2) | GATE identity |
|---|---:|---:|---|
| g++ 13.3.0 | **104.5 / 104.4** | 147.9 / 148.4 | **PASS**, 1.42x faster |
| clang 18.1.3 | **96.8 / 95.4** | 129.9 / 129.9 | **PASS**, 1.36x faster |
| MSVC, others | not measured (no MSVC bench) | | portable form, same check |

Before this commit, at `66d571b6d` and the same method: g++ 215.3 / 215.5 and clang
152.3 / 149.5. Every number above is copied from a board the driver printed
(`READ.md` in each sitting directory).

## The diagnosis

1. **Which pass.** Stage-stub builds of the runner, with the generated header copied and
   one pass compiled out (`diagnosis/stage-stub-rows.csv`, pinned, best of 3). The
   identity read without the clamp pass is **69.2 ns (g++) / 75.9 ns (clang)**, against
   216.7 / 148.8 with it. Without `TableUtf8Valid` it is 237.4 / 147.8, so the text check
   is not the cost. The clamp pass alone, on hot in-range records (`diagnosis/micro.cpp`,
   `micro-rows.txt`), is **175 ns (g++) / 85 ns (clang)** per record. The corpus records
   are full: 8 entities x 11 bounded fields plus 80 stats x 2, about 270 clamps a record.
   `TableFixedClampKnownRanges` has no ranges on the identity lineage and returns at once.
2. **Why it is scalar.** From the compilers' reports over the runner's translation unit
   (`gcc-fopt-info-vec-missed.txt`, `clang-Rpass-loop-slp.txt`):
   - g++ `-fopt-info-vec-missed`: the entities and stats loops in
     `BenchMixedFixedClampBody` (BenchTable.h:10490, :10494) are *not vectorized: number
     of iterations cannot be computed*, and every SLP group in `MixedEntityFixedClampBody`
     is *not profitable*. The trip count `value.entities_count` is an `int32_t` of the
     value the loop stores `int32_t` to. The body is not inlined (an 0x8f0-byte function),
     so the counter is a memory reference: `add %edx,(%rcx)` for every field, then a
     reload of the next field after that store (`gcc-BenchMixedFixedClampBody-head.s`).
     That is one store-to-load chain per clamp.
   - clang `-Rpass-analysis=loop-vectorize`: the same loops are *could not determine
     number of loop iterations* and *call instruction cannot be vectorized*. clang keeps
     `MixedEntityFixedClampBody` out of line and calls it once per entity. `__restrict`
     let it keep the counter in a register, which is the 185 -> 149 the earlier attempt
     measured, but the per-field store and the call remain.
3. **What does not fix it** (`inline-hoist-rows.csv`, `micro-rows.txt`). Forcing every
   body `always_inline`, then also hoisting each count into a local and counting into a
   per-loop local: clang 149 -> 142, and g++ 216 -> 253. With the counters in registers,
   g++'s SLP vectorizer mixes vector loads of the entity fields with scalar stores to
   them, which is slower. The clamp alone stays at 174 / 79. The stores are the cost.
4. **The fix.** A clamp that does not fire stores the value back unchanged and adds 0. So
   each type now also gets `<T>FixedInRange`, the same walk with every clamp's COUNT
   expression or'd into one flag and no store, and the root runs `<T>FixedClampBody`
   only on a record the check fails. With no stores, nothing the check reads can change
   under it, and its loops are or-reductions. Clamp alone: **175 -> 39 ns (g++), 85 -> 31
   ns (clang)**. The runner's identity read: **216 -> 105, 149 -> 94**
   (`check-form-rows.csv`). The body is unchanged, so a record with any out-of-range
   value clamps and counts exactly as before.

## The sittings

Each sitting was one `CXX=<c++> CC=<cc> taskset -c 6 sh bench/paired/nine.sh --read-arm
--out ... --receipt ...` at `bdb901be5d679f1cd6853e5caa667dd12374714d`
(`diagnosis/sittings.sh`). Before each one, the machine waited for three 20 s samples
in a row under 6% busy CPU, load1 under 2, and no process named on `window.go`'s
refused list. Two earlier tries were refused at preflight, by another job's `go`
(PID 1775564) and `VBCSCompiler` (PID 1783345). Neither process was this job's, neither
was touched, and no ratio was taken from those tries. The four sittings below ran back to
back in the order g++ 1, clang 1, g++ 2, clang 2. Every window is **OK**
(`window-summary.json`).

| | g++ 1 | g++ 2 | clang 1 | clang 2 |
|---|---|---|---|---|
| control delta (§2.6, bound 5%) | 0.41% | 0.41% | 0.16% | 0.98% |
| load1 during the window | 0.19–0.42 | 0.43–0.63 | 0.25–0.69 | 0.39–0.64 |
| `bench_fixed_read` best / median / worst | 104.5 / 105.9 / 108.3 | 104.4 / 106.7 / 107.5 | 96.8 / 102.0 / 108.3 | 95.4 / 101.5 / 104.3 |
| spread (rates) | 3.5% | 3.0% | 11.2% | 9.1% |
| `bench_mixed_read` best / median / worst | 147.9 / 150.2 / 151.0 | 148.4 / 150.9 / 151.3 | 129.9 / 130.2 / 130.3 | 129.9 / 130.1 / 130.7 |

- **host**: `hetzner1`, linux/amd64, AMD Ryzen 7 PRO 8700GE. Pinned to core 6 (the host has no isolated cores, so this pins without isolating).
- **revisions**: schema `bdb901be5d679f1cd6853e5caa667dd12374714d`; serialize `93b8ea2a958ea3deeef9d4aff16101171ac49e15` (v1.16.2). The tree was dirty in one build-regenerated file only, `generated/bench/paired/cpp/BenchTable.h`. It was already older than the emitter at BASE, and the board names it.
- **flags**: `-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -DBENCH_MATCHED`. g++ adds `-Wno-class-memaccess -Wno-type-limits`. The packet read is the packet leg's `checks=removed` build.
- **corpus**: `corpus_id(identity)` = `12224b99745dc057` in every sitting.
- **iterations**: the driver's own, 7 rounds.
- **clang's identity spread** (9–11%) is wider than g++'s. Its worst round (108.3 / 104.3) still beats its best packet round (129.9).
- **the anchor** still does not compile (`std::atoi` in `test/bench/fixedform_measure.cpp`, outside PATHS).

## ARM, mini-m5-1 (Apple M5 Pro, Apple clang 21.0.0), exploration only

There is no board here: the runner was run directly, unpinned (macOS has no taskset),
with load1 near 4 from this job's own builds just before. The rows are in
`diagnosis/mini-m5-1-rows.csv`, best of 5 rounds x 2,000,000:

| emitter | identity | packet |
|---|---:|---:|
| `66d571b6d`, `__restrict` only | 88.6 | 89.1 |
| this commit, the check | **72.7** | 89.1 |

## Files

- `{gcc,clang}-sitting-{1,2}/`: the driver's board (`READ.md`), `read.json`, `build.json`, the control and round CSVs, and `window-summary.json` (verdict, span, phases, sha256 of the full `window.json`, which is kept outside the tree).
- `diagnosis/`: the compiler reports filtered to the clamp code, the g++ clamp body's disassembly head, the stage-stub, inline/hoist, check-form, micro and ARM rows, `micro.cpp`, `pred.py` (the scratch transform that prototyped the check), and `sittings.sh`.
