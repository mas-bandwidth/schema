----------------------------- MODULE CardHops --------------------------------
\* A bounded model of one card record crossing builds of different languages
\* and declarations: written by one language's build, upgraded by a second,
\* edited by a third and read back by the first. Two abstract legs A and B
\* implement the same contract over two declarations, Older and Current;
\* each leg's Upgrade is the CardVersioning retained-round-trip abstraction
\* (retained decode with the leg's declaration, one accumulated report,
\* retained encode, publish or refuse). Current adds one field and renames one
\* with `was`; Older carries one field Current retired. This is not a codec.

EXTENDS Naturals, FiniteSets, TLC

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

Card == [fields : [F -> Scalar], body : BodySym, framing : Framing,
         needs : Scalar, tier : Scalar, paths : Scalar,
         test : Scalar, done : Scalar]

ActionKind == {"none", "write", "read", "upgrade", "edit", "hash"}

Status == {"empty", "ok", "refused"}

Observation == [kind : ActionKind, status : Status, leg : Leg, decl : D,
                field : EditFields, value : Scalar,
                input : Card, out : Card]

\* The fields each declaration can name.
Known(d) == CASE d = "Older"   -> {"kept", "renamed", "retired"}
             [] d = "Current"  -> {"kept", "added", "renamed"}

\* A field is physically present unless the writer elided it at its default.
Present(c) == { f \in F : c.fields[f] # "default" }

\* A present field this declaration cannot name.
UnknownPresent(d, c) == \E f \in F : f \notin Known(d) /\ c.fields[f] # "default"

\* ----------------------------------------------------------------------------
\* Each leg's Upgrade: the retained decode with the leg's declaration, one
\* accumulating report, retained encode, publish or refuse. A field the
\* declaration knows keeps its value; a field it does not know is retained
\* (kept in the retained tail) unless the DivergentLeg fault makes B write the
\* default where A retains the value.
\* ----------------------------------------------------------------------------

UpgField(leg, d, c, f) ==
  IF f \in Known(d)
  THEN c.fields[f]
  ELSE IF DivergentLeg /\ leg = "B"
       THEN "default"
       ELSE c.fields[f]

UpgOk(c) == c.framing = "ok"

UpgOut(leg, d, c) ==
  [ fields |-> [ f \in F |-> UpgField(leg, d, c, f) ],
    body |-> c.body,
    framing |-> "ok",
    needs |-> c.needs, tier |-> c.tier, paths |-> c.paths,
    test |-> c.test, done |-> c.done ]

\* The abstract result: the published rewrite on success, the original on
\* refusal.
UpgResult(leg, d, c) == IF UpgOk(c) THEN UpgOut(leg, d, c) ELSE c

\* ----------------------------------------------------------------------------
\* TaskHash is an abstract function of (body, paths, test, done) only; needs
\* and tier are excluded. The HashSeesNeeds fault folds needs into the digest.
\* ----------------------------------------------------------------------------

TaskHash(c) == IF HashSeesNeeds
              THEN <<c.body, c.paths, c.needs, c.test, c.done>>
              ELSE <<c.body, c.paths, c.test, c.done>>

OtherBody(b) == IF b = "bodyA" THEN "bodyB" ELSE "bodyA"

\* ----------------------------------------------------------------------------
\* Edit: a builder rewrite of one field. It is refused when the record holds
\* any field the declaration does not know (the c04 rule: an ordinary rewrite
\* cannot carry unknowns) and on success changes that field only. The
\* EditDropsUnknown fault lets the rewrite proceed while an unknown is present
\* and drops it; EditTouchesBody rewrites the body.
\* ----------------------------------------------------------------------------

EditGet(c, e) == CASE e = "needs" -> c.needs
                 [] e = "tier"  -> c.tier
                 [] e = "paths" -> c.paths

EditOk(d, c) == ~UnknownPresent(d, c) \/ EditDropsUnknown

EditOut(d, c, e, v) ==
  [ fields |-> [ f \in F |-> IF EditDropsUnknown /\ f \notin Known(d)
                             THEN "default" ELSE c.fields[f] ],
    body |-> IF EditTouchesBody THEN OtherBody(c.body) ELSE c.body,
    framing |-> c.framing,
    needs |-> IF e = "needs" THEN v ELSE c.needs,
    tier  |-> IF e = "tier"  THEN v ELSE c.tier,
    paths |-> IF e = "paths" THEN v ELSE c.paths,
    test |-> c.test, done |-> c.done ]

EditResult(d, c, e, v) == IF EditOk(d, c) THEN EditOut(d, c, e, v) ELSE c

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
    test |-> "one", done |-> "one" ]

emptyCard     == CardOf(FieldsOf("default", "default", "default", "default"), "bodyA", "ok")
currentCard   == CardOf(FieldsOf("one",     "two",     "one",     "default"), "bodyA", "ok")
olderCard     == CardOf(FieldsOf("one",     "default", "one",     "one"),     "bodyB", "ok")
bothCard      == CardOf(FieldsOf("one",     "two",     "one",     "one"),     "bodyA", "ok")
malformedCard == CardOf(FieldsOf("one",     "default", "one",     "default"), "bodyA", "malformed")

FixtureCards == { emptyCard, currentCard, olderCard, bothCard, malformedCard }

