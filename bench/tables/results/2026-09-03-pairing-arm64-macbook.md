# 2026-09-03 — the tables bench opens, C++ and C# — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, four other workers building on the
same machine, macOS so no `taskset`. It says the two legs agree, that the gates
fire, and roughly where the numbers stand. **It certifies nothing**, and no
number here may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core 15, the
server stopped, not live, one bench at a time, blessed per run. The target is
ready for it: `bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole
command, and it needs no argument this board does not already record.

Raw: `2026-09-03-pairing-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.

## The board

| lang | path | M msg/s (median) | MB/s | spread | ratio to C++ |
|---|---|---:|---:|---:|---:|
| cpp | write | 0.486 | 1108.9 | 2.2% | 1.00 |
| cs | write | 0.339 | 772.8 | **15.6%** | **0.70** |
| cpp | round_trip | 0.324 | 738.6 | 2.7% | 1.00 |
| cs | round_trip | 0.218 | 498.0 | 4.9% | **0.67** |

`read` is derived (round-trip minus write) and is not a row: cpp 0.970,
cs 0.614 M msg/s.

**The cs write row is over §2.3's 15% noise threshold** and is left standing
rather than re-rolled: on a shared interactive laptop that is the expected
shape, and re-running a pass until a spread looks acceptable is how a board
stops meaning anything. The box sitting is where that row gets a number.

The cs/cpp ratio is printed across a linkage difference — cpp compiles the
generated table codec inline into one translation unit and links no runtime at
all; cs compiles it into one assembly beside `serialize.cs`, which the
closure's `type` declarations need and which no line of the measured table path
enters. The tools require `--cross-linkage` to divide those rows, and the
caption above is that requirement satisfied in prose.

## The price of tolerance, measured on one machine in one session

The reason the corpus mirrors `BenchMixed` field for field is so this
comparison is legitimate: the same shape, on the bitpacked type wire and on
the tolerant table wire. The type numbers below were taken on the same
machine, in the same session, sequentially, and carry the same
pairing-check caveat.

| | type wire (438 B) | table wire (2391 B) | per message | per byte |
|---|---|---|---|---|
| cpp write | 7.50 M msg/s, 3131.9 MB/s | 0.486 M msg/s, 1108.9 MB/s | 15.4x | **2.82x** |
| cpp round_trip | 3.57 M msg/s, 1492.0 MB/s | 0.324 M msg/s, 738.6 MB/s | 11.0x | **2.02x** |
| cs write | 3.33 M msg/s, 1391.0 MB/s | 0.339 M msg/s, 772.8 MB/s | 9.8x | **1.80x** |
| cs round_trip | 1.60 M msg/s, 667.0 MB/s | 0.218 M msg/s, 498.0 MB/s | 7.3x | **1.34x** |

The wire itself is **5.46x fatter** (2391 vs 438 bytes) for the same declared
content, which is where most of the per-message factor comes from: ids, kinds
and lengths on every field, and an enum riding as a 16-bit name hash instead of
a 4-bit dense index. The per-BYTE column is the codec's own cost, and it is the
column the ladder's *"as close to its equivalent type as the neutral wire
allows"* is about.

## Three judgments, for the owner's sitting to confirm or kill

1. **C++'s write path pays 2.82x per byte and C#'s pays 1.80x.** The
   per-message factors are the wire's; the per-byte ones are the codec's. C++
   losing more than C# on the same move is the shape of a C++-side finding
   rather than a wire-side one — the C++ type writer is the fastest thing in
   the estate, so it has the most to lose, but 2.82x is worth an explanation
   before the ladder's fixed-table clause is called satisfied.

2. **C# sits closer to C++ on tables than on types** — 0.70/0.67 here against
   0.44/0.45 on the type board, same machine, same session. Consistent with a
   byte-oriented codec being less sensitive to the language than a bitpacked
   one is. It is a good sign for the port bar and it is not yet evidence.

3. **The round-trip arm carries a `Reset` per iteration in both legs**, and it
   is inside the clock deliberately (see the README). For `TableMixed` that
   reset walks 8 nested records and 80 more, so it is not free, and the derived
   read number carries it. If the box sitting shows the read arm dominated by
   reset rather than by decode, the finding is about `Reset`'s generated form
   and not about the wire.

## What the run proved beyond the numbers

- Both legs pass the §1.5 gate: variant 0 byte-identical to the golden, all 64
  variants load / re-save / compare byte-identical at the same length.
- Both legs report the same `corpus_id`, so they measured the same bytes.
- The producer's record-length refusal held on a corpus of 64 records with
  every field varied — the elision hazard the tolerant wire creates is
  contained by construction and checked mechanically.

## One defect the corpus found on the way in

Writing a table field as `uint64 | min = 0, max = 18446744073709551615` — a
bound spanning the whole storage width — makes the C++ table emitter write
`decoded_v < 0ull` and `decoded_v > 18446744073709551615ull`. Both are
tautological and g++ rejects them under `-Wtype-limits`; clang does not warn,
so it reads clean on this machine and reds on CI. The emitter ALREADY elides a
`bits(N)` width clamp when N is the storage width, so the rule it needs is one
it already has, applied to declared bounds. The corpus does not carry the
spelling (the schema says why at the field), and the fix belongs to the
emitter, in its own change.
