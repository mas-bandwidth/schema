# 2026-08-07 — C# emitter levers: bound fold + bulk-bytes + the batch opt-in

The C# lane of the post-v4 wave: the two proven emitter levers carried to the
cs backend (C++ #8's generation-time bound fold; C++ #7's bulk-bytes over
`ir.AlignedFixedByteArrays`), then the priced item — the per-type batch
opt-in targeting serialize.cs PR #3's `WriteBatch`/`ReadBatch`.

## Configurations (paired, same session, M2 quiet)

Apple M2, macOS Darwin 25.5.0, dotnet 10.0.302 (`-c Release`, workstation
GC), serialize.cs @ `e2bda99` (main, batch runtime merged) throughout — the
runtime never changes between runs; only the generated C# does.

- **A** = baseline: schema main `bb077af` generated tree
- **B** = fold + bulk-bytes (`45e65fc`)
- **C** = fold + bulk-bytes + batch opt-in (`2315b58`)

Harness: `bench/cs`, golden + round-trip self-checks gating every run (all
passed), 1 warmup + median-of-7, zero-alloc gate green on every bench in
every configuration (the batch entry is a stack ref struct — no allocation).
Raw CSVs: `2026-08-07-cs-emit-levers-{a2,b2,c2}-arm64-m2.csv`.

An earlier A/B pair from the same morning (different session, before a
sibling lane's benchmark run loaded the machine) reproduces every A2/B2
median within ±3% — the pairing is stable. One polluted C run (load average
15 from the go lane's sweep) was discarded and re-run quiet; the numbers
below are the clean interleaved set.

## The paired table (M msg/s, medians)

| bench | path | A | B | C | B/A (levers) | C/A (total) |
|---|---|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 15.82 | 15.61 | **33.43** | 0.99 | **2.11** |
| rigidbody_moving | read | 17.20 | 17.16 | **35.79** | 1.00 | **2.08** |
| rigidbody_at_rest | write | 29.24 | 28.66 | **53.42** | 0.98 | **1.83** |
| rigidbody_at_rest | read | 33.83 | 33.63 | **57.02** | 0.99 | **1.69** |
| chat | write | 39.73 | 40.14 | 39.96 | 1.01 | 1.01 |
| chat | read | 79.93 | 79.47 | 79.94 | 0.99 | 1.00 |
| test | write | 107.51 | 109.44 | **126.06** | 1.02 | **1.17** |
| test | read | 135.57 | 135.64 | **157.87** | 1.00 | **1.16** |
| inputpacket | write | 14.22 | 14.18 | **33.55** | 1.00 | **2.36** |
| inputpacket | read | 15.49 | 15.43 | **29.00** | 1.00 | **1.87** |
| shipcreate | write | 29.61 | 28.75 | **53.29** | 0.97 | **1.80** |
| shipcreate | read | 35.59 | 35.84 | **52.30** | 1.01 | **1.47** |
| ship_shallow | write | 31.73 | 30.82 | **55.13** | 0.97 | **1.74** |
| ship_shallow | read | 36.19 | 37.51 | **54.67** | 1.04 | **1.51** |
| probe_header | write | 88.14 | 87.27 | **115.15** | 0.99 | **1.31** |
| probe_header | read | 107.71 | 109.98 | **127.69** | 1.02 | **1.19** |
| probebits | write | 58.46 | 57.78 | **88.93** | 0.99 | **1.52** † |
| probebits | read | 55.88 | 55.83 | **95.77** | 1.00 | **1.71** |
| probearray | write | 21.02 | 21.21 | **40.08** | 1.01 | **1.91** |
| probearray | read | 23.66 | 23.58 | **41.31** | 1.00 | **1.75** |
| testdata | write | 6.54 | **8.17** | **13.97** | **1.25** | **2.14** |
| testdata | read | 7.56 | **10.57** | 9.99 | **1.40** | **1.32** ‡ |
| message_batch | write | 51.34 | 55.27 | **61.67** | 1.08 | **1.20** † |
| message_batch | read | 37.60 | 39.21 | **42.87** | 1.04 | **1.14** † |

† high-spread rows (probebits write 38–60%, message_batch 22–36% — the
benches' known spreads, same binary); the medians moved far outside them
except message_batch, where the direction is consistent across four runs.
‡ the one B→C negative — see finding 3.

## Predictions vs outcomes

1. **Banked (serialize.cs #3): tiny writes toward ~1.3x** — the prototype
   emitter measured 1.285x probe_header write / 1.106x test write.
   **Confirmed and slightly exceeded by the generated emitter**: 1.31x
   probe_header write, 1.17x test write; reads beat their prototype numbers
   too (1.19x vs 1.144x, 1.16x vs 1.168x). The composition verdict on the
   1.285x: **no regression from generated composition** — the inline-only
   law held all the way down. The strongest evidence is inputpacket write at
   **2.36x**: a counted array of 16 composed `Input` cores (13 scalar ops
   each) inlined into the parent batch scope, exactly the shape the 0.71x
   hazard would have destroyed if the JIT had refused the inline.

2. **Banked (C++ #8): bits-heavy writes double digits from the fold** —
   **REFUTED for C#**. B/A is 0.97–1.04 on every row the fold touches
   (probebits write 0.99, test write 1.02). Root cause, from the runtime
   source: serialize.cs's ranged calls are `AggressiveInlining` and
   `SerializeUtil.BitsRequired` is a leading-zero-count the JIT constant-
   folds at inlined call sites with constant bounds — C++ #8's win came from
   killing an *outlined* `SerializeInteger64` with a runtime
   `bits_required64` loop, and the C# runtime never had one. The fold is
   kept: it costs nothing, it simplifies the batch cores' shape, and it
   makes the emitted code state its wire widths.

3. **The batch beyond the prototype's reach**: the pairs the prototype never
   converted are the biggest wins — rigidbody 2.1x, probearray 1.9x,
   shipcreate 1.8x, ship_shallow 1.7x, the object views throughout. One
   negative inside the win: **testdata read** is 0.95x against its
   lever-only form (10.57 → 9.99; still 1.32x vs baseline). TestData's read
   body carries two delegated byte ops (the bulk `fixed_bytes` + `text`),
   and `ReadBatch` has only three fields of state to win back per scalar —
   the sync/recapture per delegated op costs more than the read-side
   register residency returns on that mix. A per-direction density rule
   (read-side B weighted heavier) is the refinement; one data point is not
   enough to fit it, so the per-type rule ships and the finding is on the
   record.

4. **Bulk-bytes (C++ #7's lever)**: testdata write **+25%** / read **+40%**
   from the levers commit alone — the 17-byte per-byte loop dying, C++ #7's
   read story in miniature. The magnitude is smaller than C++'s (+70–80%
   read) because the array is a smaller share of C#'s slower baseline and
   `text` string trafic sits beside it.

## Where the columns land (against v4's pinned C++ numbers)

Per-bench C#-time-relative-to-C++ (C++ = 100%), medians across the 11 corpus
benches, C++ from `2026-08-07-four-language-v4.md` (same machine, same
harness; C++ main has not moved since v4):

| column | v4 | now | movement |
|---|---:|---:|---|
| write | 379% | **~201%** | probe_header 1180→905%, rigidbody_moving 379→175%, testdata 431→201% |
| read | 343% | **~219%** | probebits 906→511%, inputpacket 294→156% |
| batch write | 155% | **~134%** | tags still ride the stream (future: widen batch across the message loop) |
| batch read | 204% | **~183%** | |

The cross-session caveat applies (C++ measured at the v4 pull, C# today,
same host and harness); the next four-language pass owns the official
column update.
