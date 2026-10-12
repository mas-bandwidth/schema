# tla/CardHops

A bounded TLA+ model of one card record crossing builds of different languages
and declarations: written by one language's build, upgraded by a second, edited
by a third and read back by the first (docs/SPEC-TABLES.md §4, §5, §6.6). Two
abstract legs `A` and `B` implement the same contract over two declarations,
`Older` and `Current`; each leg's `Upgrade` is the CardVersioning
retained-round-trip abstraction (`tla/CardVersioning.tla`), restated per leg so
the two-leg hop stands alone. It is a model, not a codec, and a model checked
here does not prove any implementation.

## The finite instance

Two declarations: `Current` adds one field and renames one with `was`; `Older`
carries one field `Current` retired. The four stable field identities are
`kept` (known to both), `renamed` (the `was` rename keeps identity: known to
both), `added` (known to `Current` only) and `retired` (known to `Older` only).
A card is four field values (two scalar values plus each declaration's default,
plus the `default` symbol the writer elides), one body byte-symbol, a framing
flag, the three builder-edited fields `needs`, `tier`, `paths`, and the two
fixed task fields `test` and `done`. There is one record slot. The two legs are
abstract implementations with the same declaration knowledge; the divergence
switch (`DivergentLeg`) is the only place `B`'s `Upgrade` may differ from `A`'s.

## Action mapping to the language facades

Each action maps to the facade call the language streams implement
(`tables/cards`, `make/*.mk`):

| action | facade call | contract |
|---|---|---|
| `Write(leg, decl, values, body)` | write a fresh card / `Load` | replaces the record; the only action that may change the body |
| `Read(leg, decl)` | `Load` (the tolerant read) | a view and a report; the stored record is unchanged (§4) |
| `Upgrade(leg, decl)` | `Upgrade` (the retained round trip) | one atomic attempt: retained decode, one accumulating report, retained encode, publish or refuse (§6.6) |
| `Edit(leg, decl, field, value)` | `SetNeeds` / `SetTier` / `SetPaths` | a builder rewrite of one field; refused when the record holds any field the declaration does not know (the c04 rule), on success changes that field only (§6.2) |
| `Hash(leg)` | `TaskHash` | observes the digest of `(body, paths, test, done)` only |

`needs` and `tier` are outside the task hash; `paths`, `body`, `test` and `done`
are inside it.

## Properties

- **TypeOK** — bounded state: one record slot and one `last` observation over
  the finite card set.
- **NoFieldLost** — every field identity and value present before an `Upgrade`,
  `Read` or refused `Edit` is present after it, as known or retained; a
  successful `Edit` changes only its own field.
- **BodyUnchanged** — only `Write` changes the body.
- **RefusalKeepsOriginal** — a refused `Upgrade` or `Edit` leaves the record
  exactly as it was.
- **ImplementationsAgree** — for the record and declaration just upgraded,
  `Upgrade` by `A` and by `B` produce the same abstract output (what the
  conformance check asserts byte for byte).
- **HopOrderIrrelevant** — `Upgrade` by `A` then `B` equals `Upgrade` by `B`
  then `A` equals one `Upgrade`, on the same target declaration.
- **TaskHashStable** — an `Edit` of `needs` or `tier` leaves the `TaskHash`
  unchanged; an `Edit` of `paths` changes it.

## Finite limits

No byte layout, graph numbering, allocation or bounds arithmetic. A field value
is a symbol, not a bit pattern. The record is one slot, not an arbitrary
store. The report is abstracted away: it is not checked by these properties and
no unbounded history is kept. `test` and `done` are fixed symbols that never
move, so the task hash's dependence on them is constant. The model is a pure
synchronous call: no fairness or eventual-newest liveness is owed.

## The cfgs and their expected outcomes

`MCCardHops.cfg` holds all four fault switches `FALSE` and checks all seven
properties; it must exhaust green. Each reversed cfg turns exactly one fault
switch `TRUE`, keeps the others `FALSE`, and checks `TypeOK` plus its one named
invariant:

| cfg | switch | named invariant | expected counterexample |
|---|---|---|---|
| `MCCardHopsEditDropsUnknown.cfg` | `EditDropsUnknown` | `NoFieldLost` | `Edit` while `retired` is present and unknown to `Current` succeeds and drops it to `default` |
| `MCCardHopsDivergentLeg.cfg` | `DivergentLeg` | `ImplementationsAgree` | `Upgrade` to `Current` of the `olderCard`: `A` retains `retired`, `B` writes the default |
| `MCCardHopsEditTouchesBody.cfg` | `EditTouchesBody` | `BodyUnchanged` | an `Edit` rewrites the body `bodyA`→`bodyB` |
| `MCCardHopsHashSeesNeeds.cfg` | `HashSeesNeeds` | `TaskHashStable` | an `Edit` of `needs` changes the digest that now folds `needs` in |

A parser error, deadlock, timeout or a different invariant failing is not
evidence of the fault; only the named invariant must fail, reached through an
enabled action.

## Running

From this directory, on the Linux bench, with `/opt/tla/tla2tools.jar`:

```
nice -n 19 timeout 180s java -Xmx2g -cp /opt/tla/tla2tools.jar tlc2.TLC \
  -workers 1 -config MCCardHops.cfg -metadir "$JOB/tlc-MCCardHops" CardHops
```

Repeat for each of the five cfgs. The positive cfg must exit 0 with exhaustive
completion; each reversed cfg must report its named invariant violation with the
triggering action in the counterexample. The runs are recorded in `tla/RUNS.tsv`.

## What this does not prove

A model checked here does not prove any implementation. The concrete proof is
the card conformance check (`the-card-corpus-is-a-language-neutral-conformance-set`)
and each language stream's own upgrade and edit tests; the eight language
streams cite this model from their upgrade and edit functions as the contract
they implement.
