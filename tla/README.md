# tla/CardVersioning

A bounded TLA+ model of the cards `Upgrade` wrapper's retained round trip
(`tables/cards/upgrade.go`, docs/SPEC-TABLES.md §4, §5, §6.5, §6.6), under the
one read (§6.5, 2026-11-03): `Read` returns the whole value or refuses. It checks the
contract that c03's generated Go runtime implements concretely: one atomic
retained round trip of a supplied wire value through a named declaration.

## What is modeled

Three declaration labels — `Legacy`, `Current`, `Compatible` — are finite
mathematical constants, not product fields. There is no wire or compiler
change, no runtime version number, no hash ordering, no migration registry and
no "newer means unknown" rule. The four stable field identities are `renamed`
(the `was` rename keeps identity: known to all three), `added` (known to
Current/Compatible only), `retired` (known to Legacy only) and `opaque` (known
to all three). A card is four field values, one body byte-symbol and a framing
flag; the body is a separate opaque value.

The model holds the seven named properties:

- **TypeOK** — bounded state: two record slots over the finite card set, one
  `last` observation, report counters (`unknown`, `retained`, `retain_lost`)
  in `0..4`.
- **NoFieldLost** — after a successful `Upgrade`, every pre-attempt logical
  field identity/value survives in the output's known values or retained data;
  defaults for new fields are allowed and `was` changes no identity.
- **BodyUnchanged** — `Read`/`Upgrade`, successful or refused, preserve the
  stored body byte-symbol; only `Write` replaces it.
- **RefusalKeepsOriginal** — a refused `Upgrade` returns no replacement and
  keeps the entire original input, not just its body.
- **UnknownIsReported** — physically encountered unknown ids/variants produce
  the proper report count; a compatible writer label alone forces no unknown.
- **ShippedUpgradeSucceeds** — every attempted SHIPPED fixture/target/capacity
  triple succeeds, and a same-target repeat upgrade is equal over the abstract
  normalized output.
- **ReadIsWholeOrRefused** — a `Read` refuses exactly when the card is
  malformed or failed, or a known field holds a kind mismatch or a value out of
  the reader's range; a refused read hands back no view (`NoCard`) and moves no
  counter, and an accepted one returns the whole view. An unknown field, an
  unknown variant and an absent field are evolution and read.

The model is a pure synchronous call: no fairness or eventual-newest liveness is
owed. It does not prove the Go implementation merely because a comment cites it.

## Abstractions (c03's generated-runtime tests prove the concrete operations)

The model omits actual byte layout, graph numbering, allocation and bounds
arithmetic. A field value is a symbol, not a bit pattern; retention capacity is
zero or one record, not a byte/id count. Report counters are `0..4` and
observations overwrite one `last` result, never append unbounded history. A card
carries no writer label, matching §4's versionless wire, so the report's unknown
depends only on physically present content.

## Fixtures and the action mapping

`Init` establishes the empty slots and an empty last-result observation.
`Write(r, c)` replaces a slot with one fixture card including its body (old
superseded values are not owed). `Read(d, r)` returns the decoded view and its
report, or refuses with neither, without replacing stored wire; `ReadStrict` is
the same call (§6.5) and is not a separate action. `Upgrade(d, r, cap)` is ONE atomic attempt: keep the input, derive
known/defaulted and retained data plus one accumulating report, then publish a
replacement on fully checked success or refuse with the original slot unchanged.
`Next` enables every fixture and attempt; no guard silently discards lossy or
shipped cases.

**SHIPPED triples** (each must upgrade successfully, preserving values and body):

| fixture | reader | card | c03 evidence |
|---|---|---|---|
| legacy-to-current (add/default, `was`) | Current | `legacyCard` (`renamed=one`) | `TestEveryCorpusRecordReadsWithTheCurrentModel` (corpus `initial`) |
| current rewritten idempotently | Current | `currentCard` (`added=two`) | `TestUpgradeKeepsEveryField` (corpus `consumers-and-rename`) |
| removed nondefault scalar retained | Current | `removedCard` (`retired=one`) | `TestUpgradePreservesRemovedField` |
| compatible via legacy, no present unknown | Legacy | `compatibleCard` (`added` elided) | `TestReadStrictRejectsFutureScalar` (compatible card accepted) |

**Unsupported / lossy cards** (each must report the cause and return no
replacement; the slot keeps the whole original):

