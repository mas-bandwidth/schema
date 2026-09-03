# 2026-09-03 — the Elixir leg against the force-inlined C++ arm (#350) — PAIRING CHECK, not a sitting

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, other language workers building on
the same machine, macOS so no `taskset`. It says the two legs agree, that the
gates fire, and roughly where the numbers stand. **It certifies nothing**, and
no number here may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core
reserved, one bench at a time, blessed per run.
`bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole command.

Raw: `2026-09-03-pairing-elixir-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.
Five interleaved rounds (§2.4), cpp and elixir alternating so both legs see the
same load window. Medians below.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 2.362 | 5386.0 | 1.00 |
| elixir | write | 0.065 | 147.3 | **0.027** |
| cpp | round_trip | 0.809 | 1844.6 | 1.00 |
| elixir | round_trip | 0.024 | 55.4 | **0.030** |

`read` is derived (round-trip minus write) and is not a row.

## The spreads, and why the RATIO is the number that survives them

| lang | path | median | min | max | spread |
|---|---|---:|---:|---:|---:|
| cpp | write | 2,362,042 | 1,920,240 | 2,407,343 | 20.6% |
| elixir | write | 64,592 | 55,744 | 67,338 | 17.9% |
| cpp | round_trip | 808,931 | 664,816 | 814,724 | 18.5% |
| elixir | round_trip | 24,291 | 20,399 | 25,226 | 19.9% |

Every spread is around 20%, which on this machine means the sitting is not
quiet — and the absolute rates should be read as nothing more than
order-of-magnitude. **The RATIO is what reproduced.** An earlier sitting the
same evening, contaminated far worse (54.9% and 42.3% on the C++ rows), put the
same two ratios at 36.3x and 36.9x against C++'s 1.99 and 0.82 M msg/s. Two
sittings whose absolute numbers differ by 19% agreeing on the ratio to within
10% is what says the ratio is the leg's and not the machine's.

**Elixir is about 37x slower than C++ on write and about 33x on round-trip.**

## What that number is, honestly

This is the READING TIER (docs/SPEC-TABLES.md §2's backend status), and the
figure is what the language costs rather than what the port left on the floor.

- **A save allocates its own result.** There is no caller-owned buffer on the
  BEAM, so `save` builds an iolist and flattens it once. The write arm therefore
  measures the codec AND the allocation, which is what a consumer pays; no
  configuration of this language pays less.
- **A load builds a term per field**, because a decoded value is a term.

## Two candidate levers, measured and REFUTED

Both were the obvious suspect, and neither is the cost. They are written down
because a refutation is worth as much as a win and costs the next person the
same measurement:

- **`measure` inside `save`.** The reference computes every length prefix with a
  measure pass, so `save` walks each nested body twice. Measuring `TableMixed`
  takes **2.6 µs against `save_body`'s 15.4 µs** — 17%, so switching the length
  prefixes to `IO.iodata_length` would buy a fraction of that. Left alone.
- **One `Map.put` per field on the read path.** A struct update copies the
  values tuple, so a thirty-field table looked like it was paying thirty copies
  where one `struct/2` over an accumulated list would pay one. **Banked
  prediction: 2 to 4x faster. Measured: 1.8x SLOWER** — 0.53 s against 0.97 s
  for 200,000 iterations of a thirty-field struct. ERTS's flatmap update is
  already cheap and `struct/2` reduces over the list on top of building it. A
  wrong-magnitude prediction is a refutation, and this one was wrong in the
  sign.

## The lever the Elixir round does name (#174)

#174's lead hypothesis is **generation-time bit-syntax swizzling**, and it has
**no purchase on this leg**: it targets the TYPE wire's manual integer packing,
where the wire's LSB-first order inside 64-bit words fights bit syntax's
MSB-first grain. The TABLE wire is byte-oriented throughout (§3), every segment
is byte-aligned and literal-width, and the emitter already hands each field to
the ERTS bit-syntax engine as ONE construction expression. There is no
bit-level packing left to swizzle.

What does reach this leg is #174's SECOND avenue, in its own words: **"iodata
shapes revisited under literal widths."** `save` builds one iolist cell per
field and flattens once; a run of adjacent fields whose framing bytes are
literals could merge into one construction. The obstacle is named too —
ELISION makes adjacency a runtime fact (§3), so which fields are neighbours is
not knowable at generation time. That is the pass, and it is unexplored.

And #174's third: **"an honest floor statement."** This board is that, for the
table wire.

The gate the tables layer sets for a port is that the number is REPORTED, not
that it matches C++ (bench/tables/README.md, and the ladder in
docs/SPEC-TABLES.md). This is the number.
