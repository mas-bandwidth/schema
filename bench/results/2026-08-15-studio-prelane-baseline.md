# 2026-08-15 — the Studio pre-lane baseline (first pass on the M3 Ultra)

**What this is**: the published cross-language re-baseline the overnight campaign moved to
this machine, run against the **merged mains** of the runtime repos — the state the night's
fix PRs left them in, BEFORE the morning's follow-up lanes (FMA/contraction, the C name
registry, the rounding rule) land anywhere. Label it the way the coordinator asked:
**pre-lane Studio baseline, explicitly.** The post-lane pass is a different sitting and a
different table.

Mac Studio, Apple M3 Ultra, arm64, Darwin 25.5.0, unpinned (macOS has no pinning — §7).
Harness: schema `60dfac0` (the provenance-guard + stride/hot-cold era). Runtime pins, every
one `[build-verified]` against fresh shallow clones of main (§3.5):

| runtime | commit | note |
|---|---|---|
| serialize (C++) | `ae78867` (main) | the write-spine PR merged |
| serialize.c | `bdb2234` (main) | the read-spine PR merged; `linkage=hdr+tu`, `checks=contract` |
| serialize.go | `a887f89` (main) | degenerate-range fix merged |
| serialize.rs | `bbfe1de` (main) | reads-inline-end-to-end PR merged |
| serialize.cs | `d3d34c9` (main) | **leg skipped — no dotnet SDK on this bench**; recorded `[UNVERIFIED — leg cannot run]` |

Raw CSVs: `2026-08-15-arm64-studio-O3-pass.csv` and `2026-08-15-arm64-studio-O2-pass.csv`,
with their per-symbol `.inline` ledgers beside them.

## Window evidence (§2.6)

Both passes are full interleaved driver passes: control leg A, 7 rounds × every language,
control leg B, automatic load capture.

| pass | langs | control Δ | window | spread discipline |
|---|---|---:|---|---|
| O3 | cpp c go rust (cs skipped) | **1.5%** | **OK** | 136 rows, zero noisy, zero invalid |
| O2 | cpp c go (cs skipped) | **2.1%** | **OK** | 102 rows, 2 noisy (`cpp inputpacket write` 16.4%, `cpp bench_bits write` 15.1% — auto-excluded from corpus medians), zero invalid |

