----------------------------- MODULE CardHops --------------------------------
\* A bounded model of one card record crossing builds of different languages
\* and declarations: written by one language's build, upgraded by a second,
\* edited by a third and read back by the first. Two abstract legs A and B
\* implement the same contract over two declarations, Older and Current;
\* each leg's Upgrade is the CardVersioning retained-round-trip abstraction
\* (retained decode with the leg's declaration, one accumulated report,
\* retained encode, publish or refuse) with a retention capacity, the
\* abstraction of upgrade(wire, retainBytes, retainIDs) in
\* tables/cards/upgrade.go. Current adds one field and renames one with
\* `was`; Older carries one field Current retired. This is not a codec.

EXTENDS Naturals, FiniteSets, TLC

\* The retention capacities one Upgrade may be given: how many fields the
\* leg's declaration cannot name one attempt can retain. The positive cfg sets
\* Caps = {0, 1}.
CONSTANT Caps
ASSUME Caps \subseteq Nat

\* The four fault switches. The positive cfg holds all four FALSE; each
\* reversed cfg turns exactly one TRUE and checks TypeOK plus its named
\* invariant.
CONSTANTS EditDropsUnknown,   \* an Edit while an unknown field is present is
                              \* allowed and its rewrite drops the unknown
          DivergentLeg,       \* B's Upgrade writes a default where A retains
          EditTouchesBody,    \* an Edit rewrites the body
          HashSeesNeeds       \* TaskHash folds needs into the digest
ASSUME EditDropsUnknown \in BOOLEAN
ASSUME DivergentLeg \in BOOLEAN
ASSUME EditTouchesBody \in BOOLEAN
ASSUME HashSeesNeeds \in BOOLEAN

\* ----------------------------------------------------------------------------
\* The finite instance.
\* ----------------------------------------------------------------------------

Leg == {"A", "B"}                              \* the two abstract implementations

D == {"Older", "Current"}                      \* the two declarations

\* The four stable field identities. kept and renamed are known to both (the
\* `was` rename keeps identity); added is known to Current only; retired is
\* known to Older only.
F == {"kept", "added", "renamed", "retired"}

Scalar == {"default", "one", "two"}            \* two scalar values plus each declaration's default

BodySym == {"bodyA", "bodyB"}                  \* the two body byte-string symbols

Framing == {"ok", "malformed"}                 \* the finite framing flag

EditFields == {"needs", "tier", "paths"}       \* the three builder-edited fields

\* A card: the four field values, the body, the framing flag, the three
\* builder-edited fields, and the four fixed task fields new, shared, test and
\* done that TaskHash frames with body and paths (tables/cards/edit.go).
Card == [fields : [F -> Scalar], body : BodySym, framing : Framing,
         needs : Scalar, tier : Scalar, paths : Scalar,
         new : Scalar, shared : Scalar, test : Scalar, done : Scalar]

ActionKind == {"none", "write", "read", "upgrade", "edit", "hash"}

Status == {"empty", "ok", "refused"}

Observation == [kind : ActionKind, status : Status, leg : Leg, decl : D,
                cap : Caps, field : EditFields, value : Scalar,
                input : Card, out : Card]

\* The fields each declaration can name.
Known(d) == CASE d = "Older"   -> {"kept", "renamed", "retired"}
             [] d = "Current"  -> {"kept", "added", "renamed"}

