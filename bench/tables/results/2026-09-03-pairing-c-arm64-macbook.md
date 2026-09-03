# 2026-09-03 — the C leg joins the tables board — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, sibling port workers building on the
same machine, macOS so no `taskset`. It says the two legs agree, that the gates
fire, and roughly where the numbers stand. **It certifies nothing**, and no
number here may be quoted as the tables layer's performance — the same rule the
C++/C# pairing board beside it carries, for the same reason.

The certifying sitting is the box under the estate's bench rules — core 15, the
server stopped, not live, one bench at a time, blessed per run.

Raw: `2026-09-03-pairing-c-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.

## The board

Seven rounds, cpp and c INTERLEAVED within each round (§2.4), so both legs see
the same load window; median of the seven per row.

| lang | path | M msg/s (median) | spread | ratio to C++ |
|---|---|---|---|---|
| cpp | write | 2.406 | 1.7% | 1.000 |
| c | write | 2.418 | 1.2% | **1.005** |
| cpp | round_trip | 0.820 | 0.8% | 1.000 |
| c | round_trip | 0.851 | 2.6% | **1.038** |

**C is at parity with C++ on the tolerant wire**, marginally ahead on both
rows. That is the ladder's "same speed, or not significantly slower", answered —
at pairing-check confidence, which is what this board is.

## The inline lever, paired (schema#343's C twin)

Identical source; the BEFORE arm defines the force-inline qualifier away on the
command line, so the two arms differ in one macro and nothing else. Seven
rounds interleaved, median of seven.

| row | before | after | ratio |
|---|---|---|---|
| write | 469,413 | 2,332,375 | **4.97x** |
| round_trip | 334,800 | 830,851 | **2.48x** |

Banked before measuring: "write 2x to 5x, round trip 1.5x to 2.5x; under 1.2x
on write would be a refutation." Both landed inside, and within a few percent of
the C++ result the lever was measured at there — the mechanism transfers whole,
because it is the same mechanism: the cursor lives in the caller's
`TableWriter` and a `uint8_t *` store may alias it, so an out-of-line put
reloads and re-stores it.

Raw: `2026-09-03-inline-c-arm64-macbook.csv`.
