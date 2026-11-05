------------------------------ MODULE FixedLive ------------------------------
\* A LAYOUT model of the fixed form's LOAD check loop over LIVE data
\* (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.3 steps 11 and 12;
\* docs/SPEC-TABLES.md §3.4, THE SLACK RULE, and §6.5). One record image of
\* the writer's layout, laid out byte by byte as the plan sees it:
\*
\*   offset 0          the count of a counted array of bools (its SLE word,
\*                     abstracted to the signed value it spells)
\*   offsets 1..B      the array's B element slots, B the PLAN's bound: slots
\*                     at and past the count are SLACK
\*   offset B+1        t1, the outer union's tag (the writer's arms 1 and 2)
\*   offset B+2        u0, the arms' shared storage: under arm 1 the inner
\*                     union's tag t2 (arms 1 and 2), under arm 2 a bool c
\*   offset B+3        u1: under arm 1 / inner arm 1 a bool b; slack otherwise
\*
\* The check loop's entries carry the arithmetic the readers carry: element i
\* at offset 1 + i, live while i < min(count, bound); b under the GUARD CHAIN
\* <<t1 = 1, u0 = 1>>, outermost first; c under <<t1 = 2>>. A bool byte other
\* than 0 or 1 in live data refuses (wire_damaged); a count outside 0..B
\* refuses (count_over_bound); a tag past the writer's arm count is held by the
\* bounds pass after landing (out_of_range), and the landing is reset. Slack is
\* never read and never refused. The checks run in any order and the first
\* condition met refuses (§5.3 step 11).
\*
\* The ORACLE is written from the record's MEANING, not from offsets: the
\* first `count` elements, the arm the tags select, and the payloads under it.
\* The invariant holds the offset walk to it. It is a model, not a codec.

EXTENDS Naturals, Integers, Sequences, FiniteSets, TLC

CONSTANT B          \* the plan's bound on the counted array: 3 or 4
ASSUME B \in 1..6

\* The fault switches. The positive cfg holds all FALSE; each reversed cfg
\* turns exactly one TRUE and checks TypeOK plus its named invariant.
CONSTANTS CheckSlack,      \* element liveness up to the bound, not the count:
                           \* the slack is read and garbage there refuses
          BoolNormalize,   \* the retired bool normalization: a live byte of 2
                           \* lands as true instead of refusing
          InnerGuardOnly,  \* b's liveness from its innermost link alone
                           \* (u0 = 1), the one-lane guard
          LoopToCount,     \* element liveness up to the count, before the
                           \* count is known to be in bound
          LandDuringCheck  \* values land as they are checked, so a refusal
                           \* later in the loop leaves them landed
ASSUME \A s \in {CheckSlack, BoolNormalize, InnerGuardOnly, LoopToCount,
                 LandDuringCheck} : s \in BOOLEAN

Byte   == 0..2            \* 2 stands for every byte that is not a bool
TagB   == 0..3            \* 3 stands for every tag past the writer's arms
Counts == -1..(B + 1)     \* -1 for every negative count, B+1 for every count past the bound

OffCount == 0
OffT1    == B + 1
OffU0    == B + 2
OffU1    == B + 3
Offsets  == 0..(B + 3)

Records == [count : Counts, el : [0..(B - 1) -> Byte], t1 : TagB, u0 : TagB, u1 : Byte]

\* The byte at an offset, as the plan addresses the record image.
At(r, o) == CASE o = OffT1 -> r.t1
              [] o = OffU0 -> r.u0
              [] o = OffU1 -> r.u1
              [] o \in 1..B -> r.el[o - 1]
              [] OTHER -> 0
IsBool(x) == x \in {0, 1}
Min(a, b) == IF a < b THEN a ELSE b

\* ------------------------------------------------------------- the oracle

OracleRefuses(r) ==
    \/ r.count < 0 \/ r.count > B
    \/ \E i \in 0..(B - 1) : i < r.count /\ ~IsBool(r.el[i])
    \/ r.t1 > 2
    \/ r.t1 = 1 /\ r.u0 > 2
    \/ r.t1 = 1 /\ r.u0 = 1 /\ ~IsBool(r.u1)
    \/ r.t1 = 2 /\ ~IsBool(r.u0)

OracleArm(r) ==
    CASE r.t1 = 0                -> <<0>>
      [] r.t1 = 1 /\ r.u0 = 1    -> <<1, 1, r.u1>>
      [] r.t1 = 1                -> <<1, r.u0>>
      [] OTHER                   -> <<2, r.u0>>

OracleValue(r) == [els |-> [i \in 1..r.count |-> r.el[i - 1]],
                   arm |-> OracleArm(r)]

NoValue == [els |-> <<>>, arm |-> <<>>]

\* ------------------------------------------------------------- the reader

\* The check loop's entries: the count, one per element slot (one more under
\* LoopToCount, whose index can reach past the array), and the two arm bools.
ElemTasks == IF LoopToCount THEN 0..B ELSE 0..(B - 1)
Tasks == {<<"count", 0>>, <<"b", 0>>, <<"c", 0>>} \cup {<<"el", i>> : i \in ElemTasks}

