# 2026-10-16, hetzner2 — the plan-read board, Rust leg

> **Superseded for the plan lane** by `2026-10-16-hetzner2-plan-read-rust-plan.md`:
> the lineage corpus landed and all three lanes were measured in one invocation
> (identity PASS, plan FAIL). This file stays as the first sitting's record.

Card `rust-fixed-read-beats-its-packet-read-2` (roadmap tasks
`gates/packet-read/rust/identity` and `gates/packet-read/rust/plan`), measured by
`bench/paired/nine.sh --plan-read rust`, twice, per BENCH-STANDARD.md §1.10.

## Verdict

| lane | GATE vs Rust's own packet read | sitting 1 | sitting 2 |
|---|---|---|---|
| identity (`bench_fixed_read`) | **PASS** | 138.4 vs 192.4 ns/record | 139.2 vs 192.5 ns/record |
| plan (`bench_plan_read`) | **NOT MEASURED** | no lineage corpus on this tree | same |

- The identity lane reads a record in ~0.72x the time the packet wire takes to
  read the same logical record (best against best; medians 140.3 vs 193.3 and
  140.4 vs 194.2). The ranges do not overlap in either sitting: the slowest
  identity round (144.3 ns) is faster than the fastest packet round (192.4 ns).
- The plan lane cannot be measured on this tree: §1.10's lineage corpus
  (work-list items 1-6: `bench/corpus/VOLD_bench_lineage.schema`,
  `bench/corpus/schema.lock`, the C++ dump's `bench/paired/corpus/bench_lineage.bin`
  and `.layout`) has not landed on any branch, so there is no older-layout file
  for `LOAD` to select a lineage entry with. The runner refuses if those files
  appear without a plan path, so that change has to finish this leg too.
- **Window: NOISY**, both sittings, by the driver's own rule (any foreign-process
  sample). The only foreign process sampled was one idle `VBCSCompiler`
  (dotnet's compile server, another member's, 1.5% CPU) on every sample;
  load1 6.2–7.0 (sitting 1) and 4.7–4.9 (sitting 2) on 32 hardware threads;
  every row at normal spread (≤ 4.3%).
- **No roadmap flip.** The card flips both tasks only when both lanes pass in a
  valid window; the plan lane is unmeasured and the window is not OK.

## Provenance

| | |
|---|---|
| host | `hetzner2`, AMD Ryzen 9 7950X3D 16-Core (32 threads), Linux 6.8.0-139-generic, linux/amd64, unpinned (no lane) |
| sittings (UTC) | 1: 2026-10-16T00:18:44Z–00:19:00Z · 2: 2026-10-16T00:19:26Z–00:19:41Z |
| measured revision | `70d9004d3fe9df02ec7bb88b0b7af23f20e1462f`, clean (`build.json` dirty=false) |
| = | card commit `174e6950e` + one measure-only commit: `generated/bench/paired/{cpp,rust}` exactly as `schema generate` writes it (below) |
| base | `sprint/schema-rust` at `865ffea95324` |
| serialize (C++ bridge) | `6c692bffaf63ec6020fd4423e891281321ca64b0` |
| serialize.rs | `5be639fd3ca3f7417c202b98acd7ba063881f716` |
| toolchain | rustc 1.98.1 (48a229cea 2026-09-01), cargo 1.98.1, `--release`, opt-level 3 (`opt=O3` read from the build); go 1.26.6 |
| binary | `build/paired/table-rust` sha256 `34ab423b…5c4aef`; corpus bridge `00fe10d3…1c793c` |
| corpus gate | `build/paired/corpus verify`: 64 identical logical records, packet 438 B, fixed 64 × 1244 B + 1279 B layout |
| corpus_id | `12224b99745dc057` — the plan-read invocation's own (table five files + packet two), shared by both rows; table pass `066b2822b3e5d487`, packet `fd0748942dc6ae09` |
| rounds | 7 interleaved rounds per sitting, 1 discarded warmup + 1 measured run per row per round, loop order alternating by round |
| iterations | packet 2,000,000 records per sample; table 1,805,632 (sitting 1) / 1,799,936 (sitting 2) records per sample, raised uniformly once from 200,000 to clear the 200 ms floor (§2.1) |
| bytes/record | fixed 1264.3 B (file / 64, layout included), packet 438 B — the division is this driver's alone (§5.3 rule 2) |
| checks | table=contract, packet Rust=removed (§1.9) |
| raw rounds | `2026-10-16-hetzner2-plan-read-rust.csv` beside this file (every measured round, both sittings) |

