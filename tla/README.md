# tla/CardVersioning

A bounded TLA+ model of the cards `Upgrade` wrapper's retained round trip
(`tables/cards/upgrade.go`, docs/SPEC-TABLES.md §4, §5, §6.6). It checks the
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

The model holds the six named properties:

- **TypeOK** — bounded state: two record slots over the finite card set, one
  `last` observation, report counters in `0..4`.
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
superseded values are not owed). `Read(d, r)` creates a decoded view and report
without replacing stored wire. `ReadStrict(d, r)` is a policy observation over
`Read`. `Upgrade(d, r, cap)` is ONE atomic attempt: keep the input, derive
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
| kind mismatch | `kindMismatchCard` (`renamed=kindMis`) | §4 kind mismatch (conformance manifest) |
| clamp | `clampCard` (`renamed=clamp`) | §4 clamp (bounds tightened) |
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

## Running (Step 4)

From this directory, on the Linux bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 180s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCCardVersioning.cfg -metadir "$JOB/tlc-MCCardVersioning" CardVersioning
```

(repeat for each of the five cfgs; the positive cfg must exit 0 with exhaustive
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
| `Read(leg, decl)` | `Load` (the tolerant read) | a view and a report; the stored record is unchanged (§4) |
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
(docs/LAYERS/1-framing.md; docs/SPEC-TABLES.md §3, §4, §2.9): one parent body
of `N` bytes walked by a cursor, one length-framed field at a time, carrying
the arithmetic the readers carry — the field's start, its end `start + L`, the
header that must lie inside `[start, end)`, and the resume point after a field
read whole — and the one identity fact framing owns, each field's reference
against the set its body already carried. INPUT NO WRITER PRODUCES IS DAMAGE
(docs/SPEC-TABLES.md §4; Glenn, #391: "Table reads are not trusted, they will
come over the network."), and damage ends the scan: DAMAGE DISCARDS THE WHOLE
LOAD, so nothing resumes after it. It is a model, not a codec: the kind byte
and the element values are folded away and only offsets and references
remain, which is what a layout model is for (Glenn, 2026-10-29: the bugs live
in the mapping to memory).

## The finite instance

`N = 6`, `MaxL = 7`, `HeaderBytes = {1, 2, 3}`, `Refs = {1, 2}`. Every field
chooses every reference, every `L` in `0..MaxL` and every header width (the
element kind byte plus a count of one or more LEB128 bytes), so TLC walks every
placement of a header against every `L` and every buffer end, including `L`
exactly at the buffer, one past it, a header that crosses `L` inside the
buffer and one that crosses the buffer, and every order of two references.

## Actions

| action | the rule |
|---|---|
| `Terminator` | the zero reference ends the body (F3, R1) |
| `Truncated` | no byte left for a reference: damage, and the load ends |
| `Field(R, L, H)` | one length-framed array field: `L` past the body is damage (L4); a reference the body already carried is damage (R5); fewer than two bytes is damage (H2); a header past `L` is damage (H3); otherwise the elements are bounded by `L` and the parent resumes at `end` (L1). Damage ends the load |
| `Finished` | stutter once the scan is done |

## Properties

- **TypeOK** — every offset bounded by `N + MaxL + 1`.
- **NoReadPastBuffer** — no field's read reaches past the buffer (L4, H5).
- **ResumeAtEnd** — after every field the parent did not stop on, it resumed
  exactly at `start + L` (L1).
- **DamageEndsLoad** — once `malformed` is set the scan is done: no field is
  read after damage (docs/SPEC-TABLES.md §4).
- **HeaderInsideBody** — no accepted field is shorter than its header and no
  accepted field's header runs past its `L` (H2, H3).
- **OneIdOncePerBody** — no field is accepted under a reference its body
  already carried (R5).

## Reversed witnesses

Each reversed cfg turns exactly one fault switch TRUE and checks TypeOK plus
the one invariant that names the rule; only that invariant may fail.

| cfg | switch | invariant violated | the bug it is |
|---|---|---|---|
| `MCFramingHeaderAgainstParent.cfg` | `HeaderAgainstParent` | `HeaderInsideBody` | a count spelled past `L` completed from the parent's bytes |
| `MCFramingShortBodyInert.cfg` | `ShortBodyInert` | `HeaderInsideBody` | a body of fewer than two bytes stepped over as inert |
| `MCFramingRepeatLastWins.cfg` | `RepeatLastWins` | `OneIdOncePerBody` | a body carrying one id twice accepted, the last occurrence winning |
| `MCFramingOffByOneAtL.cfg` | `OffByOneAtL` | `NoReadPastBuffer` | a payload ending one byte past the buffer accepted, and read |
| `MCFramingResumeAtHeader.cfg` | `ResumeAtHeader` | `ResumeAtEnd` | resuming after the header instead of at `end`, so the elements are read as the parent's next field |
| `MCFramingResumeAfterDamage.cfg` | `ResumeAfterDamage` | `DamageEndsLoad` | resuming past `L` after damage inside a field, the partial read the ruling of 2026-10-31 removed |

Each counterexample was checked by hand against a reader written the way its
switch reads: a header completed past `L` from the parent's bytes, a body of
fewer than two bytes stepped over, a repeated id read again, a payload one
byte past the buffer, a resume after the header, and a resume after damage.

## Running

From this directory, on a Linux bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 180s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCFraming.cfg -metadir "$JOB/tlc-MCFraming" Framing
```

Repeat for each of the seven cfgs. The positive cfg must exit 0 with exhaustive
completion; each reversed cfg must report its named invariant violation. The
runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

A model checked here does not prove any reader. The concrete proof is the
`framing` conformance surface, which runs the hand-built vectors of
docs/LAYERS/1-framing.md through every leg, and the engine's oracle check
beside it.