\* LIVE (§5.3), and the faults that misjudge it.
ElemLive(r, i) ==
    IF CheckSlack THEN i < B
    ELSE IF LoopToCount THEN i < r.count
    ELSE i < Min(r.count, B)
Chain(r, links) == \A k \in 1..Len(links) : At(r, links[k][1]) = links[k][2]
BChain == IF InnerGuardOnly THEN << <<OffU0, 1>> >> ELSE << <<OffT1, 1>>, <<OffU0, 1>> >>
CChain == << <<OffT1, 2>> >>

Lands(x) == IF BoolNormalize /\ x > 1 THEN 1 ELSE x
Damaged(x) == ~IsBool(x) /\ ~BoolNormalize

VARIABLES rec,      \* the record, chosen once
          done,     \* the check entries that have run
          phase,    \* "check", "land", "accepted", "refused"
          out,      \* the caller's destination
          readOff   \* every offset an element entry has read

vars == <<rec, done, phase, out, readOff>>

Init ==
    /\ rec \in Records
    /\ done = {}
    /\ phase = "check"
    /\ out = NoValue
    /\ readOff = {}

RefuseNow ==
    /\ phase' = "refused"
    /\ out' = IF LandDuringCheck THEN out ELSE NoValue

CheckCount ==
    /\ phase = "check"
    /\ <<"count", 0>> \notin done
    /\ IF rec.count < 0 \/ rec.count > B
       THEN RefuseNow /\ UNCHANGED <<rec, done, readOff>>
       ELSE /\ done' = done \cup {<<"count", 0>>}
            /\ UNCHANGED <<rec, phase, out, readOff>>

CheckElem(i) ==
    /\ phase = "check"
    /\ <<"el", i>> \notin done
    /\ IF ~ElemLive(rec, i)
       THEN /\ done' = done \cup {<<"el", i>>}
            /\ UNCHANGED <<rec, phase, out, readOff>>
       ELSE LET x == At(rec, 1 + i) IN
            /\ readOff' = readOff \cup {1 + i}
            /\ IF Damaged(x)
               THEN RefuseNow /\ UNCHANGED <<rec, done>>
               ELSE /\ done' = done \cup {<<"el", i>>}
                    /\ out' = IF LandDuringCheck
                              THEN [out EXCEPT !.els = Append(out.els, Lands(x))]
                              ELSE out
                    /\ UNCHANGED <<rec, phase>>

CheckArm(t, links, o) ==
    /\ phase = "check"
    /\ t \notin done
    /\ IF Chain(rec, links) /\ Damaged(At(rec, o))
       THEN RefuseNow /\ UNCHANGED <<rec, done, readOff>>
       ELSE /\ done' = done \cup {t}
            /\ UNCHANGED <<rec, phase, out, readOff>>

\* Every check passed: land the record by the plan, then the bounds pass.
LandValue(r) ==
    [els |-> [i \in 1..Min(r.count, B) |-> Lands(r.el[i - 1])],
     arm |-> CASE r.t1 = 0             -> <<0>>
               [] r.t1 = 1 /\ r.u0 = 1 -> <<1, 1, Lands(r.u1)>>
               [] r.t1 = 1             -> <<1, r.u0>>
               [] OTHER                -> <<2, Lands(r.u0)>>]

Land ==
    /\ phase = "check"
    /\ done = Tasks
    /\ phase' = "land"
    /\ out' = LandValue(rec)
    /\ UNCHANGED <<rec, done, readOff>>

\* The bounds pass over storage (§4.6): a tag past the writer's arm count
\* refuses out_of_range and the landing is reset.
Bounds ==
    /\ phase = "land"
    /\ IF rec.t1 > 2 \/ (rec.t1 = 1 /\ rec.u0 > 2)
       THEN /\ phase' = "refused"
            /\ out' = NoValue
       ELSE /\ phase' = "accepted"
            /\ UNCHANGED out
    /\ UNCHANGED <<rec, done, readOff>>

Finished == phase \in {"accepted", "refused"} /\ UNCHANGED vars

Next ==
    \/ CheckCount
    \/ \E i \in ElemTasks : CheckElem(i)
    \/ CheckArm(<<"b", 0>>, BChain, OffU1)
    \/ CheckArm(<<"c", 0>>, CChain, OffU0)
    \/ Land
    \/ Bounds
    \/ Finished

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- properties

TypeOK ==
    /\ rec \in Records
    /\ done \subseteq Tasks
    /\ phase \in {"check", "land", "accepted", "refused"}
    /\ readOff \subseteq Offsets

\* THE CONTRACT on the fixed form: whatever order the check loop ran in and
\* wherever it stopped, the read refuses exactly the records the meaning
\* refuses, with nothing landed, and otherwise lands exactly the meaning.
OneVerdict ==
    phase \in {"accepted", "refused"} =>
        /\ (phase = "refused") = OracleRefuses(rec)
        /\ phase = "refused" => out = NoValue
        /\ phase = "accepted" => out = OracleValue(rec)

\* No element entry reads outside the array's own B slots (offsets 1..B).
InsideArray == readOff \subseteq 1..B

\* No element entry reads slack: every offset read is a live element.
NoSlackRead == \A o \in readOff : o - 1 < rec.count

==============================================================================
