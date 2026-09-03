# Performance

> **Convention note:** the standard's reporting convention is C = 100%, every other
> language measured against it
> ([BENCH-STANDARD §5](../bench/BENCH-STANDARD.md#5-reporting-format)). The tables below
> are kept exactly as published, in their recorded convention (C++ = 100%).

Generated-code performance as time relative to C++ (100%; higher is slower), medians across
the corpus on an **Apple M3 Ultra**, the 2026-08-15 five-language pass **at `-O3`**
([raw CSV](../bench/results/2026-08-15-arm64-studio-postlane-O3-pass.csv),
[inline verdicts](../bench/results/2026-08-15-arm64-studio-postlane-O3-pass.inline)):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | 163% | 142% | 126% | **70%** |
| Rust | 149% | 163% | 107% | 168% |
| C# | 194% | 224% | 169% | 231% |
| Go | 356% | 460% | 317% | 242% |

Interleaved, seven measured rounds, control legs bracketing the pass at a 0.9% delta, every
leg built from a named upstream commit the harness verified against each toolchain's own
resolution before the first measurement. The methodology is normative and lives in
[bench/BENCH-STANDARD.md](../bench/BENCH-STANDARD.md); it is stricter than the table, and it
refuses to print a ratio it cannot justify.

## Both levels, because the standard requires it

[BENCH-STANDARD §3.3](../bench/BENCH-STANDARD.md): if the ranking of any two languages differs
between optimization levels, both tables publish — a single ranking would publish a coin
flip. The `-O2` companion pass
([raw CSV](../bench/results/2026-08-15-arm64-studio-postlane-O2-pass.csv),
[inline verdicts](../bench/results/2026-08-15-arm64-studio-postlane-O2-pass.inline)) fired
exactly that rule: **on the bitpacker write bench, C leads C++ at `-O2` and C++ leads by
2.6x at `-O3`.** The mechanism is known and named in the inline verdicts: at `-O3` clang
fully unrolls the 16-width group and folds widths to immediates; at `-O2` it stays rolled
and the implementations sit at parity. C++'s bitpacker-write lead is a property of one
optimization level, not of the code.

The `-O2` table (same machine, same window discipline, seven rounds, control legs at a
valid delta):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | **158%** | **151%** | **129%** | **71%** |

<!-- CAPTION: cpp checks=removed (debug asserts and bounds/range checks compile out) vs c checks=contract (debug asserts compile out; wire/API contract validation stays in every build) — this ratio includes the cost of a different safety contract. -->
<!-- CAPTION: cpp linkage=hdr vs c linkage=hdr+tu — this ratio includes the runtimes' packaging difference. -->

Only C and C++ appear because only they have real `-O2` builds: Rust's bench runner pins
its release profile (`opt-level = 3`), and the harness's own refusal rules will not ratio
an explicit `-O3` row inside an `-O2` table — no override exists, by design. Go and C#
build at their single default level and their rankings are level-independent by
construction. The Rust `-O2` leg is a named harness gap.

## Reading the table honestly

**The ratios cross safety contracts, and the harness says so in captions rather than hiding
it.** C++ compiles its debug asserts and bounds checks out; C validates the wire and API
contract in every build; Rust, C# and Go carry bounds, range and sticky-error checks in every
build by contract. A ratio between two of those columns includes the price of a different
promise. The clearest evidence that the price is real: C and Rust — the two per-field-checked
writers — land within 5% of each other on every round-trip write row, from wholly independent
implementations. Two minds arriving at the same cost for the same guarantee.

Relative numbers move with compiler and microarchitecture. Treat the table as a dated
snapshot, not a verdict. Full tables, the pre-campaign baseline of the same day, and
per-gap analysis: [bench/results/](../bench/results/).

---

Measurement code and the full tables live in [bench/](../bench/).
