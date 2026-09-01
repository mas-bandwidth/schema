# js READS round — ROUND-LOG

One line per unit: what landed, the measurement that justified it, the
decision taken. A future resume reads the round's state from this file plus
the branch commits.

Base: `origin/main` @ 587cca8. Board home: schema#237's named follow-on
("read-side window chunking"), plus serialize.js#10 as the paired runtime
lever (that half lives in the serialize.js clone / PR).

Standing before this round: js at **318%** of generated C++ on the canonical
`round_trip` blend (`bench/results/2026-09-01-sitting3-arm64-macbook.csv`).
Derived read stood at 2.00 M msg/s after #237 moved it 1.76 → 2.00 without
targeting it.

## The instrument, first — A/A NULL before any number is believed

A paired instrument (scratch, not committed: `pair.mjs`) times ONE path per
invocation over two arms loaded as separate ES modules, each arm's timing
loop built from its own source text so the two never share a
SharedFunctionInfo or a feedback vector. Arm order rotates by round parity.
The statistic is the max over 7 measured rounds (BENCH-STANDARD §2.2), after
two discarded warmup rounds in both orders. Every pair run passes a wire gate
first: both arms decode all 64 variants, agree field-for-field, and re-encode
byte-identically — arms that are not wire-equivalent are never timed.

