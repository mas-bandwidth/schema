------------------------------ MODULE ReadOrder ------------------------------
\* The one read's verdict contract (docs/SPEC-TABLES.md §4, §6.5, §3.1;
\* docs/plans/strict-only.md layer 1): for every input and declaration a read
\* is ACCEPTED with exactly one value or REFUSED with no partial value, and the
\* verdict does not depend on the order in which a reader detects conditions.
\*
\* The input is a small variable-form file reduced to what the verdict reads:
\* a root body (node 0, of the root type A) and a node table of `Records`,
\* each record of a type this reader names (A, B, or the reserved blob type
\* every reader knows) or of a type it cannot name (X). Every body carries
\* three slots:
\*
\*   p  a pointer field this reader DECLARES on A and B (A.p : *B, B.p :
\*      *bytes), a kind 17 payload; on an X record it is a field this reader
\*      cannot name
\*   q  a pointer field under an id no build of this reader names: a kind 17
\*      payload in an UNKNOWN field, skipped and counted `unknown`, still a
\*      pointer of the graph (§6.5, THE GRAPH, DEFINED ONCE)
\*   v  the value slot, one content symbol (below)
\*
\* Content symbols for v. A and B both declare v as a map with keys in 0..1;
\* A's is optional, B's is REQUIRED.
\*   "absent"  the known field did not ride (B refuses missing_required, A
\*             reads its default)
\*   "ok"      the known field rode and conforms
\*   "big"     the known field rode with a key past the reader's range
\*             (out_of_range: declaration-dependent)
\*   "desc"    the known field rode with keys descending (map order:
\*             declaration-dependent, §4 and §6.5)
\*   "wk"      the known field rode under a kind that is not the declared one
\*             and not a widening (kind_mismatch: declaration-dependent)
\*   "bad"     the known field rode with bytes the wire alone shows to be
\*             wrong: a bool byte of 2, ill-formed text, a header across its
\*             L (wire_damaged: decidable from the wire alone)
\*   "u_desc"  the known field is absent and an UNKNOWN field rode in its
\*             place carrying pairs whose keys descend: bytes no declaration
\*             of this reader judges (§4: map order applies only where this
\*             reader declares the field a map)
\*   "u_bad"   the known field is absent and an UNKNOWN field rode carrying
\*             bytes the wire alone shows to be wrong: damage inside an unknown
\*             field refuses the load (§4, "Skipped is not unread")
\* On an X record every slot is a field this reader cannot name, so only
\* "bad" and "u_bad" mean anything there.
\*
\* The spec's condition set, each a function of (input, declaration) alone
\* (SpecRefuses below, written from §4's and §6.5's lists): wire damage in any
\* body of any record; a cycle; a record no pointer reaches; one blob record
\* named from two slots; and, in every record of a type this reader names,
\* reachable through a known slot or not, a declaration condition: range,
\* kind, map order, required, and a pointer in a declared slot whose target is
\* a NAMED type other than the slot's. A pointer whose target is of a type
\* this reader cannot name reads null (evolution, §3.1, §6.5).
\*
\* The reader is a nondeterministic order of checks: it scans the bodies in
\* any order, interleaved with a depth-first walk of the pointer graph whose
\* child order is free, and it may stop at the first condition it detects.
\* Caller resource limits (§6.5) are fixed constants and out of the verdict, so
\* none is modelled. It is a model, not a codec, and it does not prove any
\* implementation.

EXTENDS Naturals, FiniteSets, Sequences, TLC

CONSTANTS Records,   \* the node table's record indices, e.g. {1, 2}
          Contents   \* the content symbols in play for slot v