\* ----------------------------------------------------------------------------
\* The actions. Write replaces the record (the only action that may change the
\* body). Read creates a view and a report without changing the stored record.
\* Upgrade is ONE atomic attempt that publishes the retained rewrite on success
\* or refuses with the record unchanged. Edit is one field's builder rewrite.
\* Hash observes the TaskHash. Next enables every action: no guard hides an
\* edit or an upgrade.
\* ----------------------------------------------------------------------------

VARIABLES rec, last

vars == <<rec, last>>

Init ==
  /\ rec = emptyCard
  /\ last = [ kind |-> "none", status |-> "empty", leg |-> "A", decl |-> "Older",
              field |-> "paths", value |-> "default",
              input |-> emptyCard, out |-> emptyCard ]

Write ==
  \E leg \in Leg, d \in D, c \in FixtureCards :
    /\ rec' = c
    /\ last' = [ kind |-> "write", status |-> "ok", leg |-> leg, decl |-> d,
                 field |-> "paths", value |-> "default",
                 input |-> c, out |-> c ]

Read ==
  \E leg \in Leg, d \in D :
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "read", status |-> "ok", leg |-> leg, decl |-> d,
                 field |-> "paths", value |-> "default",
                 input |-> rec,
                 out |-> [ fields |-> [ f \in F |-> IF f \in Known(d)
                                                  THEN rec.fields[f] ELSE "default" ],
                           body |-> rec.body, framing |-> rec.framing,
                           needs |-> rec.needs, tier |-> rec.tier, paths |-> rec.paths,
                           test |-> rec.test, done |-> rec.done ] ]

Upgrade ==
  \E leg \in Leg, d \in D :
    LET c   == rec
        ok  == UpgOk(c)
        out == UpgOut(leg, d, c)
    IN /\ rec' = IF ok THEN out ELSE rec
       /\ last' = [ kind |-> "upgrade", status |-> IF ok THEN "ok" ELSE "refused",
                    leg |-> leg, decl |-> d, field |-> "paths", value |-> "default",
                    input |-> c, out |-> IF ok THEN out ELSE c ]

Edit ==
  \E leg \in Leg, d \in D, e \in EditFields, v \in Scalar :
    LET c   == rec
        ok  == EditOk(d, c)
        out == EditOut(d, c, e, v)
    IN /\ v # EditGet(c, e)
       /\ rec' = IF ok THEN out ELSE rec
       /\ last' = [ kind |-> "edit", status |-> IF ok THEN "ok" ELSE "refused",
                    leg |-> leg, decl |-> d, field |-> e, value |-> v,
                    input |-> c, out |-> IF ok THEN out ELSE c ]

Hash ==
  \E leg \in Leg :
    /\ UNCHANGED rec
    /\ last' = [ kind |-> "hash", status |-> "ok", leg |-> leg, decl |-> "Older",
                 field |-> "paths", value |-> "default",
                 input |-> rec, out |-> rec ]

Next == Write \/ Read \/ Upgrade \/ Edit \/ Hash

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
  /\ (last.kind = "upgrade" /\ last.status = "ok" =>
        \A f \in F : last.input.fields[f] # "default" =>
          last.out.fields[f] = last.input.fields[f])
  /\ (last.kind = "read" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "refused" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "ok" =>
        \A f \in F : last.out.fields[f] = last.input.fields[f])

\* Only Write changes the body.
BodyUnchanged ==
  last.kind \in {"read", "upgrade", "edit", "hash"} =>
    last.out.body = last.input.body

\* A refused Upgrade or Edit leaves the record exactly as it was.
RefusalKeepsOriginal ==
  /\ (last.kind = "upgrade" /\ last.status = "refused" => rec = last.input)
  /\ (last.kind = "edit" /\ last.status = "refused" => rec = last.input)

\* For the record and declaration just upgraded, Upgrade by A and by B produce
\* the same abstract output (what the conformance check asserts byte for byte).
ImplementationsAgree ==
  last.kind = "upgrade" =>
    UpgResult("A", last.decl, last.input) = UpgResult("B", last.decl, last.input)

\* Upgrade by A then B equals Upgrade by B then A equals one Upgrade, on the
\* same target declaration.
HopOrderIrrelevant ==
  last.kind = "upgrade" =>
    LET d   == last.decl
        c   == last.input
        ab  == UpgResult("B", d, UpgResult("A", d, c))
        ba  == UpgResult("A", d, UpgResult("B", d, c))
        one == UpgResult("A", d, c)
    IN /\ ab = ba
       /\ ab = one

\* An Edit of needs or tier leaves TaskHash unchanged; an Edit of paths changes
\* it.
TaskHashStable ==
  /\ (last.kind = "edit" /\ last.field \in {"needs", "tier"} =>
        TaskHash(last.input) = TaskHash(last.out))
  /\ (last.kind = "edit" /\ last.field = "paths" /\ last.status = "ok" =>
        TaskHash(last.input) # TaskHash(last.out))

\* ----------------------------------------------------------------------------
\* The model is a pure synchronous call: no fairness or eventual-newest
\* liveness is owed. A model checked here does not prove any implementation;
\* the conformance check and each language's tests do.
\* ----------------------------------------------------------------------------

=============================================================================
