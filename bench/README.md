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

    bench/tools/pass-driver.sh [--rounds 7] [--langs cpp,c,go,rust,cs,js] [--inline]

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

## The benchmark set

One shape, plus the raw bitpacker. Family `gen` is oracle-gated per §1.5;
its iteration count is fixed and identical across all nine languages.

| bench        | family | pinned to golden | shape                                              |
|--------------|--------|------------------|----------------------------------------------------|
| bench_mixed  | gen    | bench_mixed      | **THE canonical benchmark** (#184): every construct the schema language expresses, in one representative game message, integers carrying 91.87% of the wire bits (438 B) |
| bitpacker    | bits   | — (read-back verified in setup) | 16-width table over a 64 KiB buffer, 24576 passes/run (§1.4) |

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

Every place that does not yet comply is named in `bench/SHAPE-GATE.allow` with
an exact count. The count is a ratchet: growing it fails, and so does leaving it
too high once the debt is paid. `go run ./bench/tools/shapegate -ledger`
regenerates the lines. What the gate cannot see is stated at the top of
`bench/tools/shapegate/main.go` — read it before trusting it.
