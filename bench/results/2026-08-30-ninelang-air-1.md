# ninelang-air-1 — 2026-08-30, M2 MacBook Air

The first nine-language pass under ONE measurement contract: the java,
dart and elixir runners converted from the serialize-family peak-style
form to conforming run.sh legs (BENCH-STANDARD: fixed iteration counts, 1
warmup + 7 measured runs, median/min/max/spread, CSV v2, §1.5 golden
gates), the js leg's four Bench-corpus shapes gaining flat-tier rows
(family gen, codec=flat — THE js path), and every runner gaining --quick.

Machine caveat: M2 MacBook Air, macOS, unpinned by construction (§7),
interactive machine quiesced, caffeinated, single run.sh sweep — a quick
tier, not a certified §2.6 window (no control legs, no twins, no bands).
Ratios within this sitting only.

## Subject labels (the artifact-killer)

Two subjects ride the four Bench-corpus shapes and the rows say which:

- family `rt` — the serialize runtime API called by hand: c, cpp, go,
  rust, cs, and the js rt rows (library context).
- family `gen` — the GENERATED codec: java, dart, elixir (their backends
  emit self-contained codecs; generated code IS their serialize path), and
  the js flat rows (codec=flat).

`relative.go` refuses gen-vs-rt ratios by family. Measured same-subject on
this machine (bench/results/harness-contract-air/), generated C++ is
2.1-3.4x its own runtime-API rows on bench_packet — so a cross-subject
reading understates the native legs. A generated-codec Bench leg for
C/C++/go/rust/cs is the named follow-on that makes the table
subject-uniform.

## Quick mode verification (--quick, one leg at a time, caffeinated)

Bound: at most one minute per language, all nine under ten.

| leg    | wall time |
|--------|----------:|
| cpp    |     4.7 s |
| c      |     4.2 s |
| go     |     9.1 s |
| rust   |     4.3 s |
| cs     |     9.4 s |
| js     |    23.9 s |
| java   |     3.1 s |
| dart   |     7.1 s |
| elixir |    18.1 s |

Combined `bench/run.sh --quick` (all nine, one invocation): **1:17.5**,
printing the blended table:

    quick mode — iteration instrument, not certification (bench_mixed only, blended write+read)
      lang             ns/msg  % of best
      java               5.60       100%
      cpp                5.89       105%
      c                  6.01       107%
      rust               9.88       176%
      dart              15.57       278%
      cs                22.43       401%
      go                24.82       443%
      js                70.89      1266%
      elixir           278.67      4976%

Read with the subject labels above: java/dart/js/elixir entries are
generated codecs, cpp/c/rust/cs/go entries are the runtime API by hand.

## Negative controls (doctored golden -> the leg must refuse)

One byte of `testdata/wire/bench_mixed.bin` flipped (byte 3 ^ 0x40); each
converted or touched leg run and required to refuse with no rows:

    java   GOLDEN GATE FAILED ... reporting nothing.  exit=1
    dart   GOLDEN GATE FAILED ... reporting nothing.  exit=1
    elixir GOLDEN GATE FAILED ... reporting nothing.  exit=1
    js     refusing to emit CSV rows from a failing run / BENCH FAILED  exit=1

Golden restored (git checkout), all gates green again before the sweep.

## The sweep

`bench/run.sh` (full form), caffeinated, one sitting:
`2026-08-30-ninelang-air-1.csv` beside this file. Findings recorded there
and in the PR.
