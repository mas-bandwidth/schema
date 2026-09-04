# bench/tables — the tables bench

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

## What it measures, and what it does not

| | measured here | where it lives instead |
|---|---|---|
| tolerant wire, write + read | **yes — every language** | this leg |
| block form fill and read | no | `make tables-block-gate2` (§12.1), and the game, on real render data |
| cook open cost | no | `test/tables/cook_main.cpp`'s open-cost gate (§7.5) |
| the JSON walk, `schema pack` | no, by design | tooling; not a hot path |

That split is the owner's scope ruling on the same issue: *"I think that's
sufficient for now for the profiling, the rest can be C++/C# specific with
render data tables as we work in space."* The per-language board is the
tolerant wire. Block and cook numbers stay C++/C#, taken in the game on real
render data, because that is where the render path actually is.

## The corpus

`bench/corpus/BenchTable.schema` declares `TableMixed`, and it **mirrors
`BenchMixed`** — the type corpus's one measured shape — field for field: same
field count, same kinds, same nesting, same bounds, same pinned structure. The
owner's sizing rule, and his reason for it: *"Make the fixed table roughly
equivalent to the profiled type in size"* … *"so we don't hyper-fixate on
serializing a few fields and it's all memcpy."*

Four of BenchMixed's declarations are refused in a table body by name
(docs/SPEC-TABLES.md §11) and each is replaced by the nearest kind that does
the same per-field work; the schema file lists all of them in its header, with
the two that drop and why. The mirror is what makes the two boards
comparable: the same shape on the bitpacked type wire and on the tolerant
table wire, so the ratio between them is the price of tolerance and nothing
else.

Pinned wire: **2391 bytes**, against BenchMixed's 438 — the ids, kinds and
lengths, made visible. **1487 of those 2391, or 62%, are framing**: field id
references, kind bytes, lengths, element counts and body terminators, not
values. That count is the price of tolerance, stated as a number so the ratio
is a measurement rather than an impression, and it is the figure
docs/SPEC-TABLES.md's performance ladder cites from this page. What it does
not license is a codec slower than those bytes require: the per-BYTE cost is
this leg's own obligation, so a per-byte gap is a defect to explain or close
and never the wire's price.

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
- **The read arm resets before it loads**, and that is not overhead the runner
  added: `Load` fills only the fields that actually rode, so a reused instance
  would otherwise keep the previous record's values in the elided ones.
  Resetting is part of a correct read into reused storage, in every language,
  so it sits inside the clock rather than hidden outside it.

### Why this is a separate pass from `bench/run.sh`

A run's `corpus_id` is FNV-1a-64 over the goldens THAT RUN LOADED (§1.6).
Folding the table corpus into the type pass would change the `corpus_id` of
every `bench_mixed` row, and the tools would then correctly refuse to divide
today's type numbers against any earlier board. Two corpora, two passes, two
boards. Every row here also carries **family `table`** (§1.9), so a
cross-family division refuses on its own (§5.3) — a tolerant-wire number and a
bitpacked-wire number are not the same measurement and the tools say so
without anyone remembering to.

## The legs, and what each one's number means

| leg | tier | what the number is |
|---|---|---|
| `cpp` | the reference | the bar every other row divides against |
| `cs`, `go`, `rust` | native or JIT | same shape, same corpus, a codec compiled to machine code |
| `elixir` | the READING TIER | what the BEAM costs: `save` allocates its own result because there is no caller-owned buffer, and `load` builds a term per field. About 37x C++ on write and 33x on round-trip, measured — a PAIRING CHECK, not a sitting (`results/2026-09-03-pairing-elixir-arm64-macbook.md`), with the lever named against #174 |

**A reading-tier row is a number to be REPORTED, not a number to be matched.**
The ladder in docs/SPEC-TABLES.md sets the bar for a language that can hold
bytes; a language whose values are the runtime's holds a different one, and
saying so is the point of the row.

## Running

    bench/tables/run.sh                    every registered leg -> bench/tables/results/
    bench/tables/run.sh --only cs          one leg
    bench/tables/run.sh --rounds 5         interleaved rounds (§2.4)
    bench/tables/run.sh --tag pairing      name the sitting in the file name
    bench/tables/run.sh --bare             rows only, no preamble, stdout

`make bench-tables` runs the default pass.

**A publishable number is a BOX sitting**, not a laptop run: core 15, the
server stopped, not live, one bench at a time, blessed per run. A run on a
shared interactive machine is a pairing check — it tells you the legs agree
and roughly where they stand, and it certifies nothing. Say which one a board
is, in the board.

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
       leg run [args...]    run the runner: --csv, --round K, --wire-dir,
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

**The ratio to C++ is the port's speed bar** — *same speed, or not
significantly slower* — the same bar the ladder sets for every rung
(docs/SPEC-TABLES.md, the performance ladder). A port that lands wide of it
has a defect to explain or close, not a trade to license.

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

- **`relative rel` is C-referenced** (Glenn, 2026-08-17: *"make C the
  reference"*), and this board's reference is C++ — the ratio a port is held
  to — so `rel` and the tables board disagree about the denominator. C now
  carries a tables leg, so a `--reference` flag serving both boards from one
  renderer is the shape this wants; until then the tables board's own table
  carries the C++ ratio in prose.
- **The tables emitters are not LOCKed** the way `bench/LOCK` locks the type
  emitters. The lock is a ruling about a profiling round, not a side effect of
  a board existing; it belongs to the round the owner opens after the first
  box sitting reads.
- **The `inline` column stays `unknown`** for both legs. The §4 verdict pass
  has no branch for the generated table codec, which is the same open item the
  type board's data-driven rows carry.
