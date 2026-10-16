# 2026-10-16, hetzner1: the C++ fixed-form clamp counters, `__restrict` per compiler

The probe for the owner decision of 2026-10-16: the counters the read-side clamp pass
(`FixedTableFixedClamp` → `*FixedClampBody`) takes are `int32_t &`, which may alias
every `int32_t` field the pass stores to. So each clamp reloads the counter, and the pass
does not vectorize. The question was which counter form each compiler does best with:
the plain reference, `__restrict`, a local counter stored once at the end, or both.
Every sitting number below is copied from a board the driver printed (`READ.md` in each
sitting directory). None was typed in by hand.

## Verdict

| compiler | form chosen (measured best) | identity read, ns/record (sitting 1 / 2) | packet read (1 / 2) | GATE identity |
|---|---|---:|---:|---|
| g++ 13.3.0 | the plain reference (unchanged) | **215.3 / 215.5** | 148.1 / 147.7 | **FAIL**, 1.46x slower |
| clang 18.1.3 | `__restrict` | **152.3 / 149.5** | 130.0 / 129.9 | **FAIL**, 1.15x slower |
| MSVC, others | the plain reference (portable fallback) | not measured (no MSVC bench) | | |

`__restrict` makes clang's identity read 1.24x faster (185.8 → 148.5 ns/record in the
exploration, 149.5 in sitting 2). The read still loses to clang's own packet read.
For g++, none of the three alternatives improves on the plain reference. `__restrict`
is 15% slower and the local counter is 43% slower. So g++ keeps the current form,
and its read still loses to its packet read. **Neither compiler's identity lane
beats the same leg's packet read.**

## The four forms, exploration (scratch emitter, not the shipped one)

The forms were compared at a scratch emitter (`exploration/exploration-emitter.diff`).
That emitter emits both `TABLE_CLAMP_RESTRICT` and `TABLE_CLAMP_LOCAL`, so one generated
header builds all four forms by `-D`. The scratch emitter was never committed. The shipped
emitter keeps the qualifier only, since no compiler's best form uses the local counter.

The exploration ran the runner's `--read-arm` directly, with the driver's flag line
(`exploration/explore.sh`, `explore-run.sh`). It used 5 rounds of 2,000,000 records per
sample, pinned with `taskset -c 6`, after a quiet wait (machine busy 0.5%, load1 0.87).
The rows are in `exploration/hetzner1-rows.csv`. Best and worst ns/record across 5 rounds:

| form | g++ identity | g++ packet | clang identity | clang packet |
|---|---:|---:|---:|---:|
| plain reference | **215.8** (–218.3) | 146.9–148.6 | 185.8 (–188.9) | 130.1–130.8 |
| `__restrict` | 247.9 (–250.9) | 147.5–147.9 | **148.5** (–150.0) | 130.2–133.5 |
| local counter | 307.7 (–311.2) | 148.2–149.2 | 148.2 (–326.1, one outlier round) | 130.3–144.9 |
| both | 309.1 (–310.0) | 148.0–149.1 | 149.3 (–161.8) | 130.0–130.8 |

For clang, `__restrict` and the local counter tie within 0.2%. `__restrict` is the
owner's named form and has the tighter spread, so it is the one chosen. Under g++ the
local counter makes the SLP vectorizer turn on in `BenchMixedFixedClampBody`: 103
xmm instructions against 23 in the plain build. It de-interleaves the `stats` and
`entities` elements with `shufps` and stores lanes back one at a time with `movd`,
and that is slower. Why g++'s `__restrict` build is slower, with no change in
vectorization, is not known.

**ARM, mini-m5-1 (Apple M5, Apple clang 21.0.0), exploration only**
(`exploration/mini-m5-1-rows.csv`, 3 rounds, unpinned: macOS has no taskset):

- plain: identity 134.8
- `__restrict`: identity **89.1**
- local: identity 90.0
- both: identity 89.8
- packet read: 88.7–91.8 ns/record

