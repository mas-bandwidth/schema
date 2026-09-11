# Fixed Table

The paired pass compares Fixed Table with Packet Wire using the same logical
records. Each results page has only two percentages per language:

- **Fixed Table %**: fastest measured table implementation = 100%.
- **vs Packet Wire %**: that language's packet implementation = 100%.

Both use blended read/write cost (the measured round trip), and lower is better.
Below 100% in the packet comparison means the table is faster than packet wire.
The compact page also labels the checks difference: packet C/C++/C# removes
bounds/range checks, packet Go always checks, and table keeps wire/API validation.
The driver refuses any different checks axes. Full semantics, raw timings and
methodology live in the result directory's `DETAILS.md`.

```sh
# Prepare once per source checkpoint: driver, all legs, and correctness gates.
go build -o bin/schema-paired ./bench/paired
bin/schema-paired -mode gate

# Fast iteration on the designated profiling host, reusing the cached build.
bin/schema-paired -mode fast -out build/paired-fast/<iteration> \
  -noise-note 'profiling reservation and current background-work context'

# Slower confirmation, during an exclusive measurement window.
bin/schema-paired -mode run -reuse-build \
  -quiet-window 'operator READY/START coordination reference' \
  -out bench/paired/results/<sitting>
```

## The nine-language table

`-mode table` is the reporting mode on top of the fast diagnostic, and
`bench/paired/nine.sh` is its one command per host:

```sh
bench/paired/nine.sh              # this host
bench/paired/nine.sh --lane 7     # Space, on that line's isolated core
```

It asks the TREE which `bench/tables/<lang>/` runners this checkout carries
(`-mode table-langs` prints that list on its own, before any build) and builds
exactly those in ONE invocation, because `build/paired/build.json` is written
whole and the measuring mode refuses a leg that manifest does not carry. It
gates them one language at a time: a table-only leg may not share `-langs` with
a paired one, and a single language is always one shape or the other, so the
wrapper needs no opinion of its own about which is which. Then it probes each
over ONE rotation of the 64 records and keeps the ones whose rows this
sitting's parser accepts. It measures those with `-fast-rounds 3` and renders
ONE markdown table into `NINE.md` beside `nine.json`: per language the fixed
form's save and round trip in ns/record and its bytes, `% of own packet` and
`% of C++ form 3`.

The probe is the corpus id. A leg whose fixed-form port has not landed loads a
different set of goldens and answers to a different `corpus_id`, which the
driver already refuses to divide against a paired packet row. Learning that in
a second beats learning it three minutes into a pass, and the language is
NAMED under the table with its refusal rather than dropped silently — "no
runner on this tree", "no leg in this driver yet" and "runner present, but its
rows do not belong to this sitting" are different facts about the merge. None
is ever estimated.

It adds no measurement and relaxes no gate. Every number is a row
`parseRowsForIterations` already accepted, at the one uniform iteration count
per wire §2.1 requires. What the mode adds is the row order, the two ratios,
and a header that says which sitting produced them — host, CPU, revision,
lane, rounds, passes, load1 range, corpus ids and counts.

The paired four are measured together, because that is the only shape that
yields a packet ratio; each table-only language is measured alone, because it
has no packet row to pair. Those consecutive passes become one table only if
they agree on build, host, architecture, corpus ids, round count and
iteration count per wire; otherwise the mode refuses rather than render a
table that mixes them. `-lane` is recorded, never applied: `nine.sh` runs the
whole sitting under `bench-lane`, so the lock is held once and every runner
inherits the pin.

The reading rule lives in `bench/README.md` under the same heading. The short
version: never mix hosts or modes in one row, a loaded host's table says so,
and the certified sitting is still `-mode run` on Space inside the quiet
window.

Fast mode never builds or generates. It requires a clean checkpoint with an
existing build of the languages it is asked for, whose source, host, runtime
settings, binaries and corpora still match. It runs the existing correctness
gates before any clocks, then one complete round across the requested languages
and both wires.

