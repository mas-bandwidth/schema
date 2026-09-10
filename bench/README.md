# bench — cross-language serialize profiling harness

Measures two families per language, every row labelled with its family
(§1 of the standard):

- **`gen`** — the schema-GENERATED code against its serialize runtime: write
  and round-trip over `bench/corpus/Bench.schema`'s ONE measured shape,
  `BenchMixed`, driven by the committed variant corpus (issue #177, #191).
- **`bits`** — the raw bit packer: the §1.4 16-width table (227 bits/group)
  over a 65536-byte buffer, the ONE bitpacker workload in the estate.

**`bench/BENCH-STANDARD.md` is the normative measurement contract** — what a
number means, when two numbers may be divided, and when the tools refuse.
This README is the operating manual.

`bench/run.sh` builds and runs whichever language runners are available and
collects everything into one CSV under `bench/results/`. The C++ runner
(`bench/cpp/bench_main.cpp`) is the reference implementation; the c, go,
rust, cs and js runners (`bench/c`, `bench/go`, `bench/rust`, `bench/cs`,
`bench/js`) are its ports, wired per the contract below. Three further
legs — `bench/java`, `bench/dart`, `bench/elixir` — measure those
backends' GENERATED codecs over the same shape under the same contract
(see "The codegen-only legs" below).

## Running

    bench/run.sh                 # Release, results in bench/results/<date>-<arch>-<host>.csv
    bench/run.sh --debug         # also the Debug pair (matched-pair methodology)
    bench/run.sh --only c|cpp|go|rust|cs|js|java|dart|elixir   # one language leg
    bench/run.sh --quick         # the iteration instrument: bench_mixed only,
                                 # 3 measured runs per leg, golden gate intact,
                                 # and the blended table (per-message time
                                 # averaged over write+read, fastest = 100%)
                                 # printed after the CSV — SINGLE-SUBJECT over
                                 # family gen with the family printed per row
                                 # (#177). NEVER a certification run; scaling
                                 # constants are PROPOSED in
                                 # BENCH-STANDARD.md §2.8.
    bench/run.sh --inline        # + the §4 inline verdict pass: writes the
                                 # per-symbol ledger and backfills the inline
                                 # column (rows stay un-ratioable without it)
    SERIALIZE=path/to/serialize bench/run.sh     # (SERIALIZE_C/_GO/_RS/_CS/_JS likewise;
                                 # every leg BUILDS against its var and the run
                                 # refuses if a build would not — §3.5, verified
                                 # per pass by bench/tools/verify-runtime-paths.sh)
    BENCH_OPT_LEVEL=O2 bench/run.sh              # the C/C++ O2 leg (§3.3)
    BENCH_NOISE="NOISY: ..." bench/run.sh        # free-text supplement — load capture is automatic

`make bench` runs the Release pass.

**A publishable pass is a driver pass**, not a bare run.sh invocation:

    bench/tools/pass-driver.sh [--rounds 7] [--langs cpp,c,go,rust,cs,js,java,dart,elixir] [--inline]

`--langs` defaults to all nine — the languages the published table carries —
and a leg whose toolchain is absent is skipped and recorded (`# skipped:`)
rather than failing the pass.

The driver runs the §2 methodology: a C++ control leg, N interleaved rounds
(every language once per round via `--round K`, so every leg sees the same
load window), the same control leg again, automatic load capture into the
preamble, and the window verdict — `# window: INVALID` when the control legs
disagree by more than 5%, and the tools refuse ratios from an invalid pass.
The driver, not the runner, computes max/median/min/spread across rounds.

## Methodology (why the numbers can be trusted)

Follows the serialize repo's `bench.cpp` conventions (see the `const-params`
experiment there for the reasoning, learned the hard way):

- **Escape barriers** — the output buffer and the decoded object are observed
  through an empty-asm memory clobber, so the compiler cannot delete the work
  and report fictional throughput.
- **Per-iteration variation** — every write loop mutates value fields
  through a serially dependent LCG (`rng * 6364136223846793005 +
  1442695040888963407`); with constant data, optimizers precompute scratch
  words at compile time. Structure fields (counts, lengths, branch bools)
  stay fixed so bytes/op is constant — the runner asserts this.
- **Variant read buffers** — the read loop reads from 64 pre-written variant
  buffers round-robin, not one buffer the branch predictor can memorize.
- **Self-checks before benching** — every pinned instance is byte-compared
  against its wire golden and round-tripped (write → read → re-write →
  memcmp). A runner that does not produce corpus-identical bytes refuses to
  produce numbers.
