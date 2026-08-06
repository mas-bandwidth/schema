# bench — cross-language serialize profiling harness

Measures the schema-GENERATED code per language against its serialize runtime:
write path and read path, messages/sec and MB/sec, over the pinned corpus
instances (the same instances `test/main.cpp` pins to `testdata/wire/*.bin`)
plus one large synthetic message batch (4096 mixed messages through the
Message dispatch surface) for steady-state throughput.

`bench/run.sh` builds and runs whichever language runners are available and
collects everything into one CSV under `bench/results/`. The C++ runner
(`bench/cpp/bench_main.cpp`) is the reference implementation; the go, rust,
and cs runners (`bench/go`, `bench/rust`, `bench/cs`) are its ports, wired
per the contract below.

## Running

    bench/run.sh                 # Release, results in bench/results/<date>-<arch>-<host>.csv
    bench/run.sh --debug         # also the Debug pair (matched-pair methodology)
    SERIALIZE=path/to/serialize bench/run.sh
    BENCH_NOISE="NOISY: ..." bench/run.sh    # label a noisy host in the preamble

`make bench` runs the Release pass.

## Methodology (why the numbers can be trusted)

Follows the serialize repo's `bench.cpp` conventions (see the `const-params`
experiment there for the reasoning, learned the hard way):

- **Escape barriers** — the output buffer and the decoded object are observed
  through an empty-asm memory clobber, so the compiler cannot delete the work
  and report fictional throughput.
- **Per-iteration variation** — every write loop mutates message fields
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
- **Fixed iteration counts, warmup, median-of-7** — one warmup run per path,
  then 7 measured runs; the report is the median rate with min/max and
  spread (`(max-min)/median`). Only Release numbers are meaningful; the
  Debug pair exists so pathological debug regressions are visible.
- **Pinning** — `taskset -c $BENCH_CPU` where taskset exists (Linux); none on
  macOS. The preamble records pinning and the host noise label.
- **MB/s means MiB/s** (1024*1024), following serialize `bench.cpp`.

C++ flags: the schema repo's own flags (`-std=c++17 -Wall -Wextra -Werror
-ffp-contract=off`) plus the serialize repo's Release bench configuration
(`-O3 -DNDEBUG -fno-rtti -DSERIALIZE_RELEASE`). Deliberate divergence from
serialize's own bench: **no `-ffast-math`** — this repo pins wire determinism
with `-ffp-contract=off` and the generated quantize paths do real float math.
Every results file records the exact compiler and flags in its preamble.

## Results format

One CSV per host+build, preamble lines starting `#` (date, host, arch, os,
cpu, build, compiler, flags, pinning, noise label, schema + runtime commits),
then rows:

    lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct

`path` is `write` or `read`. `bytes_per_op` is the actual wire bytes per
message (constant per benchmark by construction). Human-readable tables live
beside the CSVs in `bench/results/`.

## The benchmark set

| bench             | pinned to golden      | shape                                             |
|-------------------|-----------------------|---------------------------------------------------|
| rigidbody_moving  | rigidbody_moving      | 13 doubles + branch bool (105 B)                  |
| rigidbody_at_rest | rigidbody_at_rest     | the untaken branch (57 B)                         |
| chat              | chat                  | string framing, 11 chars                          |
| test              | —                     | tiny: 16 raw bits + 3 ranged ints                 |
| inputpacket       | inputpacket           | counted array of nested Input                     |
| shipcreate        | shipcreate_flags      | quantized composites + bool-gated flags           |
| ship_shallow      | ship_shallow          | object view: 10 ranged ints + flags + projections |
| probe_header      | probe_header          | wire const + reserved + align + 64 bits           |
| probebits         | probebits             | odd bit widths (9/33/64) + full-range ints        |
| probearray        | probearray            | nested samples, both branch arms, counted arrays  |
| testdata          | testdata              | the everything message (floats, strings, arrays)  |
| message_batch     | (message_stream golden checks the dispatch wire) | 4096 mixed messages + terminator through WriteMessage/ReadMessage, steady-state |

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
5. emits CSV rows on stdout (given `--csv`) in the format above with its own
   `lang` value, human table on stderr.

`run.sh` detects each runner by its build file and runs it automatically:

- **go**: `bench/go/main.go` (+ `go.mod` wiring like `test/go`) — run as
  `go run . --csv`
- **rust**: `bench/rust/Cargo.toml` (+ manifest wiring like `test/rust`) —
  run as `cargo run --release -- --csv`
- **cs**: `bench/cs/*.csproj` (wiring like `test/cs`) — run as
  `dotnet run -c Release -- --csv`

If a runner or its toolchain is missing, `run.sh` prints `SKIP <lang>`
with the reason.