The launchd estate stayed live and untouched throughout (nova-sweep woke 08:33 AEST inside
the O3 rounds; a sibling session's dotnet builds ran during the O2 rounds). The driver's
quiet-window gate paused legs while sibling build daemons were present, every round
boundary recorded load and foreign-process counts, and the control legs judged both
windows clean. Measured rounds were timed between the estate's calendar wakes; nothing was
disabled or unloaded.

## THE RELATIVE TABLE — O3 (time relative to C++, higher is slower; §2.2 best-rate)

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | **158%** | **149%** | **126%** | **68%** |
| Rust | **143%** | **166%** | **108%** | **166%** |
| Go | **357%** | **454%** | **319%** | **239%** |

Tool captions (printed by `rel --label-checks --cross-linkage`, reproduced verbatim):

- cpp checks=removed (debug asserts and bounds/range checks compile out) vs c checks=contract (debug asserts compile out; wire/API contract validation stays in every build) — this ratio includes the cost of a different safety contract.
- cpp checks=removed (debug asserts and bounds/range checks compile out) vs go checks=always (bounds, range and sticky-error checks in every build by contract) — this ratio includes the cost of a different safety contract.
- cpp checks=removed (debug asserts and bounds/range checks compile out) vs rust checks=always (bounds, range and sticky-error checks in every build by contract) — this ratio includes the cost of a different safety contract.
- cpp linkage=hdr vs c linkage=hdr+tu — this ratio includes the runtimes' packaging difference.
- cpp linkage=hdr vs go linkage=pkg — this ratio includes the runtimes' packaging difference.
- cpp linkage=hdr vs rust linkage=crate — this ratio includes the runtimes' packaging difference.
- cpp opt=O3 vs go opt=default — a language without an optimization-level knob rides in every level's table (§3.3).

## THE RELATIVE TABLE — O2 (same shape; Rust absent by construction)

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | **158%** | **148%** | **128%** | **73%** |
| Go | **350%** | **431%** | **309%** | **223%** |

Same caption set as above with `opt=O2` on the cpp side. Rust has exactly one release
build (`cargo --release`, opt-level 3), so its rows carry an explicit `O3` and §5.3 rule 4
correctly refuses them a seat in an O2 table — absence, not omission.

**§3.3 both-levels check**: for every language pair measured at both levels (C++/C, C++/Go,
C/Go), the ranking is identical at O2 and O3 in all four columns — including C beating C++
on batch read at both levels. No pairwise ranking differs, so a single ranking is
publishable; both tables are published anyway because both passes exist and absolutes only
mean anything within their own pass (§7).

## Family `rt` — where the runtimes now stand on hand-written reads and writes (O3, best M msg/s)

| bench | path | C++ | C | Rust | Go |
|---|---|---:|---:|---:|---:|
| bench_packet | write | 108.5 | 85.5 | 93.0 | 32.3 |
| bench_packet | read | 142.4 | 123.0 | 120.7 | 44.6 |
| bench_ints | write | 231.9 | 140.6 | 143.5 | 55.0 |
| bench_ints | read | 248.9 | 164.0 | 139.9 | 52.0 |
| bench_bits | write | 232.5 | 140.3 | 140.6 | 72.0 |
| bench_bits | read | 301.2 | 214.0 | 153.5 | 73.2 |
| bench_mixed | write | 191.4 | 116.2 | 126.1 | 51.7 |
| bench_mixed | read | 236.6 | 171.9 | 124.0 | 51.2 |

The night's story, visible in the inline column rather than inferred: **C's rt chains are
`full`** on every leg except bench_packet write (`partial:1` — the same single hot call
C++'s own bench_packet write carries), which is the read-spine/hot-spine work landing.
**Rust's rt chains are all `full`** — the read-path fix with its cold error edges. C's rt
reads now sit at 70–87% of C++ on the same corpus, on the same machine, in the same window
— on 2026-08-14 (M2, pre-fix, `tu` linkage) the C read column was the slowest in the
estate. **Go is the unmoved row**: `partial:8–12` on every rt leg, the known cost-budget
wall (§4.3 ledger); its overnight fix was wire-correctness, not inlining, and the table
says exactly that.

## Honest notes

- **C# is a recorded skip, not a row.** This bench has no dotnet SDK; the harness refused
  to pretend otherwise (`# skipped: cs`, provenance line `[UNVERIFIED — leg cannot run]`).
  A portable SDK appeared mid-morning in a sibling session's workspace; it was not adopted
  — an SDK that materializes mid-pass is not a provenance story to publish numbers over.
  The macbook O3 pass (`2026-08-15-arm64-macbook-O3-pass.csv`) remains the latest C# record.
- **The O2 pass's C inline verdict was re-run from a pinned `60dfac0` tree** after the
  morning's landing sweep moved this checkout's generated C names (`b30b78f`) between the
  measured build and the verdict's shadow compile — the shadow must compile the sources the
  measured binary was built from, and briefly could not. Same measured binary, same runtime
  clone, ledger updated in place. The measurement itself was never re-run: both windows
  closed valid before any of this.
- **Reconciliation**: nothing under `bench/results/` moved upstream while these passes ran;
  these files are additive. The landing sweep's post-`60dfac0` commits (`2397582` rounding,
  `b30b78f` C names, `8772cef` FMA/contraction) change the *generated code under test* —
  the post-lane pass re-measures against those and does not supersede this baseline; it
  answers a different question.
- Cross-machine ratios remain out of scope (§7): nothing here compares to the M2 numbers.
  What is comparable is the shape, and the shape moved: the C column stopped being the
  estate's cautionary tale in one night.