**A/A null** — both arms the flat module at `origin/main`, arm B a pure
rename (`SC` → `SCX`, which also defeats V8's source-keyed compilation cache).
3 invocations per path, 800,000 iterations × 7 rounds:

| path | A/A null (B/A on the max) | band | verdict |
|---|---|---|---|
| **read** | 0.9965, 1.0087, 1.0017 | **±0.9%** | trustworthy |
| **round_trip** | 0.9999, 0.9974, 1.0005 | **±0.3%** | trustworthy |
| write | 0.9955, 0.9947, 0.9876 | **±1.3%**, biased to A | usable above ~2% only |

The write path carries a consistent ~0.6% bias toward arm A across all three
invocations — not a slot effect (rotation is in place), so a write claim on
this instrument needs to clear ~2%, and this round makes none. Read and
round_trip are far tighter than elixir #240's bands; the js pair runs are
single-path for the same reason #240 gave, and the null was measured that way.

Corroboration that the instrument measures the shipped thing: its pure-read
arm at `origin/main` prints **2.00 M msg/s**, the same figure #237's derived
read reported from the certification harness.

## The profile decided the round

`node --prof` and `node --cpu-prof` over a pure READ loop, 4,000,000
iterations of `bench_mixed`, production mode, node 26.7 pinned, M2:

- **78.9%** of ticks inside `ReadBenchMixedFlat` itself — straight-line
  window arithmetic, no calls;
- **19.0%** in V8 C++ runtime calls made from it (macOS renders them as one
  bogus nearest-symbol, exactly as #237 recorded) — BigInt traffic;
- GC **3.2–3.8%** — BigInt intermediates.

Line ticks (`--cpu-prof` `positionTicks`, 15,662 samples) put the weight
where the follow-on predicted:

| line | share | what |
|---|---|---|
| the `Stats` loop body (8-bit + 10-bit fields) | ~30% combined | two full window loads per 18-bit element, 80 elements per message |
| the `Entities` loop body (14 fields, 135 bits) | large | 14 window loads per entity |
| `value.Flux = -2^100n + bg` | 8.1% | 128-bit offset add |
| `bg \|= SC.getBigUint64(0, true) << 64n` (×2) | 10.7% | 128-bit assembly |
| `e0.Damage = BigInt(v)` | 2.0% | BigInt construction from a number |

The named candidate — read-side window chunking — is where the time is.

## Units

- **UNIT 1 — read-side window chunking (the emitter).** One 64-bit pair load
  carries exactly 32 valid bits from the cursor, so consecutive fields whose
  widths sum to ≤ 32 all extract from the SAME `out` at literal RELATIVE
  shifts — static even where the absolute cursor is dynamic, which is what
  reaches the loop bodies. `bench_mixed`: the stats element goes 2 loads → 1,
  the entity element 14 → 5.

  Prototyped first, as the round requires: a transformer over the generated
  file (scratch) rewrote the emitter's exact output shape and priced the
  ceiling at **read 1.290–1.296x, rt 1.167x** before any emitter work.

  Paired numbers for the landed emitter, 3 invocations each, 800k × 7:

  | path | before (max) | after (max) | ratio | null |
  |---|---|---|---|---|
  | **read** | 1.92–1.96 | 2.53–2.58 | **1.3151, 1.3168, 1.3152** | ±0.9% |
  | **round_trip** | 1.213 | 1.428 | **1.1765, 1.1779, 1.1808** | ±0.3% |
  | write (control) | 3.067 | 3.063 | 0.9986 | ±1.3% — untouched, as intended |

  Safety is by construction, not by inspection: while a window is open the
  cursor `br` LAGS by the bits already served, so `pf` — the single emission
  choke point — settles the window before any text that names `br` or that
  opens or closes a scope. The one deliberate exception is the value-refusal
  guard (`readRefuse`), which carries braces but only ever tests `v`/`bg` and
  only ever `return false`s: it neither reads the cursor nor lets control fall
  past it with the window stale, and it panics if its condition names `br`.
  A second lock: a window never joins across an indent change.

  **Negative control, run rather than assumed**: with the relative shift off
  by one bit, `test/js` prints **93 FAILED lines and exits 1**; restored,
  green, regenerated tree byte-identical.

  **Differential oracle** (old readers at `origin/main` vs branch readers, the
  #237 review's method): the whole pinned corpus + its 64-byte slices + seeded
  bit-flip mutations + pure random buffers, at two `numBits` each, over ten
  generated units, both NODE_ENV modes. **820,160 cases per mode, 535,734
  accepted, zero divergences** in verdict, fields (bit-exact, so −0 and NaN
  payloads count) and cross read-back.

  **The oracle needed a null too, and it changed a claim.** The re-encode leg
  is NOT deterministic across module instances: an A/A null (origin/main
  against a byte-identical copy) printed 3 re-encode divergences in one
  invocation and 0 in the next, always at float NaN sites. V8 does not promise
  to preserve a NaN's payload as a JS number travels through heap numbers,
  unboxed double fields and DataView stores, and the choice depends on JIT
  state. The re-encode leg therefore SKIPS values carrying a NaN (6,495 of
  them per mode, reported) — and with that exclusion the A/A null is a
  deterministic zero across three consecutive invocations, which is what makes
  the branch's zero mean something. Verdicts and non-NaN fields stay under the
  full comparison, and that is where a read-side change lives.

- **UNIT 2 — wide offset fields decoded in the number domain.** A fresh
  profile of the post-UNIT-1 floor put the single largest line at
  `value.Flux = -2^100n + bg` (11.1% of read ticks), with the two
  `bg |= SC.getBigUint64(0, true) << 64n` assemblies at 7.1% and the whole
  BigInt lane at ~21%. A 128-bit-domain BigInt operation is multi-digit and
  allocating; the shape cost a second `getBigUint64`, a shift by 64, an or,
  a BigInt comparison and a BigInt add.

  When a wide field is more than 64 bits and its HIGH half is at most 53 bits,
  that half is an exact Number. The range refusal then splits across the two
  halves (numeric on the high, and the low comparison only runs on the exact
  boundary), the offset is a numeric add, and exactly ONE signed 64-bit BigInt
  comes out of the scratch — a shift and an add is all that is left.

  Conditions, every one required for exactness and all checked in the emitter:
  bits in (64, 117]; `min` a nonzero multiple of 2^64, so the offset touches
  only the high half and the low 64 bits pass through with no borrow; and
  both `min`'s high half and the adjusted high half inside 2^53. Anything else
  keeps the general wide path untouched — extending it to a full-width high
  half, or to an offset with low bits, is a named follow-on.

  The high word is emitted as `(nw - nl) / 4294967296` with `nl = nw >>> 0`,
  not as a rounding function of `nw / 2^32`: `nw - nl` is an exact multiple of
  2^32, so the division is exact and there is no rounding mode in the
  argument at all.

  | path | ratio (3 invocations) | null |
  |---|---|---|
  | **read** | **1.0781, 1.0766, 1.0742** | ±0.9% |
  | **round_trip** | **1.0444, 1.0430, 1.0443** | ±0.3% |

  **Cumulative for the branch against `origin/main`: read 1.4199 / 1.4175 /
  1.4185, round_trip 1.2250 / 1.2286 / 1.2305.**

  **The negative control found a hole in the oracles, and closing it is part
  of this unit.** With the high word taken as `Math.trunc(nw / 2^32)` instead
  of the exact form — the precise bug the shape invites, wrong only when the
  adjusted high half is negative and not a multiple of 2^32 — **`test/js`
  stayed green and the buffer-mutation differential stayed green across
  189,120 cases.** Neither can reach that band: `BenchMixed`'s header carries
  a pinned magic constant, so a random or bit-flipped buffer is refused long
  before any field is read, and the pinned corpus's own `Flux` values all sit
  above zero.

  What does reach it is SEEDED INSTANCE MUTATION (the #237 review's method):
  decode a pinned instance, perturb ONE leaf across its own domain, re-encode
  through the writer this round does not touch, and run both readers on the
  bytes. One leaf at a time is the load-bearing detail — the checked writer
  refuses a whole instance for any single out-of-contract field, so mutating
  everything at once produces refusals and the deep domains never encode. On
  that oracle the `trunc` control goes red immediately on `BenchMixed.Flux`,
  and a direct probe of the band (`Flux` at -1, -2, -2^32-1, -2^64, -2^70+7,
  -2^99-12345) shows 7 of 11 values decoding wrong. Restored: green.

  Both oracles, both NODE_ENV modes, against `origin/main`'s readers:
  **152,000 single-leaf instance mutations (129,829 / 152,000 encoded) and
  263,360 buffer cases per mode, zero divergences.**

## Refusals, with numbers

Each was prototyped on the paired instrument against the same nulls, and each
is reported because a measured refusal is a deliverable.

- **Window carry** — when a window opens exactly 32 bits after the previous
  one the cursor's byte index has advanced by exactly 4, so the previous
  window's `whi` IS the new `wlo` and `s2` is unchanged: one move and one load
  instead of two loads. Prototyped over the chunked tree, 20 windows carried,
  wire gate 64/64 byte-identical. **1.0060, 1.0123, 1.0039 against a ±0.9%
  null** — two of three inside the null. NOT TAKEN: the load it saves is
  already an L1 hit, and after UNIT 1 the read path is not load-bound.

- **Small-value BigInt table** — the elixir round's decode-table lever
  (#240's lever C) applied to `e0.Damage = BigInt(v)`, an 8-bit wide field:
  a frozen 256-entry array of BigInt constants, indexed instead of
  constructed. Wire gate green. **0.9718, 0.9709, 0.9706 — measurably
  SLOWER**, consistently. NOT TAKEN, and the reason is worth carrying: V8's
  `BigInt(smallNumber)` beats a heap-array load of a shared BigInt, so the
  table lever does not transfer from the BEAM to V8. (Its ceiling, with the
  construction replaced by a literal, was 1.049–1.059 — the whole gap is the
  table's own cost.)

- **BigInt range refusals in the number domain**, on their own: ceiling
  measured with both `bg > Rn` comparisons removed outright — **1.0083,
  1.0060, inside the ±0.9% null**. A BigInt comparison allocates nothing;
  it is not where the lane's cost is. Folded into UNIT 2 where it is free,
  never pursued on its own.

- **64-bit offset fields decoded in the number domain** (`WorldTime`, 41 bits,
  raw range 2×10^12 — comfortably inside 2^53): ceiling measured with the
  refusal AND the offset add removed outright — **0.9953, 0.9995, inside the
  null**. A 64-bit BigInt is one or two digits and its ops are cheap; only
  the 128-bit domain pays. This is why UNIT 2 is gated at `bits > 64` rather
  than at "fits in 2^53".
