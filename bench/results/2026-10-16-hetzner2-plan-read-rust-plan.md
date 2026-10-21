# 2026-10-16, hetzner2 — the plan-read board, Rust leg, all three lanes

Card `rust-fixed-read-beats-its-packet-read-2` attempt 2 (roadmap tasks
`gates/packet-read/rust/identity` and `gates/packet-read/rust/plan`), measured by
`bench/paired/nine.sh --plan-read rust --pin 3`, twice, per BENCH-STANDARD.md
§1.10. This is the sitting the first one
(`2026-10-16-hetzner2-plan-read-rust.md`) could not be: the lineage corpus is on
the tree and the plan lane is MEASURED, in the same invocation as the identity
lane and the packet read.

## Verdict

| lane | GATE vs Rust's own packet read | sitting 1 | sitting 2 |
|---|---|---|---|
| identity (`bench_fixed_read`) | **PASS** | 150.0 vs 199.8 ns/record | 150.9 vs 199.3 ns/record |
| plan (`bench_plan_read`) | **FAIL** | 739.8 vs 199.8 ns/record | 741.2 vs 199.3 ns/record |
| plan/identity (reported, never judged) | — | 4.93x | 4.91x |

- **The plan lane FAILS, by a wide margin, in both sittings.** A read of the
  OLDER layout through the compiled plan costs ~740 ns/record against ~200 for
  the packet wire reading the same logical record: 3.7x the packet read, where
  the gate asks for faster. The ranges do not touch: the fastest plan round
  (739.8 ns) is 3.6x the slowest packet round (207.4 ns). No reasonable window
  moves this verdict; the load below moves all three lanes together.
- **The identity lane PASSES** in both sittings (best 150.0 / 150.9 against
  199.8 / 199.3; slowest identity round 163.4 < fastest packet round 199.3).
- **plan/identity is 4.9x**, against the ≤ 2x the standard EXPECTS (§1.10,
  "What the withdrawn 2.0x is still good for"). Per that section a leg far
  above it is "dispatching per field where the plan should have coalesced";
  the cause, below, is exactly that, and it is named rather than fixed here.
- **Window: NOISY**, both sittings, by the driver's rule: one idle foreign
  `VBCSCompiler` on every sample (39 per sitting). Declared load, which the
  driver's name list does not flag: another member's `java` at ~415% CPU on
  every one of the 78 samples (16 cores, SMT off); load1 4.54–4.76 and
  4.94–4.97. Core 3 was pinned (`taskset -c 3`), not isolated. Every row at
  normal spread (≤ 8.3%).
- **No roadmap flip.** Both tasks flip only when both lanes pass in an OK
  window; the plan lane fails and the window is NOISY. `docs/roadmap.sexp` is
  untouched and `make roadmap` was not run.

## The cause of the FAIL, and the profile that would confirm it

The plan the reader compiles for the VOLD layout (lineage index 0) has **157
entries per record**; the identity plan has **1** (one `Copy` of the body). By
op (read off the generated reader's compiled plan in a scratch build on
hetzner2, not committed):

| op | entries | source bytes |
|---|---:|---:|
| `Copy` | 78 | 816 |
| `Widen` | 64 | 128 |
| `Ordinal` | 8 | 8 |
| `Count` | 3 | 88 |
| `Const` | 3 | 3 |
| `Text` | 1 | 8 |

128 of the 157 come from the one widened array: §5.9 #33 forbids folding a run
across a widening, so each of the 64 `MixedStat` elements costs a `Widen` for
`delta` AND a separate `Copy` for `stat_id` beside it, and the 8 `Ordinal`
remaps (the enum append) split the entity block the same way. (740 − 150 ns)
/ 156 extra entries ≈ 3.8 ns per dispatched entry. That is CONSISTENT with
per-entry dispatch being the cost; it is an inference from the entry count and
the timings, not a profile. The prefill (4 holes) and the scatter are the
identity lane's too.

