# 2026-09-03 — the Go leg joins the tables bench — PAIRING CHECK, not a sitting

**SUPERSEDED by `2026-09-03-pairing-go2-arm64-macbook.md`.** The C++ arm
measured here is the pre-#350 one. #350 forced the generated C++ bodies and
the writer primitives inline and took the C++ write arm from 0.322 to 1.454 M
msg/s, so the go/cpp ratios below (0.82 write, 0.78 round-trip) describe an
arm that no longer exists. The Go numbers are unchanged and still stand.

**Read the label first.** This board is a PAIRING CHECK on a MacBook Air (M2):
an interactive desktop, no core reservation, other workers building on the same
machine, macOS so no `taskset`. It says the three legs agree, that the gates
fire, and roughly where the numbers stand. **It certifies nothing**, and no
number here may be quoted as the tables layer's performance.

The certifying sitting is the box under the estate's bench rules — core 15, the
server stopped, not live, one bench at a time, blessed per run.
`bench/tables/run.sh --rounds 7 --tag <sitting>` is the whole command.

Raw: `2026-09-03-pairing-go-arm64-macbook.csv`.
Corpus: `bench/corpus/BenchTable.schema`, `corpus_id 2f7567e1e25ba918`,
2391 wire bytes per record, 64 records.
Seven interleaved rounds (§2.4), medians below.

## The board

| lang | path | M msg/s (median) | MB/s | ratio to C++ |
|---|---|---:|---:|---:|
| cpp | write | 0.322 | 734.5 | 1.00 |
| cs | write | 0.244 | 555.4 | **0.76** |
| go | write | 0.264 | 601.4 | **0.82** |
| cpp | round_trip | 0.223 | 507.5 | 1.00 |
| cs | round_trip | 0.139 | 317.1 | **0.62** |
| go | round_trip | 0.173 | 394.3 | **0.78** |

`read` is derived (round-trip minus write) and is not a row.

**Go sits between C# and C++ on both rows**, which is where the ladder's bar
for a port puts it — *same speed, or not significantly slower*
(docs/SPEC-TABLES.md, the performance ladder). It is not a surprise and it is
explained rather than shrugged at: a profile of the Go read path over the
conformance corpus's richest fixed root spends about a fifth of its time in the
reader's `Has` — the bounds check every framing read owes before it happens,
which the format's contract requires and which C++'s compiler can hoist further
— and the rest in the same getters, the same prefill and the same per-field
bodies the C++ leg runs. There is no allocation on it at all, no interface
dispatch, and nothing is out of line: the shape is the reference's, and the gap
is the language's bounds discipline paid where the format asks for it.

The go/cpp ratio is printed across a linkage difference, as the cs/cpp one is:
cpp compiles the generated table codec inline into one translation unit and
links no runtime at all; go compiles it as ordinary package code in the leg's
binary and links none either, which the CSV records as `pkg` against C++'s
`hdr`. The `opt` column says `default` for go, because the Go compiler has one
optimization configuration and no flag to name.

**How much the machine itself moves.** An earlier pass of this tree on this
laptop put cpp write at 0.447 M msg/s against the 0.322 above — 28% on nothing
but what else was compiling at the time. That is the size of the effect the box
sitting exists to remove, and it is why nothing here is a certification.
