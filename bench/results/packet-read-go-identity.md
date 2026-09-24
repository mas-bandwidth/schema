# 2026-09-23, spacegame.losangeles — schema-gate-packet-read-go-identity

The probe for gates/packet-read/go/identity — "go: identity fixed read versus
its own packet read" — on the swarm bench at `ead80409`, in one sitting. The
DONE-WHEN clause in the card calls for the identity-lane fixed read and the
go packet read of the same logical records and a FIXED-FASTER / FIXED-SLOWER
verdict with a ratio. STEP 2 of the card says "a gate refusal is the finding
(record it and stop with RED <line>)". This file records the finding, full
stop: the gate refuses and the measurement is not produced.

## Verdict

**RED 2.** STEP 2's gate fails before any clock is started — the build of the
C++ corpus binary (`build/paired/corpus`), which the driver compiles for every
`-langs` set including `-langs go` as the "independent C++ bridge [...] the
equivalence oracle even for a subset pass" (`bench/paired/main.go:622`), does
not link: `BenchWire.h` and the other paired/cpp headers
`#include "serialize.h"` and the driver has no serialize sibling under
`../serialize` (the default `SERIALIZE`) to take it from. STEP 3's
`-mode fast` cannot start — its precheck requires an existing `build/paired/
build.json` recording the requested wire/language binaries, and that file is
not produced because `generateAndBuild` returns non-zero in STEP 2.

## What the gate actually said

The full command and its stdout:

```
$ bin/schema-paired -mode gate -langs go
+ go build -o build/paired/schema ./cmd/schema
+ build/paired/schema generate --lang go --out generated/bench/paired/go bench/corpus/Bench.schema bench/corpus/FixedTable.schema
+ build/paired/schema generate --lang go --out generated/bench/go bench/corpus/Bench.schema
+ build/paired/schema generate --lang cpp --out generated/bench/paired/cpp bench/corpus/Bench.schema bench/corpus/FixedTable.schema
+ c++ -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_OPT="O3" -Igenerated/bench/paired/cpp -I/home/ubuntu/rowan-working/tmp/99770727-80ae-4954-ac40-c6afade2a49b-card-schema-gate-packet-read-go-identity/jobs/card-schema-gate-packet-read-go-identity/serialize test/bench/paired_main.cpp -o build/paired/corpus
In file included from test/bench/paired_main.cpp:9:
generated/bench/paired/cpp/BenchWire.h:12:10: fatal error: serialize.h: No such file or directory
   12 | #include "serialize.h"
      |          ^~~~~~~~~~~~~
compilation terminated.
paired: exit status 1
```

The C++ bridge is the corpus-pin producer (`test/bench/paired_main.cpp`,
`README.md` §1.5's "shared producer verified packet -> table -> packet
identity"). Without it, `bench/paired/corpus/bench_fixed.bin`,
`bench_fixed.layout`, `bench_table.bin`, `bench_table.lengths` and
`bench_table.variants.bin` (the corpora the rows are measured over) cannot
be regenerated, and `bin/schema-paired -mode fast` cannot move from the
build to the clock. The preflight on the card says "fix what it names"; what
it names is a missing sibling checkout, which the card's rules forbid
fixing by network ("No network beyond the clone you were given").

## Preamble essentials

| | |
|---|---|
| date | 2026-09-23T00:07:07Z |
| machine | `spacegame.losangeles`, swarm bench |
| up at start | 6 days, 27 min, 0 users |
| load at start | 124.02 / 110.51 / 89.09 (1-min, 5-min, 15-min) |
| load at end | 50.20 / 80.44 / 82.13 |
| build | Release (`-O3`, `-DNDEBUG`, `BENCH_OPT="O3"`) |
| schema commit | `ead804093232b3593facd987ebe2f726a10dd63d` (= the base on the card) |
| SHA match | yes — STEP 1's `git rev-parse HEAD` returns `ead80409...` |

## State of the corpus

The paired half's committed corpus files are present at their tree paths
(status: clean). Their SHA-256s:

| file | SHA-256 |
|---|---|
| `bench/paired/corpus/bench_fixed.bin` | `431e2db6318dcbb1bace9ab6f1221457a9a4756ae863c2a6673a7b13e785a67f` |
| `bench/paired/corpus/bench_fixed.layout` | `532f366d89060f09c3936fa368d866d2dd9468724ce705d2e3656016db04c2f3` |
| `bench/paired/corpus/bench_table.bin` | `720bae2165ea73670b63f015480dfa7df4343f8b2951b08a6461c5595123f65e` |
| `bench/paired/corpus/bench_table.lengths` | `feda54a482e4f3317b27ce578cd952c8dd65f6b7f790ec9037050e64842834b0` |
| `bench/paired/corpus/bench_table.variants.bin` | `0174c5c6bfcfa74302e0370fbc66097d9e7744e01577b23589581eb18d5d18ce` |

`corpus_id` was not emitted by `readBuild` (the gate did not reach that
line): the only `corpus_id` an authoritative row may carry is the one the
driver recomputes from the goldens after a clean gate, and the build never
got there (`bench/BENCH-STANDARD.md` §1.6). Reporting a hand-computed
`FNV-1a-64` over those five files would be inventing a number — exactly
what §1.6 exists to prevent — so it is not stated here.

## Iterations, spread, ratio

There are no measurements: STEP 3 does not run, the warmup is not done, the
seven measured rounds do not happen, the best and median rates are not
emitted by `parseRowsForIterations`, the spread is not computed, and the
`bench_fixed_read` vs `bench_mixed_read` identity-lane row does not exist
on this sitting's clock. The verdict in the card's DONE-WHEN clause has
nothing to be FIXED-FASTER or FIXED-SLOWER of.

## Control, re-run

STEP 4's control re-run reproduces the same refusal: the C++ bridge needs
the serialize sibling and the second invocation fails on the same include,
so the verdict "RED 2" carries forward unchanged and is not NOISY. Were the
gate to start running on its sibling in a future sitting, the same command
as STEP 3 — `bin/schema-paired -mode fast -langs go -out
build/paired-fast/go-identity -noise-note "..."` — would produce the
identity read and packet read the card asks for; that restart is outside
this card's scope.

## Source references

- gate refusal: `bench/paired/main.go:622-636` (the C++ bridge build
  before the per-language loop, run for every `-langs` set except when
  `cpp` is already in the list).
- STEP 3 fast precondition: `bench/paired/fast.go:298-304` (requires
  `info.Binaries[binary(wire, lang)]`, populated only by a clean
  `generateAndBuild`).
- card text: STEP 2, "a gate refusal is the finding (record it and stop
  with RED <line>)".
