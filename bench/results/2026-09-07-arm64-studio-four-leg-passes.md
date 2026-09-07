# Studio, 2026-09-07: two nine-language sittings, four four-leg driver passes, and what moved since the README's sitting

All on the shared Mac Studio (Apple M3 Ultra, 32 cores, 512 GiB, Darwin 25.6.0, Apple clang
21.0.0 clang-2100.1.1.101, go 1.27.1, cargo 1.98.0), unpinned, a desktop session and a Codex
session live on the account throughout, the keeper's estate under another user. Loads are in
each preamble.

## The two sittings

`bench/run.sh` sittings, seven measured runs per leg, every leg gated on the wire goldens
(`corpus_id 6b213fbfa1a03a99`), schema main 3d01b479. The runtimes were at the tags
`.github/workflows/ci.yml` checks out (serialize v1.16.2, serialize.c v1.9.2, serialize.go
v1.15.1, serialize.rs v2.4.0, serialize.cs v1.9.1, serialize.js v1.4.2; each preamble records
the commit its tag resolved to). Five legs ran on the toolchains the repository pins: the
codegen-only legs on `make/*.mk`'s (OpenJDK 21.0.12.1, the Temurin build; Dart 3.13.2;
Erlang/OTP 29.0.5 with Elixir 1.20.4), C# on .NET SDK 10.0.400 for the `10.0` in
`.github/dotnet-version`, JavaScript on node 20.20.2. The other four ran on the machine's
compilers, which the repository does not pin: Apple clang 21.0.0, go 1.27.1 (CI runs 1.26),
cargo 1.98.0 (CI resolves Rust's stable at run time).
Sitting 1 (`2026-09-07-arm64-studio-ninelang-sitting.csv`, 14:22:47Z) skipped JavaScript because
the runner finds node on the PATH and the pinned node was not on it; that leg ran alone into
`2026-09-07-arm64-studio-ninelang-sitting-js.csv` afterwards (14:29:25Z). Sitting 2
(`2026-09-07-arm64-studio-ninelang-sitting-2.csv`, 14:32:38Z) ran all nine in one file and is the
README's. Rendered by `bench/render.awk` (round-trip best rate, C++ = 100%), except sitting 1's
JavaScript, which render.awk refuses (no C++ row in its file) and is computed here by hand from
the two files' best rates:

| language | sitting 2 | sitting 1 | Air sitting, 2026-09-01 |
|---|---:|---:|---:|
| C++ | 100% | 100% | 100% |
| C | 107% | 109% | 98%, tie |
| Java | 169% | 175% | 162% |
| Rust | 172% | 174% | 154% |
| Go | 230% | 238% | 210% |
| C# | 253% | 260% | 225% |
| Dart | 256% | 264% | 227% |
| JavaScript | 387% | 392% (by hand, two files) | 264% (node 26.7.0) |
| Elixir | 1427% | 1489% | 1283% |

Best round-trip rates, messages per second, with the spread of the seven runs:

| leg | sitting 2 | spread | sitting 1 | spread | Air |
|---|---:|---:|---:|---:|---:|
| cpp | 4,637,510 | 1.7% | 4,801,451 | 2.5% | 3,594,435 |
| c | 4,343,435 | 2.0% | 4,398,224 | 2.6% | 3,662,957 |
| java | 2,739,857 | 3.3% | 2,747,725 | 1.2% | 2,216,563 |
| rust | 2,692,124 | 1.9% | 2,757,736 | 1.5% | 2,336,475 |
| go | 2,013,759 | 1.3% | 2,018,407 | 0.6% | 1,715,160 |
| cs | 1,836,489 | 0.7% | 1,846,057 | 0.9% | 1,599,897 |
| dart | 1,812,096 | 2.4% | 1,820,703 | 2.1% | 1,586,561 |
| js | 1,199,841 | 1.0% | 1,224,748 | 2.4% | 1,361,635 |
| elixir | 324,874 | 0.7% | 322,502 | 5.7% | 280,148 |

The sittings, ten minutes apart, differ by 1.1 to 4.2% on every ratio (Elixir's 62 points is
4.2% of 1489), most of it the C++ denominator; no row in either is past the §2.3 noisy line.

## The four driver passes: the two-by-two

`bench/tools/pass-driver.sh`, four legs (cpp, c, go, rust), seven interleaved rounds, every leg
twice per round as §2.6.1 A/A twins, a C++ control leg before and after, the two changes since
the README sitting (schema 7eba63f to today's main; the runtimes at their 2026-08-25 to 09-01
commits, cebaed2, 37db942, 287e2fe, 6b52aa6, to CI's tags) crossed:

| pass | schema (preamble) | runtimes | control delta | twins | load peak | CSV |
|---|---|---|---:|---|---:|---|
| A | `b414f078-dirty` | the tags | 0.7% | OK | 10.5 | `2026-09-07-arm64-studio-four-legs-twins-pass.csv` |
| B | `7eba63fe` | the tags | 4.0% | OK | 20.5 | `2026-09-07-arm64-studio-four-legs-twins-schema7eba63f-pass.csv` |
| C | `a7b80a42` | the sitting's | 0.7% | OK | 6.0 | `2026-09-07-arm64-studio-four-legs-twins-oldruntimes-pass.csv` |
| D | `7eba63fe` | the sitting's | 1.2% | OK | 7.7 | `2026-09-07-arm64-studio-four-legs-twins-schema7eba63f-oldruntimes-pass.csv` |

Four separate passes, at 13:38, 13:45, 13:53 and 14:40Z: the control legs and the twins
certify each pass within itself, not one pass against another. Cell D ran last, after the two
sittings, with node and dotnet on the PATH (its preamble records them where A, B and C record
`not present`; none of the four runners uses either).

Rendered, with the best round-trip rates:

| cell | C measured | Rust | Go | cpp | c | rust | go |
|---|---:|---:|---:|---:|---:|---:|---:|
| D: the sitting's schema, the sitting's runtimes | 99.1%, tie | 163% | 216% | 4,394,048 | 4,431,927 | 2,693,626 | 2,035,252 |
| B: the sitting's schema, today's runtimes | 101.9%, tie | 167% | 222% | 4,538,961 | 4,454,055 | 2,714,060 | 2,040,720 |
| C: today's schema, the sitting's runtimes | 103.3%, tie | 169% | 223% | 4,504,872 | 4,359,488 | 2,671,639 | 2,017,175 |
| A: today's schema, today's runtimes | 106.7%, tie | 174% | 233% | 4,733,118 | 4,434,673 | 2,727,040 | 2,033,271 |
| the Air sitting | 98.1%, tie | 154% | 210% | 3,594,435 | 3,662,957 | 2,336,475 | 1,715,160 |

**What moved.** Rust's best round-trip rate stays between 2,671,639 and 2,757,736 a second
across the four cells and both sittings (3.2% from lowest to highest), Go's between 2,013,759
and 2,040,720 (1.3%), C's between 4,343,435 and 4,454,055 (2.5%); cell D to cell A, Go is
-0.1% and C +0.1%. C++'s best rate on today's code is 5.5 to 9.3% above cell D's 4,394,048:
7.7% in cell A (4,733,118), 9.3% and 5.5% in the two sittings (4,801,451 and 4,637,510). Both
changes contribute and the split between them is not resolved: read along one path of the
two-by-two, the schema commits since 7eba63f add 2.5% (D to C) and serialize.h between
cebaed2 and v1.16.2 adds 5.1% (C to A); along the other, 4.3% (B to A) and 3.3% (D to B).
Each step is smaller than the C++ round-trip spread inside the passes it is read from (2.0,
3.0, 6.8 and 13.4% in D, B, A and C). The schema side changed the C++ emitter,
`internal/codegen/cpp`, and the generated bench code, `generated/bench/cpp`, not the C++ runner
in `bench/cpp`; the serialize.h side is 583 lines; which commit is responsible is not
isolated. C++ is the denominator, so every other language's percentage widened by that much
while the other three legs stayed inside their own spreads. Ratios move with
microarchitecture, and cell D, the Air's code on the Air's runtimes, measures that move: Rust
163% and Go 216% on this machine against 154% and 210% on the Air. The Air's own three
sittings of 2026-09-01 render Rust 153, 153, 154 and Go 208, 209, 210, so its noise on these
two ratios is about a point; on its JavaScript and Dart rows it is not (below). `bands A B C`,
the standard's ±15% cross-pass check, reports every row of A inside the band of B and C; the
check is coarser than the 7.7% move and does not resolve it. Absolute rates do not compare
across machines under §2, and the Air-to-Studio rises are reported here for the ledger only:
cpp 22% (cell D) to 32% (cell A), c 19 to 22%, go 18 to 19%, rust 14 to 17%.

**JavaScript.** The Air's 264% ran node 26.7.0 where the repo pins 20.20.2, which is what ran
here. The Studio's absolute JavaScript rate, 1.20 to 1.22 million a second, is below the
Air's 1.36 million while every other leg is 14 to 29% above it. Two terms are open and the
data does not separate them: the node version, and the Air's own variance on this leg. The
Air's three sittings of 2026-09-01 rendered JavaScript 461%, 318% and 264% on that one node
(762,465, 1,124,935 and 1,361,635 a second) and Dart 363, 225, 227; the README's Air number
was the fast end of that range; the Studio's two sittings agree on JavaScript within 2%. A
node 26 run on this machine is the measurement that would settle the version term and has
not been made.

**What to know before trusting a row.** Pass B's control delta, 4.0%, is inside the 5% window
and in the standard's warning zone (its control fell from 6,994,002 to 6,717,419 under a
one-minute load that peaked at 20.5). Pass C's `rust/bitpacker/write` row carries a 16.12%
spread, past the §2.3 noisy line, and its C and C++ round-trip spreads of 13.8% and 13.4% are
why its tie band is 27 points wide. The twin CSVs the gate compared are not committed; the
`# twin_gate: OK` line in each preamble is the record. One toolchain difference against the Air
sitting is not excluded: it ran go 1.27.0, these go 1.27.1, and CI runs 1.26; clang and rustc
are identical strings.

**Provenance.** Pass A's preamble reads `b414f078-dirty`: it started 29 seconds before the
bench tools fix (#689) was first committed in the same worktree, and that fix sat uncommitted
when the preamble was stamped. Pass C started on `a7b80a42`, the fix branch, whose whole diff
from b414f078 is the four files of `bench/tools`. The runners are built from
`generated/bench/<lang>` and `bench/<lang>`, which the fix does not touch, so A and C measure
b414f078's generated code. Passes B and D ran the 7eba63f tree's own driver, byte-identical to
today's, with `BENCH_TOOLS` pointed at a build of the fixed tools. Every runtime path was
§3.5-verified against its toolchain's resolution before the first leg. These are the first
passes the driver has aggregated since 2026-08-31: `aggregate` had refused every pass since
then, failing closed, because its parameter shadowed the path-column list; today's first pass
ran to the end and refused, which is how the shadow was found and fixed. That first pass, the
same configuration as A earlier the same day, rendered Rust 172% and Go 234%, and is not published:
it was aggregated by hand after the fix rather than by the driver. When these passes ran, the
driver refused the three codegen-only legs at its §3.5 pre-flight (no case for java, dart and
elixir; `bench/run.sh` exempted them by name; #692 gives the pre-flight the case); that is why
the nine-language numbers are sittings and the two-by-two is four legs.

```sh
bench/run.sh --out bench/results/2026-09-07-arm64-studio-ninelang-sitting-2.csv     # node, dotnet on PATH
bench/tools/pass-driver.sh --rounds 7 --langs cpp,c,go,rust --twins --out bench/results/2026-09-07-arm64-studio-four-legs-twins-pass.csv
# in a worktree at 7eba63f, beside the same runtime checkouts:
BENCH_TOOLS=<fixed bench/tools binary> bench/tools/pass-driver.sh --rounds 7 --langs cpp,c,go,rust --twins --out bench/results/2026-09-07-arm64-studio-four-legs-twins-schema7eba63f-pass.csv
SERIALIZE=<cebaed2> SERIALIZE_C=<37db942> SERIALIZE_GO=<287e2fe> SERIALIZE_RS=<6b52aa6> bench/tools/pass-driver.sh --rounds 7 --langs cpp,c,go,rust --twins --out <pass C or D>
awk -F, -v skips="" -f bench/render.awk <csv>
```
