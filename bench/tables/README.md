# bench/tables — the tables bench

For the current four-language **Fixed Table vs Packet Wire** comparison, use
[`bench/paired`](../paired/README.md). It carries identical logical data on both
wires. This page documents the retained representative table corpus and its
historical passes; their independent values cannot supply the paired ratio.


**One measured shape: a representative fixed table, written and read on the
tolerant wire.** That is the whole leg, and it is the tables layer's
per-language release gate.

The owner's framing (issue #330): *"At minimum we should have a representative
fixed table"* / *"that's the sort of equivalent to like protobufs/flatbuffers"*
/ *"I'd hate to call any of these tables prod ready if we haven't stress tested
and profiled them each per-lang."* A person who has a number for protobuf or
flatbuffers has a number for this, because it is the same job: a message of
scalars, strings, an enum, a union, a nested record and bounded arrays of
records, framed by ids, kinds and lengths.

## Scope and performance standard

The current profile is the fixed table on the variable form (form 1) in **C, C++, C# and Go**.
Glenn's September 8 direction is the **fastest correct implementation for each
language**. The shared workload fixes the work being measured; each language may
use its best correct representation, compiler and runtime techniques. Optimize
from profiles and confirm gains with paired measurements and correctness checks.

The next representative corpus is a variable table. Message form, block form,
and cooked save/load follow as separate measured operations. They do not acquire
a performance result from this fixed-table pass.

**The FIXED form (form 3) is measured through the paired driver, not through
this board.** `bench/paired/main.go` names the fixed rows `bench_fixed` and
answers them to the paired corpus's own id; `bench/tables/cpp`, `bench/tables/c`
and `bench/tables/elixir` carry that path. The Elixir leg carries **form 3 and
nothing else** — form 1 is deferred to schema#515, so it has no `bench_table`
row and therefore no `leg` script on this board yet; `run.sh`'s row filter
accepts `bench_table` only. That registration is the named follow-on, and it is
what would put Elixir on the board below rather than only in a paired
diagnostic.

## The corpus

[`bench/corpus/BenchTable.schema`](../corpus/BenchTable.schema) declares
`TableMixed`, the existing fixed-table counterpart of the representative
[`BenchMixed`](../corpus/Bench.schema) packet workload: eight nested entity
updates, eighty statistic records, a tagged event, bounded text and payload,
and mixed scalar fields. Every language generates its codec from this one file.
The runners name the generated root once and do not duplicate its fields.

This retained corpus predates wide table scalars. It substitutes compressed
floats for the packet's fixed-point fields and 64-bit integers for its 128-bit
fields, and replaces/drops packet framing declarations. Those are properties of
this benchmark version, **not current language limitations**. Its range/default
and enum-presence choices keep all variants the same length. It therefore
measures a representative counterpart, not an identical scalar payload or the
isolated cost of tolerance. A future corpus that restores these kinds needs its
own version and golden identity; do not silently reinterpret older results.

The current pinned file is **2,147 bytes** per record. `bench-table-check`
reconstructs all 64 records and compares them byte-for-byte before profiling.
The packet corpus and its historical measurements are unchanged.

## The data, and why no runner builds an instance

`test/bench/table_main.cpp` is the corpus producer and the oracle. It is the
ONE place that names a field of `BenchTable.schema`; every language leg reads
its output blind, exactly as the type legs read
`bench/corpus/variants/bench_mixed.variants.bin` (BENCH-STANDARD.md §1.5).

    make bench-table-corpus     rewrite the golden and the variant corpus
    make bench-table-check      rebuild both in memory and byte-compare

    testdata/wire/bench_table.bin                     record 0, the pinned instance
    bench/corpus/variants/bench_table.variants.bin    64 records, record 0 first

**Its refusal to know about is the record-length one.** The tolerant wire
elides a field holding its declared default (§3), so unlike the bitpacked type
wire it frames by PRESENCE and not by width: a varied value landing on zero
would silently shorten a record and move `bytes_per_op` under §2.7. The
producer therefore holds every value off its default by construction — bools
true, the enum never `None`, no zero scalar, string and byte-buffer lengths
pinned — and refuses to emit a corpus whose 64 records are not all the same
length. That check is what proves the construction held.