| cause | card / triple | c03 evidence |
|---|---|---|
| unknown retainable scalar at capacity 0 | `removedCard`, `cap=0` | `TestUpgradeTinyCapacityRetainLost` |
| excluded node-index payload | `nodeIndexCard` (`retired=nodeIdx`) | §6.6 excluded class; `internal/codegen/gotable` `TestRetainUnknownNodeRecord` |
| unknown enum/union value in a known field | `enumVariantCard` (`renamed=enumVar`) | `TestUpgradeRefusesUnknownColumnVariant` |
| kind mismatch (refuses `Read` too) | `kindMismatchCard` (`renamed=kindMis`) | §4 kind mismatch (conformance manifest) |
| out of range (refuses `Read` too; the card keeps its old name `clampCard`) | `clampCard` (`renamed=clamp`) | §4 `out_of_range` (range tightened) |
| malformed input | `malformedCard` | `TestUpgradeRefusesMalformed` |
| failed load/measure/save | `failedCard` | `TestUpgradeRefusesMalformed` (truncated wire) |

## Reversed witnesses

Each reversed cfg turns exactly one fault switch TRUE, keeps the others FALSE,
and checks TypeOK plus its named invariant. The counterexample is `Init` →
`Write(fixture)` → `Upgrade` (the triggering input, action and exact invariant):

| cfg | switch | named invariant violated | counterexample |
|---|---|---|---|
| `MCCardVersioningDropRetained.cfg` | `DropRetained` | `NoFieldLost` | `removedCard` read by Current: output `retired=default`, input `retired=one` |
| `MCCardVersioningSuppressUnknown.cfg` | `SuppressUnknown` | `UnknownIsReported` | `removedCard` read by Current: `report.unknown=0`, expected `1` |
| `MCCardVersioningChangeBody.cfg` | `ChangeBody` | `BodyUnchanged` | `Upgrade` of the empty card: body `bodyA`→`bodyB` |
| `MCCardVersioningRefuseShipped.cfg` | `RefuseShipped` | `ShippedUpgradeSucceeds` | `legacyCard` read by Current: `published=FALSE` |
| `MCCardVersioningTolerantRead.cfg` | `TolerantRead` | `ReadIsWholeOrRefused` | `Init` → `Write(nodeIndexCard)` → `Read` by Legacy: `retired` is known to Legacy and holds a node index, a kind mismatch (§4: a pointer and a scalar are not one kind), and the retired tolerant read returns `status = ok` with `retired = default` |

## Running (Step 4)

