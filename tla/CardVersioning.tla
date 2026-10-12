----------------------------- MODULE CardVersioning -----------------------------
\* A bounded model of the cards Upgrade wrapper's retained round trip
\* (tables/cards/upgrade.go, docs/SPEC-TABLES.md §4, §5, §6.6). One atomic
\* attempt: keep the pre-attempt input, derive known/defaulted and retained data
\* plus ONE accumulating report across load and save, then publish a replacement
\* on fully checked success or refuse with the original slot unchanged.
\*
\* Declarations are finite mathematical labels (Legacy, Current, Compatible),
\* not product fields. No wire/compiler change, no runtime version number, no
\* hash ordering, no migration registry, no "newer means unknown" rule.
\*
\* ABSTRACTIONS (c03's generated-runtime tests prove the concrete operations):
\*   - no byte layout, graph numbering, allocation or bounds arithmetic;
\*   - a field value is a symbol, not a bit pattern; retention capacity is zero
\*     or one record, not a byte/id count;
\*   - the report counters are bounded 0..4 and observations overwrite one last
\*     result, never append unbounded history;
\*   - a card carries no writer label, matching §4's versionless wire: the
\*     report's unknown depends only on physically present content, so a
\*     "compatible" writer label alone forces no unknown count.

EXTENDS Naturals, FiniteSets, TLC

\* The four fault switches. The positive cfg holds all four FALSE; each reversed
\* cfg turns exactly one TRUE and checks TypeOK plus its named invariant.
CONSTANTS DropRetained,    \* drop a present retained scalar on a successful Upgrade
          SuppressUnknown, \* suppress a physically encountered unknown's count
          ChangeBody,      \* change the body on Upgrade
          RefuseShipped    \* refuse a supported SHIPPED case
ASSUME DropRetained \in BOOLEAN
ASSUME SuppressUnknown \in BOOLEAN
ASSUME ChangeBody \in BOOLEAN
ASSUME RefuseShipped \in BOOLEAN

\* ----------------------------------------------------------------------------
\* The finite instance.
\* ----------------------------------------------------------------------------

D == {"Legacy", "Current", "Compatible"}   \* the three declaration labels \*

F == {"renamed", "added", "retired", "opaque"}  \* the four stable field identities \*

Scalar == {"default", "one", "two"}        \* two scalar values plus each declaration's fixed default \*

BodySym == {"bodyA", "bodyB"}              \* the two body byte-string symbols \*

Payload == {"nodeIdx", "enumVar", "kindMis", "clamp"}  \* lossy payload categories (§6.6) \*

Val == Scalar \cup BodySym \cup Payload    \* a field's value \*

Framing == {"ok", "malformed", "failed"}   \* the finite malformed / failure flag \*

Cap == {0, 1}                              \* retention capacity: zero or one record \*

Slot == {1, 2}                             \* the two record slots \*

Card == [fields : [F -> Val], body : BodySym, framing : Framing]

Report == [unknown : 0..4, retained : 0..4, retainLost : 0..4,
           kindMismatch : 0..4, clamped : 0..4, malformed : BOOLEAN, failed : BOOLEAN]

Observation == [kind : {"none", "write", "read", "strict", "upgrade"},
                status : {"empty", "ok", "refused"},
                reader : D, slot : Slot,
                input : Card, out : Card,
                report : Report, cap : Cap, published : BOOLEAN]

\* The fields each declaration can name. "renamed" is known to all three (the
\* `was` rename keeps identity); "added" is known to Current/Compatible only;
\* "retired" is known to Legacy only; "opaque" is known to all three.
Known(d) == CASE d = "Legacy"     -> {"renamed", "retired", "opaque"}
             [] d = "Current"     -> {"renamed", "added", "opaque"}
             [] d = "Compatible"  -> {"renamed", "added", "opaque"}

\* A field is physically present unless the writer elided it at its default.
Present(c) == { f \in F : c.fields[f] # "default" }

\* How a present field (value v # "default") is read by declaration d.
\*   known field, scalar/opaque value  -> "known"      (reads through, silent)
\*   known field, node index (kind 17) -> "kindMismatch"
\*   known field, wrong kind           -> "kindMismatch"
\*   known field, out of bound         -> "clamped"
\*   known field, unknown variant      -> "excluded"   (§6.6: field is the reader's)
\*   unknown field, scalar/opaque      -> "retained"   (unknown retainable)
\*   unknown field, lossy payload      -> "excluded"   (§6.6 six excluded classes)
Classify(d, f, v) ==
  IF f \in Known(d)
  THEN CASE v \in Scalar \cup BodySym -> "known"
        [] v \in {"nodeIdx", "kindMis"} -> "kindMismatch"
        [] v = "clamp" -> "clamped"
        [] v = "enumVar" -> "excluded"
        [] OTHER -> "known"
  ELSE CASE v \in Scalar \cup BodySym -> "retained"
        [] v \in Payload -> "excluded"
        [] OTHER -> "excluded"

\* The report a retained read accumulates (unknown counts every id the reader
\* cannot name, whether retained or excluded; retained/excluded split §6.6).
UnknownCount(d, c) ==
  Cardinality({ f \in Present(c) : Classify(d, f, c.fields[f]) \in {"retained", "excluded"} })

RetainedBefore(d, c) == { f \in Present(c) : Classify(d, f, c.fields[f]) = "retained" }

\* Zero or one record of capacity: a retained record is kept only with capacity 1.
RetainedKept(d, c, cap) == IF cap = 1 THEN RetainedBefore(d, c) ELSE {}

ExcludedFields(d, c) == { f \in Present(c) : Classify(d, f, c.fields[f]) = "excluded" }

KindMismatchFields(d, c) == { f \in Present(c) : Classify(d, f, c.fields[f]) = "kindMismatch" }

ClampedFields(d, c) == { f \in Present(c) : Classify(d, f, c.fields[f]) = "clamped" }

RetainLostCount(d, c, cap) ==
  Cardinality(ExcludedFields(d, c))
    + (Cardinality(RetainedBefore(d, c)) - Cardinality(RetainedKept(d, c, cap)))

\* The one accumulating report across load and save (the SuppressUnknown fault
\* zeroes only the unknown counter).
UpgradeReport(d, c, cap) ==
  [ unknown |-> IF SuppressUnknown THEN 0 ELSE UnknownCount(d, c),
    retained |-> Cardinality(RetainedKept(d, c, cap)),
    retainLost |-> RetainLostCount(d, c, cap),
    kindMismatch |-> Cardinality(KindMismatchFields(d, c)),
    clamped |-> Cardinality(ClampedFields(d, c)),
    malformed |-> (c.framing = "malformed"),
    failed |-> (c.framing = "failed") ]

\* The tolerant plain read (no retention): known values plus the read report.
ReadReport(d, c) ==
  [ unknown |-> UnknownCount(d, c),
    retained |-> 0,
    retainLost |-> 0,
    kindMismatch |-> Cardinality(KindMismatchFields(d, c)),
    clamped |-> Cardinality(ClampedFields(d, c)),
    malformed |-> (c.framing = "malformed"),
    failed |-> (c.framing = "failed") ]

ReadView(d, c) ==
  [ fields |-> [ f \in F |-> IF f \in Present(c) /\ Classify(d, f, c.fields[f]) = "known"
                             THEN c.fields[f] ELSE "default" ],
    body |-> c.body,
    framing |-> c.framing ]

\* The replacement value of one field after a successful Upgrade: a retained,
\* kept field and a known field keep their value; every dropped, lost,
\* kind-mismatched or clamped field falls to default. The DropRetained fault
\* drops the present retained scalar.
UpgField(d, c, cap, f) ==
  IF f \in RetainedKept(d, c, cap) /\ ~DropRetained
  THEN c.fields[f]
  ELSE IF f \in Present(c) /\ Classify(d, f, c.fields[f]) = "known"
       THEN c.fields[f]
       ELSE "default"

OtherBody(b) == IF b = "bodyA" THEN "bodyB" ELSE "bodyA"

\* The published replacement: known values plus the retained tail, body kept
\* unless the ChangeBody fault flips it.
UpgradeOutput(d, c, cap) ==
  [ fields |-> [ f \in F |-> UpgField(d, c, cap, f) ],
    body |-> IF ChangeBody THEN OtherBody(c.body) ELSE c.body,
    framing |-> "ok" ]

\* ----------------------------------------------------------------------------
\* The fixtures: four SHIPPED triples and the unsupported / lossy cards.
\* ----------------------------------------------------------------------------

CardOf(ren, add, ret, op, body, framing) ==
  [ fields |-> [ f \in F |-> CASE f = "renamed" -> ren
                               [] f = "added" -> add
                               [] f = "retired" -> ret
                               [] f = "opaque" -> op ],
    body |-> body,
    framing |-> framing ]

\* SHIPPED: legacy-to-current (add/default and was); current rewritten
\* idempotently; a removed nondefault scalar retained; a compatible
\* later-produced input read through Legacy with no physically present unknown.
legacyCard     == CardOf("one", "default", "default", "bodyA", "bodyA", "ok")
currentCard    == CardOf("one", "two", "default", "bodyB", "bodyB", "ok")
removedCard    == CardOf("one", "default", "one", "bodyA", "bodyA", "ok")
compatibleCard == CardOf("two", "default", "default", "bodyA", "bodyA", "ok")

ShippedTriples == { [reader |-> "Current", card |-> legacyCard,     cap |-> 1],
                    [reader |-> "Current", card |-> currentCard,    cap |-> 1],
                    [reader |-> "Current", card |-> removedCard,    cap |-> 1],
                    [reader |-> "Legacy",  card |-> compatibleCard, cap |-> 1] }

IsShipped(d, c, cap) == \E t \in ShippedTriples : t.reader = d /\ t.card = c /\ t.cap = cap

\* Unknown/Retained alone are not loss; RetainLost/KindMismatch/Clamped/Malformed
\* or any failed call prevents publication (and the RefuseShipped fault refuses
\* a supported SHIPPED case).
UpgradeOk(d, c, cap) ==
  /\ c.framing = "ok"
  /\ RetainLostCount(d, c, cap) = 0
  /\ Cardinality(KindMismatchFields(d, c)) = 0
  /\ Cardinality(ClampedFields(d, c)) = 0
  /\ ~(RefuseShipped /\ IsShipped(d, c, cap))

\* UNSUPPORTED / LOSSY cards: unknown node-bearing field, unknown enum/union
\* value in a known field, retention capacity exhaustion (removedCard at cap 0),
\* kind mismatch, clamp, malformed input, failed load/measure/save.
nodeIndexCard    == CardOf("one", "default", "nodeIdx", "bodyA", "bodyA", "ok")
enumVariantCard  == CardOf("enumVar", "default", "default", "bodyA", "bodyA", "ok")
kindMismatchCard == CardOf("kindMis", "default", "default", "bodyA", "bodyA", "ok")
clampCard        == CardOf("clamp", "default", "default", "bodyA", "bodyA", "ok")
malformedCard    == CardOf("one", "default", "default", "bodyA", "bodyA", "malformed")
failedCard       == CardOf("one", "default", "default", "bodyA", "bodyA", "failed")

FixtureCards == { legacyCard, currentCard, removedCard, compatibleCard,
                  nodeIndexCard, enumVariantCard, kindMismatchCard, clampCard,
                  malformedCard, failedCard }

EmptyCard == CardOf("default", "default", "default", "default", "bodyA", "ok")

ZeroReport == [unknown |-> 0, retained |-> 0, retainLost |-> 0, kindMismatch |-> 0,
               clamped |-> 0, malformed |-> FALSE, failed |-> FALSE]

\* ----------------------------------------------------------------------------
\* The actions. Init establishes the finite declarations, fixture records and an
\* empty last-result observation. Write replaces a slot with one allowed
\* fixture, including its body (old superseded values are not owed). Read
\* creates a decoded view and report without replacing stored wire. Upgrade is
\* ONE atomic attempt that publishes on fully checked success or refuses with
\* the original slot unchanged. ReadStrict is a policy observation over Read.
\* ----------------------------------------------------------------------------

VARIABLES slot, last

vars == <<slot, last>>

Init ==
  /\ slot = [ r \in Slot |-> EmptyCard ]
  /\ last = [ kind |-> "none", status |-> "empty", reader |-> "Legacy", slot |-> 1,
              input |-> EmptyCard, out |-> EmptyCard, report |-> ZeroReport,
              cap |-> 0, published |-> FALSE ]

Write ==
  \E r \in Slot, c \in FixtureCards :
    /\ slot' = [slot EXCEPT ![r] = c]
    /\ last' = [ kind |-> "write", status |-> "ok", reader |-> "Legacy", slot |-> r,
                 input |-> c, out |-> c, report |-> ZeroReport, cap |-> 0,
                 published |-> FALSE ]

Read ==
  \E r \in Slot, d \in D :
    /\ UNCHANGED slot
    /\ last' = [ kind |-> "read", status |-> "ok", reader |-> d, slot |-> r,
                 input |-> slot[r], out |-> ReadView(d, slot[r]),
                 report |-> ReadReport(d, slot[r]), cap |-> 0, published |-> FALSE ]

ReadStrict ==
  \E r \in Slot, d \in D :
    LET rep == ReadReport(d, slot[r])
        ok  == rep.unknown = 0 /\ rep.kindMismatch = 0 /\ rep.clamped = 0
               /\ ~rep.malformed /\ ~rep.failed
    IN /\ UNCHANGED slot
       /\ last' = [ kind |-> "strict", status |-> IF ok THEN "ok" ELSE "refused",
                    reader |-> d, slot |-> r,
                    input |-> slot[r], out |-> ReadView(d, slot[r]), report |-> rep,
                    cap |-> 0, published |-> FALSE ]

Upgrade ==
  \E r \in Slot, d \in D, cap \in Cap :
    LET c   == slot[r]
        rep == UpgradeReport(d, c, cap)
        ok  == UpgradeOk(d, c, cap)
        out == UpgradeOutput(d, c, cap)
    IN /\ slot' = IF ok THEN [slot EXCEPT ![r] = out] ELSE slot
       /\ last' = [ kind |-> "upgrade", status |-> IF ok THEN "ok" ELSE "refused",
                    reader |-> d, slot |-> r,
                    input |-> c, out |-> IF ok THEN out ELSE c, report |-> rep,
                    cap |-> cap, published |-> ok ]

