# 2026-10-16, hetzner1 — the read arm, C++: identity read versus its own packet read, and the anchor

The probe for `gates/packet-read/cpp/identity`, `gates/packet-read/cpp/plan` and
`gates/reference-cost` (BENCH-STANDARD.md §1.10, "The gate is the packet wire,
not a ratio" and "The per-leg reporting shape"). Every number below is copied
from a board the driver printed (`READ.md` in each directory), never typed.

## Verdict

| gate | verdict | numbers (best of 7 rounds) |
|---|---|---|
| `gates/packet-read/cpp/identity` | **FAIL** | identity read **215.7** ns/record vs packet read **147.7** ns/record (sitting 1); 215.8 vs 148.0 (sitting 2): the fixed read is 1.46x SLOWER |
| `gates/packet-read/cpp/plan` | **not measured** | no lineage corpus on this tree (`bench/corpus/VOLD_bench_lineage.schema`, `bench/corpus/schema.lock`, `bench/paired/corpus/bench_lineage.bin`, `bench_lineage.layout` absent; §1.10 work list items 1–7) |
| `gates/reference-cost` | **not measured at this revision** (the reference does not compile); **FAIL** in a diagnostic with the proposed fix | at the measured revision: `test/bench/fixedform_measure.cpp:517:38: error: ‘atoi’ is not a member of ‘std’`. With the fix (`anchor-diagnostic-scratch/`): identity 86.4 / straight-line 46.5 = **1.86x** against the 1.5x bound |

None of the three roadmap tasks is flipped: in the follow-on clamp-check sitting at
`bdb901be5` ([`../2026-10-16-x86_64-hetzner1-cpp-clamp-check/README.md`](../2026-10-16-x86_64-hetzner1-cpp-clamp-check/README.md)),
`<T>FixedInRange` cut the read-side clamp cost so `gates/packet-read/cpp/identity` now
**PASSES** on both compilers (g++ 13.3: **104.5 / 104.4** vs **147.9 / 148.4** ns/record,
1.42x faster; clang 18.1: **96.8 / 95.4** vs **129.9 / 129.9** ns/record, 1.36x faster),
while the plan lane (`gates/packet-read/cpp/plan`) still has no lineage corpus on this
branch and the §3.4 anchor (`gates/reference-cost`, `test/bench/fixedform_measure.cpp`
outside `PATHS`) remains red at 1.86x against the 1.5x bound.

## The sittings

Both sittings ran `bench/paired/nine.sh --read-arm --receipt ...` at
`bb445934bf801613b70ce8e17d35d3953200575d`, the commit that adds the arm, on
the same host, back to back. Each window is **OK** (`window-summary.json`):
no competing build or bench process was sampled, and the C++ packet control
leg moved 0.81% (sitting 1) and 0.46% (sitting 2) between the start and the
end of the sitting, inside §2.6's 5%.

| | sitting 1 | sitting 2 |
|---|---|---|
| window | OK, control delta 0.81% | OK, control delta 0.46% |
| load1 during the window | 0.37–0.62 | 0.46–0.65 |
| `bench_fixed_read` best / median / worst ns | 215.7 / 216.7 / 221.3 | 215.8 / 216.3 / 218.2 |
| spread (rates, §2.3) | 2.5% | 1.1% |
| `bench_mixed_read` best / median / worst ns | 147.7 / 148.0 / 148.5 | 148.0 / 148.1 / 148.4 |
| spread | 0.6% | 0.3% |
| GATE identity | FAIL | FAIL |

- **host**: `hetzner1`, linux/amd64, AMD Ryzen 7 PRO 8700GE, `c++ (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0`; unpinned (no `--lane`; this host has no isolated cores).
- **revisions**: schema `bb445934bf801613b70ce8e17d35d3953200575d`; serialize `93b8ea2a958ea3deeef9d4aff16101171ac49e15` (v1.16.2, the CI pin). The tree was dirty in ONE build-regenerated file, `generated/bench/paired/cpp/BenchTable.h`: the committed copy at this revision is older than the committed emitter, so every C++ build rewrites it; the board names it.
- **flags**: `-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -DBENCH_MATCHED` (+ `-Wno-class-memaccess -Wno-type-limits`, the driver's gcc pair). NDEBUG selects `SERIALIZE_RELEASE` in serialize.h, so the packet read here is the packet leg's own `checks=removed` build.
- **corpus**: `corpus_id(identity)` = `12224b99745dc057`, the union of the packet wire's files (`testdata/wire/bench_mixed.bin`, `bench/corpus/variants/bench_mixed.variants.bin`) and the table wire's (`bench/paired/corpus/bench_table.{bin,lengths,variants.bin}`, `bench_fixed.{bin,layout}`), because one invocation loads both. The ordinary pass's ids at this build are packet `fd0748942dc6ae09`, table `066b2822b3e5d487`; these rows divide against neither.
- **iterations**: 4,000,000 records per sample, both wires; 1 discarded warmup + 1 measured run per round (§2.4), 7 rounds, the two wires alternating which runs first each round. Every sample is above the 200 ms floor (the driver refuses one that is not).
- **bytes per record**: fixed 1264.3 (the 1244-byte record plus the layout once, over 64), packet 438. The checks axes differ and are labelled: table `contract`, packet `removed`.

## What the identity read is spending, and the profile that would confirm it

`perf` is not available to the bench user on hetzner1 (`perf_event_paranoid = 4`),
so the attribution below is a source-level decomposition, run in a scratch
harness that was NOT committed (three runs, same host, load1 < 0.5, the same
64 records out of `bench_fixed.bin`, same flags):

| what is timed, per record | ns |
|---|---:|
| the shipped `FixedTableFixedLoad` over the whole file | 222.6 – 226.2 |
| `TableFixedRun` alone (the identity plan, the shipped loop) | 76.3 – 78.4 |
| `TableFixedRun` + `TableFixedClampKnownRanges` | 76.3 – 78.3 |
| `TableFixedRun` + `TableFixedClampKnownRanges` + `FixedTableFixedClamp` | 219.0 – 223.0 |

The plan run is about a third of the read. About **143 ns of the 216** is the
read-side bounds pass, `FixedTableFixedClamp` → `BenchMixedFixedClampBody`
(docs/SPEC-TABLES.md §3.4's ranged-scalar and ordinal clamps, element by
element over `entities` and `stats`, and the text content rule), which the
packet read built `checks=removed` does not do. The anchor diagnostic agrees
from the other side: its arms A and A3 (the plan run alone) are 86–88 ns.
The identity plan's run alone would beat the packet read; the identity read
as shipped does not.

**The profile that would find the cause**: `perf record -g` (or `perf stat -e cycles,instructions,branch-misses`) over `build/paired/table-cpp --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --round 0 --iterations 8000000 --packet-iterations 64`, on a bench where perf
is permitted, attributing cycles inside `BenchMixedFixedClampBody` and
`MixedEntityFixedClampBody` / `MixedStatFixedClampBody` (per-element clamps
the compiler may not be vectorising, the bool and ordinal checks, and the
UTF-8 well-formedness walk of `player_name`).

## The anchor

At the measured revision the reference does not compile (`std::atoi` without
`<cstdlib>`), and with that fixed it segfaults: arms A and A3 call
`TableFixedRun` with six arguments, so the pooled guard chain (§5.8 row 6) is
read through a null pool. Both fixes are one file outside this card's PATHS,
`test/bench/fixedform_measure.cpp`, and are proposed, not committed:
`anchor-diagnostic-scratch/PROPOSED-fixedform_measure.diff`.

`anchor-diagnostic-scratch/` is the same driver and the same command at a
SCRATCH commit (`41296e7535aa3ee513585dcb79d857bcba050412` = `bb445934b` + that
diff, never pushed), window OK, control delta 0.40%. It is a diagnostic, not a
receipt: the revision is not on any branch.

- arm A, identity plan, shipped loop: best **86.4** ns/record (median 87.1, spread 1.7%)
- arm B, hand-written straight-line: best **46.5** ns/record (median 46.6, spread 4.1%)
- ratio best/best **1.86x**; the seven per-round ratios run 1.78–1.89x; bound 1.5x: **FAIL**
- the same sitting's identity and packet reads reproduce the two sittings above: 215.6 and 148.0 ns.

Arm A2 (the reference harness's own coalescing interpreter) runs the same plan
in 41–44 ns, faster than the straight-line arm, so the 1.86x is the shipped
loop's dispatch over its 42 entries rather than the plan's shape: the profile
above, over `TableFixedRun`/`TableFixedApply`, is the one that would say which.

## Files

- `sitting-1/`, `sitting-2/`: `READ.md` (the board as the driver printed it), `read.json` (its input, every per-round sample), `build.json` (compilers, flags, binary and corpus hashes), `control-{start,end}-packet.csv`, `round-*-cpp-read.csv` (every measured row), `window-summary.json` (the window's verdict, span and sample count, and the sha256 of the full 1.6 MB process-sample `window.json` kept outside the tree).
- `anchor-diagnostic-scratch/`: the diagnostic's `READ.md`, `read.json`, every round's raw reference output, and the proposed diff.