`-langs` names what to build, gate and measure. **A confirmation pass takes the
published set exactly** — `cpp,c,go,cs` — because a pass is sealed over that
set and a pass measuring anything else would render as a pass it is not. **Fast
mode is the diagnostic and takes any non-empty subset of the known legs**, which
is `cpp,c,go,cs,elixir`, plus table-only `rust` asked alone: it publishes
nothing and seals nothing, and it is where a leg that is not in a published
pass yet is measured at all. The Elixir leg is exactly that today — it carries
the FIXED form (form 3) and no other table wire (form 1 is deferred to
schema#515), so its table rows are named `bench_fixed` and it is not part of a
confirmation pass. `rust` is table-only (see below) and is never mixed with a
paired request.

An interpreted leg has no compiled artifact, so what stands in place of a hashed
executable is a manifest of its inputs — the leg's own scripts and every
generated module it loads — each recorded and re-hashed individually, so an edit
after the build is refused exactly as a recompiled binary would be. Its
toolchain is pinned the way `make/elixir.mk` pins it (`BEAM_PATH`, defaulting to
the repo-local `dist/`), and `-mode build` holds the generated Elixir to the
same two gates the make targets do: `mix format --check-formatted` and
`elixirc --warnings-as-errors`. Each path
retains one discarded warmup at its requested sample count. Packet `--quick`
skips the unrelated bitpacker workload.

The initial counts are 2,000,000 Packet operations and 200,000 Table
operations, both whole rotations of the unchanged 64 records. `-packet-iters`
and `-table-iters` override these defaults. Every accepted write and
round-trip sample still needs at least 200 ms, derived from its recorded
iterations and measured rate. A short sample raises the count for that wire's
whole group, never for the short leg alone: BENCH-STANDARD §2.1 fixes one
count per benchmark, identical across every language, so all four languages
are measured again at the largest whole-rotation count any of them needed, at
most three uniform attempts. All attempts remain in the evidence; only those
at the final count supply a row, so one table cannot mix counts. `fast.json`
records that final count per wire and the generated `README.md` states it
once. `-fast-rounds 2` or `3` requests additional complete rounds, rotating
language order and alternating each language's wire order.

The target is about one minute, with a five-minute deadline including gates.
The initial duration model halves a previously observed approximately 104-second
all-eight Space round, then removes Packet bitpacker work; fixed startup/JIT and
gate costs do not halve. This is an estimate, not a measured guarantee for a new
machine or revision. `-fast-timeout` can shorten the deadline but cannot exceed
five minutes. Timeout kills only the current owned runner and retains incomplete
evidence in the printed temporary directory. Final file bookkeeping may follow
the deadline. A slow or noisy machine can fail to complete an adequate pass.

`fast.json` records the build, actual commands and counts, measured sample
durations, attempts, noise warnings and artifact hashes. Process/load
snapshots use the same bounded calls as confirmation, but foreign work
qualifies this diagnostic instead of refusing it. `README.md` reports median
costs and the matched ratios. At one round its Range column is degenerate —
the single measured value is its own minimum and maximum — so it reads as zero
variance by construction, not as measured stability. One round does not
establish stability; a warmup at the requested count does not prove
managed-runtime steady state. Fast results are explicitly **not certified**:
no bracketing drift controls, quiet-window verdict or confirmation seal is
produced. Use the separate seven-round confirmation mode for accepted
performance claims. Its counts, controls and validation are unchanged.

Run from the repository root. The tool needs the C, C++, Go and .NET toolchains
and the same sibling serialize runtimes as the standard packet pass. `CC`,
`CXX`, `SERIALIZE`, `SERIALIZE_C`, `SERIALIZE_GO`, `SERIALIZE_CS` and
`BENCH_OPT_LEVEL` select the builders and runtimes. Every requested leg is
required. `-langs cpp,go,cs` permits a partial build or correctness check, but
publishing requires all four languages and at least seven rounds.

## Table-only languages

**A language outside the paired four may ride the table wire alone, and it
never appears in a ratio, a board or a confirmation pass.** The percentages on
this page are a division, and a division needs both halves over the same
records; a language with one half has one number and says so.

`rust` is the first. It is table-only because its packet leg does not meet this
driver's contract, not because anybody chose to skip it: `bench/rust` is a real
packet runner whose CSV is already the seventeen columns, and it has neither
`--gate` nor `--iterations` — the two flags this driver passes on every
invocation — reports the median of seven runs of its own choosing where this
driver requires one measured run per round, and measures the packet corpus's own
variant set rather than this pairing's. Those are three changes to a leg whose
numbers are already published, so they are separate work with their own ruling;
filling the driver's second wire with a fabricated packet row would be inventing
a measurement, so the driver accepts a table-only row instead. `CARGO` (or
`RUSTUP_BIN`) names the toolchain; `SERIALIZE_RS` relocates the sibling runtime
the unit's packet module links.

`java` is the second, table-only for the same reason reached down a different
road: `bench/java/Main.java` is the TYPE BOARD's runner, with neither `--gate`
nor `--iterations` either, and the paired set is the four the board is defined
over. Java's packet number is the type board's, from its own runner over the
same sixty-four records. `JAVA` and `JAVAC` name the JDK (the repository-local
pin under `dist/` when it is there, as in make/java.mk).

`js` is the third. It is table-only because its packet leg does not exist to be
run, not because anybody chose to skip it: `bench/js/main.mjs` imports the
serialize.js sibling runtime, has neither `--gate` nor `--iterations` — the two
flags this driver passes on every invocation — and appends the §5.1 `codec`
column, so its rows are eighteen columns where the parser requires seventeen.
The table codec has none of those problems: the generated JS table modules
import no runtime at all. Filling the driver's second wire with a fabricated
packet row would be inventing a measurement, so the driver accepts a table-only
row instead. `NODE` names the interpreter.

    go run ./bench/paired -mode build -langs rust   generate, build, gate the leg
    go run ./bench/paired -mode gate  -langs rust   the no-clock gate alone
    go run ./bench/paired -mode fast  -langs rust -fast-rounds 3 -noise-note "..."

    go run ./bench/paired -mode build -langs java   generate, compile, then gate the leg
    go run ./bench/paired -mode gate  -langs java   the no-clock gate alone
    go run ./bench/paired -mode fast  -langs java -fast-rounds 3 -noise-note "..."

    go run ./bench/paired -mode build -langs js     generate, then gate the leg
    go run ./bench/paired -mode gate  -langs js     the no-clock gate alone
    go run ./bench/paired -mode fast  -langs js -fast-rounds 3 -noise-note "..."

A request is one shape or the other and never a mixture: `-langs rust` (or
`-langs java`, or `-langs js`) is asked for alone, `-mode run` refuses it, and
its fast summary prints the table wire's own cost with **NO RATIO** written
where the percentages would be. Because none of these ports has a form-1 table
wire — the fixed form is the first for each — each leg measures the FIXED form
and names its rows `bench_fixed` — the same corpus, the same corpus id and the
same `table` family as every other table row (docs/SPEC-TABLES.md §3.4).

The standard packet runners keep their existing generated storage, release
flags and timed loops. Table runners use a separately generated closure so a
table representation cannot slow the packet reference. All binaries are built
before the first clock. Each round runs both wires for each language, alternating
their order. There is one discarded warmup per measured path, using the full 4,000,000 packet
or 400,000 table operations. The measured iteration counts are the same, both
whole rotations of the 64 records. Seven rounds follow the packet reporting
convention, including the 4/3 wire-order imbalance. Public table Load restores
declared defaults inside the clock; the runner adds no separate reset. Packet
round trips do not need that reset. Each row must exceed 200 ms; a spread above
15% refuses publication of the complete pass. ARM and x64 are separate sittings
and separate results pages.

Table round trips recorded before `16f0603a` included an additional caller reset
and must not be compared as the same timed operation. The reused-target gate
checks the pinned corpus; its ability to detect a missing reset depends on which
fields that corpus elides. Broader default/refusal correctness belongs to the
language table suites, not to this performance corpus alone.

The comparison deliberately retains each packet runner's fastest release mode.
The CSV semantics match `bench/tools/relative.go`: `removed` means debug asserts
and bounds/range checks compile out; `always` means bounds, range and sticky-error
checks stay in every build by contract; `contract` means debug asserts compile
out while wire/API validation stays. These are labelled comparisons of the
fastest correct implementations, with different validation work.

C++ controls for both wires bracket the measured rounds; a movement above 5%
refuses the complete sitting. Controls remain separate from the seven samples
and cannot improve a headline by adding extra attempts.

`build.json` records architecture, compiler/runtime versions, source HEAD,
binary hashes and corpus hashes. Reuse refuses a changed HEAD, host, runtime
setting, binary or corpus. `window.json` requires the operator's READY/START
receipt or reference, records START/END and an `OK` or `INVALID` verdict, and
contains process names, PIDs, parent PIDs, CPU percentages and load. Every
control and measured runner must have matching before/after samples. Arguments,
executable paths and process environments are never collected.

The macOS/Linux monitor samples before clocks, every two seconds during each
runner, and at runner boundaries. Missing coverage beyond ten seconds refuses
publication. Known foreign compiler/build/benchmark processes refuse even when
momentarily idle; this includes external `dotnet` processes because their name
alone cannot establish that they are harmless. Only the coordinator, its actual
ancestors and descendants are excluded. Other agents under the same shell/app
are still foreign. The monitor stops with its owned runner and never kills a
competing process. Windows timing refuses unsupported monitoring; build/gate
modes remain available.

Normal desktop CPU and load are recorded without a universal rejection threshold.
CPU percentages retain `ps`'s platform-specific meaning. Sampling can miss short
or unusually named work, including a workload behind a generic interpreter name.
An `OK` verdict means these checks passed; it is not proof of exclusive CPU use.
The operator receipt and bracketing drift checks remain necessary. Samples are
bounded to 10,000; exceeding that limit refuses the sitting. The monitor forks
`ps` every two seconds and at runner boundaries, plus `sysctl` on macOS. It also
allocates process snapshots and appends them to disk, so it perturbs the measured
machine. Samples remain in memory; this count limit is not a byte budget, and
memory use grows with both sample and process counts.

During collection, each complete sample is appended once as compact JSONL to
`window.samples.jsonl`, with no per-sample `fsync`. The small `window.json` marker
stays incomplete; the growing history is never rewritten before a runner.
After END or INVALID, the complete final `window.json` is assembled once, synced,
closed and renamed into place. Only then is the journal removed. Interruptions
and failed final writes retain the journal and cannot seal; failed windows remain
invalid even after their final evidence is saved. `load.json` is the load-only
view of the completed samples.

Failed passes keep their raw evidence in the printed temporary directory; a
completed result directory appears only after every gate succeeds.
`completion.json` seals the exact build/window/load/control/round file set with
SHA-256 hashes after checking the actual binaries and corpus again. Run
`go run ./bench/paired -mode render -out <sitting>` to regenerate the compact page.
It verifies that seal before deriving any numbers and refuses missing, extra,
changed or incompatible evidence. Archived rendering does not require the old
local binaries. The seal detects altered evidence; it is not cryptographic
protection against an operator deliberately rewriting both data and manifest.

## One definition and one set of values

`bench/corpus/Bench.schema` remains the sole definition of `BenchMixed`, and
its canonical packet golden and 64-record variant corpus are unchanged.
`FixedTable.schema` adds a wrapper that reaches `BenchMixed`, causing the
existing compiler to emit table codecs for that type. The measured table root
is **BenchMixed itself**; the wrapper is never serialized or timed.

`test/bench/paired_main.cpp` reads the canonical packet records, decodes each
once, and saves that same object on the table wire. It verifies table load and
re-save byte identity, then writes the table-decoded object back onto the packet
wire and requires the original packet bytes. There is no second field list or
independent value generator. Fixed-point and 128-bit values retain their original
types and bounds. Packet constants, reserved bits and alignment carry no logical
fields and have no table payload.

Table default elision and enum ID sets vary the encoded record lengths even
though the schema is a fixed table. The paired table corpus therefore has a
64-entry little-endian uint32 length index beside its concatenated records.
Each runner validates every length, the exact record count and total, the first
record's golden, and all 64 load/re-save results before timing. Two further
rotations load into one reused target without caller resets and must re-save
the exact expected bytes, including the last-to-first transition. Timed operations
check the corresponding record length. `bytes_per_op` reports the exact mean;
staggered buffer padding never counts as serialized bytes.

`make bench-paired-corpus` deliberately re-pins the table half; ordinary builds
only verify it. The historical `BenchTable.schema`, table corpus and results
remain intact. They were representative counterparts with different logical
types and values, so their historical ratios cannot supply this second column.

## The plan-read arm

`-mode plan-read` is the arm that puts a BACKWARD read on the clock.
BENCH-STANDARD.md §1.10 is its normative text; this is the operating manual.

```sh
go run ./bench/paired -mode build -langs cpp   generate both generations, build, gate
go run ./bench/paired -mode gate  -langs cpp   the no-clock gate alone
go run ./bench/paired -mode plan-read -langs cpp,c,go,cs -fast-rounds 3 -noise-note "..."
```

**What it measures and why it is not already measured.** A fixed-form read of a
CURRENT file takes the identity lane: the reader's own wire hash selects the
identity entry, whose fills are EMPTY and whose record size is `8 + C(root)`
(docs/FIXED-FORM-ALGORITHM.md §5.3 step 8), and no plan compiler runs. A read of
an OLDER file selects a LINEAGE entry, prefills the destination with that
entry's fills (§4.3) and scatters through the compiled plan (§4.4, §4.5). Those
are two different amounts of work and the estate has a number for neither.
`test/bench/fixedform_measure.cpp` arm A3 is the nearest thing and it is not
this: it reads the SAME layout through a runtime copy of the identity plan, so
it prices the plan's indirection with nothing to prefill and nothing to scatter.

**The arm is read-only.** A fixed table writes its own layout and no other, so
there is no old-layout write to time and no round trip to close. The rows are
`read`, **measured** — BENCH-STANDARD §2.9's "`read` is DERIVED" binds the gen
family's `bench_mixed` rows, where a round trip exists to derive it from.

**The corpus.** `bench/paired/corpus/bench_lineage.bin` and
`bench_lineage.layout`, written by the C++ dump
(`test/tables/fixedform_dump.cpp`, the form's byte oracle) from
`bench/corpus/VOLD_bench_lineage.schema` — `BenchMixed` one generation back,
whose widening touches an appended field, a widened array bound, a widened
text, a new union arm, an appended enum variant and an integer widen inside the
80-element `stats` array. That last one is the row that matters most: §5.9 #33
forbids folding a run across a widening, so it is 80 widen entries per record
and the only arm putting arithmetic rather than a copy on the clock.
**There is no length index and there must not be one** — a form-3 file's records
are constant-size by construction, which is exactly what the form-1 paired
corpus needed its 64-entry index for. The 64 records are the SAME 64 logical
records, decoded once from the canonical packet corpus: no second field list,
no independent value generator.

**The lock is what makes the reader's layout the newer one.**
`bench/corpus/schema.lock` carries BenchMixed's lineage with the VOLD layout at
index 0, the identity at index 1 and `floor = 0`. Without that entry every leg
answers `layout_newer` and the arm times a refusal. The lock is committed
corpus and hashes into `corpus_id` with the bytes, because a lock edit silently
changes which plan ran.

**The rows.** `bench_fixed` is untouched — `write` + `round_trip`, read derived
— so no published board moves. The arm adds three:

| row | `LOAD` selects | path |
|---|---|---|
| `bench_fixed_read` | the identity entry, read alone | `read` |
| `bench_plan_read` | the OLDER entry, by its hash | `read` |
| `bench_mixed_read` | — the PACKET wire, same 64 logical records, read alone | `read` |

`bench_fixed_read` exists because §2.9 forces it: a compared read has to be a
measured read, and `bench_fixed`'s read is derived to stderr precisely so it can
never be divided. `bench_mixed_read` exists for the same reason on the packet
side: the gate below is against the packet wire's READ, and the gen family's
packet read is derived too, so this arm times it alone. It belongs to this arm's
sitting and is never folded into the published `gen` / `bench_mixed` series.

All three rows come out of ONE invocation, at the iteration count §2.1 fixes for
each wire (fixed per benchmark, identical across languages), over the same 64
records, in the same sitting, interleaved. That invocation loads the two lineage files and the lock in
ADDITION to the identity goldens, so its `corpus_id` differs from the ordinary
table pass's by §1.6's own definition: the ratio's two halves share an id and
are divisible, and neither can ever be divided against a `bench_fixed` row it
was not measured beside. **§5.3 rule 2 refuses the plan/identity ratio on
`bytes_per_op` and is right to** — the old record is a different length by
construction — so this driver is the only tool that prints it, the same
exception it already holds for table-versus-packet, and its caption names both
record sizes.

**The gate before the clock is the versioning law's.** No leg starts this clock
until its plan-read mode has proved, on all 64 records, that every old value
landed exactly, that the reader's appended tail holds its declared default, and
that the counters are what §5.4 says for these six classes. A plan read that
decoded wrongly does not get to be fast. The prefill, the run and the bounds
pass are all INSIDE the timed operation: the destination is reused across
iterations, as the shipped call reuses it, but every iteration pays its own
prefill. A runner that hoists the prefill out of the loop has measured the
identity lane twice.

**The board: nine legs, three numbers each.**

```
# fixed form: the identity lane, the plan lane, and the same leg's packet wire
# host / arch / rev / lane / rounds / corpus_id(identity) / corpus_id(plan)
# bytes_per_op: fixed 000 B/record, packet 000 B/record (§5.3 rule 2 — this driver's division alone)
# checks: table=contract, packet C/C++/C#=removed, packet Go=always
# C++ anchor: identity 000.0 ns/record / straight-line 000.0 = 0.00x  (bound 1.5x)

leg      identity read    plan read    packet read    GATE identity  GATE plan    plan/identity
         ns/record        ns/record    ns/record      vs packet      vs packet    x (reported)
```

**The gate is the packet wire, not a ratio.** Glenn's ruling, 2026-09-11,
verbatim: "We can find the bound only after we know how it should perform. I
think if we find that we are slower than the equivalent packet wire (we should
be faster) for the same lang, then we know we are a fail. The rest is by hand."

So, per leg: each lane — identity AND plan, judged separately — must be FASTER
than the SAME LEG's packet-wire read of the SAME logical record, or that leg
FAILS. The rows compared are `bench_fixed_read` and `bench_plan_read` against
`bench_mixed_read`, all measured, all out of the one invocation above, so no
derived number is ever divided and no row is compared against one it was not
measured beside. A conservative shortcut is always available: a round trip is
never cheaper than the read inside it, so a fixed-form read slower than the
packet leg's whole `round_trip` is already a FAIL. §5.3 rule 2 refuses this
division on `bytes_per_op` — the two wires' records are different lengths by
construction — so this driver is the only tool that prints it and its caption
names the size on each wire. The checks mismatch is labelled on the board and
not excused. Per leg, never an average, because the gate is nine languages
performant and an average hides the one that is not. **Everything else on this
board is judged by hand; there is no other automatic threshold.**

**plan / identity ≤ 2.0x is WITHDRAWN as a gate** (it was PROPOSED by PR #935)
and is now a REPORTED number in the last column, informational, failing nothing.
The reasoning is kept as the expectation it checks: the identity lane is one
pass over the record, a lineage read adds exactly one structural thing — the
prefill, a second pass over the same destination bytes — and fragmenting the
runs costs loop overhead but touches no new bytes, so two passes where identity
pays one is the ceiling we expect. A leg far above it is likely re-reading the
record or dispatching per field where the plan should have coalesced: a
coalescing defect worth a look, read by hand.

**identity / hand-written straight-line ≤ 1.5x is untouched**, and the two
coexist. It is ratified in docs/SPEC-TABLES.md §3.4 — the reference's own, older
rule — and C++'s alone: arm B of `fixedform_measure.cpp` is hand-written, and
nine hand-maintained layouts would be nine chances for the comparand to be wrong
in a leg-specific way, so the anchor line is where that ratio lives and no leg
owes a hand-written arm. Glenn's ruling neither relaxes nor restates it; nothing
multiplies the two together any more.

A leg with no runner is NAMED with its reason in `nine.sh`'s own vocabulary and
never estimated. Eight table runners exist under `bench/tables/` today; Dart's
fixed-form leg has landed and its bench runner has not, so the ninth line is a
named refusal until it does.

This mode publishes nothing and seals nothing until a confirmation pass carries
it: like fast mode it is the diagnostic, and the certified sitting is still
`-mode run` inside the quiet window. `bench/tools/ledger.go` must admit
`(family table, bench_fixed_read | bench_plan_read, path read)` as series of
their own — and `bench_mixed_read` beside them under family `gen` — keyed by row
name and never folded into the `gen` / `bench_mixed` `round_trip` curve the
C/C++ pair lock is defined over; the `--check` teeth stay off these rows,
because extending a gate to a new statistic is a ruling and not a filter change.
The packet-wire gate above is this driver's board, where both wires are measured
in one sitting; the ledger's across-time teeth are a separate question.