**The profile that would find it:** `perf record -e cycles` over
`build/paired/plan-rust --plan-read --round 1` on a host with
`kernel.perf_event_paranoid <= 2` (hetzner2 is at 4, and changing it is the
operator's), attributing cycles inside `table_fixed_run` per op arm, against
the same profile of the identity lane. The experiment it would point at is a
STRIDED widen entry (one entry for a whole widened array, element stride in
the entry) — which is a question for §5.9 #33's no-fold rule, not a runner
change, and is not attempted here.

## How the plan lane is measured

- **The corpus** (`bench/tables/rust/lineage/`): `VOLD_bench_lineage.schema`
  (BenchMixed one generation back: the six narrowings of §1.10's table),
  `schema.lock` (FixedTable's lineage: VOLD `0x0c2ce17509b80494` at index 0,
  identity `0x5f1320927e9ad910` at 1, floor 0), and the committed
  `bench_lineage.bin` (64 OLD records of 977 B, 63,708 B with its layout) and
  `bench_lineage.layout` (1,160 B).
- **The reader** is the leg's ordinary generated codec: `-mode build`
  generates `bench/corpus/Bench.schema` + `FixedTable.schema`, unchanged, a
  second time beside that lock, and builds `src/main.rs` against it with the
  `lineage` feature as `build/paired/plan-rust`. The ordinary `table-rust`
  is built exactly as before and refuses `--plan-read` by name.
- **The gate before the clock** (`gate-rust-plan-read.log`, both sittings):
  the lineage is the lock's; the committed file is re-derived from the paired
  corpus's own 64 records byte for byte; the VOLD codec reads it clean; the
  NEW reader lands every OLD value exactly, the appended `idle_ticks` and the
  bound's 16 tail slots at their declared defaults, and the counters are
  §5.4's — `widened=4096` (64 records × min(64, 80)), `clamped=0`,
  `unknown=0`, `kind_mismatch=0`; a destination poisoned with the identity
  values reads to the same answer.
- **Timed:** the whole `LOAD` of the 64-record file into one reused
  destination, prefill and scatter inside it, the same loop the identity lane
  runs over its own file; the three loops rotate their order by round.

Two things about the corpus that differ from §1.10's text, named:

- **The producer is the VOLD Rust codec, not the C++ dump** (§1.10 item 4,
  outside this card's paths). Its fixed writer is held to the C++ dump's bytes
  by `make tables-rust-fixedform`; the move of the pair to `bench/corpus/` +
  `bench/paired/corpus/` with the C++ dump as producer is still owed.
- **77 of the corpus's 512 entity weapon ordinals are variants the NEW enum
  appended** (§1.10 did not foresee this), which no OLD writer could write; the
  producer folds each onto an OLD variant (`((v − 1) mod 12) + 1`, never onto
  None) and the gate expects the fold.
- One blind spot, found by a negative control: with the prefill copy deleted
  from the generated reader the gate still passes, because the filled slots'
  declared defaults are zero and LOAD builds each record in a zeroed per-call
  image. A one-byte flip in `bench_lineage.bin` is refused (gate 3).

## Provenance

| | |
|---|---|
| host | `hetzner2`, AMD Ryzen 9 7950X3D 16-Core (16 threads, SMT off), Linux 6.8.0-139-generic, linux/amd64, pinned `taskset -c 3` (not isolated) |
| sittings (UTC) | 1: 2026-10-16T01:22:54Z–01:23:30Z · 2: 01:23:36Z–01:24:12Z (back to back) |
| measured revision | `a6befea79ea94394b83b08e0c8c39d561b3d7a2a`, clean |
| = | card commit `5ede1343e` + one measure-only commit (never pushed): `generated/bench/paired/{cpp,rust}` exactly as `-mode build` writes it (stale at base, the same defect the first sitting names) |
| base | `sprint/schema-rust` at `865ffea95324` |
| serialize (C++ bridge) / serialize.rs | `6c692bffaf63ec6020fd4423e891281321ca64b0` / `5be639fd3ca3f7417c202b98acd7ba063881f716` |
| toolchain | rustc 1.98.1 (48a229cea 2026-09-01), cargo 1.98.1, `--release`, opt-level 3 (`opt=O3` read from the build); go 1.26.6 |
| binaries (sha256) | `build/paired/plan-rust` `e3364b9036b70d41…`; `build/paired/table-rust` `7387a8c3a69b1f0e…`; corpus bridge `00fe10d37fad6363…` |
| lineage corpus (sha256) | `bench_lineage.bin` `dddb25d342657e14…`, `.layout` `e57253e45ac31d8d…`, `schema.lock` `b5f040f6d12d656c…`, `VOLD_bench_lineage.schema` `c35dfe69c9e3a6d1…` |
| corpus_id | `50d0ee5a2c62bc00` — the plan-read invocation's own (table five + packet two + lineage three, lock included), shared by all three rows, divisible against no row of the ordinary table pass |
| rounds | 7 per sitting, 1 discarded warmup + 1 measured run per row per round |
| iterations | packet 2,000,000 records per sample; table 1,704,768 (sitting 1) / 1,690,176 (sitting 2) records per sample, both table lanes at the one count, raised uniformly once from 200,000 to clear the 200 ms floor (§2.1) |
| bytes/record | identity 1264.3 B, plan 995.4 B (the OLDER layout's file / 64, layout included), packet 438 B — §5.3 rule 2: the division is this driver's alone |
| checks | table=contract, packet Rust=removed (§1.9) |
| raw rounds | `2026-10-16-hetzner2-plan-read-rust-plan.csv` beside this file |

## Sitting 1

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host hetzner2 / linux/amd64 AMD Ryzen 9 7950X3D 16-Core Processor / rev a6befea79ea94394b83b08e0c8c39d561b3d7a2a / lane taskset:3 / rounds 7 / corpus_id(identity) 50d0ee5a2c62bc00 / corpus_id(plan) 50d0ee5a2c62bc00 (one invocation: shared with the identity rows, and with no row of the ordinary table pass)
# bytes_per_op (rust): fixed 1264.3 B/record (identity), 995.4 B/record (plan: the OLDER layout's file), packet 438 B/record (§5.3 rule 2 — the division is this driver's alone)
# iterations per sample: table 1,704,768 records, packet 2,000,000 records (one count per wire, §2.1)
# checks: table=contract, packet C/C++/C#/Rust=removed, packet Go=always (§1.9)
# C++ anchor: not in this sitting (the C++ leg has no plan-read arm on this tree)
# window: NOISY: 39 foreign-process warnings · load1 4.54–4.76

leg      identity read    plan read    packet read    GATE identity  GATE plan     plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet     x (reported)
rust             150.0        739.8          199.8             PASS FAIL          4.93
cpp, c, go, cs, js, java, dart, elixir: no plan-read arm in this leg's runner on this tree
```

| leg | row | best ns/record | median | worst | spread | disposition (§2.3) |
|---|---|---:|---:|---:|---:|---|
| rust | bench_fixed_read (identity) | 150.01 | 159.03 | 162.81 | 8.3% | normal |
| rust | bench_plan_read (plan) | 739.80 | 766.77 | 778.79 | 5.2% | normal |
| rust | bench_mixed_read (packet) | 199.82 | 200.25 | 207.38 | 3.7% | normal |

## Sitting 2

```
# host hetzner2 / … / rev a6befea79ea94394b83b08e0c8c39d561b3d7a2a / lane taskset:3 / rounds 7 / corpus_id(identity) 50d0ee5a2c62bc00 / corpus_id(plan) 50d0ee5a2c62bc00
# iterations per sample: table 1,690,176 records, packet 2,000,000 records
# window: NOISY: 39 foreign-process warnings · load1 4.94–4.97

leg      identity read    plan read    packet read    GATE identity  GATE plan     plan/identity
rust             150.9        741.2          199.3             PASS FAIL          4.91
```

| leg | row | best ns/record | median | worst | spread | disposition (§2.3) |
|---|---|---:|---:|---:|---:|---|
| rust | bench_fixed_read (identity) | 150.94 | 155.11 | 163.38 | 7.8% | normal |
| rust | bench_plan_read (plan) | 741.24 | 766.84 | 773.15 | 4.3% | normal |
| rust | bench_mixed_read (packet) | 199.31 | 200.15 | 207.04 | 3.8% | normal |

Not certified: the fast machinery's sitting, not `-mode run`'s seal, on a
shared bench with no quiet window.