On ARM, `__restrict` is 1.51x faster and ties the packet read, but does not beat it.
The driver's sitting there was refused at preflight: a pre-existing `go` process
(PID 683, not this job's) is a competing process by `bench/paired/window.go`'s list.
So ARM has no board.

## The sittings

Each sitting was one `CXX=<c++> CC=<cc> taskset -c 6 sh bench/paired/nine.sh --read-arm
--receipt ...` run at `8649a725d7e8509df529481e0e2c584f4485ae53`, the commit that ships
the qualifier. Before each sitting, the machine waited for a quiet window: three 20 s
samples under 6% busy CPU, load1 under 2, and none of the window's refused process
names present. The four ran back to back, in the order g++ 1, clang 1, g++ 2, clang 2.
Every window is **OK** (`window-summary.json`).

| | g++ 1 | g++ 2 | clang 1 | clang 2 |
|---|---|---|---|---|
| control delta (§2.6, bound 5%) | 0.36% | 0.55% | 0.25% | 0.02% |
| load1 during the window | 0.31–0.66 | 0.52–0.71 | 0.40–0.76 | 0.48–0.82 |
| `bench_fixed_read` best / median / worst | 215.3 / 216.0 / 217.4 | 215.5 / 215.8 / 219.6 | 152.3 / 156.4 / 161.9 | 149.5 / 152.2 / 154.6 |
| spread (rates, §2.3) | 1.0% | 1.9% | 6.1% | 3.3% |
| `bench_mixed_read` best / median / worst | 148.1 / 148.2 / 148.7 | 147.7 / 147.9 / 148.7 | 130.0 / 132.7 / 132.9 | 129.9 / 132.3 / 132.7 |
| spread | 0.4% | 0.7% | 2.2% | 2.1% |
| GATE identity | FAIL | FAIL | FAIL | FAIL |

- **host**: `hetzner1`, linux/amd64, AMD Ryzen 7 PRO 8700GE. Pinned to core 6 (the host has no isolated cores, so this pins without isolating).
- **revisions**: schema `8649a725d7e8509df529481e0e2c584f4485ae53`; serialize `93b8ea2a958ea3deeef9d4aff16101171ac49e15` (v1.16.2). The tree was dirty in ONE build-regenerated file, `generated/bench/paired/cpp/BenchTable.h`. The committed copy was already older than the emitter at BASE, so every C++ build rewrites it, and the board names it. This commit moves it only by the qualifier's delta.
- **flags**: `-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -DBENCH_MATCHED`. g++ adds `-Wno-class-memaccess -Wno-type-limits`. NDEBUG selects `SERIALIZE_RELEASE`, so the packet read is the packet leg's `checks=removed` build.
- **corpus**: `corpus_id(identity)` = `12224b99745dc057` in every sitting and in the exploration.
- **iterations**: 4,000,000 records per sample, both wires; 7 rounds; 1 discarded warmup + 1 measured run per round.
- **the anchor** does not compile at this revision (`std::atoi` without `<cstdlib>` in `test/bench/fixedform_measure.cpp`, outside this card's PATHS). See the read-arm card's proposed diff.

## The profile that would find the cause

`perf` is not available to the bench user on hetzner1 (`perf_event_paranoid = 4`).
The profile that would find why the identity read still loses is
`perf record -g` / `perf stat -e cycles,instructions,branch-misses`, on a bench where perf is
permitted, over:

```
build/paired/table-cpp --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus \
  --read-arm --round 0 --iterations 8000000 --packet-iterations 64
```

It should attribute cycles inside `BenchMixedFixedClampBody`, `MixedEntityFixedClampBody`
and `MixedStatFixedClampBody`, and inside `TableUtf8Valid` over `player_name`. Run it for
each compiler, with `-fopt-info-vec-missed` (g++) and `-Rpass-missed=loop-vectorize`
(clang) on the same translation unit. That shows which clamp loop each one leaves
scalar, and why the g++ `__restrict` build is slower.

## Files

- `gcc-sitting-{1,2}/`, `clang-sitting-{1,2}/`: `READ.md` (the board), `read.json`, `build.json`, `control-{start,end}-packet.csv`, `round-*-cpp-read.csv`, and `window-summary.json`. The summary records the window verdict, span and phases, and the sha256 of the full `window.json`, which is kept outside the tree.
- `exploration/`: the four-form rows from hetzner1 and mini-m5-1, the scratch emitter diff, and the two scripts.
