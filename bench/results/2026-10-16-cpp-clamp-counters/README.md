# 2026-10-16: the C++ fixed-form clamp counters, by compiler

The read-side bounds pass (`*FixedClampBody`) counts into two `int32_t &` counters that may
alias the record's own `int32_t` fields as far as the compiler can prove. This sitting set
measures the identity read with the counters plain, `__restrict`, held in a local stored once
at the end, and both, on each compiler, and records the form each compiler now takes
(`internal/codegen/cpptable/fixedform.go`, `emitFixedClampCounterForm`).

Chosen: **clang `__restrict__` (x86-64 and arm64); GCC, MSVC and every other compiler the
plain reference.**

Corpus ids: identity `12224b99745dc057` (the read arm's two-wire id), paired table
`066b2822b3e5d487`, packet `fd0748942dc6ae09`. serialize `93b8ea2a9`. 7 rounds, 4,000,000
records per sample per wire. Best of 7 ns/record; spread is the driver's (rates).

## x86-64, hetzner1 (AMD Ryzen 7 PRO 8700GE), pinned `taskset -c 6`

Receipts (`-mode plan-read`, the read arm's own window; `sit.sh` is the exact command line):

| sitting | rev | compiler | form | window | load1 | identity read | packet read | GATE |
|---|---|---|---|---|---|---:|---:|---|
| `base-gxx` | aab858b62 (base + read-arm driver) | g++ 13.3 | plain | OK, ctl 0.07% | 1.84–2.38 | 215.3 (5.0%) | 148.0 (0.7%) | FAIL |
| `base-clang` | aab858b62 | clang 18.1.3 | plain | OK, ctl 0.05% | 1.51–1.77 | 188.6 (4.2%) | 130.0 (0.4%) | FAIL |
| `gxx-s1` | e889242c6 (this change) | g++ 13.3 | plain (chosen) | OK, ctl 0.35% | 1.10–1.17 | 216.2 (2.5%) | 148.1 (0.6%) | FAIL |
| `gxx-s2` | e889242c6 | g++ 13.3 | plain (chosen) | OK, ctl 0.44% | 1.41–1.63 | 216.4 (2.3%) | 148.1 (0.4%) | FAIL |
| `clang-s1` | e889242c6 | clang 18.1.3 | `__restrict__` (chosen) | OK, ctl 0.37% | 0.98–1.07 | 149.0 (8.5%) | 130.0 (2.5%) | FAIL |
| `clang-s2` | e889242c6 | clang 18.1.3 | `__restrict__` (chosen) | OK, ctl 0.72% | 1.06–1.09 | 148.4 (7.9%) | 130.3 (2.6%) | FAIL |

`gxx-s2` is its third try: the second was refused INVALID by the driver (a competing `go`
build at 43.5% CPU during round 3) and is not used. The boards say "dirty, build-regenerated
only: generated/bench/paired/cpp/BenchTable.h": that committed file is stale at base against
the emitter and carries only this change's delta, and the build regenerates it whole.

The form sweep that chose them (`forms-diagnostic.txt`): the same runner, built per form
through `-DTABLE_FIXED_CLAMP_FORM=N` at the scratch commit b6a44e602 that carried all four
forms, run directly (`direct.sh`, pinned core 6), each form started only when no build was
running and load1 was under 1.5. A diagnostic, not a receipt: it has no window.

| form | g++ 13.3 identity | clang 18.1.3 identity |
|---|---:|---:|
| 0 plain | **216.4** | 185.9 |
| 1 `__restrict` | 242.2 | **149.3** |
| 2 local accumulator, stored once | 310.0 | 148.2 |
| 3 both | 310.3 | 149.0 |

On clang forms 1–3 are one result within the spread (7–18% there); `__restrict` is the owner's
decision and is kept. On g++ every form is slower than plain, so GCC keeps plain.

## arm64, mini-m5-1 (Apple M5 Pro, Apple clang 21.0.0), UNPINNED

Darwin has no core affinity. Load1 about 1.1–1.9 throughout, declared per file. The driver's
window refuses every sitting on this host: a four-day-old `go version` process (PID 683, 0%
CPU) that this work did not start is on its named-competitor list, and it was left alone. So
every number here is the same runner run directly (`direct.sh`, 7 rounds, best of 7), a
diagnostic, not a receipt.

| run | rev | form | identity read | packet read |
|---|---|---|---:|---:|
| `base-direct`, `arm-f0`, `arm-f0-b` | aab858b62, then scratch | plain | 133.7 · 137.6 · 137.4 | 88.6 · 90.3 · 90.3 |
| `arm-f1`, `arm-f1-b` | scratch b6a44e602 / a7eb3e05b | `__restrict` | 88.3 · 88.7 | 88.7 · 89.5 |
| `arm-f2`, `arm-f2-b` | scratch | local | 89.8 · 91.1 | 89.4 · 90.5 |
| `arm-f3`, `arm-f3-b` | scratch | both | 90.6 · 89.8 | 89.9 · 88.8 |
| `final-arm-direct-s1` | e889242c6 | `__restrict__` (chosen) | 88.2 (3.9%) | 88.4 (4.5%) |
| `final-arm-direct-s2` | e889242c6 | `__restrict__` (chosen) | 88.0 (6.8%) | 89.0 (3.1%) |

(`base-direct` is `direct.sh` at aab858b62, before the scratch forms existed; the scratch revisions differ only by test files.)

## What it says

- **clang x86-64:** 188.6 → 149.0 / 148.4 ns/record, 1.27x faster, and still **slower than
  its packet read** (130.0 / 130.3). GATE identity FAIL.
- **g++ x86-64:** unchanged at 216.2 / 216.4, because no counter form helps it; **slower than
  its packet read** (148.1). GATE identity FAIL.
- **Apple clang arm64:** 137.4 → 88.2 / 88.0, 1.56x faster, and level with its packet read
  (88.4 / 89.0): ahead by 0.2 and 1.0 ns on best-of-7, inside the spread. Not a receipt.

The profile that would find the rest is a cycle profile of `FixedTableFixedLoad` with the
clamp split out, per compiler: `perf record -e cycles` / `perf annotate` on the g++ build of
`bench/tables/cpp/table_main.cpp --read-arm` (perf is blocked on hetzner1), paired with g++'s
`-fopt-info-vec-all -fopt-info-inline-missed` for the `*FixedClampBody` lines under plain and
`__restrict`, to say why restrict makes g++ slower and which of the clamp's loops neither
compiler vectorizes.
