# 2026-09-07, Apple M3 Ultra Studio — the final nine-language sitting

`bench/run.sh` over all nine legs on `main` at `eb3fe549`, taken after everything of
2026-09-07 had landed (#691, #692, #694, #695, #696, #697, #698, #700, #701, #702). This is
the sitting the README's table is rendered from.

CSV: [`2026-09-07-arm64-studio-ninelang-final-sitting.csv`](2026-09-07-arm64-studio-ninelang-final-sitting.csv)

## Preamble essentials

| | |
|---|---|
| date | 2026-09-07T19:07:43Z |
| machine | `arm64 studio`, Darwin 25.6.0, Apple M3 Ultra |
| build | Release |
| schema commit | `eb3fe549` (= `origin/main`) |
| corpus | `6b213fbfa1a03a99`, one 438-byte packet, 4,000,000 iterations, 7 measured runs per leg |
| C / C++ | Apple clang 21.0.0, `-O3 -DNDEBUG` |
| Go | go 1.27.1 |
| Rust | cargo/rustc 1.98.0, `--release`, opt-level 3 |
| .NET | SDK 10.0.400, Release, workstation GC |
| node | v26.7.0 (the pin), `NODE_ENV=production` |
| Java | OpenJDK 21.0.12.1 (the pin), no `-ea` |
| Dart | 3.13.2 (the pin), AOT |
| Elixir | 1.20.4 on OTP 29.0.5 (the pins) |
| pinning / noise | none / unlabelled |

Runtime checkouts, at CI's tags (`.github/workflows/ci.yml`):

| runtime | tag | commit |
|---|---|---|
| serialize | v1.16.2 | `93b8ea2` |
| serialize.c | v1.10.0 | `a742a3d` |
| serialize.go | v1.15.1 | `963f6df` |
| serialize.rs | v2.4.0 | `5e26a78` |
| serialize.cs | v1.9.1 | `1bf2b19` |
| serialize.js | v1.4.2 | `de0591c` |

All nine legs ran — no `# skipped:` line, no ABSENT row. Every leg passed its wire golden
gate (`OK (corpus_id 6b213fbfa1a03a99)` nine times).

## The rendered table

`awk -F, -v skips="" -f bench/render.awk bench/results/2026-09-07-arm64-studio-ninelang-final-sitting.csv`

```
subject: schema-GENERATED code (family gen) — what the compiler delivers; C++ = 100%
  language        %
  c            100%
  cpp          100%
  rust         166%
  java         170%
  go           228%
  cs           238%
  dart         266%
  js           288%
  elixir      1479%
  §2.8 TIE: c measured 99.4% of cpp, inside the tie band (4.5 points) — reported as a statistical tie at 100%
```

C is INSIDE the §2.8 band, so the lock's C-outside-the-band warning (PR #699) does not fire on
this sitting. A sitting carries no window verdict — no control legs, no twins, no bands — so
these ratios hold within this sitting only.

## The rows behind the render

Best (`max`) rate, `bench_mixed`, family `gen`, the js flat tier:

| leg | round_trip best | spread | write best | spread | `checks` |
|---|---:|---:|---:|---:|---|
| C | 4,630,594 | 1.79% | 9,072,866 | 8.75% | removed |
| C++ | 4,602,160 | 2.67% | 9,039,191 | 2.79% | removed |
| Rust | 2,779,604 | 1.83% | 8,430,347 | 1.56% | removed |
| Java | 2,709,171 | 1.53% | 6,776,821 | 2.30% | contract |
| Go | 2,016,309 | 0.76% | 4,046,120 | 2.72% | always |
| C# | 1,936,083 | 1.23% | 4,114,886 | 1.02% | removed |
| Dart | 1,727,886 | 3.03% | 2,950,146 | 0.45% | contract |
| JavaScript | 1,599,166 | 1.17% | 2,977,833 | 2.04% | contract |
| Elixir | 311,093 | 2.12% | 473,543 | 6.71% | contract |

Rust's and C#'s `checks` columns read `always` in the node-26 sitting and `removed` here:
that is #696 and #697, and their write legs moved +11.6% and +9.2% with it. C's round trip
moved +5.8% on #701 and #702. What moved against the node-26 sitting and the Air's, row by
row, is in [docs/PERFORMANCE.md](../../docs/PERFORMANCE.md) under "The table at the end of
the day".

`go run ./bench/tools ledger --check`: green, exit 0, no failure output (the series prints). The newest cpp round-trip
point on the (`arm64 studio`, `6b213fbfa1a03a99`) axis is 217.29 ns/msg against the previous
sitting's 218.15 — an improvement, nowhere near the gate.
