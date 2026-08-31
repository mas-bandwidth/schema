# The two-harness-contract "3x" decomposed — 2026-08-30, M2 MacBook Air

Issue #170 forensics. The claim under test: schema's two bench harness
contracts (the run.sh BENCH-STANDARD legs vs the serialize-family tight
loops the dart/java/elixir runners shipped with) produce ~3x different
numbers on identical shapes — run.sh's C++ bench_packet at 93 write / 130
read M msgs/s against a scratch generated-C++ harness at 314-416 on the
same shape and machine.

**Verdict: the "3x" was never the harness. It is the SUBJECT.** run.sh's
`bench_packet` rows are family `rt` — the hand-written serialize runtime
API. The scratch harness (and the java/dart/elixir runners) measured the
GENERATED monomorphic codec for the same shape. Measured under one
instrument with everything else held equal, the subject term is 2.1-3.4x;
the harness-contract term is 0-22% depending on path. The numbers were
never wrong — the contracts (and above all the subjects) were unlabeled.

## Instrument

A one-off scratch harness, not retained in the tree: one binary,
bench_packet (49 B), two subjects x two contracts, everything else
identical — same LCG variation, same 64 variant read buffers, same pinned
instance, same flags. It hand-coded its shape and wire size and embedded
the rt subject verbatim, so it could only ever describe the corpus of the
day it was run; this write-up is the durable record, and all profiling
now happens inside the bench.

- subject `gen` = the generated C++ codec (`generated/bench/cpp/BenchWire.h`)
- subject `rt`  = the hand-written runtime-API packet
  (`bench/cpp/bench_main.cpp`'s `RtBenchPacket`, verbatim)
- contract A = BENCH-STANDARD style: 1 warmup + 7 measured runs, 32M
  iters, noinline loop symbols, asm escape barriers, variant stride 4160,
  median-of-7 (max printed beside)
- contract B = serialize-family style: 5 trials best-of, 1M iters, tight
  loops, `sink +=` consumption, packed 256 B variants

Consumption discipline, stated with the numbers (two artifacts of this
class died this week): every write's return feeds the sink; every read
decodes into a hoisted target that is escape-barriered (A) or has a field
drained into the sink (B); the pinned instance is golden-gated and all 64
variants length-checked before any timing.

Built at -O3 -DNDEBUG -DSERIALIZE_RELEASE -std=c++17 -ffp-contract=off
-fno-rtti and run caffeinated.

## Raw output (one sitting, Apple M2, Apple clang 21.0.0, unpinned)

    gen: bytes_per_op 49
    gen A(std,32M,noinline,escape) write raw: 296.9 298.8 299.2 299.1 294.1 292.8 293.6  median 296.9 max 299.2 M/s
    gen A(std,32M,noinline,escape) read  raw: 285.3 283.8 283.6 285.2 282.6 207.1 255.2  median 283.6 max 285.3 M/s
    gen B(family,1000K,tight,sink) write raw: 312.1 316.4 265.9 312.5 257.3  best 316.4 M/s
    gen B(family,1000K,tight,sink) read  raw: 259.3 284.3 268.5 293.5 282.1  best 293.5 M/s
    gen B-at-32M(tight,sink,best5): write best 290.7 M/s  read best 285.3 M/s
    rt : bytes_per_op 49
    rt  A(std,32M,noinline,escape) write raw: 94.3 93.9 93.9 94.0 94.0 94.1 94.1  median 94.0 max 94.3 M/s
    rt  A(std,32M,noinline,escape) read  raw: 113.9 113.0 118.1 116.3 121.5 115.4 112.8  median 115.4 max 121.5 M/s
    rt  B(family,1000K,tight,sink) write raw: 93.7 94.1 92.3 94.2 93.8  best 94.2 M/s
    rt  B(family,1000K,tight,sink) read  raw: 140.1 140.5 137.4 138.6 138.3  best 140.5 M/s
    rt  B-at-32M(tight,sink,best5): write best 94.2 M/s  read best 137.9 M/s

External anchor: the run.sh cpp leg's rt bench_packet rows on this machine
(2026-08-18 sixlang quick pass) sit at 92.7 write / 137.2 read — the rt
row under contract A above reproduces the write and brackets the read.

## Factor -> contribution (bench_packet)

| factor (one toggle at a time)                       | write        | read        |
|-----------------------------------------------------|--------------|-------------|
| **SUBJECT: generated codec vs runtime API** (contract held) | **3.16x (A) / 3.36x (B)** | **2.46x (A) / 2.09x (B)** |
| harness contract, whole (B vs A, subject held, gen)  | +6.6%        | +3.5%       |
| harness contract, whole (B vs A, subject held, rt)   | +0.2%        | +21.8%      |
| ... of which tight-loop + sink consumption (rt, B-at-32M best 137.9 vs A max 121.5) | ~0% | ~+13% |
| ... of which statistic best-vs-median (rt A: median 115.4 -> max 121.5) | +0.3% | +5.3% |
| ... of which iteration count 1M vs 32M (rt B: 140.5 vs 137.9) | -0.1% | +1.9% |
| cross-language round interleaving                    | not isolated (bounded by the whole-contract deltas above) | |

The rt READ path is the only place the harness contract matters at all,
and it is the consumption discipline plus the statistic — the escape
barrier that observes every decoded field costs ~13% against a
single-field sink drain, and best-of-N adds ~5% over median on this
machine's noise. The rt WRITE path does not move at all: the runtime API's
write cost dominates every harness term.

## Consequence

- "Generated Java 127.7/131.1 beats run.sh C++ 93/130" compared Java's
  generated codec against C++'s hand-written runtime API. Same-subject,
  same-instrument: generated C++ is ~297/284 — 2.2x/2.1x ahead of
  generated Java. The artifact class dies by labeling, which is what the
  family/codec columns now do: the java/dart/elixir legs and the js flat
  Bench rows carry family=gen and refuse ratios against rt rows.
- A generated-codec Bench leg in the C++/C runners (so the sweep itself
  carries the same-subject column) is the named follow-on.

Machine caveat: M2 MacBook Air, macOS, unpinned by construction (§7),
agents quiesced but not certified; ratios within one sitting only.