Next == Write \/ Read \/ ReadStrict \/ Upgrade

Spec == Init /\ [][Next]_vars

\* ----------------------------------------------------------------------------
\* The invariants.
\* ----------------------------------------------------------------------------

TypeOK ==
  /\ slot \in [Slot -> Card]
  /\ last \in Observation

\* After a SUCCESSFUL Upgrade, each pre-attempt logical field identity/value
\* survives in the output's known values or retained data; defaults for new
\* fields are allowed, and `was` changes no identity (renamed is known to all).
NoFieldLost ==
  last.kind = "upgrade" /\ last.status = "ok" =>
    \A f \in F : last.input.fields[f] # "default" => last.out.fields[f] = last.input.fields[f]

\* Read/Upgrade, successful or refused, preserves the stored body byte-symbol
\* exactly; only Write may replace it.
BodyUnchanged ==
  last.kind \in {"read", "strict", "upgrade"} => last.out.body = last.input.body

\* A refused Upgrade returns no replacement and keeps the entire original input,
\* not just its body.
RefusalKeepsOriginal ==
  last.kind = "upgrade" /\ last.status = "refused" =>
    /\ slot[last.slot] = last.input
    /\ ~last.published

ExpectedUnknown(d, c) == UnknownCount(d, c)

\* Physically encountered unknown ids/variants produce the proper report count;
\* a compatible writer label alone forces no unknown count (cards carry no
\* writer label, and the compatibleCard fixture reads with zero unknown).
UnknownIsReported ==
  last.kind = "upgrade" => last.report.unknown = ExpectedUnknown(last.reader, last.input)

\* Every attempted SHIPPED triple succeeds (not vacuously implied only by
\* success: the positive Next relation enables each shipped Write/Upgrade), and
\* a same-target repeat upgrade is equal over the abstract normalized output.
RepeatUpgradeStable ==
  \A d \in D, c \in FixtureCards, cap \in Cap :
    UpgradeOk(d, c, cap) =>
      UpgradeOutput(d, UpgradeOutput(d, c, cap), cap) = UpgradeOutput(d, c, cap)

ShippedUpgradeSucceeds ==
  /\ (last.kind = "upgrade" /\ IsShipped(last.reader, last.input, last.cap)
        => last.status = "ok")
  /\ RepeatUpgradeStable

\* ----------------------------------------------------------------------------
\* The model is a pure synchronous call: no fairness or eventual-newest liveness
\* is owed. State that the model does not prove the Go implementation merely
\* because a comment cites it.
\* ----------------------------------------------------------------------------

=============================================================================
