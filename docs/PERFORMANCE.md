# Performance

> **Convention note:** the standard's reporting convention is C = 100%, every other
> language measured against it
> ([BENCH-STANDARD §5](../bench/BENCH-STANDARD.md#5-reporting-format)). The tables below
> are kept exactly as published, in their recorded convention (C++ = 100%).

## 2026-09-07: the Studio's nine-language table, and what moved since the Air's

The README's table is now a sitting on the Apple M3 Ultra Studio taken on the pinned node
26.7.0 ([CSV](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-node26.csv),
15:45:27Z), every leg gated on the wire goldens, seven measured runs per leg, rendered by the
README's own instrument (`bench/render.awk`: round-trip best rate, C++ = 100%). Its preamble
records schema commit 3d01b479; main's 7622f23b after it changed only `bench/tools`,
`bench/run.sh`, docs and results, so the generated code and the runtime checkouts this sitting
measured are the ones on today's main. Five legs ran on the toolchains the repository pins:
node 26.7.0 (the pin was 20.20.2 until 2026-09-07; the two older Studio sittings below ran on
it, the Air's already on 26.7.0), OpenJDK 21.0.12.1 (the Temurin build `make/java.mk` names),
Dart 3.13.2, Erlang/OTP 29.0.5 with Elixir 1.20.4, and .NET SDK 10.0.400 for the `10.0` in
`.github/dotnet-version`. The other four ran on the machine's compilers, which the repository
does not pin: Apple clang 21.0.0, go 1.27.1 (CI runs 1.26) and cargo 1.98.0 (CI resolves stable
at run time); the runtimes were at the tags CI checks out. It is C++ = 100% like the tables
below it because the C = 100% form the standard prefers is not producible for these rows: the
`rel` tool refuses rows without an inline verdict, and none of these carry one. The two node-20
Studio sittings it replaces, and the Air's table before them, stand beside it:

| language | [Studio, node 26.7.0](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-node26.csv) (the README's) | [Studio, sitting 2](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-2.csv), node 20.20.2 | [Studio, sitting 1](../bench/results/2026-09-07-arm64-studio-ninelang-sitting.csv), node 20.20.2 | [Air (Apple M2), 2026-09-01](../bench/results/2026-09-02-sitting4-arm64-macbook.csv), node 26.7.0 |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | 107% | 107% | 109% | 100%, a §2.8 tie (measured 98%) |
| Java | 169% | 169% | 175% | 162% |
| Rust | 173% | 172% | 174% | 154% |
| Go | 231% | 230% | 238% | 210% |
| C# | 253% | 253% | 260% | 225% |
| Dart | 267% | 256% | 264% | 227% |
| JavaScript | 292% | 387% | 392% ([its own file](../bench/results/2026-09-07-arm64-studio-ninelang-sitting-js.csv), computed by hand across the two files) | 264% |
| Elixir | 1451% | 1427% | 1489% | 1283% |

Against sitting 2, one leg moved with the runtime: JavaScript, 387% to 292%. Of the rest,
Dart's 256% to 267% is the largest move in proportion (its absolute rate fell 3.6% against a
denominator up 0.6%) and Elixir's 1427% to 1451% the next (down 1.0%); both are
sitting-to-sitting variance, not measured to anything. The two node-20 Studio sittings differ
by 1.1 to 4.2% on every row but C++'s, the denominator, and most of that spread is the
denominator itself (4,801,451 then 4,637,510 messages a second); sitting 1's JavaScript leg
ran in its own file because the runner finds node on the PATH and the pinned node was not on
it.

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
cell and all three sittings (3.2% from lowest to highest), Go's between 2.01 and 2.04 million
(1.3%), C's within 2.5%. C++'s best rate on today's code is 5.5 to 9.3% above the first row's
(7.7% in the last row; 9.3%, 6.2% and 5.5% in the three sittings). Both changes contribute and
the split between them is not resolved: read one way through the table, the schema commits
since 7eba63f add 2.5% (D to C) and serialize.h between cebaed2 and v1.16.2 adds 5.1% (C to A);
read the other way, 4.3% (B to A) and 3.3% (D to B). The C++ round-trip spreads inside the four
passes those steps are read from are 2.0, 3.0, 6.8 and 13.4% in D, B, A and C: the steps are of
the same order as the spreads, so the split is not resolved and which commit is responsible is
not isolated. C++ is the denominator, so every other language's percentage widened by that much
while the other three legs stayed inside their own spreads. Ratios move with microarchitecture,
as this page says below, and the first row measures that move: the Air's code on the Air's
runtimes renders Rust 163% and Go 216% on this machine against 154% and 210% on the Air.

**JavaScript, and the node version.** JavaScript was the one row these passes left with an
unmeasured term: the Air's 264% ran node 26.7.0 where the pin was then 20.20.2, and the
Studio's absolute JavaScript rate was *below* the Air's while every other leg was 14 to 29%
above it. Two runs of the JavaScript leg alone, on this one machine 101 seconds apart, settle
the version term. Seven measured runs each, best round-trip rate:

| node | best round-trip, msgs/sec | spread | CSV |
|---|---:|---:|---|
| 20.20.2, 15:42:19Z | 1,222,630 | 2.08% | [js node 20](../bench/results/2026-09-07-arm64-studio-js-node20.csv) |
| 26.7.0, 15:44:00Z | 1,595,367 | 1.46% | [js node 26](../bench/results/2026-09-07-arm64-studio-js-node26.csv) |

That is +30.5%, one machine, back to back, each spread an order below the step. On node 26 the
Studio's JavaScript leg runs 17.4% above the Air's, inside the 10.1 to 29.8% range the other
eight legs occupy in the same sitting — the row no longer stands apart. The Air's own variance
on this leg remains true and is now a secondary term: its three sittings of 2026-09-01
rendered JavaScript 461%, 318% and 264% on that one node, and the README's Air number was the
fast end of that range, so Air-to-Studio comparisons of this row still carry it.

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
it.** C++ compiles its debug asserts and bounds checks out; **so does C, from 2026-09-07** —
the ruling was "Every language, by design, compiles out asserts/checks in release build. This
is the whole point!", and the C backend's per-field write-side range and bounds refusals
became `serialize_assert`s that vanish under `NDEBUG`, the tier C++'s are in. The same day the
last exception went with them: a counted array's count outside `[A, B]` (SPEC §4.6) had been
refused in every build in all nine targets, and is now a debug assert wherever the language
has that idiom — "checks are *DEBUG ONLY*" — so a C or C++ release build now holds NO
write-side range check at all. C's two remaining gaps closed in the same change, a flags value
wider than its wire width and interior nulls in a `string(N)` on write, so the C and C++
write-side check sets are identical. NO write-side check stays in every build in either: the
union tag outside its variant set (§4.8) was the last structural holdout — it had been argued
to be dispatch rather than a guard — and it too is a `serialize_assert` now, so a C or C++
release build performs no write-side validation whatsoever. Every
read-side check stays in every build everywhere, by the other half of the same ruling: "Of
course, on read side we MUST always do the checks!" Rust, C# and Go carry bounds, range and
sticky-error checks in every build by contract, Rust's and C#'s pending their own change. A
ratio between two of those columns includes the price of a different promise.

**Measured the same day, the C change bought nothing the instrument can see.** A twins pass on
the C and C++ legs with the asserts in place
([CSV](../bench/results/2026-09-07-arm64-studio-c-asserts-twins-pass.csv), seven interleaved
rounds, window OK, control delta 1.6%) renders C at 105.2% of C++, inside the §2.8 tie band
(the pair's combined round-trip spread, 4.2 + 4.4 = 8.6 points) and so reported as a tie: write 8,858,514 against 8,863,042 messages a second,
round trip 4,408,928 against 4,636,506. Two passes over identical code earlier the same day
differed by 0.4%, and a pass with the refusals compiled out by hand moved C by 0.4% and 0.1%
against them. By row, C is within 3% of C++ on the packet write and faster on the raw
bit-packer write, and C++ reads raw bits 1.58 times faster; the remaining C-to-C++ distance is
the C runtime's read path, not the generated code's checks. serialize.c v1.10.0 closed that read path
later the same day: latching a failed read in the cursor, the way C++'s `ReadStream` does, lets the
per-read past-end test fold into the caller's remaining-bits guard, and the raw bit read went from
76,492 to 122,221 messages a second — 61.9% of C++ to 99.2%
([CSV](../bench/results/2026-09-07-arm64-studio-serialize-c-1-10-0-pass.csv), seven interleaved
rounds, window OK, control delta 2.2%). The round trip is where it was: 4,226,614 messages a second
against 4,279,494 on the pass before it, 93.1% of C++ against 94.3%, both moves inside the rows'
spread — the fix bought the raw-read row and left the packet path alone. So the remaining
distance is no longer the raw reader, at 99.2%, but the generated packet read, where the round
trip's seven points now live; at this pass's precision (combined spread 5.8 points against 8.3
before) that is outside the §2.8 band and no longer a tie, a finding under investigation. That
investigation closed the same day: the generated C read was executing ~193 more loads and ~200
more spill reloads per message than C++'s for identical work, because clang spilled
`stream->num_bits` and reloaded it for every field's past-end test, and the C backend now emits
one `serialize_read_bits_remaining` guard at the top of a read function whose struct has a FIXED
wire width — every per-field test folds into it, the refusal is unchanged because every field is
always read — taking the round trip from 93.1% to 101.9% of C++ best, 4,226,614 to 4,669,166
messages a second, which is inside the §2.8 band (6.5 points) and so a tie again
([CSV](../bench/results/2026-09-07-arm64-studio-c-read-guard-pass.csv), seven interleaved rounds,
window OK, control delta 0.5%, twin gate OK).

**The two dated `<!-- CAPTION -->` lines above this section are the provenance the
2026-08-15 passes recorded, and they predate both halves of C's move**: the runtime's own
release checks went in serialize.c ruling #20 on 2026-08-17 (the bench runner has recorded
`checks=removed` for C since), and the generated code's went today. The sentence those
captions carry — that C's wire and API contract validation stays in every build — was true of
the runs they caption and is not true of C now. The C/C++ ratio is no longer a ratio across
two check models; the pass above is the first rendered without one, and the 2026-08-15 tables
stand as they were measured.

Rust remains the per-field-checked writer of the set. The older reading of the table — that C
and Rust, the two then-per-field-checked writers, landing within 5% of each other on every
round-trip write row was independent confirmation of the cost of that guarantee — describes
the C that measured, not the C that is here now.

Relative numbers move with compiler and microarchitecture. Treat the table as a dated
snapshot, not a verdict. Full tables, the pre-campaign baseline of the same day, and
per-gap analysis: [bench/results/](../bench/results/).

---

Measurement code and the full tables live in [bench/](../bench/).
