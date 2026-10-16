# The read arm: Dart

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host b1 / arch linux/amd64 / AMD Ryzen 7 255 w/ Radeon 780M Graphics / rev e1d63663ed5b6d93b7bb9dd13820d856e1d59b04 (clean) / serialize none (generated AOT) / lane 6 / rounds 7
# corpus_id(identity) 0930f96c8320a75d / corpus_id(plan) 0930f96c8320a75d (one invocation, one corpus: identity, lineage and packet files)
# window: OK / control_delta_pct 0.32 (bound 5) / load1 1.00–1.04
# iterations: 4,000,000 records per sample per wire, 1 discarded warmup + 1 measured run per round (§2.1, §2.4)
# bytes_per_op: fixed 1264.3 B/record, plan (lineage file) 995.4 B/record, packet 438 B/record (§5.3 rule 2 — the division is this driver's alone)
# checks: table=contract, packet C/C++/C#=removed, packet Go=always (§1.9)
# anchor: none for Dart (SPEC-TABLES §3.4's straight-line reference, test/bench/fixedform_measure.cpp, is the C++ leg's alone)

leg      identity read    plan read    packet read    GATE identity  GATE plan    plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet    x (reported)
dart            544.4       1666.5          247.5    FAIL           FAIL         3.06
cpp                 —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
c                   —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
go                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
rust                —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
cs                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
js                  —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
java                —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
elixir              —            —              —    —              —            —     runner present, but it has no read arm yet (BENCH-STANDARD §1.10 work list items 9–15)
```

Lower is better. The ns columns are each row's BEST round (§2.2); the GATE compares best against best, per lane, never an average; plan/identity is REPORTED, never a verdict.

| row | best ns/record | median | worst | spread (rates) | mark |
|---|---:|---:|---:|---:|---|
| dart `bench_fixed_read` (identity lane, family table) | 544.4 | 546.3 | 554.7 | 1.9% |  |
| dart `bench_plan_read` (plan lane, family table) | 1666.5 | 1681.8 | 1686.2 | 1.2% |  |
| dart `bench_mixed_read` (packet read, family gen) | 247.5 | 247.9 | 250.2 | 1.1% |  |

Every row comes out of ONE `--read-arm` invocation per round, the wires rotating which runs first; none is derived from a round trip (§2.9). The identity lane's op is one record of the shipped `LOAD` of the whole 64-record file (hash check and plan run inside the clock); the packet read's op is one read stream plus the generated read of one 438-byte record. The plan lane's op is one record of the same `LOAD` over the lineage file (64 records one generation back, read through the static plan the lock's lineage entry compiled into the unit generated beside it; its gate before the clock requires 64 records read with no counter raised but widened, a poisoned destination reading to the same answer, and an answer that is not the identity corpus's).

Compiler: Dart SDK version: 3.13.2 (stable) (Tue Aug 25 01:01:12 2026 -0700) on "linux_x64". Flags: `dart compile exe (AOT, product mode: asserts off; paired unit generated beside bench/tables/dart/lineage/schema.lock)`. Window receipt: ps -A: quiet window on amd-1 (b1); pinned to CPU 6; Dart read-arm sitting 2 of 2