\* A field is physically present unless the writer elided it at its default.
Present(c) == { f \in F : c.fields[f] # "default" }

\* The present fields this declaration cannot name: what a retaining decode
\* must keep in its retained tail.
Unknown(d, c) == { f \in Present(c) : f \notin Known(d) }

UnknownPresent(d, c) == Unknown(d, c) # {}

\* ----------------------------------------------------------------------------
\* Each leg's Upgrade: the retained decode with the leg's declaration, one
\* accumulating report, retained encode, publish or refuse. A field the
\* declaration knows keeps its value; a field it does not know is retained
\* (kept in the retained tail) unless the DivergentLeg fault makes B write the
\* default where A retains the value. The attempt is refused (RetainLost,
\* tables/cards/upgrade.go lossErr) when the record holds more unknown fields
\* than the capacity can retain, and when the wire is malformed.
\* ----------------------------------------------------------------------------

UpgField(leg, d, c, f) ==
  IF f \in Known(d)
  THEN c.fields[f]
  ELSE IF DivergentLeg /\ leg = "B"
       THEN "default"
       ELSE c.fields[f]

UpgOk(d, c, cap) ==
  /\ c.framing = "ok"
  /\ Cardinality(Unknown(d, c)) <= cap

UpgOut(leg, d, c) ==
  [ fields |-> [ f \in F |-> UpgField(leg, d, c, f) ],
    body |-> c.body,
    framing |-> "ok",
    needs |-> c.needs, tier |-> c.tier, paths |-> c.paths,
    new |-> c.new, shared |-> c.shared, test |-> c.test, done |-> c.done ]

\* The abstract result: the published rewrite on success, the original on
\* refusal.
UpgResult(leg, d, c, cap) == IF UpgOk(d, c, cap) THEN UpgOut(leg, d, c) ELSE c

\* ----------------------------------------------------------------------------
\* TaskHash is an abstract function of (body, paths, new, shared, test, done),
\* the frame TaskHash in tables/cards/edit.go hashes; needs and tier are
\* excluded. The HashSeesNeeds fault folds needs into the digest.
\* ----------------------------------------------------------------------------

TaskHash(c) == IF HashSeesNeeds
              THEN <<c.body, c.paths, c.needs, c.new, c.shared, c.test, c.done>>
              ELSE <<c.body, c.paths, c.new, c.shared, c.test, c.done>>

OtherBody(b) == IF b = "bodyA" THEN "bodyB" ELSE "bodyA"

\* ----------------------------------------------------------------------------
\* Edit: a builder rewrite of one field. It is refused when the record holds
\* any field the declaration does not know or is malformed (refuseLossy in
\* tables/cards/edit.go: an ordinary rewrite cannot carry unknowns) and on
\* success changes that field only. The EditDropsUnknown fault lets the
\* rewrite proceed while an unknown is present and drops it; EditTouchesBody
\* rewrites the body.
\* ----------------------------------------------------------------------------

EditGet(c, e) == CASE e = "needs" -> c.needs
                 [] e = "tier"  -> c.tier
                 [] e = "paths" -> c.paths

EditOk(d, c) ==
  /\ c.framing = "ok"
  /\ (~UnknownPresent(d, c) \/ EditDropsUnknown)

EditOut(d, c, e, v) ==
  [ fields |-> [ f \in F |-> IF EditDropsUnknown /\ f \notin Known(d)
                             THEN "default" ELSE c.fields[f] ],
    body |-> IF EditTouchesBody THEN OtherBody(c.body) ELSE c.body,
    framing |-> c.framing,
    needs |-> IF e = "needs" THEN v ELSE c.needs,
    tier  |-> IF e = "tier"  THEN v ELSE c.tier,
    paths |-> IF e = "paths" THEN v ELSE c.paths,
    new |-> c.new, shared |-> c.shared, test |-> c.test, done |-> c.done ]

\* ----------------------------------------------------------------------------
\* The fixtures: a small set of cards Write can produce, covering every field
\* combination the properties need.
\* ----------------------------------------------------------------------------

FieldsOf(kept, added, renamed, retired) ==
  [ f \in F |-> CASE f = "kept"    -> kept
                 [] f = "added"    -> added
                 [] f = "renamed"  -> renamed
                 [] f = "retired"  -> retired ]

CardOf(fields, body, framing) ==
  [ fields |-> fields, body |-> body, framing |-> framing,
    needs |-> "default", tier |-> "default", paths |-> "default",
    new |-> "one", shared |-> "one", test |-> "one", done |-> "one" ]

emptyCard     == CardOf(FieldsOf("default", "default", "default", "default"), "bodyA", "ok")
currentCard   == CardOf(FieldsOf("one",     "two",     "one",     "default"), "bodyA", "ok")
olderCard     == CardOf(FieldsOf("one",     "default", "one",     "one"),     "bodyB", "ok")
bothCard      == CardOf(FieldsOf("one",     "two",     "one",     "one"),     "bodyA", "ok")
malformedCard == CardOf(FieldsOf("one",     "default", "one",     "default"), "bodyA", "malformed")

FixtureCards == { emptyCard, currentCard, olderCard, bothCard, malformedCard }

\* ----------------------------------------------------------------------------
\* The actions. Write replaces the record (the only action that may change the
\* body). Read creates a view and a report without changing the stored record.
\* Upgrade is ONE atomic attempt with a capacity: it publishes the retained
\* rewrite on success (UpgradePublished) or refuses with the record unchanged
\* (UpgradeRefused). Edit is one field's builder rewrite, published
\* (EditPublished) or refused (EditRefused). Hash observes the TaskHash. The
\* refused branches are their own disjuncts of Next so TLC's coverage reports
\* them. Next enables every action: no guard hides an edit or an upgrade.
\* ----------------------------------------------------------------------------

VARIABLES rec, last

vars == <<rec, last>>

Init ==
  /\ rec = emptyCard
  /\ last = [ kind |-> "none", status |-> "empty", leg |-> "A", decl |-> "Older",
              cap |-> 0, field |-> "paths", value |-> "default",
              input |-> emptyCard, out |-> emptyCard ]

Write ==
  \E leg \in Leg, d \in D, c \in FixtureCards :
    /\ rec' = c
    /\ last' = [ kind |-> "write", status |-> "ok", leg |-> leg, decl |-> d,
                 cap |-> 0, field |-> "paths", value |-> "default",
                 input |-> c, out |-> c ]

Read ==
  \E leg \in Leg, d \in D :
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "read", status |-> "ok", leg |-> leg, decl |-> d,
                 cap |-> 0, field |-> "paths", value |-> "default",
                 input |-> rec,
                 out |-> [ fields |-> [ f \in F |-> IF f \in Known(d)
                                                  THEN rec.fields[f] ELSE "default" ],
                           body |-> rec.body, framing |-> rec.framing,
                           needs |-> rec.needs, tier |-> rec.tier, paths |-> rec.paths,
                           new |-> rec.new, shared |-> rec.shared,
                           test |-> rec.test, done |-> rec.done ] ]