- **Fixed iteration counts, warmup, 7 measured runs** — one warmup run per
  path, then 7 measured runs (or one per round under a driver pass). **The
  headline statistic is the best (max) rate** — interference only ever slows
  a run — with median/min/spread beside it, never optional (§2.2). Spread
  over 15% is noisy and leaves corpus-median tables; over 40% the row never
  publishes (§2.3). Only Release numbers are meaningful; the Debug pair
  exists so pathological debug regressions are visible.
- **Pinning** — `taskset -c $BENCH_CPU` where taskset exists (Linux); none on
  macOS. The preamble records pinning and the host noise label.
- **MB/s means MiB/s** (1024*1024), following serialize `bench.cpp`.

C++ flags: the schema repo's own flags (`-std=c++17 -Wall -Wextra -Werror
-ffp-contract=off -Itest`, the last for the corpus's native type mapping onto
`test/vec_math.h`) plus the serialize repo's Release bench configuration
(`-O3 -DNDEBUG -fno-rtti -DSERIALIZE_RELEASE`). Deliberate divergence from
serialize's own bench: **no `-ffast-math`** — this repo pins wire determinism
with `-ffp-contract=off` and the generated quantize paths do real float math.
Every results file records the exact compiler and flags in its preamble.

C flags: `-std=c99 -Wall -Wextra -Werror -O3 -DNDEBUG`, with
`$SERIALIZE_C/serialize.c` compiled in and **no `-flto`** — every leg is
measured in its language's ordinary release configuration (the Rust leg is
`cargo run --release`, no LTO either). Read the C row knowing its
history: **both legs are header-only (`linkage=hdr`) since serialize.c #25, 2026-08-17** —
the runners and the certified space CSV record it per row. Before that date serialize.c was
a compiled translation unit, every runtime call crossed a TU boundary the C++ runtime did
not have, and that boundary was the largest single term in the C row; results from that era
carry the old attribution and stay correct for what they measured. The `-flto` diagnostic
from the TU era — median 1.11x, up to 2.25x on the many-small-call paths, recorded in
`bench/results/2026-08-14-c-lto-diagnostic-arm64-macbook.csv` — is likewise historical: with
both legs header-only there is no TU boundary for LTO to recover (issue #66).
That is how a config divergence gets reported here: label the leg, record the
flag, keep it beside the default pass — the way the `DOTNET_TieredCompilation=0`
diagnostic did.

## Results format

One CSV per host+build, preamble lines starting `#` (date, host, arch, os,
cpu, build, compilers, flags, pinning, noise, the schema commit and every
runtime's commit + branch, and — from the driver — rounds, load capture,
corpus_id and the window verdict), then CSV v2 rows (§5.1):

    lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline

`path` is `write` or `read`, and — for the data-driven `bench_mixed` rows,
which is every language's `gen` family (issue #191) — `write` or
`round_trip`; the derived `read` prints to stderr only, never as a row. The
write/round_trip pair is RATIFIED as §2.9: the tools blend `round_trip`, and
a `--quick` run whose headline section would be empty REFUSES with a
non-zero exit rather than printing nothing.
`bytes_per_op` is the actual wire bytes per
message (constant per benchmark by construction). The six v2 columns carry
what the row measured: `corpus_id` (FNV-1a-64 of the goldens the runner
actually loaded, §1.6 — corpus drift becomes a tool error, not a published
ratio), `family` (`gen` | `bits`, per row), `linkage`/`checks`/`opt` (the recorded
conditions, §3), and `inline` (`full` | `partial:N` | `none` | `unknown`,
§4.2 — filled by the verdict pass, and `unknown` refuses to ratio). The
per-symbol inline ledger lives beside the CSV as `<name>.inline`. v1 CSVs
(11 columns) still load and are un-ratioable, which is correct: legacy data
cannot be trusted to be comparable.

`bench/tools/relative.go` (`go run ./bench/tools ...`) renders the tables
and REFUSES to divide rows that measured different things — §5.3's nine
rules; there is no `--force`. `--label-checks` / `--cross-linkage` print a
ratio across a contract/packaging difference with the caption that names
it. Human-readable tables live beside the CSVs in `bench/results/`.

## Locking C and C++ together