\* The fault switches. The positive cfg holds all FALSE; each reversed cfg
\* turns exactly one TRUE and checks TypeOK plus OneVerdict (or, for
\* EvolutionReached, the vacuity witness).
CONSTANTS PartialValue,         \* (a) a refusal hands back what it decoded
          EagerUnreachable,     \* (b) "unreachable" judged when the record is
                                \*     scanned, from the pointers seen so far
          KnownSlotsOnly,       \* (b) the graph walked from known slots of
                                \*     named records only (the cold read's bug)
          ClampOutOfRange,      \* (c) a key past the range is clamped and read
          UnknownTargetRefuses, \* a pointer to an unnamed type refuses
                                \*     instead of reading null
          MapOrderOnUnknown     \* map order judged on bytes in an unknown
                                \*     field

ASSUME Records \subseteq 1..3
ASSUME Contents \subseteq {"absent", "ok", "big", "desc", "wk", "bad", "u_desc", "u_bad"}
ASSUME "absent" \in Contents
ASSUME \A s \in {PartialValue, EagerUnreachable, KnownSlotsOnly, ClampOutOfRange,
                 UnknownTargetRefuses, MapOrderOnUnknown} : s \in BOOLEAN

Nodes   == {0} \cup Records
Targets == {0} \cup Records          \* 0 is the null pointer
Slots   == {"p", "q"}
Named   == {"A", "B", "Blob"}        \* the types this reader names

\* The type the declared slot p requires: A.p : *B, B.p : *bytes.
PRequires(t) == IF t = "A" THEN "B" ELSE "Blob"

BodyShape(types) == [type : types, p : Targets, q : Targets, v : Contents]
BlobRecord == [type |-> "Blob", p |-> 0, q |-> 0, v |-> "absent"]
RootShapes   == BodyShape({"A"})
RecordShapes == BodyShape({"A", "B", "X"}) \cup {BlobRecord}

\* Every per-node value is a record [v, p], so TLC compares like with like: a
\* decoded named record, a blob ("blob"), a record this reader cannot name
\* ("skip"), a node not yet decoded ("unset"), and the no-value a refusal
\* hands back ("none" at every node).
Rec(v, p)  == [v |-> v, p |-> p]
Unset      == Rec("unset", 0)
NoValue    == [n \in Nodes |-> Rec("none", 0)]
NodeValues == [v : {"ok", "dflt", "clamped", "blob", "skip"}, p : Targets]

HasFields(f, n) == f[n].type # "Blob"
Decoded(f, n)   == f[n].type \in {"A", "B"}   \* a named record with fields

\* -------------------------------------------------------------- the oracle
\* Written from the spec's lists, over the whole input, with no walk order.

WireDamage(f) == \E n \in Nodes : f[n].v \in {"bad", "u_bad"}

\* The pointer graph: every kind 17 payload, known to this reader or not.
Succ(f, n) == IF HasFields(f, n) THEN {f[n][s] : s \in Slots} \ {0} ELSE {}

RECURSIVE Closure(_, _, _)
Closure(f, S, k) ==
    IF k = 0 THEN S
    ELSE Closure(f, S \cup UNION {Succ(f, n) : n \in S}, k - 1)

Reach(f)       == Closure(f, {0}, Cardinality(Nodes))
ReachPlus(f, n) == Closure(f, Succ(f, n), Cardinality(Nodes))

Unreachable(f) == Records \ Reach(f) # {}
Cycle(f)       == \E n \in Reach(f) : n \in ReachPlus(f, n)

NamesOf(f, b) == Cardinality({ns \in Nodes \X Slots :
                                HasFields(f, ns[1]) /\ f[ns[1]][ns[2]] = b})
BlobShared(f) == \E b \in Records : f[b].type = "Blob" /\ NamesOf(f, b) >= 2

\* The declaration conditions, applied to EVERY record of a type this reader
\* names, reachable through a known slot or not (§6.5).
KnownAbsent(c) == c \in {"absent", "u_desc", "u_bad"}
DeclCondition(f, n) ==
    LET t == f[n].type
        c == f[n].v
        tgt == f[n].p
    IN \/ c \in {"big", "desc", "wk"}
       \/ (t = "B" /\ KnownAbsent(c))
       \/ (tgt # 0 /\ f[tgt].type \in Named /\ f[tgt].type # PRequires(t))

SpecRefuses(f) ==
    \/ WireDamage(f)
    \/ Unreachable(f)
    \/ Cycle(f)
    \/ BlobShared(f)
    \/ \E n \in Nodes : Decoded(f, n) /\ DeclCondition(f, n)

\* The one value: every named record decoded, unknown fields skipped, an
\* absent optional at its default, a pointer to an unnamed type null.
DecodedP(f, n) == IF f[n].p = 0 \/ f[n].type = "X" THEN 0
                  ELSE IF f[f[n].p].type = "X" THEN 0 ELSE f[n].p
SpecNodeValue(f, n) ==
    CASE Decoded(f, n)        -> [v |-> IF f[n].v = "ok" THEN "ok" ELSE "dflt",
                                  p |-> DecodedP(f, n)]
      [] f[n].type = "Blob"   -> Rec("blob", 0)
      [] OTHER                -> Rec("skip", 0)
SpecValue(f) == [n \in Nodes |-> SpecNodeValue(f, n)]

\* ------------------------------------------------------------- the reader

VARIABLES in,        \* the input, chosen once
          scanned,   \* the bodies whose checks have run
          names,     \* how many slots name each blob, from the scanned bodies
          reached,   \* nodes the walk has entered
          stack,     \* the walk's open descents, each a node and its unvisited slots
          walked,    \* the walk is over
          out,       \* what the reader has decoded so far
          verdict,   \* "running", "accepted" or "refused"
          value      \* what the read hands the caller

vars == <<in, scanned, names, reached, stack, walked, out, verdict, value>>


WalkSlots(f, n) ==
    IF ~HasFields(f, n) THEN {}
    ELSE IF KnownSlotsOnly THEN (IF f[n].type \in {"A", "B"} THEN {"p"} ELSE {})
    ELSE Slots

Init ==
    /\ \E r \in RootShapes, rs \in [Records -> RecordShapes] :
          in = [n \in Nodes |-> IF n = 0 THEN r ELSE rs[n]]
    /\ scanned = {}
    /\ names = [b \in Records |-> 0]
    /\ reached = {0}
    /\ stack = << [n |-> 0, rest |-> WalkSlots(in, 0)] >>
    /\ walked = FALSE
    /\ out = [n \in Nodes |-> Unset]
    /\ verdict = "running"
    /\ value = NoValue

\* A refusal stops the read. Nothing decoded is handed back, unless the
\* PartialValue fault hands back what was decoded so far.
Refuse ==
    /\ verdict' = "refused"
    /\ value' = IF PartialValue THEN out ELSE NoValue

\* What scanning body n decodes, when it detects nothing.
ScanValue(n) ==
    LET s == in[n] IN
    CASE Decoded(in, n) ->
           [v |-> CASE s.v = "ok"  -> "ok"
                    [] s.v = "big" -> "clamped"   \* reached only under ClampOutOfRange
                    [] OTHER       -> "dflt",
            p |-> DecodedP(in, n)]
      [] s.type = "Blob" -> Rec("blob", 0)
      [] OTHER -> Rec("skip", 0)

\* One body's checks, in one step: its wire conditions, its declaration
\* conditions when its type is named, and the blob names its slots carry.
\* Which condition it meets first does not matter: any one refuses.
Scan(n) ==
    LET s == in[n]
        named2 == [b \in Records |->
                     names[b] + Cardinality({sl \in Slots :
                         HasFields(in, n) /\ s[sl] = b /\ in[b].type = "Blob"})]
        wire == \/ s.v \in {"bad", "u_bad"}
                \/ (MapOrderOnUnknown /\
                      (s.v = "u_desc" \/ (s.type = "X" /\ s.v = "desc")))
                \/ \E b \in Records : named2[b] >= 2
        decl == /\ Decoded(in, n)
                /\ \/ s.v \in (IF ClampOutOfRange THEN {"desc", "wk"}
                               ELSE {"big", "desc", "wk"})
                   \/ (s.type = "B" /\ KnownAbsent(s.v))
                   \/ (s.p # 0 /\ in[s.p].type \in Named /\
                         in[s.p].type # PRequires(s.type))
                   \/ (UnknownTargetRefuses /\ s.p # 0 /\ in[s.p].type = "X")
        eager == EagerUnreachable /\ n \in Records /\ n \notin reached
    IN /\ verdict = "running"
       /\ n \notin scanned
       /\ IF wire \/ decl \/ eager
          THEN /\ Refuse
               /\ UNCHANGED <<in, scanned, names, reached, stack, walked, out>>
          ELSE /\ scanned' = scanned \cup {n}
               /\ names' = named2
               /\ out' = [out EXCEPT ![n] = ScanValue(n)]
               /\ UNCHANGED <<in, reached, stack, walked, verdict, value>>

OnStack == {stack[i].n : i \in 1..Len(stack)}

\* One step of the depth-first walk from the root: take any unvisited slot of
\* the innermost open descent (the child order is free), or close it.
Walk ==
    /\ verdict = "running"
    /\ ~walked
    /\ LET top == stack[Len(stack)] IN
       IF top.rest = {}
       THEN /\ stack' = SubSeq(stack, 1, Len(stack) - 1)
            /\ walked' = (Len(stack) = 1)
            /\ UNCHANGED <<in, scanned, names, reached, out, verdict, value>>
       ELSE \E sl \in top.rest :
              LET t == in[top.n][sl]
                  popped == [stack EXCEPT ![Len(stack)].rest = top.rest \ {sl}]
              IN IF t # 0 /\ t \in OnStack
                 THEN \* a pointer naming a node whose descent is open: a cycle
                      /\ Refuse
                      /\ UNCHANGED <<in, scanned, names, reached, stack, walked, out>>
                 ELSE IF t = 0 \/ t \in reached
                 THEN /\ stack' = popped
                      /\ UNCHANGED <<in, scanned, names, reached, walked, out, verdict, value>>
                 ELSE /\ stack' = Append(popped, [n |-> t, rest |-> WalkSlots(in, t)])
                      /\ reached' = reached \cup {t}
                      /\ UNCHANGED <<in, scanned, names, walked, out, verdict, value>>

\* The walk is over and a record was never entered: unreachable.
Unreached ==
    /\ verdict = "running"
    /\ walked
    /\ Records \ reached # {}
    /\ Refuse
    /\ UNCHANGED <<in, scanned, names, reached, stack, walked, out>>

\* Every check has run and none refused.
Accept ==
    /\ verdict = "running"
    /\ walked
    /\ Records \subseteq reached
    /\ scanned = Nodes
    /\ verdict' = "accepted"
    /\ value' = out
    /\ UNCHANGED <<in, scanned, names, reached, stack, walked, out>>

Finished == verdict # "running" /\ UNCHANGED vars

Next ==
    \/ \E n \in Nodes : Scan(n)
    \/ Walk
    \/ Unreached
    \/ Accept
    \/ Finished

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- properties

TypeOK ==
    /\ in \in [Nodes -> RootShapes \cup RecordShapes]
    /\ scanned \subseteq Nodes
    /\ names \in [Records -> 0..(2 * Cardinality(Nodes))]
    /\ reached \subseteq Nodes
    /\ walked \in BOOLEAN
    /\ out \in [Nodes -> NodeValues \cup {Unset}]
    /\ verdict \in {"running", "accepted", "refused"}
    /\ value \in [Nodes -> NodeValues \cup {Unset, Rec("none", 0)}]

\* THE CONTRACT. Every run that ends, in whatever order it checked and wherever
\* it stopped, ends in the verdict the condition set gives: refused exactly
\* when one condition holds, with no value; accepted otherwise, with the one
\* value. All runs therefore agree with each other.
OneVerdict ==
    verdict # "running" =>
        /\ (verdict = "refused") = SpecRefuses(in)
        /\ verdict = "refused" => value = NoValue
        /\ verdict = "accepted" => value = SpecValue(in)

\* A vacuity witness, not a rule: an input that carries evolution (an unknown
\* field, or a declared pointer to a type this reader cannot name) and is
\* accepted. MCReadOrderEvolutionReached.cfg checks it and must FAIL, which
\* shows the positive model accepts evolution rather than refusing it.
CarriesEvolution(f) ==
    \/ \E n \in Nodes : HasFields(f, n) /\ f[n].q # 0
    \/ \E n \in Nodes : f[n].v \in {"u_desc"}
    \/ \E n \in Nodes : Decoded(f, n) /\ f[n].p # 0 /\ f[f[n].p].type = "X"
NoEvolutionAccepted == ~(verdict = "accepted" /\ CarriesEvolution(in))

==============================================================================
