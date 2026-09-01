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
