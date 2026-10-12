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