**The id table is part of that length.** A record's trailer carries one
eight-byte entry per distinct id the record uses, and an enum rides as its
VARIANT's id, so a variant drawn at random moves the record length by eight
bytes per entry the draw added or dropped. Every enum is therefore pinned by
its slot rather than drawn: the eight entity slots take `Fists` through
`Grenade` in every record, so the set of ids is fixed while the value still
differs from slot to slot. A `flags` needs no such pin, because it rides as
raw bits and names no id.

## The protocol

The sitting is the type bench's, clause for clause (BENCH-STANDARD.md §1.5,
§2.1–§2.4, §2.7, §2.9, §5.1):

- **The golden gate runs before the clock.** Variant 0 is byte-compared to
  `testdata/wire/bench_table.bin`, then every one of the 64 variants must
  load, re-save at the same length, and come back byte-identical. A leg that
  fails refuses to produce numbers and emits no rows.
- **1 warmup + 7 measured runs**, median reported beside min, max and spread.
  `--round K` drops that to one warmup and one measured run so a driver can
  interleave legs across rounds and aggregate itself.
- **The rows are `write` and `round_trip`** (§2.9's contract), and `read` is
  DERIVED — round-trip minus write — printed to stderr and never emitted as a
  row, because a derived number in a CSV gets divided as if it had been
  measured.
- **No arm can be dead-code eliminated.** The write arm's result is folded
  into a `volatile` sink and the buffer is observed through an empty-asm
  memory clobber (C++) or `GC.KeepAlive` and the same sink (C#). The read arm
  needs no sink of its own: its output IS the re-save's input, so every loaded
  field is observed by construction.
- **Variation is the 64 rotating instances**, and the producer proves they are
  pairwise distinct, so no single buffer can be memorised by the branch
  predictor or the caches. `bytes_per_op` is constant by construction rather
  than by assertion.
- **Public `Load` restores declared defaults** before overlaying the fields
  on the wire. That work stays inside the clock; the runner adds no separate
  reset. Before timing, two complete corpus rotations load into one reused
  target without caller resets and must re-save byte-identically, including
  the last-to-first transition.

### Why this is a separate pass from `bench/run.sh`

A run's `corpus_id` is FNV-1a-64 over the goldens THAT RUN LOADED (§1.6).
Folding the table corpus into the type pass would change the `corpus_id` of
every `bench_mixed` row, and the tools would then correctly refuse to divide
today's type numbers against any earlier board. Two corpora, two passes, two
boards. Every row here also carries **family `table`** (§1.9), so a
cross-family division refuses on its own (§5.3) — a tolerant-wire number and a
bitpacked-wire number are not the same measurement and the tools say so
without anyone remembering to.

## The four-language pass

C and C++ use their generated fixed codecs in release builds. Go uses its
generated package in the ordinary optimized build. C# currently uses generated
managed authoring values with reused output storage. The configuration is part
of every result; a different representation or JIT setting must be measured
and identified as a separate candidate before being called faster.

A passing shared corpus is required before any timing. The broader table
conformance, unit, ownership and wire checks remain independent acceptance
evidence. A passing fixed-table corpus does not clear an unrelated failing
message or variable-table check.

## Running

    bench/tables/run.sh                    every registered leg -> bench/tables/results/
    bench/tables/run.sh --only c,cpp,cs,go --gate --bare
                                          build and check all four, no timing
    bench/tables/run.sh --only c,cpp,cs,go --rounds 7 --tag fixed
                                          the four-language interleaved pass
    bench/tables/run.sh --only cs          one required leg
    bench/tables/run.sh --rounds 5         interleaved rounds (§2.4)
    bench/tables/run.sh --tag pairing      name the sitting in the file name
    bench/tables/run.sh --bare             rows only, no preamble, stdout

`make bench-tables` runs the default pass. An explicitly selected language that
cannot build is a failure, not a skipped row. All selected legs build before
measurement. For multiple rounds, every language runs once before the next
round begins; the packet benchmark's aggregation tool checks measurement
identity and computes the combined statistics. A failed leg emits no completed
pass: stdout and the result file are published only after every round succeeds.

Record the machine, toolchains, optimization settings, revision, corpus and
noise for every pass. Run one benchmark at a time and coordinate a quiet window
on a shared machine. A Studio result is a local measurement; it is not a
production-server certification. A server deployment or server benchmark keeps
its own access and operational approval requirements.

## Registering a port

**One command at `bench/tables/<lang>/leg`, and nothing else.** `run.sh`
discovers every leg by that path, in name order: no case in it, no flag, no
list anywhere. The unit it runs over is generated by the leg's own
`make/<lang>.mk`, which registers the stamp in `BENCH_TABLES_LEGS` so `make
bench-tables` generates it (`docs/CONTRIBUTING.md`, "Adding a language").

1. Write a command at `bench/tables/<lang>/leg`, run from the repository root,
   answering two verbs:

       leg build            build the leg; exit 2 if its toolchain or
                            generated sources are not present (that prints
                            SKIP and is not a failure)
       leg run [args...]    run the runner: --gate (no timing), --csv, --round K, --wire-dir,
                            --variant-dir

2. Write the runner itself in `bench/tables/<lang>/`. Port
   `bench/tables/cpp/table_main.cpp` — it is the reference implementation —
   and keep the two properties that make the board mean anything:

   - **shape-blind**: name the generated type at one call site and nothing
     else. No field, no pinned value, no wire size. `make shape-gate` holds
     this mechanically, and every place that does not comply is named with an
     exact count in `bench/SHAPE-GATE.allow`.
   - **gated before the clock**: the golden and the 64-variant round trip, or
     no rows.

3. Generate the unit in `make/<lang>.mk` and add its stamp to
   `BENCH_TABLES_LEGS` there.

The results page shows one blended percentage per language: the fastest
measured implementation is 100%, and twice its cost is 200%. Use the best
round-trip rate, as the packet README does; do not add write to round-trip.
Keep the page to the two-column table. Raw timings, spread and methodology
live in a separate supporting record. The implementation target remains the
fastest correct code for each language.

## The board

`bench/tables/results/` holds one CSV per sitting, with a human board beside
it. The CSV is CSV v2 (§5.1) and `bench/tools/relative.go` renders and refuses
it under the same nine rules as the type board's:

    go run ./bench/tools abs bench/tables/results/<sitting>.csv

## Named follow-ons

Stated so a reader knows what is not here, and why:

- **A Go leg's `opt` column is `default`.** Go has one optimisation
  configuration and no flag to name, so the column says what is true rather
  than borrowing C++'s spelling. `linkage` is `pkg`: the generated table codec
  is ordinary package code in the leg's binary and names no runtime at all.

- **`relative rel` is C-referenced.** This historical
  tool convention does not select the results page's reference: that page
  normalizes to the fastest measured implementation for its own bench.
- **The tables emitters are not LOCKed** the way `bench/LOCK` locks the type
  emitters. The lock is a ruling about a profiling round, not a side effect of
  a board existing; it belongs to the round the owner opens after the first
  box sitting reads.
- **`bench/tables/rust` has a runner and NO `leg`.** `run.sh` discovers a leg
  by the path `bench/tables/<lang>/leg`, and this page's leg measures the
  TOLERANT wire — which the Rust port does not have: its backend emits the
  fixed form and the two accelerators and no form-1 codec at all. So there is
  nothing here for this pass to run, and registering a command that could only
  ever SKIP would put a row of noise on the board.
  `bench/tables/rust/src/main.rs` is instead the FIXED form's leg for
  [`bench/paired`](../paired/README.md), where it is a table-only language: it
  names the same flags and emits the same CSV columns as the C++ reference, it
  is gated by `make tables-rust-fixed-matched`, and its rows are named
  `bench_fixed`. When a form-1 Rust wire lands, its `leg` lands with it.

- **The `inline` column stays `unknown`** for both legs. The §4 verdict pass
  has no branch for the generated table codec, which is the same open item the
  type board's data-driven rows carry.
