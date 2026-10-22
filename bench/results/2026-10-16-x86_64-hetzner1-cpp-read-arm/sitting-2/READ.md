# The read arm: C++

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host hetzner1 / arch linux/amd64 / AMD Ryzen 7 PRO 8700GE w/ Radeon 780M Graphics / rev bb445934bf801613b70ce8e17d35d3953200575d (dirty, build-regenerated only: generated/bench/paired/cpp/BenchTable.h) / serialize 93b8ea2a958ea3deeef9d4aff16101171ac49e15 / lane none / rounds 7
# corpus_id(identity) 12224b99745dc057 / corpus_id(plan) — (no lineage corpus on this tree: bench/corpus/VOLD_bench_lineage.schema, bench/corpus/schema.lock, bench/paired/corpus/bench_lineage.bin, bench/paired/corpus/bench_lineage.layout absent (BENCH-STANDARD §1.10 work list items 1–7))
# window: OK / control_delta_pct 0.46 (bound 5) / load1 0.46–0.65
# iterations: 4,000,000 records per sample per wire, 1 discarded warmup + 1 measured run per round (§2.1, §2.4)
# bytes_per_op: fixed 1264.3 B/record, packet 438 B/record (§5.3 rule 2 — the division is this driver's alone)
# checks: table=contract, packet C/C++/C#=removed, packet Go=always (§1.9)
# C++ anchor: — test/bench/fixedform_measure.cpp does not compile: test/bench/fixedform_measure.cpp:517:38: error: ‘atoi’ is not a member of ‘std’; did you mean ‘atoi’?  (bound 1.5x, SPEC-TABLES §3.4)

leg      identity read    plan read    packet read    GATE identity  GATE plan    plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet    x (reported)
cpp             215.8            —          148.0    FAIL           — (absent)   —
c                   —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
go                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
rust                —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
cs                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
js                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
java                —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
dart                —            —              —    —              —            —     no leg in this driver yet (bench/tables/dart is not built, generated or measured here)
elixir              —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
```

Lower is better. The ns columns are each row's BEST round (§2.2); the GATE compares best against best, per lane, never an average. The plan lane and plan/identity are absent, not estimated: no lineage corpus on this tree: bench/corpus/VOLD_bench_lineage.schema, bench/corpus/schema.lock, bench/paired/corpus/bench_lineage.bin, bench/paired/corpus/bench_lineage.layout absent (BENCH-STANDARD §1.10 work list items 1–7).

| row | best ns/record | median | worst | spread (rates) | mark |
|---|---:|---:|---:|---:|---|
| cpp `bench_fixed_read` (identity lane, family table) | 215.8 | 216.3 | 218.2 | 1.1% |  |
| cpp `bench_mixed_read` (packet read, family gen) | 148.0 | 148.1 | 148.4 | 0.3% |  |

Both GATE rows come out of ONE `--read-arm` invocation per round, the two wires alternating which runs first; neither is derived from a round trip (§2.9). The identity lane's op is one record of the shipped `LOAD` of the whole 64-record file (hash check and plan run inside the clock); the packet read's op is one `ReadStream` plus the generated read of one 438-byte record. The anchor is the reference's own process (arms A and B side by side, its best of 2 per round at 100,000 rotations of the 64 records), compiled with the C++ legs' flag line, and its ratio is best A over best B across the rounds.

The anchor did not run: `test/bench/fixedform_measure.cpp` does not compile: test/bench/fixedform_measure.cpp:517:38: error: ‘atoi’ is not a member of ‘std’; did you mean ‘atoi’?. No ratio is printed for it and none is estimated.

Compiler: c++ (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0. Flags: `-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -DBENCH_MATCHED`. Window receipt: unattended sitting by Rowan worker (card cpp-fixed-read-beats-its-packet-read-and-holds-the-reference-bound.w1) on hetzner1; no human READY/START coordination; process sampling and C++ packet control legs are the only window evidence
