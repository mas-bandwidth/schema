# C++ identity plan versus independent straight-line reference

Status: **FAIL — measurement did not run.**

The recorded command builds the C++ paired runner, which requires the
`serialize.h` C++ runtime. That runtime is not present in this checkout,
so the build fails before any timing can occur.

## Recorded command

```
go build -o bin/schema-paired ./bench/paired && bin/schema-paired -mode gate -langs cpp
```

## Host and revision

- host: `hulk`
- load (start): 9.38 10.50 15.90
- revision: `ead804093232b3593facd987ebe2f726a10dd63d`
- branch: `fixed-table-form`

## Corpus

The fixed-form corpus files are present (`bench/paired/corpus/bench_fixed.bin`,
`bench_fixed.layout`, etc.) and the reference reader
`test/bench/fixedform_measure.cpp` (arm B, straight-line constant-offset loads)
exists. No `corpus_id` was produced because the paired driver never reached the
gate.

## Iterations and spread

Per `bench/BENCH-STANDARD.md` §2.1 / §1.9, the fixed-form table leg would use
400,000 iterations. Warmup, rounds, and spread were never measured.

## Rates and ratio

| metric | value |
| --- | --- |
| identity-plan read | **N/A** (build failed) |
| straight-line reference read | **N/A** (build failed) |
| ratio identity / straight-line | **N/A** |
| bound | 1.5x (`bench/BENCH-STANDARD.md` §1.10 / docs/SPEC-TABLES.md §3.4) |
| verdict | **FAIL** |

## Build failure

```
+ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -Igenerated/bench/paired/cpp -I/home/gaffer/rowan-working/tmp/2be4c180-7b8d-4805-a71d-c451c1634cfa-card-schema-gate-reference-cost/jobs/card-schema-gate-reference-cost/serialize test/bench/paired_main.cpp -o build/paired/corpus
In file included from test/bench/paired_main.cpp:9:
generated/bench/paired/cpp/BenchWire.h:12:10: fatal error: serialize.h: No such file or directory
   12 | #include "serialize.h"
      |          ^~~~~~~~~~~~~
compilation terminated.
paired: exit status 1
```

The expected C++ runtime path (`../serialize` relative to the repo root) does
not exist in this environment, and the generated packet headers require
`serialize.h`. Without that runtime the C++ leg cannot be built, timed, or
gated.

## Control re-run

Re-running the recorded command reproduces the same `serialize.h: No such file
or directory` failure.
