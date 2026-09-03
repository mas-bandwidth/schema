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
| cpp | write | 0.447 | 1020.1 | 0.1% | 1.00 |
| cs | write | 0.327 | 745.9 | 1.8% | **0.73** |
| cpp | round_trip | 0.309 | 704.4 | 0.7% | 1.00 |
| cs | round_trip | 0.206 | 469.1 | 0.4% | **0.67** |

`read` is derived (round-trip minus write) and is not a row: cpp 0.998,
cs 0.554 M msg/s.

The cs/cpp ratio is printed across a linkage difference — cpp compiles the
generated table codec inline into one translation unit and links no runtime at
all; cs compiles it into one assembly beside `serialize.cs`, which the
closure's `type` declarations need and which no line of the measured table path
enters. The tools require `--cross-linkage` to divide those rows, and the
caption above is that requirement satisfied in prose.

**How much the machine itself moves.** An earlier pass of exactly this tree, on
this laptop, put cpp write at 0.486 M msg/s against the 0.447 above — 8% on
nothing but the machine's mood, with both passes' own spreads under 5%. That
is the size of the effect the box sitting exists to remove, and it is why every
comparison below was re-taken inside this one window rather than quoted across
passes.

## The price of tolerance — one machine, one window, sequential

The reason the corpus mirrors `BenchMixed` field for field is so this
comparison is legitimate: the same shape, on the bitpacked type wire and on the
tolerant table wire. The type rows were taken immediately after the table rows,
same machine, same window, one leg at a time, and carry the same pairing-check
caveat.

| | type wire (438 B) | table wire (2391 B) | per message | per byte |
|---|---|---|---|---|
| cpp write | 7.08 M msg/s, 2956.7 MB/s | 0.447 M msg/s, 1020.1 MB/s | 15.8x | **2.90x** |
| cpp round_trip | 3.35 M msg/s, 1399.2 MB/s | 0.309 M msg/s, 704.4 MB/s | 10.8x | **1.99x** |
| cs write | 3.12 M msg/s, 1301.2 MB/s | 0.327 M msg/s, 745.9 MB/s | 9.5x | **1.74x** |
| cs round_trip | 1.50 M msg/s, 625.3 MB/s | 0.206 M msg/s, 469.1 MB/s | 7.3x | **1.33x** |

The wire itself is **5.46x fatter** (2391 vs 438 bytes) for the same declared
content, which is where most of the per-message factor comes from: ids, kinds
and lengths on every field, and an enum riding as a 16-bit name hash instead of
a 4-bit dense index. The per-BYTE column is the codec's own cost, and it is the
column the ladder's *"as close to its equivalent type as the neutral wire
allows"* is about.

## Three judgments, for the owner's sitting to confirm or kill

1. **C++'s write path pays 2.90x per byte and C#'s pays 1.74x.** The
   per-message factors are the wire's; the per-byte ones are the codec's. C++
   losing more than C# on the same move is the shape of a C++-side finding
   rather than a wire-side one — the C++ type writer is the fastest thing in
   the estate, so it has the most to lose, but 2.90x is worth an explanation
   before the ladder's fixed-table clause is called satisfied.

2. **C# sits closer to C++ on tables than on types** — 0.73/0.67 here against
   0.44/0.45 on the type rows, same machine, same window. Consistent with a
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
emitter, in its own change: schema#342.
