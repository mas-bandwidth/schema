# 2026-09-03 — the Dart leg against the C++ arm — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, other work on the same machine,
macOS so no `taskset`. It says the two legs agree, that the gates fire, and
roughly where the numbers stand. **It certifies nothing**, and no number here
may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core
reserved, one bench at a time, blessed per run.
`bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole command.

Raw: `2026-09-03-pairing-dart-cpp-arm64-macbook.csv` and
`2026-09-03-pairing-dart-dart-arm64-macbook.csv` — two `--only` runs,
back to back on the same box, three rounds each; `run.sh --only` takes one
leg, so the two arms were NOT interleaved. Medians below.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 2.408 | 5491.2 | 1.00 |
| dart | write | 0.437 | 997.2 | **0.18** |
| cpp | round_trip | 0.846 | 1928.0 | 1.00 |
| dart | round_trip | 0.227 | 518.5 | **0.27** |

The Dart leg is AOT-compiled (`dart compile exe`), the language's one release
configuration and the one a shipping consumer runs; the CSV's opt column says
`aot`. The C++ arm is `-O3`.

## What the leg holds before any clock starts

- the golden: variant 0 IS the pinned instance, byte for byte
- every one of the 64 variants loads, re-saves, and comes back byte-identical
  at the same length
- the reader and the writer are OWNED by the runner and re-pointed with
  `attach` per call — the shape a Dart consumer in a hot loop takes, and the
  shape `make tables-dart-alloc` holds at zero scavenges under AOT
- the read arm RESETS before it loads, inside the clock: the tolerant wire
  elides a field at its default, so a reused instance keeps the previous
  record's values otherwise, and resetting is part of a correct read into
  reused storage in every language

## Reading it

**C++ is 5.5× faster on write and 3.7× on round-trip.** Wide of "same speed",
and to be read as a pairing check. What the Dart leg pays that C++ does not:
every multi-byte scalar is assembled from bytes rather than loaded as a word,
because a `ByteData` is a second object over the same memory and the one
currency of the reader is the `Uint8List`; every bounds test is against a
`limit` the reader carries rather than a sub-view; and a `double` field's
elision comparison narrows to a float32 through a scratch view first. The
byte assembly is what leaves the read path with nothing to allocate, and
`make tables-dart-alloc` is where that is held.