*(Added 2026-09-07, on the owner's word: "So we should now be able to lock C
and C++ together now in perf and make sure we don't regress.")*

The C and C++ legs compile the same word-codec shape with the same clang
backend at the same flags, and BENCH-STANDARD §2.8 reports them as one figure
when they sit inside the pair's own spread. Two legs reported as one number
have to be held together, or the tie becomes a place a regression can hide:
either leg could slide while the headline still printed 100%.

**The lock lives in the committed record and in CI's ledger check** — not in a
CI benchmark. `go run ./bench/tools ledger --check` walks every CSV under
`bench/results/` and applies two gates:

- **The time axis.** For each *locked leg* — `cpp` and `c`, one list in
  `bench/tools/ledger.go`, not two code paths — the newest `bench_mixed`
  `round_trip` point on a `(machine, corpus_id)` axis must not sit above **the
  best (lowest ns/msg) of the previous three points** on that axis by more
  than the noise gate: `max(2 × the two sittings' summed spread, 5%)`. Beyond
  it, `LEDGER RED` and exit 1. **The machine leads the key because absolute
  rates do not compare across machines (§2)** — a Studio point is never gated
  against a laptop point, so the pass that renews the lock has to run on the
  same box as the point before it.
- **The pair axis.** On each `(machine, corpus_id)` axis, **the newest
  certified pass** carrying *both* legs' `bench_mixed` `round_trip` rows has
  C's percentage of C++ computed on the best rate and held to §2.8's tie band
  — the two within-sitting `spread_pct` values summed, floored at 3.0 points —
  computed exactly as `bench/render.awk` computes it for the headline table.
  Outside the band, the check names the CSV, the percentage, the band, both
  spreads and the axis. That pass is named in the output even when it is
  green (`pair lock: … inside`), because the gate has to say which pass it is
  holding. A leg whose spread is over §2.3's 40% INVALID line yields **no
  verdict at all**: `render.awk` prints `—` for such a row and refuses a
  number, so no ratio is defined from it and the gate prints a skip line
  instead of a ruling — and, publishing no figure, such a pass is not the one
  the lock holds either; the newest pass that does publish one keeps the axis
  locked. A test runs the committed `render.awk` and the gate over the same
  fixture and requires their verdicts to match, because the 3.0-point floor is
  written out once in each language.

**The lock holds the newest certified pass on the axis, and only that one.**
Older passes on the axis print as history — one line each, `pair history:
<file> <pct> against <band>: inside|outside` — and gate nothing. The lock is a
claim about where the pair stands *now* (the owner's word was "land parity
between C and C++ and then lock"), and the record is a record: it keeps the
passes that FOUND things, not only the clean ones.
`bench/results/2026-09-07-arm64-studio-serialize-c-1-10-0-pass.csv` is exactly
that — a `window: OK` pass measuring C at 107.4% of C++ against a 5.8-point
band, and that measurement *is* the finding, a 7% read-path gap, which the
next pass, `2026-09-07-arm64-studio-c-read-guard-pass.csv`, closed at 98.2%
inside 6.5. Holding every historical pass forever would leave CI red on a
finding that is already fixed, and **a gate that reds on something already
fixed teaches people to ignore the gate**. Holding the newest certified pass
keeps both teeth: the separation that was found stays readable in the record
and in the check's own output, and a regression away from parity goes red the
moment the pass that shows it is committed — because that pass is then the
newest one on its axis.

**Why the best of the previous three, and what that still does not stop.**
Comparing the newest point against its immediate neighbour was defeated by a
single commit: land two CSVs that are each ~50% slower, and the newest is
measured against the *other regressed file*, so the step between them is ~0%,
the check exits 0, and the axis has quietly reset at the worse level. Measured
against the best of the previous three, a regression has to beat the recent
best, which two files landing together cannot arrange. Plainly, though: **a
commit that lands four or more points on one axis can still reset the
baseline**, because the fourth point pushes the last pre-commit point out of
the window. Three is a depth, not a proof. The answer to a suspicious series
is to read it, and plain `ledger` (no `--check`) prints the whole series.

**The time axis is built from both-leg files only.** A point enters a locked
leg's series only from a CSV that carries *both* legs' `bench_mixed` `gen`
`round_trip` rows. The lock is a claim about the pair, so a single-leg
experiment file is by construction not a like measurement of it — and the
record already showed what that costs: the Studio `c` axis was interleaved
with four single-leg C experiments (`packet-void-c-studio`,
`c-defaults-c-studio`, `c-utf8-c-studio`, `c-wide-c-studio`, 228–242 ns/msg)
among passes at 224–233, and the `c-defaults` → `c-utf8` step alone is +5.35%
against a 5.00% gate, so the next single-leg C experiment committed last would
have red CI for something that is not a regression of the locked pair at all.
This **also narrows the pre-existing `cpp` gate** to both-leg files: one rule,
both legs, no special case. After the narrowing, each Studio axis gates eleven
points (the eight twins/pair passes and the three nine-language sittings) and
each macbook axis twelve; the `x86_64 spacegame` axes are down to one point
each and gate nothing, and the laptop axis disappears entirely — both its
files carried one leg.

**What the lock covers, exactly.** `bench_mixed`, family `gen`, path
`round_trip`, best rate. That is the headline statistic §2.8 rules on, and it
is the whole of the lock. **The `bitpacker` rows and the `write` path are
outside it**, and knowing that matters: in
`bench/results/2026-09-07-arm64-studio-bitpacker-checked-read-pass.csv` — a
`window: OK` pass this lock calls green at 106.1% — `bitpacker`/`read` has C
at 161.4% of C++. Locking that row would be a ruling about a different
statistic against a different band, and it is **not** invented here; it would
need its own §2.8-style paragraph in `BENCH-STANDARD.md` first.

**A pass reds; a sitting warns.** A pass runs control legs at both ends and
the driver stamps `# window: OK` (§2.6), which is precisely the certificate
that says a ratio may be published from it — so a separation in the axis's
newest such pass is a real separation and exits 1. A sitting has no control
legs and no window verdict; it cannot tell a separation from a drifting box,
so it prints `LEDGER WARN` and exits 0 — every sitting, not just the newest
one, since they are informational and there is nothing to single out. That is
not leniency, it is what the record already says: measured on the tree of
2026-09-07, five committed sittings sit outside the band (three Studio
nine-language sittings at 106.6–109.2%, the x86_64 spacegame sitting at
111.3%, one macbook quick sitting at 112.6%) while every
twins pass of the same era holds inside it — 99.1–106.7% against bands of
4.0–27.2 points, the C-asserts twins pass landing at 105.2% inside 8.6. A pass
whose window is stamped `INVALID` locks nothing either: §2.6 already refuses
to publish ratios from it.

Either way the message ends with the same line, because it is the ruling's own
exit clause: **a separation beyond the combined spread is a finding to
investigate, not a number to publish.** The response to a red is to find out
what moved, not to widen the band.

**Renewing the lock — `make bench-lock`.** On the Studio:

    make bench-lock

which is a `cpp,c` A/A twins pass, seven rounds, into
`bench/results/<date>-<arch>-<host>-lock-pass.csv`, followed by
`ledger --check`. `--twins` is not optional: the lock is a claim about two
binaries measured in one window, and §2.6.1's A/A legs are what rules out
state-selective interference dressing up as a separation. Run it on the same
machine as the previous point — the Studio — commit the CSV, and CI's ledger
step reads it from then on. The intent is a scheduled pass on the Studio,
committed by whoever runs it.

There is deliberately **no GitHub-hosted-runner benchmark**: a shared hosted
runner cannot hold a window the standard would accept (§2.6, §7), and a gate
built on windows the standard refuses would red on the weather. CI reads the
record; the maintainer makes it.

## The benchmark set

One shape, plus the raw bitpacker. Family `gen` is oracle-gated per §1.5;
its iteration count is fixed and identical across all nine languages.

| bench        | family | pinned to golden | shape                                              |
|--------------|--------|------------------|----------------------------------------------------|
| bench_mixed  | gen    | bench_mixed      | **THE canonical benchmark** (#184): every construct the schema language expresses, in one representative game message, integers carrying 91.87% of the wire bits (438 B) |
| bitpacker    | bits   | — (read-back verified in setup) | 16-width table over a 64 KiB buffer, 24576 passes/run (§1.4). **write:** the language's bit writer, unchecked under NDEBUG in every language (capacity and range are debug asserts). **read:** the language's CHECKED bit read — one past-end test per read, refusal terminal |

**bench_mixed is THE Bench-corpus shape** (owner's ruling, issue #184: *"I'd
rather we just have ONE good benchmark we can apply to all serialize and
schema implementations"*, and the 2026-08-31 ruling that there be *"only a
single schema bench: Bench.schema"*). The three diagnostic stress shapes that
used to ride beside it — `bench_packet`, `bench_ints`, `bench_bits` — are
**retired from measurement**, and so is the whole §1.2 example corpus that
used to ride the full sweep: `rigidbody_moving`, `rigidbody_at_rest`, `chat`,
`test`, `inputpacket`, `shipcreate`, `probe_header`, `probebits`,
`probearray`, `testdata` and `real_packet`. Between them they were every
hand-written pin/vary/sink/driver line in the harness, and every runner now
measures BenchMixed and nothing else in family `gen`.

Their type declarations and `testdata/wire` goldens survive as CONFORMANCE
fixtures — `test/main.cpp`, `test/c/main.c`, `test/bench/main.cpp`,
`test/bench/c_main.c` and the port suites gate on every one of them under
`make test`, in up to nine languages — and `examples/*.schema` is untouched.
No bench reads them.

bench_mixed's definition is `bench/corpus/Bench.schema`, and its weighting
law — integers carry at least 90% of the wire bits — is a GATE:
`bench/corpus/budget_test.go` computes the share from the schema and fails the
build below the floor, printing the full bit accounting. Two serialize.h
operations are named as NOT expressible in schema v1 rather than silently
skipped: `serialize_wstring` and `serialize_int_relative`, both deferred with
their wire already decided (SPEC §4.10).

bench_mixed is measured through the GENERATED code (`generated/bench/<lang>`)
in every runner, per the #170 profiling doctrine: generated best case, the
plain optimized build, no PGO. Its `inline` column stays `unknown` until the
§4 verdict pass learns to attribute it (named follow-on on #177).

The `bits` timed loops live in noinline symbols (`bitpacker_*_loop`) so the
§4.1 inline verdict counts the emitted body of the timed loop directly, and
every benched op has exactly two call sites (§3.2): its untimed
oracle/setup helper and its timed loop.

**Which layer the bitpacker rows call, and why (2026-09-07).** Until today the
C and C++ read legs did not measure the same work. The C leg called
`serialize_read_bits`, serialize.c's only bit-read entry point, whose past-end
test is a real branch in every build and whose refusal is terminal through a
poisoned limit. The C++ leg called `BitReader::ReadBits`, whose past-end test
is a `serialize_assert` and therefore absent under `-DNDEBUG`; C++'s read-side
check lives one layer up, in `ReadStream` (`WouldReadPastEnd`, then `Fail()`),
which that leg never touched. Measured at `-O3 -DNDEBUG` on arm64 clang, per
16-read group (instructions in the emitted noinline body, conditional
branches): C++ 135 / 2, C 270 / 33, and C with its check forced off 135 / 2 — in emitted code, the published C-vs-C++ read gap
was the check. The row was comparing a checked reader with an unchecked one.

The C++ read leg now goes through `ReadStream::SerializeBits` — same width
asserts, one past-end test per read, refusal terminal — in both its call
sites, the untimed verify gate and the timed loop. That is
`serialize_read_bits`' exact counterpart, and it is the ONLY layer the two
languages share: serialize.c ships no unchecked raw reader to meet C++'s
`BitReader` at. The maintainer's posture decides which way the row resolves:
*"on read side we MUST always do the checks!"* (2026-09-07).

The **write** legs were already fair and are untouched: `serialize_write_bits`'
capacity and range guards are `serialize_assert` (issue #52's ruling, *"C
should match C++ and have no checks on write at all (except assert)"*), so C's
stream writer and C++'s `BitWriter` are the same unchecked work under NDEBUG.
The receipt is mechanical — `bitpacker_write_loop` compiles to **317
instructions and 36 conditional branches in both languages**, unchanged by
this commit.

What the read row means now, precisely: **both legs ask their runtime for a
checked bit read.** The emitted code still differs, and the reason is a
runtime property rather than a harness one. C++'s failure latch poisons
`m_bitsRead`, the same counter the group's entry condition tests, so clang
proves all sixteen past-end tests dead and emits 135 / 2. C's latch poisons
`bits_limit`, a *second* field, while the entry condition reads `num_bits`, so
the proof does not go through and clang emits 270 / 33. Two probes pin that down. Give the C++ leg
a buffer length the compiler cannot fold and the same source emits 261 / 33 —
C's shape — so the C++ check is genuinely
compiled in, not absent. Point C's past-end test at `num_bits` instead of
`bits_limit` (a scratch probe against a copied header; the runtime is NOT
changed) and the C leg emits 135 / 2 — C++'s shape exactly. That pins the residual
C-vs-C++ bitpacker/read gap to one field indirection in serialize.c's failure
latch, in emitted code; the probe's rate was not measured, and the probe
breaks the sticky-failure contract, so it is a diagnosis and not a fix. serialize.c **v1.10.0** made
the fix: the failed-read latch moved into the cursor and the past-end test now reads `num_bits`,
exactly the shape the probe predicted, and the C leg's bitpacker/read went from **61.9% to 99.2%
of C++ best** — 76,492 to 122,221 messages a second — on the pass that moved this repo's pin
(`bench/results/2026-09-07-arm64-studio-serialize-c-1-10-0-pass.csv`).

So the read row now reports how well each runtime's CHECKED read optimizes,
which is a real difference between the runtimes — not a difference in what the
harness asked them to do. No number in this pass moved: the change is to what
the row means, and the receipt is that the C++ leg's emitted loop is
identical in shape before and after (the same 135 instructions and 2 branches;
register allocation differs).

**And the packet round trip closed the same day.** With the raw reader at
99.2% the remaining C-to-C++ distance sat in the GENERATED packet read, where
clang was spilling `stream->num_bits` and reloading it for every field's
past-end test; the C backend now emits one `serialize_read_bits_remaining`
guard at the top of a read function whose struct has a FIXED wire width, the
per-field tests fold into it (the entity read loop goes 157 to 111 instructions counting blocks by their loop header, 161 to 115 counting the contiguous region
; spill reloads per message 307 to 120), and the round trip went
from 93.1% to 101.9% of C++ best — 4,226,614 to 4,669,166 messages a second,
inside the §2.8 tie band and so reported as a tie
(`bench/results/2026-09-07-arm64-studio-c-read-guard-pass.csv`, seven
interleaved rounds, window OK, control delta 0.5%, twin gate OK).

Two further rows — `bench_string` and `bench_wstring` — are DEFINED in
BENCH-STANDARD §1.8 (measure-first, issue #64) and not yet implemented in
these runners; the definitions land ahead of any further string/wstring
optimization, and the rows themselves land as their own additive change
(corpus type, goldens, runner rows, new corpus_id).

The js leg carries the Bench-corpus shape through the FLAT generated tier
(family `gen`, `codec=flat` — THE js path), golden-gated and cross-validated
against the runtime-call tier by the same oracle.

## The codegen-only legs (java, dart, elixir)

schema's Java, Dart and Elixir backends emit SELF-CONTAINED monomorphic
codecs — no runtime library exists for these languages, so the generated
code IS their serialize path. Their runners
(`bench/java/Main.java`, `bench/dart/main.dart`, `bench/elixir/main.exs` +
`runner.exs`) measure the generated codecs over the Bench-corpus shape
under the full BENCH-STANDARD contract: the iteration count identical to
the six runners', 1 warmup + 7 measured runs (1 under `--round K`), the
committed variant corpus, §1.5 golden gate before any timing, CSV v2 rows
with `corpus_id` over the goldens loaded.

Their rows carry **family `gen`**, like every other leg. New `linkage`
values, same recorded-property rule as `esm`: `class` (Java codec
classfiles compiled beside the caller into one JVM), `aot` (Dart
whole-program AOT executable), `beam` (Elixir modules compiled beside the
caller into one BEAM VM). `checks=contract` (caller-error asserts dormant,
wire-contract validation unconditional in the reader), `opt=default`,
`inline=unknown` (no AOT artifact the §4 verdict pass can walk — Dart AOT
attribution is an open follow-on).

Peak-style numbers from these runners' earlier serialize-family form
(tight per-shape loops, warmup then best-of-five) are NOT comparable to
their rows under this contract: the statistic, loop discipline and
iteration counts all differ, and the measured harness-contract term alone
moves a number up to ~20% on the read paths (issue #170's decomposition).
Toolchains are the repo-pinned `dist/` installs (the Makefile's own
defaults; `JAVA`/`JAVAC`/`DART`/`BEAM_PATH` override).

## Runner contract (how go/rust/cs plug in)

A runner is a standalone program in `bench/<lang>/` that:

1. builds against that language's generated code (`generated/<lang>`) and its
   serialize port runtime (sibling checkout paths, same as the Makefile);
2. self-checks variant 0 against `testdata/wire/bench_mixed.bin`
   byte-for-byte and round-trips EVERY variant (decode, re-encode, same
   length, same bytes) before benching — refuse on mismatch;
3. implements the same benchmark set over the same committed variant corpus
   as the C++ reference (`bench/cpp/bench_main.cpp` — port `bench_datadriven`
   exactly). It names no field of the shape it measures: shape knowledge
   lives in the variant data and in the generated codec, nowhere else;
4. uses the same discipline: escape barriers (or the language's equivalent,
   e.g. `runtime.KeepAlive` / `std::hint::black_box` / `GC.KeepAlive`),
   warmup, 7 measured runs, median + min/max + spread. The read-side sink
   problem §2.7 named is dissolved rather than equalized: the round-trip
   path's decode is observed by its own re-encode, in every language;
5. emits CSV v2 rows on stdout (given `--csv`) in the format above with its
   own `lang` value and its recorded `linkage`/`checks`/`opt` constants,
   computing `corpus_id` from the goldens it loaded; human table on stderr;
6. supports `--round K` (§2.4): exactly one warmup plus one measured run of
   every benchmark, then exit — the driver aggregates across rounds.

`run.sh` detects each runner by its build file and runs it automatically:

- **c**: `bench/c/bench_main.c` — compiled with the repo's C flags plus
  `-O3 -DNDEBUG`, linking `$SERIALIZE_C/serialize.c`, run as `--csv`
- **go**: `bench/go/main.go` (+ `go.mod` wiring like `test/go`) — run as
  `go run . --csv`
- **rust**: `bench/rust/Cargo.toml` (+ manifest wiring like `test/rust`) —
  run as `cargo run --release -- --csv`
- **cs**: `bench/cs/*.csproj` (wiring like `test/cs`) — run as
  `dotnet run -c Release -- --csv`
- **js**: `bench/js/main.mjs` (no wiring file: the runner imports the
  serialize.js sibling by module-relative path, `SERIALIZE_JS` overrides it,
  and `--print-runtime` prints node's own resolution for the §3.5 guard) —
  run as `env NODE_ENV=production node main.mjs --csv`. NODE_ENV=production
  is the release leg: serialize.js forks checked/production at module load,
  and the runner records the mode that ran in its `checks` column
  (production = `contract` — caller validation gone, wire-contract checks
  stay; checked = `always`). `linkage=esm` (ES modules in one isolate, the
  runtime's packaging), `opt=default`, and `inline` stays `unknown` — the
  §4 verdict pass has no js branch because a JIT leg has no AOT artifact to
  attribute, so js rows never ratio against inline-filled rows. No alloc
  note: Node exposes no per-thread
  allocation counter, so the reuse discipline is structural (persistent
  holders, stream `reset()`, pre-bounded variant views). The gen-family rows
  measure the FLAT tier (`codec=flat`, §5.1) — THE js path, per-call,
  golden-gated and cross-validated against the runtime tier (bytes, fields,
  verdicts, 64 variants) before any timing. The `bits` family measures the
  serialize.js bitpacker itself and carries no codec column.

- **java**: `bench/java/Main.java` — compiled with the pinned dist JDK's
  javac (`--release 17 -Xlint:all -Werror`) beside `generated/bench/java`,
  run from `bench/java` as `java -cp ../../build/bench/java Main --csv`
- **dart**: `bench/dart/main.dart` — AOT-compiled with the pinned dist SDK
  (`dart compile exe`, the timed form), run from `bench/dart` as
  `../../build/bench/schema_bench_dart --csv`
- **elixir**: `bench/elixir/main.exs` — run from `bench/elixir` under the
  pinned BEAM toolchain PATH as `elixir main.exs --csv`

If a runner or its toolchain is missing, `run.sh` prints `SKIP <lang>`
with the reason.

## Decision probes (not legs)

`bench/tools/cs-union-form` is a **one-off decision probe**, not a
bench-standard leg: it emits no CSV row, `run.sh` does not know about it,
`make test` does not run it, and it measures no `bench/corpus/` shape. It
backs one recorded language decision — which C# spelling a table union takes,
schema#262 — with numbers anyone can reproduce (`dotnet run -c Release` in
that directory) instead of with an anecdote. Its own README states the
sitting and states plainly that nothing in it may be divided against a bench
CSV row.

A probe is the right shape when the question is about a LANGUAGE (two
storage forms of the same data) rather than about the wire. A question about
the wire belongs in a leg, under the standard — and the table wire's leg is
`bench/tables/`, below.

## The tables leg

`bench/tables/` is the second pass and the second board: ONE representative
fixed table written and read on the tolerant table wire, over
`bench/corpus/BenchTable.schema`, which mirrors `BenchMixed` field for field
so the two boards carry one shape on two wires. Its rows carry family
`table` (BENCH-STANDARD.md §1.9) and its own `corpus_id`, so nothing there can
be divided against a row here by accident.

    make bench-tables            the pass
    bench/tables/README.md       the operating manual and the port contract

It is a separate pass rather than a leg of `run.sh` for one mechanical reason:
`corpus_id` covers the goldens a RUN loaded, so folding the table corpus in
would change every `bench_mixed` row's id and make today's type numbers
un-ratioable against every earlier board.

## The nine-language table

One table, one command, per host. Per language: the fixed form's **save** and
**round trip** in nanoseconds per record and its **bytes** per record, against
that language's OWN packet wire and against C++'s fixed form.

    bench/paired/nine.sh                    # the Studio (macOS, unpinned by construction)
    bench/paired/nine.sh --lane 7           # Space, on that line's own isolated core

Both lines run `bench/paired -mode fast -fast-rounds 3` and render
`NINE.md` (and `nine.json`) into the output directory, and print the table.
`--lane N` is the Space form: it builds through `~/bin/pin-spread` so the
compilers spread over the isolated cores, then runs the WHOLE sitting through
`~/bin/bench-lane N`, which holds that core's lock once and `taskset`s every
runner the driver forks onto it. Space's lanes are 1..15 and each line owns
one; there is no coordinator in the loop.

**The tree decides the rows, not the script.** `-mode table` asks the
filesystem which `bench/tables/<lang>/` runners this checkout carries, then —
before any clock — runs each of them over ONE rotation of the 64 records and
hands the output to the same parser that will accept or refuse the measured
rows. That probe is what catches a leg whose fixed-form port has not landed:
it loads a different set of goldens, reports a different `corpus_id`, and is
not divisible against a paired packet row. Such a leg is NAMED under the table
with its refusal, exactly as a leg with no runner at all is named with that.
Neither is ever estimated, and no row is ever carried in from another sitting.

So the same unedited line runs on a leg branch, on the integration branch and
on main, each printing the truth about the tree it ran on — and a leg's row
appears on its own the day that leg lands, with nothing here to edit.

The driver measures the paired languages together (the only shape that yields
a packet ratio) and each table-only language alone; a table refuses to combine
passes that disagree about build, host, architecture, corpus id, round count
or iteration count per wire.

### The reading rule

- **Never mix hosts or modes in one row, or in one table.** A Studio number
  and a Space number are two sittings and two tables. So are `-mode fast` and
  `-mode run`. The header names the host, the CPU, the revision, the lane, the
  round and pass counts, the corpus ids and the iteration counts precisely so
  that a pasted table cannot lose which sitting it came from.
- **A loaded Studio row says so.** The table's header carries the observed
  load1 range, and a sitting with any process-monitor warning carries a bold
  line saying the host was not quiet. Read those rows accordingly — do not
  quietly drop the warning when pasting.
- **The `Wire` column is load-bearing.** A leg whose fixed-form port has not
  landed still measures the tolerant form, and its row says `form 2
  (tolerant)`. Both forms carry the same 64 logical records under the same
  corpus id, so both are divisible against one packet row — but they are not
  the same wire and the table never implies they are.
- **`% of own packet` is that language's own packet wire = 100%**, so below
  100% means the table wire beats it. A language with no packet leg reads
  `—`; no other language's packet may stand in for it.
- **`% of C++ form 3` is C++'s fixed form = 100%.** A sitting with no C++
  fixed-form row renders that whole column unavailable rather than promoting
  some other language to the reference.
- **Nothing here is certified.** `-mode fast` has reduced iteration counts, no
  bracketing drift controls and no quiet-window seal. **The certified sitting
  is `-mode run` on Space inside the quiet window**, seven rounds, with the
  operator's READY/START receipt — `bench/paired/README.md`. A fast table is
  for iteration and for a PR's "after" record; it is not a performance claim.

## The shape gate

`make shape-gate` (CI job `shape-gate`, `bench/tools/shapegate`) enforces the
one-benchmark rule mechanically. The estate has exactly one sanctioned
benchmark — this bench — and shape knowledge belongs in `bench/corpus/*.schema`
and the code the compiler generates from it. Hand-written RUNNERS are fine and
are the design; hand-written MEASUREMENT of a schema shape is not, anywhere.

The gate extracts the shape vocabulary from the corpus itself, so it tracks a
rename in the same commit, and refuses: a corpus identifier named under
`bench/`, a timing primitive anywhere outside the runner and tool directories,
a bench-shaped source path outside them, or a shape's wire size written down as
a literal.

Every place that does not yet comply is named in a `SHAPE-GATE.allow` — `bench/SHAPE-GATE.allow` for shared tooling, one beside each leg for its own — with
an exact count. The count is a ratchet: growing it fails, and so does leaving it
too high once the debt is paid. `go run ./bench/tools/shapegate -ledger`
regenerates the lines. What the gate cannot see is stated at the top of
`bench/tools/shapegate/main.go` — read it before trusting it.