UpgradePublished ==
  \E leg \in Leg, d \in D, cap \in Caps :
    /\ UpgOk(d, rec, cap)
    /\ rec' = UpgOut(leg, d, rec)
    /\ last' = [ kind |-> "upgrade", status |-> "ok", leg |-> leg, decl |-> d,
                 cap |-> cap, field |-> "paths", value |-> "default",
                 input |-> rec, out |-> UpgOut(leg, d, rec) ]

UpgradeRefused ==
  \E leg \in Leg, d \in D, cap \in Caps :
    /\ ~UpgOk(d, rec, cap)
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "upgrade", status |-> "refused", leg |-> leg, decl |-> d,
                 cap |-> cap, field |-> "paths", value |-> "default",
                 input |-> rec, out |-> rec ]

Upgrade == UpgradePublished \/ UpgradeRefused

EditPublished ==
  \E leg \in Leg, d \in D, e \in EditFields, v \in Scalar :
    /\ v # EditGet(rec, e)
    /\ EditOk(d, rec)
    /\ rec' = EditOut(d, rec, e, v)
    /\ last' = [ kind |-> "edit", status |-> "ok", leg |-> leg, decl |-> d,
                 cap |-> 0, field |-> e, value |-> v,
                 input |-> rec, out |-> EditOut(d, rec, e, v) ]

EditRefused ==
  \E leg \in Leg, d \in D, e \in EditFields, v \in Scalar :
    /\ v # EditGet(rec, e)
    /\ ~EditOk(d, rec)
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "edit", status |-> "refused", leg |-> leg, decl |-> d,
                 cap |-> 0, field |-> e, value |-> v,
                 input |-> rec, out |-> rec ]

Edit == EditPublished \/ EditRefused

Hash ==
  \E leg \in Leg :
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "hash", status |-> "ok", leg |-> leg, decl |-> "Older",
                 cap |-> 0, field |-> "paths", value |-> "default",
                 input |-> rec, out |-> rec ]

Next == Write \/ Read \/ UpgradePublished \/ UpgradeRefused
        \/ EditPublished \/ EditRefused \/ Hash

Spec == Init /\ [][Next]_vars

\* ----------------------------------------------------------------------------
\* The invariants.
\* ----------------------------------------------------------------------------

TypeOK ==
  /\ rec \in Card
  /\ last \in Observation

\* Every field identity and value present before an Upgrade, Read or refused
\* Edit is present after it, as known or retained; a successful Edit changes
\* only its own field.
NoFieldLost ==
  /\ (last.kind = "upgrade" =>
        \A f \in F : last.input.fields[f] # "default" =>
          rec.fields[f] = last.input.fields[f])
  /\ (last.kind = "read" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "refused" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "ok" =>
        /\ \A f \in F : last.out.fields[f] = last.input.fields[f]
        /\ \A e \in EditFields : e # last.field =>
             EditGet(last.out, e) = EditGet(last.input, e))

\* Only Write changes the body.
BodyUnchanged ==
  last.kind \in {"read", "upgrade", "edit", "hash"} =>
    rec.body = last.input.body

\* A refused Upgrade or Edit leaves the record exactly as it was.
RefusalKeepsOriginal ==
  /\ (last.kind = "upgrade" /\ last.status = "refused" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "refused" => rec = last.input)

\* For every reachable record, declaration and capacity, Upgrade by A and by B
\* produce the same abstract output (what the conformance check asserts byte
\* for byte).
ImplementationsAgree ==
  \A d \in D, cap \in Caps :
    UpgResult("A", d, rec, cap) = UpgResult("B", d, rec, cap)

\* Upgrade by A then B equals Upgrade by B then A equals one Upgrade, on the
\* same target declaration and capacity, for every reachable record.
HopOrderIrrelevant ==
  \A d \in D, cap \in Caps :
    LET ab  == UpgResult("B", d, UpgResult("A", d, rec, cap), cap)
        ba  == UpgResult("A", d, UpgResult("B", d, rec, cap), cap)
        one == UpgResult("A", d, rec, cap)
    IN /\ ab = ba
       /\ ab = one

\* An Edit of needs or tier leaves TaskHash unchanged; an Edit of paths to a
\* different value changes it.
TaskHashStable ==
  /\ (last.kind = "edit" /\ last.field \in {"needs", "tier"} =>
        TaskHash(last.input) = TaskHash(rec))
  /\ (last.kind = "edit" /\ last.field = "paths" /\ last.status = "ok" =>
        TaskHash(last.input) # TaskHash(rec))

\* ----------------------------------------------------------------------------
\* The model is a pure synchronous call: no fairness or eventual-newest
\* liveness is owed. A model checked here does not prove any implementation;
\* the conformance check and each language's tests do.
\* ----------------------------------------------------------------------------

=============================================================================