**Why a measure-only commit.** At `865ffea95324` the committed
`generated/bench/paired/` output is stale against the compiler: `-mode build`
regenerates it (BenchTable.h, FixedTableTable.h, rust `bench_fixed.rs`,
`fixed_runtime.rs`, `lib.rs`, new `message_runtime.rs`, +963/-110), which
dirties the tree, and both `-mode table` and `-mode plan-read` refuse a dirty
checkpoint. The measured code is therefore the current compiler's output at the
card's commit; anyone running `-mode build` at `174e6950e` gets those bytes.
Refreshing the committed generated tree is outside this card's PATHS and is
named in its report.

## Sitting 1

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host hetzner2 / linux/amd64 AMD Ryzen 9 7950X3D 16-Core Processor / rev 70d9004d3fe9df02ec7bb88b0b7af23f20e1462f / lane none / rounds 7 / corpus_id(identity) 12224b99745dc057 / corpus_id(plan) — (plan lane not measured)
# bytes_per_op (rust): fixed 1264.3 B/record, packet 438 B/record (§5.3 rule 2 — the division is this driver's alone)
# iterations per sample: table 1,805,632 records, packet 2,000,000 records (one count per wire, §2.1)
# checks: table=contract, packet C/C++/C#/Rust=removed, packet Go=always (§1.9)
# C++ anchor: not in this sitting (the C++ leg has no plan-read arm on this tree)
# window: NOISY: 32 foreign-process warnings · load1 6.21–7.03

leg      identity read    plan read    packet read    GATE identity  GATE plan     plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet     x (reported)
cpp                  —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
c                    —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
go                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
rust             138.4            —          192.4             PASS NOT MEASURED  —
cs                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
js                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
java                 —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
dart                 —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
elixir               —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
```

| leg | row | best ns/record | median | worst | spread | disposition (§2.3) |
|---|---|---:|---:|---:|---:|---|
| rust | bench_fixed_read (identity) | 138.36 | 140.29 | 144.30 | 4.2% | normal |
| rust | bench_plan_read (plan) | — | — | — | — | not measured: no lineage corpus on this tree (bench/corpus/VOLD_bench_lineage.schema, bench/corpus/schema.lock, bench/paired/corpus/bench_lineage.{bin,layout}: BENCH-STANDARD.md §1.10 work list items 1-6) |
| rust | bench_mixed_read (packet) | 192.40 | 193.28 | 195.42 | 1.6% | normal |

## Sitting 2

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host hetzner2 / linux/amd64 AMD Ryzen 9 7950X3D 16-Core Processor / rev 70d9004d3fe9df02ec7bb88b0b7af23f20e1462f / lane none / rounds 7 / corpus_id(identity) 12224b99745dc057 / corpus_id(plan) — (plan lane not measured)
# bytes_per_op (rust): fixed 1264.3 B/record, packet 438 B/record (§5.3 rule 2 — the division is this driver's alone)
# iterations per sample: table 1,799,936 records, packet 2,000,000 records (one count per wire, §2.1)
# checks: table=contract, packet C/C++/C#/Rust=removed, packet Go=always (§1.9)
# C++ anchor: not in this sitting (the C++ leg has no plan-read arm on this tree)
# window: NOISY: 32 foreign-process warnings · load1 4.67–4.87

leg      identity read    plan read    packet read    GATE identity  GATE plan     plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet     x (reported)
cpp                  —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
c                    —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
go                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
rust             139.2            —          192.5             PASS NOT MEASURED  —
cs                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
js                   —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
java                 —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
dart                 —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
elixir               —            —              —                — —             —     no plan-read arm in this leg's runner on this tree
```

| leg | row | best ns/record | median | worst | spread | disposition (§2.3) |
|---|---|---:|---:|---:|---:|---|
| rust | bench_fixed_read (identity) | 139.19 | 140.41 | 143.88 | 3.3% | normal |
| rust | bench_plan_read (plan) | — | — | — | — | not measured: no lineage corpus on this tree (bench/corpus/VOLD_bench_lineage.schema, bench/corpus/schema.lock, bench/paired/corpus/bench_lineage.{bin,layout}: BENCH-STANDARD.md §1.10 work list items 1-6) |
| rust | bench_mixed_read (packet) | 192.52 | 194.16 | 201.13 | 4.3% | normal |

## What was not measured, and what would be

- **The plan lane.** Needs §1.10 items 1-7 (VOLD schema, lock, C++ dump of the
  lineage pair and its pin, the reference's NEW-READS-OLD counters case), then
  this runner's plan path (item 11): `LOAD` of `bench_lineage.bin` through the
  compiled plan with the pre-clock counters gate (80 `widened` per record),
  prefill and scatter on the clock.
- **The C++ anchor line** (identity / straight-line, SPEC-TABLES §3.4) is C++'s
  and has no plan-read arm on this tree.
- **A quiet window.** hetzner2 is a shared bench; an OK verdict needs a sitting
  with no foreign process sampled.
