# Every leg to 100%: the remaining cells of lua, luau, squirrel, moo and lobster

Glenn, 2026-11-03: *"let's widen to push to 100% across all languages."*

This page is the work list for the five newer legs whose columns in
[ROADMAP.md](../../ROADMAP.md) are not yet 100%. It lists every cell that is
not done on `main`, grouped into work items, and says for each item whether it
can start now or must wait for the one-read cut
([strict-only.md](strict-only.md), roadmap work-set
`strict-only-read-2026-11-03`), so that nothing is built on the tolerant read
that the cut removes. The roadmap work-set for this page is
`legs-100-2026-11-03` in `docs/roadmap.sexp`, one task per work item below.

## The contract these items are built to

Every item is built to the contract on `rowan/strict-only-spec` (spec PR
#2790), never to the one it replaces:

- **One read.** Every read of every form returns the whole value or refuses the
  whole load. No partial value, no repair, no clamp, no `...Tolerant` entry
  point.
- **The verdict is the cross-language contract.** A leg either accepts an input
  with the same value, shown by writing it back byte-identical (wire, JSON and
  text), or refuses it. Reasons, counters and sizes are per-language
  diagnostics and are not compared across legs.
- **Evolution is kept.** Unknown fields, variants, arms and slots are skipped
  and counted `unknown` (or retained), absent fields take their defaults,
  widening is exact, and the fixed form selects its plan by hash.
- **Writes are deterministic.** The binary and JSON write algorithms are
  specified on `rowan/deterministic-write` and `rowan/deterministic-json`.
  Written-back bytes are compared against those algorithms.

No item adds a tolerant path, a repair counter or a test that compares reason
names across languages.

## Where the columns stand on main

Counted from `docs/roadmap.sexp` and `docs/roadmap-evidence/` at `origin/main`
(2026-11-03). A cell is ✅ only when every task under it is `:done` with
evidence.

| leg | packet wire (28) | fixed tables (57) | variable tables (33) | open cells | toolchain |
|---|---|---|---|---|---|
| lua | 0 / 28 | 0 / 57 | 0 / 33 | 118 | PUC-Rio 5.4.8 from source: any host |
| luau | 0 / 28 | 0 / 57 | 0 / 33 | 118 | `luau-ubuntu.zip` 0.741: **Linux only** |
| squirrel | 0 / 28 | 0 / 57 | 29 / 33 | 89 | 3.2 from source: any host (see S0) |
| moo | 28 / 28 | 57 / 57 | 31 / 33 | 2 | moo from source (cmake): **Linux only** in practice |
| lobster | 0 / 28 | 0 / 57 | 0 / 33 | 118 | release archives, macOS and Linux |
| **total** | | | | **445** | |

**Most of these cells are built but not yet accepted.** The code is on `main`
for every leg (`internal/codegen/<lang>` and `<lang>table`, the conformance
drivers, the fuzz and hostile-corpus makefiles). What is missing is acceptance:
each row's clauses named by a test in `:evidence`, the bar Glenn ruled on
2026-09-19 (*"done means everything is tested and verified working. not done
until tested."*). Several roadmap notes are out of date. The luau and squirrel
packet and fixed notes still say "Not started: no generator, runtime or
conformance leg exists", and the lua notes still describe a
`schema_leg_lua` build tag that has been dropped. So most items below are
acceptance work (proof suites and evidence), not new codecs.

**Blocked while the Linux benches are offline:** every luau and moo item
(their pins resolve on Linux only), and goose's open work outside this page.
lua, squirrel and lobster run on macOS.

## How the cells split under the cut

**Startable now (contract-independent).** These are cells whose acceptance
depends on no tolerant answer:

- **Packet wire, all 28 rows.** The packet wire is out of the cut's scope; it
  already refuses on damage.
- **Fixed tables, 49 of 57 rows.** These are the value, layout, plan, lineage
  and file rows, accepted the way moo's were on 2026-11-03: a per-row proof
  suite against a live C++ reference built for the same schema and values. The
  leg's `FixedSave` bytes must equal C++'s, each side must load and re-save
  the other's bytes byte-equal, and each row gets a file and a writer negative
  control (`internal/codegen/mootable/fixed_proof_suite_test.go`, PR #2738).
- **Variable tables, 8 accelerator and descriptor rows:** reflection
  descriptors, doc comments and tags, the unit registry and UnitView, block
  read, block build, build version, cook open and cook write. These are
  descriptor, write and open/refuse surfaces, and their verdicts are the same
  under both contracts.

**Wait for the cut.** Two groups:

- **Fixed tables, 8 rows that the cut rewrites:** numeric widening and
  landing rules, exact counters and report semantics, array counts and writer
  bounds, text lengths and named refusals, scalar bounds and compressed floats,
  enum and union ordinals, bool and present-byte normalization, and
  hostile-input checks. FIXED-FORM-ALGORITHM §4.5, §4.6 and §5.3–§5.9 change
  each of them: clamps become `out_of_range`, a bool byte past 1 is
  `wire_damaged`, the `malformed` flag goes, and the batch is checked in phase
  order. They are accepted after `strict-only/leg-<lang>`. This includes
  **moo's 8 rows that are already ✅**: the cut reopens them, because their
  evidence was taken under the clamp rules.
- **Variable tables, the other 25 rows.** These are the read and report
  cells: the fixed and variable classes, optionals, keyed arrays, the text
  forms, wide scalars, unions, pointer and union arrays, optional arrays,
  blobs and string identity, maps, any-type union arms, defaults and renames,
  the id-table wire, wstring, unbounded arrays, the message form,
  retain-unknown, widening and refusal reasons, the wire fuzzer gate and
  cards. Their acceptance runs on the conformance harness, and the harness
  itself is rebuilt to read-then-write in strict-only layer 3. They wait for
  `strict-only/engine` (layer 2), `strict-only/harness` (layer 3) and the
  leg's own `strict-only/leg-<lang>` (layer 4, order go, cpp, c, cs, rust,
  java, js, dart, elixir, goose, moo, lua, luau, squirrel, lobster). Accepting
  them now would record evidence that the cut then retires.

## Implemented or absent

The roadmap marks alone cannot tell these apart, so the tree was checked
against them (the generators under `internal/codegen/<lang>` and `<lang>table`,
the Go tests beside them, the `test/packet-*/<lang>` gate legs, the surfaces
each conformance driver lists, the fuzz and hostile makefiles, and the
`tables/cards/<lang>` facades). Every item below carries one of three statuses:

- **implemented, evidence owed:** the code and its tests are on main. The work
  is to run them on the pin, add any missing clause test, and record the
  overlay in `docs/roadmap-evidence/<lang>.sexp`.
- **absent, to build:** the code or the gate leg does not exist.
- **implemented, rework under the cut:** the code exists on the tolerant read
  and is rebuilt to the one read by `strict-only/leg-<lang>` before it is
  accepted.

Absent pieces found on main (2026-11-03):

- the lua legs of packet-text, packet-wide and packet-defaults;
- the squirrel legs of packet-text and packet-defaults;
- possibly a lobster packet-wide leg (B1 decides whether one is needed beyond the packet text-fuzz);
- the squirrel cards facade and driver;
- squirrel retention on the message form;
- a pinned per-row fixed proof suite on every leg except moo.

Everything else these five legs' cells name is implemented.

## Sizes

- **S:** under a day for one worker.
- **M:** one to three days.
- **L:** more than three days, or several workers.

Sizes count the work in this item only. They do not include the leg's own
`strict-only/leg-<lang>` item from the cut plan, which is where each leg's
tolerant twin is deleted.

## lua (118 open; any host)

| id | item | status | cells | when | size |
|---|---|---|---|---|---|
| L1 | **Packet gates and acceptance.** Add the lua legs of `test/packet-text`, `test/packet-wide` and `test/packet-defaults`, each with its negative control (lua has only `packet-finite` today). Then record `port/lua/packet` evidence: `TestLuaTypeWireReadsTheGoldens` and `TestLuaTypeWireWritesTheGoldens` with their controls, the vendored `serialize.lua` pin test, and the four gates | packet codec implemented, evidence owed; three gate legs absent, to build | packet 28 | now | M |
| L2 | **Fixed proof suite.** Port moo's per-row proof suite to `luatable` and record the 49 untouched rows in `docs/roadmap-evidence/lua.sexp` | fixed form implemented; proof suite absent, to build | fixed 49 | now | M |
| L3 | **Fixed rows the cut rewrites.** Accept the 8 rows on the one read, after `strict-only/leg-lua` deletes `ReadFixedTolerant` and the clamp paths in `runtime/fixed.lua` | implemented, rework under the cut | fixed 8 | after the cut | S |
| L4 | **Variable-table accelerators and descriptors.** Pin the 8 rows' clauses to named tests: block, cook, cook-write, reflection, doc-tags and unit registry | implemented, evidence owed | vt 8 | now | M |
| L5 | **Variable-table read cells.** Accept the 25 rows on the read-then-write harness. Close the two named `port/lua/open/*` gaps (`retain-message-cross-build`, `bench-board`) to the one-read text | implemented, rework under the cut | vt 25 | after the cut | L |
| L6 | **Roadmap note hygiene.** Rewrite the `port/lua/*` notes to the present state: no build tag, `LoadTolerant` named as owed to the cut | notes only | (notes) | now | S |

## luau (118 open; Linux only, blocked while the benches are offline)

| id | item | status | cells | when | size |
|---|---|---|---|---|---|
| U0 | **Notes and the pin.** Rewrite the stale `port/luau/*` notes ("no generator, runtime or conformance leg exists" is false: `internal/codegen/luau`, `luautable` and the conformance driver are on main). Add a `sha256sum`/`shasum` fallback to `dist-luau`, as `dist-lua` has. Whether to add a macOS pin (the release `luau-macos.zip`) so luau stops depending on the Linux benches is listed under "Decisions for Glenn" | notes and pin only | (notes) | now | S |
| U1 | **Packet acceptance.** The luau legs of packet-text, packet-wide, packet-defaults and packet-finite are on main: run them on the pin and record `port/luau/packet` with `luau/read_test.go`, `write_test.go`, the analyzer-strict gate and each gate's control | implemented, evidence owed | packet 28 | now (Linux) | S |
| U2 | **Fixed proof suite** for `luautable`, 49 rows | fixed form implemented; proof suite absent, to build | fixed 49 | now (Linux) | M |
| U3 | **Fixed rows the cut rewrites**, after `strict-only/leg-luau` (`F.loadTolerant`, the bounds and census at `schema_fixed.luau:416`, `:922-931`) | implemented, rework under the cut | fixed 8 | after the cut | S |
| U4 | **Variable-table accelerators and descriptors**, 8 rows | implemented, evidence owed | vt 8 | now (Linux) | M |
| U5 | **Variable-table read cells**, 25 rows on the read-then-write harness | implemented, rework under the cut | vt 25 | after the cut | L |

## squirrel (89 open; any host)

| id | item | status | cells | when | size |
|---|---|---|---|---|---|
| S0 | **Notes and the pin.** Rewrite the stale packet and fixed notes ("Not started"; the type wire, `fixed.nut` and `fixedform.go` are on main). `dist-squirrel` calls `sha256sum` only, which macOS does not ship, so give it the `shasum -a 256` fallback that `dist-lua` has | notes and pin only | (notes) | now | S |
| S1 | **Packet gates and acceptance.** Add the squirrel legs of `test/packet-text` and `test/packet-defaults` with their controls (packet-wide and packet-finite exist), then record `port/squirrel/packet` | packet codec, packet-wide and packet-finite implemented, evidence owed; packet-text and packet-defaults legs absent, to build | packet 28 | now | M |
| S2 | **Fixed proof suite** for `squirreltable`, 49 rows | fixed form implemented; proof suite absent, to build | fixed 49 | now | M |
| S3 | **Fixed rows the cut rewrites**, after `strict-only/leg-squirrel` (`FixedLoadTolerant`, `clampScalar`) | implemented, rework under the cut | fixed 8 | after the cut | S |
| S4 | **Doc comments and tags.** A test that pins `<T>TableFields` doc and tags against the reference descriptors, with a negative control. The only squirrel vt row that is open and contract-independent | descriptors implemented; pin test absent, to build | vt 1 | now | S |
| S5 | **Finite floats on the table read** (`tables-finite/squirrel`): every read refuses a NaN or an infinity by its raw bits, and every writer refuses one. This is one of the two reds on the squirrel fuzzer and is already law. The other red, a duplicate-damaged message-batch mutant, is a repeated id, which the cut makes damage. So S5 fixes only the finite half | implemented, defect to fix | (fuzzer prerequisite) | now | S |
| S6 | **The wire fuzzer gate, retain-unknown on the message form, and cards.** The fuzzer goes green on the verdict pipe (verdict and written-back bytes). Retention on form 2 is built to the one read. The cards facade and driver are `tables/cards/squirrel` (none exist yet), held to J6 | fuzzer implemented (red); message-form retention and cards absent, to build | vt 3 | after the cut | M |

The other squirrel variable-table cells are ✅ on main. The cut reopens the
read cells among them when `strict-only/leg-squirrel` lands: the harness
evidence they cite is retired in strict-only layer 3. Re-acceptance is part of
S6's size.

## moo (2 open; Linux only, blocked while the benches are offline)

| id | item | status | cells | when | size |
|---|---|---|---|---|---|
| M1 | **The wire fuzzer gate.** The table-wire fuzz runs on both arms (docs/PORT-MOO.md, layer 6), but the roadmap note still says "Not started". Accept it on the verdict pipe | implemented, evidence owed after the cut | vt 1 | after the cut | S |
| M2 | **Cards.** `tables/cards/moo` exists. Accept `cards-moo-conformance` on read-then-write and record it | implemented, evidence owed after the cut | vt 1 | after the cut | S |
| M3 | **Re-acceptance after the cut.** The 8 rewritten fixed rows and the variable-table read cells, re-run on the one read once `strict-only/leg-moo` lands | implemented, rework under the cut | (reopened) | after the cut | M |

## lobster (118 open; Stella's lane)

Lobster is **Stella's lane** (Glenn, 2026-10-19: add Lobster as a language;
`port/lobster` in the roadmap). These items are planned here so the
column has a complete list. They are hers to take, reorder or reject, and
nothing here is assigned to anyone else.

| id | item | status | cells | when | size |
|---|---|---|---|---|---|
| B1 | **Packet acceptance.** The packet wire is implemented, read and write. Stella's hosted baseline (run 41736071665 at 2dc319bb5) passed 36 test events with 0 failures and 682 packet text-fuzz cases with 0 divergences, covering the C++ golden read and write and the text, default, finite and void controls. The roadmap note that the reader is owed is stale. The one coverage gap, the full LudicrousState 128-bit, ranged and fixed fixtures, is her draft PR #2793. Then add the overlay for `port/lobster/packet`, and check whether a packet-wide leg is needed beyond the text-fuzz | implemented, evidence owed (#2793 + overlay) | packet 28 | now | S |
| B2 | **Fixed proof suite.** The fixed form agrees with the byte oracle (`TestLobsterFixedFormAgreesWithTheByteOracle`) and its evolution is built. Accept the 49 rows per row | fixed form implemented; proof suite absent, to build | fixed 49 | now | M |
| B3 | **Fixed rows the cut rewrites**, after `strict-only/leg-lobster` (`fixedevolve.go:1432`, the bool path at `runtime/fixedevolve.lobster:1220`) | implemented, rework under the cut | fixed 8 | after the cut | S |
| B4 | **Accelerators and descriptors.** Block read and build, build version, cook open and cook write, reflection and doc-tags are implemented (`block_form.go`, `cook.go`, `descriptors.go`, with their tests, and the block, cook and forgery surfaces in `main.lobster`). The roadmap's "owed" notes for cook and cook write are stale. Stella is repairing the unit registry and UnitView row on `codex/schema-lobster-unit-view`. That work found two more real defects to fix there: a minimum int64 emitted as a decimal that overflows, and packet union descriptors that are missing | implemented, evidence owed (unit registry: in repair) | vt 8 | now | M |
| B5 | **Read surfaces noted as owed, which are implemented.** The message form (`messageform.go`), the JSON text form (`json.go`, `text_form.go`), retain-unknown (`retain_unknown.go`) and the wire-fuzz differential (`wirefuzz.lobster`, `make/lobster-fuzz.mk`) are on main, and the driver lists their surfaces. They are rebuilt to the one read (`load_tolerant`, `load_messages_tolerant`, the tolerant `json_read`) by `strict-only/leg-lobster`, then accepted on read-then-write with the deterministic JSON writer. **MUST-FIX:** reading the `feature_full` instance from JSON and saving it writes 233 bytes where the reference writes 313. Lobster's JSON read loses values, and identical values is the one cross-language contract. The fixture is kept in Stella's Lobster work record, and the strict-only read-then-write gate catches it | implemented, rework under the cut | vt (part of 25) | after the cut | L |
| B6 | **Built read cells, accepted.** Optionals, maps, blobs, wide kinds, the variable class and the rest of the 25, on read-then-write | implemented, rework under the cut | vt (rest of 25) | after the cut | L |

## Shared items

| id | item | when | size |
|---|---|---|---|
| X1 | **One fixed proof suite for every leg.** Moo's suite is moo-specific. Lifting its driver (the live C++ reference build, the per-row value and control) into a shared helper, with each leg providing only its runner, makes L2, U2, S2 and B2 a small task each instead of four ports. Do this first if more than one of those four is taken | now | M |
| X2 | **A grep gate for stale notes.** A roadmap test that refuses a `:note` saying "Not started" on a cell whose leg has the generator and conformance driver on main | now | S |

## Order

1. **Now, on any host:** X1, then L1, L2, L4, L6, S0, S1, S2, S4, S5, X2;
   B1, B2 and B4 are Stella's to schedule.
2. **When the Linux benches return:** U0 to U2 and U4. M1 and M2 wait for the
   cut either way.
3. **After the cut lands for the leg:** L3, L5, U3, U5, S3, S6, M1 to M3, B3,
   B5 and B6, in the cut's leg order.

## Decisions for Glenn

1. **A macOS pin for luau.** Today luau only runs on the Linux benches.
   Options: (a) add the `luau-macos.zip` release of the same tag beside the
   Ubuntu one, each archive with its own sha256, so the leg runs on any
   developer machine; (b) keep it Linux-only. Recommendation: (a). It is the
   lobster pattern (one archive per OS), and it unblocks U1, U2 and U4 now.
   moo builds from source with cmake, so it may build on macOS as well. That
   should be measured before it is promised.
2. **Who carries moo's reopened fixed rows.** The cut reopens 8 moo rows that
   are ✅ today, so moo's column drops from 100% when `strict-only/leg-moo`
   lands. Options: (a) let the column show the drop, honestly, until M3; (b)
   hold `strict-only/leg-moo` and M3 as one landing so the column never drops.
   Recommendation: (b). The cell flips once, and only when it is proven on the
   new rule.