From this directory, on the Linux bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 180s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCCardVersioning.cfg -metadir "$JOB/tlc-MCCardVersioning" CardVersioning
```

(repeat for each of the six cfgs; the positive cfg must exit 0 with exhaustive
completion, each reversed cfg must report its named invariant violation). The
model does not prove the Go implementation; the concrete generated-runtime proof
is `make cards-test`, whose `TestUpgradeKeepsEveryField` pins the shipped cases.

# tla/CardHops

A bounded TLA+ model of one card record crossing builds of different languages
and declarations: written by one language's build, upgraded by a second, edited
by a third and read back by the first (docs/SPEC-TABLES.md §4, §5, §6.6). Two
abstract legs `A` and `B` implement the same contract over two declarations,
`Older` and `Current`; each leg's `Upgrade` is the CardVersioning
retained-round-trip abstraction (`tla/CardVersioning.tla`) with a retention
capacity, restated per leg so the two-leg hop stands alone. It is a model, not
a codec, and a model checked here does not prove any implementation.

## The finite instance

Two declarations: `Current` adds one field and renames one with `was`; `Older`
carries one field `Current` retired. The four stable field identities are
`kept` (known to both), `renamed` (the `was` rename keeps identity: known to
both), `added` (known to `Current` only) and `retired` (known to `Older` only).
A card is four field values (two scalar values plus the `default` symbol the
writer elides), one body byte-symbol, a framing flag, the three builder-edited
fields `needs`, `tier`, `paths`, and the four fixed task fields `new`,
`shared`, `test` and `done`. There is one record slot. The retention capacity
set `Caps = {0, 1}` is the number of fields the leg's declaration cannot name
that one `Upgrade` may retain, the abstraction of
`upgrade(wire, retainBytes, retainIDs)` in `tables/cards/upgrade.go`. The two
legs are abstract implementations with the same declaration knowledge; the
divergence switch (`DivergentLeg`) is the only place `B`'s `Upgrade` may differ
from `A`'s.

## Action mapping to the language facades

Each action maps to the facade call the language streams implement
(`tables/cards`, `make/*.mk`):

| action | facade call | contract |
|---|---|---|
| `Write(leg, decl, values, body)` | write a fresh card / `Load` | replaces the record; the only action that may change the body |
| `Read(leg, decl)` | `Load` | a view and a report; the stored record is unchanged (§4). NOT YET UPDATED for the one read (§6.5): this model's `Read` still answers `ok` on a malformed card, which is the retired tolerant read; it is owed the CardVersioning change |
| `Upgrade(leg, decl, cap)` | `Upgrade` (the retained round trip) with its retention capacity | one atomic attempt: retained decode, one accumulating report, retained encode, publish (`UpgradePublished`) or refuse (`UpgradeRefused`, the `RetainLost` branch of `lossErr`: more unknown fields than `cap` can retain, or a malformed wire); a refusal keeps the record (§6.6) |
| `Edit(leg, decl, field, value)` | `SetNeeds` / `SetTier` / `SetPaths` | a builder rewrite of one field, published (`EditPublished`) or refused (`EditRefused`, the `refuseLossy` rule: the record holds a field the declaration does not know, or is malformed); on success it changes that field only (§6.2) |
| `Hash(leg)` | `TaskHash` | observes the digest of `(body, paths, new, shared, test, done)`, the frame `TaskHash` hashes |

`needs` and `tier` are outside the task hash; `body`, `paths`, `new`, `shared`,
`test` and `done` are inside it. The refused branches are their own disjuncts
of `Next` so TLC's coverage reports them.

## Properties

- **TypeOK** — bounded state: one record slot and one `last` observation over
  the finite card set.
- **NoFieldLost** — every field identity and value present before an `Upgrade`,
  `Read` or refused `Edit` is present after it, as known or retained; a
  successful `Edit` changes only its own field.
- **BodyUnchanged** — only `Write` changes the body.
- **RefusalKeepsOriginal** — a refused `Upgrade` or `Edit` leaves the record
  exactly as it was.
- **ImplementationsAgree** — for every reachable record, declaration and
  capacity, `Upgrade` by `A` and by `B` produce the same abstract output (what
  the conformance check asserts byte for byte).
- **HopOrderIrrelevant** — `Upgrade` by `A` then `B` equals `Upgrade` by `B`
  then `A` equals one `Upgrade`, on the same target declaration and capacity.
- **TaskHashStable** — an `Edit` of `needs` or `tier` leaves the `TaskHash`
  unchanged; an `Edit` of `paths` to a different value changes it.

## Finite limits

No byte layout, graph numbering, allocation or bounds arithmetic. A field value
is a symbol, not a bit pattern; a capacity is a count of retained fields, not a
byte or id count. The record is one slot, not an arbitrary store. The report is
abstracted away: it is not checked by these properties and no unbounded history
is kept. `new`, `shared`, `test` and `done` are fixed symbols that never move,
so the task hash's dependence on them is constant. Each declaration has exactly
one field it cannot name, so capacity `1` retains every fixture and capacity `0`
refuses any fixture carrying an unknown. The model is a pure synchronous call:
no fairness or eventual-newest liveness is owed.

## The cfgs and their expected outcomes

`MCCardHops.cfg` sets `Caps = {0, 1}`, holds all four fault switches `FALSE`
and checks all seven properties; it must exhaust green, and its coverage must
show `UpgradeRefused` and `EditRefused` reached. Each reversed cfg turns exactly
one fault switch `TRUE`, keeps the others `FALSE`, and checks `TypeOK` plus its
one named invariant:

| cfg | switch | named invariant | expected counterexample |
|---|---|---|---|
| `MCCardHopsEditDropsUnknown.cfg` | `EditDropsUnknown` | `NoFieldLost` | `Edit` while `retired` is present and unknown to `Current` succeeds and drops it to `default` |
| `MCCardHopsDivergentLeg.cfg` | `DivergentLeg` | `ImplementationsAgree` | a record carrying `retired` is reachable: `Upgrade` to `Current` at capacity `1` by `A` retains it, by `B` writes the default |
| `MCCardHopsEditTouchesBody.cfg` | `EditTouchesBody` | `BodyUnchanged` | an `Edit` rewrites the body `bodyA`→`bodyB` |
| `MCCardHopsHashSeesNeeds.cfg` | `HashSeesNeeds` | `TaskHashStable` | an `Edit` of `needs` changes the digest that now folds `needs` in |

A parser error, deadlock, timeout or a different invariant failing is not
evidence of the fault; only the named invariant must fail, reached through an
enabled action.

## Running

From this directory, on the Linux bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 180s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -coverage 1 -config MCCardHops.cfg -metadir "$JOB/tlc-MCCardHops" CardHops
```

Repeat for each of the five cfgs. The positive cfg must exit 0 with exhaustive
completion and coverage lines for `UpgradeRefused` and `EditRefused` showing
states reached; each reversed cfg must report its named invariant violation with
the triggering action in the counterexample. The runs are recorded in
`tla/RUNS.tsv`, one row per cfg, keyed by the cfg file name in its first column.

## What this does not prove

A model checked here does not prove any implementation. The concrete proof is
the card conformance check (`the-card-corpus-is-a-language-neutral-conformance-set`)
and each language stream's own upgrade and edit tests; the eight language
streams cite this model from their upgrade and edit functions as the contract
they implement.

# tla/Framing

A bounded LAYOUT model of the table wire's record scan
(docs/LAYERS/1-framing.md; docs/SPEC-TABLES.md §3, §4, §2.9), under the one
read (§6.5): one parent body of `N` bytes walked by a cursor, one length-framed
field at a time, carrying the arithmetic the readers carry — the field's start,
its end `start + L`, the header read INSIDE THE FIELD'S OWN `L` (§3, THE HEADER
INSIDE THE BODY; H2, H3), the elements filling `L` exactly (H5, L6), and the
resume point after a field read whole. DAMAGE DISCARDS THE WHOLE LOAD (§4,
D1): a body too short to carry its header, a header that crosses `L`, elements
that run past `L`, bytes left inside `L` after the count's elements and an `L`
past the buffer each end the scan, and nothing resumes, so there is no INERT
body and no read-on past damage. It is a model, not a codec: the reference, the
kind byte and the element values are folded away and only offsets remain, which
is what a layout model is for (Glenn, 2026-10-29: the bugs live in the mapping
to memory).

## The finite instance

`N = 8`, `MaxL = 9`, `HeaderBytes = {2, 3, 4}` (the element kind byte plus a
count of one to three LEB128 bytes). Every field chooses every `L` in
`0..MaxL`, every header width and every element span `E` in `0..MaxL`, so TLC
walks every placement of a header and of the elements against every `L` and
every buffer end, including `L` of zero and one, `L` exactly at the buffer, one
past it, a header that crosses `L`, elements that run past `L` and elements
that stop short of it.

## Actions

| action | the rule |
|---|---|
| `Terminator` | the zero reference ends the body (F3, R1) |
| `Truncated` | no byte left for a reference: damage, and the load ends |
| `Field(L, H, E)` | one length-framed array field: `L` past the body is damage and ends the load (L4); fewer than two bytes is damage and ends the load (H2); a header that crosses the field's own `L` is damage and ends the load (H3); elements that run past `L` (H5) or stop short of it (L6) are damage and end the load; otherwise the header and the elements fill `L` and the parent resumes at `end` (L1) |
| `Finished` | stutter once the scan is done |

## Properties

- **TypeOK** — every offset bounded by `N + MaxL + 1`.
- **NoReadPastBuffer** — no field's read reaches past the buffer (L4, H5).
- **ResumeAtEnd** — after every field the parent did not stop on, it resumed
  exactly at `start + L` (L1).
- **DamageEndsLoad** — once damage is met the scan is done: no field is read
  after damage, so nothing a later field held can stand beside it (D1).
- **HeaderInsideBody** — every field the parent stepped past carried a whole
  header of at least two bytes inside its own `L` (H2, H3).
- **ElementsInsideBody** — no field the parent stepped past was read beyond its
  own end (H5).
- **HeaderDamageIsCrossing** — header damage is exactly a header past the
  field's own `L`.
- **EveryCaseRuled** — no field the parent stepped past held bytes inside its
  `L` after the count's last element (L6): such bytes are damage.

## Reversed witnesses

Each reversed cfg turns exactly one fault switch TRUE and checks TypeOK plus
the one invariant that names the rule; only that invariant may fail. The first
two are the RETIRED tolerant rules, kept as witnesses that the strict rule
excludes them. The counterexamples are TLC's with `-workers 1`.

| cfg | switch | invariant violated | counterexample |
|---|---|---|---|
| `MCFramingHeaderAgainstParent.cfg` | `HeaderAgainstParent` | `HeaderInsideBody` | a field at 1 with `L = 2` and a three-byte header: the header ends at 4, past the field's end at 3 but inside the buffer, and the retired N3 rule read it and resumed at 3 |
| `MCFramingInertShortBody.cfg` | `InertShortBody` | `HeaderInsideBody` | a field at 1 with `L = 0`: the retired H2 rule stepped over it as INERT and read on, where H2 makes it damage |
| `MCFramingOffByOneAtL.cfg` | `OffByOneAtL` | `NoReadPastBuffer` | a field at 1 with `L = 8`: its payload ends at 9, one past the buffer of 8, and is read whole |
| `MCFramingResumeAfterDamage.cfg` | `ResumeAfterDamage` | `DamageEndsLoad` | a field at 1 with `L = 3`, a two-byte header and no elements: the byte left in `L` is damage (L6), and the walk resumed at 4 instead of ending |
| `MCFramingTrailingElements.cfg` | `TrailingAccepted` | `EveryCaseRuled` | a field at 1 with `L = 3`: the array body `element kind, count 0, one more byte`, the header inside `L` and one byte left after the last element, stepped over to 4 and read whole where L6 makes it damage |

A resume-at-cursor fault has no witness of its own: with the header inside `L`
and the elements filling it, the cursor after a field read whole IS its end, so
the fault cannot be told apart from the rule.

## Running

From this directory, on a bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 java -Xmx6g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCFraming.cfg -metadir "$JOB/tlc-MCFraming" Framing
```

Repeat for each of the six cfgs. The positive cfg must exit 0 with exhaustive
completion; each reversed cfg must report its named invariant violation. The
runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

A model checked here does not prove any reader. The concrete proof is the
`framing` conformance surface, which runs the hand-built vectors of
docs/LAYERS/1-framing.md through every leg, and the engine's oracle check
beside it.

# tla/ReadOrder

The one read's verdict contract (docs/SPEC-TABLES.md §4, §6.5, §3.1;
docs/plans/strict-only.md layer 1): for every input and declaration a read is
ACCEPTED with exactly one value or REFUSED with no partial value, and the
verdict does not depend on the order in which a reader detects conditions.
Refusal holds exactly when one condition of the spec's set holds, each a
function of (input, declaration) alone; caller resource limits are fixed
constants and are not modelled (§6.5).

## The finite instance

A root body (node 0, of the root type `A`) and a node table of `Records`. Each
record is of a type this reader names (`A`, `B`, or the reserved blob type
every reader knows) or of a type it cannot name (`X`). Every body with fields
carries three slots: `p`, the pointer field `A` and `B` declare (`A.p : *B`,
`B.p : *bytes`); `q`, a pointer in a field no build of this reader names (a
kind 17 payload in an UNKNOWN field, still part of the graph); and `v`, the
value slot, a map with keys in `0..1` on both `A` (optional) and `B`
(REQUIRED). `v` holds one content symbol:

| symbol | what rode | its class |
|---|---|---|
| `absent` | nothing | `B` refuses `missing_required`; `A` reads its default |
| `ok` | the known field, conforming | read |
| `big` | the known field, a key past the range | declaration: `out_of_range` |
| `desc` | the known field, keys descending | declaration: map order |
| `wk` | the known field under another kind | declaration: `kind_mismatch` |
| `bad` | the known field, bytes the wire alone shows wrong (a bool byte of 2, ill-formed text, a header across its `L`) | wire alone: damage |
| `u_desc` | an UNKNOWN field whose pairs descend; the known field absent | evolution: skipped, never judged by a declaration |
| `u_bad` | an UNKNOWN field holding damage; the known field absent | wire alone: damage inside an unknown field refuses |

On an `X` record every slot is a field this reader cannot name. The ORACLE
(`SpecRefuses`, `SpecValue`) is written from §4's and §6.5's lists over the
whole input with no walk: wire damage in any body; a cycle reachable from the
root; a record the root does not reach over EVERY kind 17 payload, known or
not; a blob named from two slots, known or not; and, in every record of a
named type whether or not a known slot reaches it, range, kind, map order,
required, and a declared pointer whose target is a named type other than the
slot's. A pointer to an `X` record reads null.

The READER is a nondeterministic order of checks: `Scan(n)` runs one body's
wire and declaration checks and counts its blob names; `Walk` is a depth-first
walk of the pointer graph from the root whose child order is free, refusing a
pointer to a node whose descent is still open; `Unreached` refuses a record
the finished walk never entered; `Accept` returns the decoded value once every
check has run. Scans and walk steps interleave in any order and any detected
condition stops the read.

## Properties

- **TypeOK** — every variable in its finite domain.
- **OneVerdict** — every run that ends, whatever its order and wherever it
  stopped, is refused exactly when `SpecRefuses` holds, with no value, and
  otherwise accepted with exactly `SpecValue`. All runs therefore agree.
- **NoEvolutionAccepted** — not a rule: the vacuity witness. It must FAIL in
  `MCReadOrderEvolutionReached.cfg`, which shows the positive model accepts
  an input carrying evolution (an unknown field, a pointer to an unnamed type)
  rather than refusing it.

## Reversed witnesses

Each reversed cfg runs the single-record instance with exactly one fault
switch TRUE and checks TypeOK plus `OneVerdict`.

| cfg | switch | counterexample |
|---|---|---|
| `MCReadOrderPartialValue.cfg` | `PartialValue` (a) | root `v = big`: the scan refuses `out_of_range` and hands back its destination as it stood (here still unset at every node) instead of no value; a longer run hands back decoded nodes |
| `MCReadOrderEagerUnreachable.cfg` | `EagerUnreachable` (b) | root `q → 1`, record 1 a blob: scanning record 1 before the walk has reached it judges it unreachable and refuses; the oracle, and every run that walks first, accepts. The verdict depends on order |
| `MCReadOrderKnownSlotsOnly.cfg` | `KnownSlotsOnly` (b) | root `q → 1`, record 1 a blob: the walk follows known slots only, never reaches record 1 and refuses it as unreachable; it is reached through the unknown field, and the oracle accepts. The bug the cold read found |
| `MCReadOrderClampOutOfRange.cfg` | `ClampOutOfRange` (c) | root `v = big`: the key is clamped and the read accepts with `v = clamped`; the oracle refuses `out_of_range` |
| `MCReadOrderUnknownTargetRefuses.cfg` | `UnknownTargetRefuses` | root `p → 1`, record 1 of type `X`: the declared pointer refuses instead of reading null; the oracle accepts |
| `MCReadOrderMapOrderOnUnknown.cfg` | `MapOrderOnUnknown` | root `v = u_desc`: map order judged on pairs in an unknown field refuses; the oracle accepts, because map order applies only where this reader declares the map |
| `MCReadOrderEvolutionReached.cfg` | none (vacuity) | root `q → 1`, `v = u_desc`, record 1 a blob: accepted with `v = dflt` and the blob decoded |

## Running

From this directory, on a bench (the Mac Studio):

```
nice -n 19 java -Xmx6g -cp ~/rowan-working/sdk/tla/tla2tools.jar tlc2.TLC \
  -workers 4 -config MCReadOrder.cfg -metadir "$JOB/tlc-MCReadOrder" ReadOrder
```

`MCReadOrder.cfg` is one record with all eight content symbols;
`MCReadOrderGraph.cfg` is two records with `absent` and `ok`, for cycles of two,
pointers inside an `X` record and a blob named from two bodies;
`MCReadOrderWide.cfg` is two records with `absent`, `ok`, `big` and `u_bad`.
Each must exhaust green; each reversed cfg must report `OneVerdict` (or
`NoEvolutionAccepted`) violated. The runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

Byte layout (tla/Framing and tla/FixedLive carry it), the message, JSON and
text reads, and nesting deeper than one body per record. A model checked here
does not prove any reader; the concrete proof is the read-then-write
conformance gate (docs/plans/strict-only.md layer 3).

# tla/FixedLive

A LAYOUT model of the fixed form's LOAD check loop over LIVE data
(docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.3 steps 11 and 12;
docs/SPEC-TABLES.md §3.4, THE SLACK RULE). One record image of the writer's
layout, byte by byte: a count (its SLE word, abstracted to the signed value it
spells), the `B` element slots of a counted array of bools, `B` being the
plan's bound, then an outer union tag `t1`, the arms' shared byte `u0` (arm 1:
the inner union's tag; arm 2: a bool `c`) and `u1` (arm 1 / inner arm 1: a bool
`b`). The check loop's entries carry the readers' arithmetic: element `i` at
offset `1 + i`, live while `i < min(count, B)`; `b` under the guard chain
`<<t1 = 1, u0 = 1>>`, outermost first; `c` under `<<t1 = 2>>`. A bool byte other
than `0` or `1` in live data refuses, a count outside `0..B` refuses, a tag past
the writer's arms is refused by the bounds pass after landing and the landing is
reset, and slack is never read and never refused. The entries run in any order
and the first condition met refuses. The ORACLE is written from the record's
MEANING — the first `count` elements, the arm the tags select, the payloads
under it — and not from offsets.

## Properties

- **TypeOK** — every variable in its finite domain.
- **OneVerdict** — whatever order the check loop ran in and wherever it
  stopped, the read refuses exactly the records the meaning refuses, with
  nothing landed in the caller's destination, and otherwise lands exactly the
  meaning.
- **InsideArray** — no element entry reads outside the array's own `B` slots.
- **NoSlackRead** — every offset an element entry reads is a live element.

## Reversed witnesses

| cfg | switch | invariant violated | counterexample |
|---|---|---|---|
| `MCFixedLiveCheckSlack.cfg` | `CheckSlack` | `OneVerdict` | `count = 0`, slot 2 holds `2`: liveness taken to the bound reads the slack and refuses; the slack rule (§3.4) reads it correctly |
| `MCFixedLiveBoolNormalize.cfg` | `BoolNormalize` | `OneVerdict` | `t1 = 1`, `u0 = 1`, `b = 2`: the retired normalization lands `b` as true and accepts; §4.5 refuses `wire_damaged` |
| `MCFixedLiveInnerGuardOnly.cfg` | `InnerGuardOnly` | `OneVerdict` | `t1 = 0` (no arm), `u0 = 1`, `u1 = 2`: `b` judged live from its inner link alone, read under an arm that never rode, refused; the meaning is an empty union and reads |
| `MCFixedLiveLoopToCount.cfg` | `LoopToCount` | `InsideArray` | `count = 4` past `B = 3`: an element entry run before the count check reads index 3 at offset 4, which is `t1`'s byte |
| `MCFixedLiveLandDuringCheck.cfg` | `LandDuringCheck` | `OneVerdict` | `count = 1`, `t1 = 1`, `u0 = 1`, `b = 2`: element 0 lands as it is checked, then `b` refuses and the destination keeps element 0; §5.3 says a refusal of the check loop writes not one destination byte |

## Running

```
nice -n 19 java -Xmx6g -cp ~/rowan-working/sdk/tla/tla2tools.jar tlc2.TLC \
  -workers 4 -config MCFixedLive.cfg -metadir "$JOB/tlc-MCFixedLive" FixedLive
```

`MCFixedLive.cfg` is `B = 3` and `MCFixedLiveFour.cfg` is `B = 4`, both
exhaustive over every record in the byte domain. The runs are recorded in
`tla/RUNS.tsv`.

## What this does not prove

Text and blob lengths, ranged scalars, enum ordinals and the plan's own
compile; batches of more than one record (the "total over the batch" rule is
held here as the one-record case of "nothing lands before every check
passes"). A model checked here does not prove any reader; the concrete proof is
the fixed-form corpus and its negative controls (§5.7).

# tla/NodeExtent

A LAYOUT model of the node extent (docs/SPEC-TABLES.md §2.8, §2.9, §7.2,
§7.6): where a cooked node's maps and lists lay their arrays. It carries the
arithmetic the writers carry — `internal/tablecook/extent.go`, the C++
reference's `ExtentAt` and `ExtentPack`, the Rust carve in
`internal/codegen/rusttable/cookcarve.go` — and an abstract "the entries are
somewhere in the node" cannot see: the node's base at its node alignment, the
record rounded to where the extent begins, the running offset, each array rounded to its
element's alignment, `count × size`, and the PRE-ORDER rule (a container's whole
array first, then each element's own arrays).

One node holds two containers in field order, A then B, and each element of A
may hold one child container. Every shape is chosen at Init: the record's
alignment (eight or sixteen, because a holder holds a sixteen-byte slot at
eight), the node's base, the record's size, each element's size and alignment
(up to sixteen, a 128-bit kind's), every count and every child count. The node
aligns to the greater of its record's alignment and every element alignment
its three shapes ask for, and the extent begins at the record's size rounded
to the greater of eight and that (docs/SPEC-TABLES.md §6.2, THE TWO ALIGNMENT
RULES; `ir.CookNodeAlign`, `ir.CookExtentStart`). A map's entries are a list's
elements with the generated `{ key, value }` table as the element; the key order
is not index arithmetic and is not modelled.

## Properties

- **AlignedInRegion** — every array sits at its element's alignment in the region.
- **NoOverlap** — no two arrays share a byte (lists and maps are one population).
- **Contained** — every array ends inside the extent the layout reserved.
- **OutsideRecord** — no array touches the record's own storage.
- **PreOrder** — a container's whole array comes before its elements' arrays.
- **SlotResolves** — each child slot lives inside its parent's array.

## Reversed witnesses

| cfg | switch | invariant violated | the bug it is |
|---|---|---|---|
| `MCNodeExtentSkipAlign.cfg` | `SkipAlign` | `AlignedInRegion` | an array laid at the running offset unaligned (the Rust carve's control `test/rust-cookmap/control align`, and the tool's `tables-lists-tool-cook-negative-control`) |
| `MCNodeExtentChildrenFirst.cfg` | `ChildrenFirst` | `NoOverlap` | an element's child array laid before the parent's whole array is reserved |
| `MCNodeExtentExtentOverRecord.cfg` | `ExtentOverRecord` | `OutsideRecord` | the extent begun at the node's base instead of after the record |
| `MCNodeExtentFloorOnlyStart.cfg` | `FloorOnlyStart` | `AlignedInRegion` | the extent begun at the record's size rounded to eight alone, so a sixteen-aligned array lands at eight modulo sixteen when the size is (the tool's `tables-tool-cook-node-align-negative-control`, its `start` half) |
| `MCNodeExtentRecordAlignedNode.cfg` | `RecordAlignedNode` | `AlignedInRegion` | the node aligned to its record alone, so a sixteen-aligned array lands at eight modulo sixteen when the node does (the same control, its `node` half) |

## Running

From this directory, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 300s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCNodeExtent.cfg -metadir "$JOB/tlc-MCNodeExtent" MCNodeExtent
```

Repeat for each of the six cfgs. The runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

The key order of a map's entries, which is not index arithmetic, and nesting
deeper than one level below the node's own containers.

# tla/PGroup

The supervision policy of `tools/pgroup` (`supervise.go`'s `step`), which every
self-hosted CI job runs its steps under. A command runs as the leader of its
own process group and may start a child that outlives it; pgroup must leave no
member of the group alive once it is done, whichever way the command ends.

## The instance

One leader and one child (`orphan`), which is all the policy distinguishes: the
leader's exit is observed, the child's is not. The OS is the environment: the
leader may die at any time, a forwarded SIGTERM may or may not kill each member,
and only SIGKILL is certain. The grace timer, once armed, fires once.

## Actions

`Spawn` and `LeaderDies` are the environment. `Exit`, `Stop`, `Wall` and
`Grace` are the four events `step` takes (`evExit`, `evStop`, `evWall`,
`evGrace`), each with the actions `step` returns. `Done` is pgroup having
exited.

## Properties

- **TypeOK**.
- **NoSurvivor**: once pgroup is done, the leader and its child are dead.
- **StopEnds**: once stopping (a signal or the wall), pgroup finishes, under
  weak fairness of its own two events and none for the environment.

## Reversed witnesses

| cfg | switch | property | the bug |
| --- | --- | --- | --- |
| `MCPGroup.cfg` | none | all hold | |
| `MCPGroupKillOnlyLeader.cfg` | `KillOnlyLeader` | `NoSurvivor` | a plain exec: the child outlives the command and holds the step open |
| `MCPGroupNoGraceKill.cfg` | `NoGraceKill` | `StopEnds` | grace never escalates to SIGKILL, so a member that catches SIGTERM keeps the step open |

The Go side of NoSurvivor is `TestEveryWayOutKillsTheGroup`; the process-level
proof is `TestOrphanDiesWithTheCommand` with its negative control
`TestOrphanOutlivesABareCommand` (`tools/pgroup`, under `SCHEMA_SLOW=1`).

## Running

```
nice -n 19 timeout 120s java -Xmx1g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCPGroup.cfg -metadir "$JOB/tlc-MCPGroup" PGroup
```

Repeat for each of the three cfgs. The runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

A member that leaves the group on purpose (setsid, Setpgid), and the window in
which a reaped leader's pid could be reused as a new group's id before the
group kill lands.
