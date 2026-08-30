# bench — cross-language serialize profiling harness

Measures three families per language, every row labelled with its family
(§1 of the standard):

- **`gen`** — the schema-GENERATED code against its serialize runtime: write
  and read over the pinned corpus instances (the same instances
  `test/main.cpp` pins to `testdata/wire/*.bin`, plus `real_packet` — the
  §1.7 realistic snapshot `test/bench/main.cpp` pins).
- **`rt`** — the serialize runtime API called BY HAND: the four
  `bench/corpus/Bench.schema` shapes as hand-written packets, §1.5
  oracle-gated against the goldens the GENERATED code pinned
  (`testdata/wire/bench_*.bin`, produced by `test/bench/main.cpp`). A
  mismatch refuses the entire run: a failing runner emits NO rows.
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
backends' GENERATED codecs over the four Bench-corpus shapes under the
same contract (see "The codegen-only legs" below).

## Running

    bench/run.sh                 # Release, results in bench/results/<date>-<arch>-<host>.csv
    bench/run.sh --debug         # also the Debug pair (matched-pair methodology)
    bench/run.sh --only c|cpp|go|rust|cs|js|java|dart|elixir   # one language leg
    bench/run.sh --quick         # the iteration instrument: bench_mixed only,
                                 # 3 measured runs per leg, golden gate intact,
                                 # and the blended table (per-message time
                                 # averaged over write+read, fastest = 100%)
                                 # printed after the CSV. NEVER a
                                 # certification run; scaling constants are
                                 # PROPOSED in BENCH-STANDARD.md §2.8.
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

`path` is `write` or `read`. `bytes_per_op` is the actual wire bytes per
message (constant per benchmark by construction). The six v2 columns carry
what the row measured: `corpus_id` (FNV-1a-64 of the goldens the runner
actually loaded, §1.6 — corpus drift becomes a tool error, not a published
ratio), `family` (`gen` | `rt` | `bits`, per row), `linkage`/`checks`/`opt` (the recorded
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

| bench             | pinned to golden      | shape                                             |
|-------------------|-----------------------|---------------------------------------------------|
| rigidbody_moving  | rigidbody_moving      | 13 doubles + branch bool (105 B)                  |
| rigidbody_at_rest | rigidbody_at_rest     | the untaken branch (57 B)                         |
| chat              | chat                  | string framing, 11 chars                          |
| test              | —                     | tiny: 16 raw bits + 3 ranged ints                 |
| inputpacket       | inputpacket           | counted array of nested Input                     |
| shipcreate        | shipcreate_flags      | quantized composites + bool-gated flags           |
| probe_header      | probe_header          | wire const + reserved + align + 64 bits           |
| probebits         | probebits             | odd bit widths (9/33/64) + full-range ints        |
| probearray        | probearray            | nested samples, both branch arms, counted arrays  |
| testdata          | testdata              | the everything message (floats, strings, arrays)  |
| real_packet       | real_packet           | the §1.7 realistic snapshot (`bench/corpus/RealWorld.schema`): ~93 riding individually serialized small fields of every scalar kind, 204 B, 0% bulk share by bits; pin = the all-defaults instance |

Family `rt` (hand-written runtime API, oracle-gated per §1.5; iteration
counts fixed and identical across all six languages) and family `bits`:

| bench        | family | pinned to golden | shape                                              |
|--------------|--------|------------------|----------------------------------------------------|
| bench_packet | rt     | bench_packet     | the serialize/bench.cpp stream packet (49 B)       |
| bench_ints   | rt     | bench_ints       | 10 ranged ints (14 B)                              |
| bench_bits   | rt     | bench_bits       | 8 raw bit fields incl. one 48-bit (20 B)           |
| bench_mixed  | rt     | bench_mixed      | a generated-looking packet, hand-written (21 B)    |
| bitpacker    | bits   | — (read-back verified in setup) | 16-width table over a 64 KiB buffer, 24576 passes/run (§1.4) |

The `rt` timed loops live in noinline symbols (`rt_bench_*_write_loop` /
`..._read_loop`, and `bitpacker_*_loop`) so the §4.1 inline verdict counts
the emitted body of the timed loop directly, and every benched op has
exactly two call sites (§3.2): its untimed oracle/setup helper and its
timed loop.

Two further `rt` rows — `bench_string` and `bench_wstring` — are DEFINED in
BENCH-STANDARD §1.8 (measure-first, issue #64) and not yet implemented in
these runners; the definitions land ahead of any further string/wstring
optimization, and the rows themselves land as their own additive change
(corpus type, goldens, runner rows, new corpus_id).

The js leg additionally carries the four Bench-corpus shapes through the
FLAT generated tier (family `gen`, `codec=flat` — THE js path), golden-
gated and cross-validated against the runtime-call tier by the same oracle
as the corpus shapes; its family `rt` rows keep the same bench names and
measure the serialize.js runtime API beside them.

## The codegen-only legs (java, dart, elixir)

schema's Java, Dart and Elixir backends emit SELF-CONTAINED monomorphic
codecs — no runtime library exists for these languages, so the generated
code IS their serialize path. Their runners
(`bench/java/Main.java`, `bench/dart/main.dart`, `bench/elixir/main.exs` +
`runner.exs`) measure the generated codecs over the four Bench-corpus
shapes under the full BENCH-STANDARD contract: fixed iteration counts
identical to the six runners' rows for these benches, 1 warmup + 7
measured runs (1 under `--round K`), per-iteration LCG variation, 64
rotating read variants, §1.5 golden gate before any timing, CSV v2 rows
with `corpus_id` over the goldens loaded.

Their rows carry **family `gen`** — a ratio against another language's
family `rt` row (the runtime API called by hand) is a subject difference,
not a language difference, and `relative.go` refuses it. New `linkage`
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
2. self-checks the pinned corpus instances against `testdata/wire/*.bin`
   byte-for-byte and round-trips them before benching — refuse on mismatch;
3. implements the same benchmark set, the same pinned instances, the same
   LCG, and the same vary-function field mappings as the C++ reference
   (`bench/cpp/bench_main.cpp` — port the `pin_*` and `vary_*` functions and
   the batch builder exactly);
4. uses the same discipline: escape barriers (or the language's equivalent,
   e.g. `runtime.KeepAlive` / `std::hint::black_box` / `GC.KeepAlive`),
   warmup, 7 measured runs, median + min/max + spread;
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
  attribute, so js rows never ratio against inline-filled rows. Carries the
  full C/C++ row set including `real_packet` (`generated/bench/js/realworld`
  already rides `make test`). No alloc note: Node exposes no per-thread
  allocation counter, so the reuse discipline is structural (persistent
  holders, stream `reset()`, pre-bounded variant views). The gen-family rows
  measure the FLAT tier (`codec=flat`, §5.1) — THE js path, per-call,
  golden-gated and cross-validated against the runtime tier (bytes, fields,
  verdicts, 64 variants) before any timing; the runtime-call generated rows
  ride as labeled supplementary rows (`codec=runtime`). The `rt` and `bits`
  families measure the serialize.js library itself and carry no codec column.

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
