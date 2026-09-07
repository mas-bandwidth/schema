# Performance

> **Convention note:** the standard's reporting convention is C = 100%, every other
> language measured against it
> ([BENCH-STANDARD §5](../bench/BENCH-STANDARD.md#5-reporting-format)). The tables below
> are kept exactly as published, in their recorded convention (C++ = 100%).

## 2026-09-07: the Studio's nine-language table, and what moved since the Air's

The README's table is now a sitting on the Apple M3 Ultra Studio, schema main at 3d01b479,
every leg gated on the wire goldens, seven measured runs per leg, rendered by the README's
own instrument (`bench/render.awk`: round-trip best rate, C++ = 100%). Five legs ran on the
toolchains the repository pins: node 20.20.2, OpenJDK 21.0.12.1 (the Temurin build
`make/java.mk` names), Dart 3.13.2, Erlang/OTP 29.0.5 with Elixir 1.20.4, and .NET SDK
10.0.400 for the `10.0` in `.github/dotnet-version`. The other four ran on the machine's
compilers, which the repository does not pin: Apple clang 21.0.0, go 1.27.1 (CI runs 1.26)
and cargo 1.98.0 (CI resolves stable at run time); the runtimes were at the tags CI checks
out. It is C++ = 100% like the tables below it because the C = 100% form the standard prefers
is not producible for these rows: the `rel` tool refuses rows without an inline verdict, and
none of these carry one. The Air's table it replaces, and a second Studio sitting ten minutes
earlier, stand beside it:

| language | [Studio, sitting 2](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-2.csv) (the README's) | [Studio, sitting 1](../bench/results/2026-09-07-arm64-studio-ninelang-sitting.csv) | [Air, 2026-09-01](../bench/results/2026-09-02-sitting4-arm64-macbook.csv) |
|---|---:|---:|---:|
| C++ | 100% | 100% | 100% |
| C | 107% | 109% | 100%, a §2.8 tie (measured 98%) |
| Java | 169% | 175% | 162% |
| Rust | 172% | 174% | 154% |
| Go | 230% | 238% | 210% |
| C# | 253% | 260% | 225% |
| Dart | 256% | 264% | 227% |
| JavaScript | 387% | 392% ([its own file](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-js.csv), computed by hand across the two files) | 264%, on node 26.7.0 |
| Elixir | 1427% | 1489% | 1283% |

The two Studio sittings differ by 1.1 to 4.2% on every row, most of it the C++ denominator
(4,801,451 then 4,637,510 messages a second); sitting 1's JavaScript leg ran in its own file
because the runner finds node on the PATH and the pinned node was not on it.

**What moved, measured on one machine.** Four four-leg driver passes (C++, C, Go, Rust; seven
interleaved rounds, twins, control legs inside the 5% window every time; each pass controlled
within itself, none against another; a fifth, the day's first, is not published) fill the
two-by-two of the Air sitting's schema commit and runtime commits against today's:

| schema | runtimes | C++ round-trip, best | Rust | Go | CSV |
|---|---|---:|---:|---:|---|
| 7eba63f | the sitting's | 4,394,048 | 163% | 216% | [pass D](../bench/results/2026-09-07-arm64-studio-four-legs-twins-schema7eba63f-oldruntimes-pass.csv) |
| 7eba63f | today's tags | 4,538,961 | 167% | 222% | [pass B](../bench/results/2026-09-07-arm64-studio-four-legs-twins-schema7eba63f-pass.csv) |
| a7b80a42 (today's) | the sitting's | 4,504,872 | 169% | 223% | [pass C](../bench/results/2026-09-07-arm64-studio-four-legs-twins-oldruntimes-pass.csv) |
| b414f078, dirty (today's; the note says how) | today's tags | 4,733,118 | 174% | 233% | [pass A](../bench/results/2026-09-07-arm64-studio-four-legs-twins-pass.csv) |

Rust's best round-trip rate stays between 2.67 and 2.76 million messages a second across every
cell and both sittings (3.2% from lowest to highest), Go's between 2.01 and 2.04 million
(1.3%), C's within 2.5%. C++'s best rate on today's code is 5.5 to 9.3% above the first row's
(7.7% in the last row, 9.3% and 5.5% in the two sittings). Both changes contribute and the
split between them is not resolved: read one way through the table, the schema commits since
7eba63f add 2.5% and serialize.h between cebaed2 and v1.16.2 adds 5.1%; read the other way,
4.3% and 3.3%; each step is smaller than the C++ spread inside the passes it is read from, and
which commit is not isolated. C++ is the denominator, so every other language's percentage
widened by that much while the other three legs stayed inside their own spreads. Ratios move
with microarchitecture, as this page says below, and the first row measures that move: the
Air's code on the Air's runtimes renders Rust 163% and Go 216% on this machine against 154%
and 210% on the Air. JavaScript is the one row with a further term: the Air's 264% ran node
26.7.0 where the pin is 20.20.2, the Studio's absolute JavaScript rate is below the Air's
while every other leg is 14 to 29% above it, and the Air's own three sittings of 2026-09-01
rendered JavaScript 461%, 318% and 264% on that one node; whether the version or the Air's
variance on this leg is the term is not measured on one machine.

These are the first passes the driver has aggregated since 2026-08-31: its `aggregate` had
refused every pass since then, failing closed, because a parameter shadowed the path-column
list (#689), and these passes were the ones that found it. The passes' own caveats and the
rates are in [the note beside the CSVs](../bench/results/2026-09-07-arm64-studio-four-leg-passes.md).

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
