------------------------------ MODULE FixedLive ------------------------------
\* A LAYOUT model of the fixed form's LOAD check loop over LIVE data and SLACK
\* (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §4.6, §5.3 steps 11 and 12;
\* docs/SPEC-TABLES.md §3.4, THE SLACK RULE, and §6.5; docs/rulings.json
\* fixed-dead-bytes-zero). One record image of the writer's layout, laid out
\* byte by byte as the plan sees it:
\*
\*   offset 0          the count of a counted array of bools (its SLE word,
\*                     abstracted to the signed value it spells)
\*   offsets 1..B      the array's B element slots, B the PLAN's bound: slots
\*                     at and past the count are SLACK
\*   offset B+1        t1, the outer union's tag (the writer's arms 1 and 2)
\*   offset B+2        u0, the arms' shared storage: under arm 1 the inner
\*                     union's tag t2 (arm 1 a bool b, arm 2 payload-free),
\*                     under arm 2 a bool c
\*   offset B+3        u1: under arm 1 / inner arm 1 the bool b; SLACK
\*                     otherwise (behind arm 2, behind inner arm 2, under an
\*                     inner tag 0, under an outer tag 0)
\*
\* Under an outer tag 0 every arm byte (u0 and u1) is slack. The outer arms are
\* two bytes wide at the widest (arm 1: the inner tag and its one-byte arm), so
\* arm 2's bool leaves one byte of slack behind it; the inner arms are one byte
\* wide at the widest, so inner arm 2 and inner tag 0 leave u1 as slack.
\*
\* The check loop's entries carry the arithmetic the readers carry: element i
\* at offset 1 + i, live while i < min(count, bound); b under the GUARD CHAIN
\* <<t1 = 1, u0 = 1>>, outermost first; c under <<t1 = 2>>. A bool byte other
\* than 0 or 1 in live data refuses (wire_damaged); a count outside 0..B
\* refuses (count_over_bound); a tag past the writer's arm count is held by the
\* bounds pass after landing (out_of_range), and the landing is reset.
\*
\* EVERY DEAD BYTE IS ZERO (the owner, 2026-11-10: "Every dead byte", "Be
\* strict"): a byte of slack other than zero refuses the whole record
\* (wire_damaged), and slack is held to zero and to NOTHING ELSE: never a bool
\* rule. The count entry, once the count is in bound, holds the tail
\* 1 + count .. B to zero; each union carries one DEAD-ZERO entry per tag value
\* the writer can write (0 included), under that tag's own guard chain, over the
\* bytes from the end of that arm to the widest. The checks run in any order and
\* the first condition met refuses (§5.3 step 11).
\*
\* The ORACLE is written from the record's MEANING, not from offsets: the
\* first `count` elements, the arm the tags select, the payloads under it, and
\* every byte nothing taken covers. The invariants hold the offset walk to it.
\* It is a model, not a codec.

EXTENDS Naturals, Integers, Sequences, FiniteSets, TLC

CONSTANT B          \* the plan's bound on the counted array: 3 or 4
ASSUME B \in 1..6

\* The fault switches. The positive cfg holds all FALSE; each reversed cfg
\* turns exactly one TRUE and checks TypeOK plus its named invariant.
CONSTANTS CheckSlack,      \* element liveness up to the bound, not the count,
                           \* and no zero check on the tail: the slack is
                           \* asked the BOOL rule instead of the zero rule
          BoolNormalize,   \* the retired bool normalization: a live byte of 2
                           \* lands as true instead of refusing
          InnerGuardOnly,  \* b's liveness from its innermost link alone
                           \* (u0 = 1), the one-lane guard
          LoopToCount,     \* element liveness up to the count, before the
                           \* count is known to be in bound
          LandDuringCheck, \* values land as they are checked, so a refusal
                           \* later in the loop leaves them landed
          SkipDeadZero,    \* the retired rule: slack unspecified on read and
                           \* never a refusal, so no dead-zero check runs
          ArmWidthZero     \* the dead-zero entry behind outer arm 2 laid as if
                           \* the arm took no bytes (a plan without its widths)
ASSUME \A s \in {CheckSlack, BoolNormalize, InnerGuardOnly, LoopToCount,
                 LandDuringCheck, SkipDeadZero, ArmWidthZero} : s \in BOOLEAN

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

\* THE SLACK, by the meaning (§3.4): the element slots past the count, and the
\* union bytes no taken arm covers. Under a forged tag the record refuses
\* whatever this says.
Slack(r) ==
    {1 + i : i \in {j \in 0..(B - 1) : j >= r.count}}
    \cup (CASE r.t1 = 0                         -> {OffU0, OffU1}
            [] r.t1 = 1 /\ r.u0 \in {0, 2}      -> {OffU1}
            [] r.t1 = 2                         -> {OffU1}
            [] OTHER                            -> {})

OracleRefuses(r) ==
    \/ r.count < 0 \/ r.count > B
    \/ \E i \in 0..(B - 1) : i < r.count /\ ~IsBool(r.el[i])
    \/ r.t1 > 2
    \/ r.t1 = 1 /\ r.u0 > 2
    \/ r.t1 = 1 /\ r.u0 = 1 /\ ~IsBool(r.u1)
    \/ r.t1 = 2 /\ ~IsBool(r.u0)
    \/ \E o \in Slack(r) : At(r, o) # 0

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
\* LoopToCount, whose index can reach past the array), the two arm bools, and
\* each union's dead-zero entries, one task per union (only the entry whose
\* guard chain holds can run, so one task stands for them all).
ElemTasks == IF LoopToCount THEN 0..B ELSE 0..(B - 1)
ZeroTasks == {<<"zo", 0>>, <<"zi", 0>>}
Tasks == {<<"count", 0>>, <<"b", 0>>, <<"c", 0>>} \cup ZeroTasks
         \cup {<<"el", i>> : i \in ElemTasks}

\* LIVE (§5.3), and the faults that misjudge it.
ElemLive(r, i) ==
    IF CheckSlack THEN i < B
    ELSE IF LoopToCount THEN i < r.count
    ELSE i < Min(r.count, B)
Chain(r, links) == \A k \in 1..Len(links) : At(r, links[k][1]) = links[k][2]
BChain == IF InnerGuardOnly THEN << <<OffU0, 1>> >> ELSE << <<OffT1, 1>>, <<OffU0, 1>> >>
CChain == << <<OffT1, 2>> >>

\* THE DEAD-ZERO SPANS, as the plan lays them: the count entry's tail, the
\* element slots from the count to the bound; a union's entry under tag value
\* t, the bytes from armAt + taken(t) to armAt + widest.
Span(from, to) == {o \in Offsets : from <= o /\ o < to}
TailSpan(r) == Span(1 + r.count, 1 + B)
OuterTaken(t) == CASE t = 1 -> 2
                   [] t = 2 -> IF ArmWidthZero THEN 0 ELSE 1
                   [] OTHER -> 0
InnerTaken(t) == IF t = 1 THEN 1 ELSE 0
OuterZero == { << << <<OffT1, t>> >>, Span(OffU0 + OuterTaken(t), OffU0 + 2) >> : t \in 0..2 }
InnerZero == { << << <<OffT1, 1>>, <<OffU0, t>> >>, Span(OffU1 + InnerTaken(t), OffU1 + 1) >> : t \in 0..2 }
\* the bytes the entries of one union whose chain holds hold to zero
ZeroRead(r, entries) == UNION {e[2] : e \in {x \in entries : Chain(r, x[1])}}

Lands(x) == IF BoolNormalize /\ x > 1 THEN 1 ELSE x
Damaged(x) == ~IsBool(x) /\ ~BoolNormalize
Stained(r, span) == \E o \in span : At(r, o) # 0

VARIABLES rec,      \* the record, chosen once
          done,     \* the check entries that have run
          phase,    \* "check", "land", "accepted", "refused"
          out,      \* the caller's destination
          readOff,  \* every offset an element entry has read
          zeroOff,  \* every offset a dead-zero check has read
          armRead   \* every arm entry that read its byte under its chain

vars == <<rec, done, phase, out, readOff, zeroOff, armRead>>

Init ==
    /\ rec \in Records
    /\ done = {}
    /\ phase = "check"
    /\ out = NoValue
    /\ readOff = {}
    /\ zeroOff = {}
    /\ armRead = {}

RefuseNow ==
    /\ phase' = "refused"
    /\ out' = IF LandDuringCheck THEN out ELSE NoValue

\* The count, then (in bound) its tail held to zero, as the pre-scan's count
\* entry does.
CheckCount ==
    /\ phase = "check"
    /\ <<"count", 0>> \notin done
    /\ UNCHANGED armRead
    /\ IF rec.count < 0 \/ rec.count > B
       THEN RefuseNow /\ UNCHANGED <<rec, done, readOff, zeroOff>>
       ELSE IF SkipDeadZero \/ CheckSlack
       THEN /\ done' = done \cup {<<"count", 0>>}
            /\ UNCHANGED <<rec, phase, out, readOff, zeroOff>>
       ELSE /\ zeroOff' = zeroOff \cup TailSpan(rec)
            /\ IF Stained(rec, TailSpan(rec))
               THEN RefuseNow /\ UNCHANGED <<rec, done, readOff>>
               ELSE /\ done' = done \cup {<<"count", 0>>}
                    /\ UNCHANGED <<rec, phase, out, readOff>>

CheckElem(i) ==
    /\ phase = "check"
    /\ <<"el", i>> \notin done
    /\ UNCHANGED <<zeroOff, armRead>>
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
    /\ UNCHANGED <<readOff, zeroOff>>
    /\ armRead' = IF Chain(rec, links) THEN armRead \cup {t} ELSE armRead
    /\ IF Chain(rec, links) /\ Damaged(At(rec, o))
       THEN RefuseNow /\ UNCHANGED <<rec, done>>
       ELSE /\ done' = done \cup {t}
            /\ UNCHANGED <<rec, phase, out>>

\* One union's dead-zero entries: the one whose chain holds reads its span.
CheckZero(t, entries) ==
    /\ phase = "check"
    /\ t \notin done
    /\ UNCHANGED <<readOff, armRead>>
    /\ IF SkipDeadZero
       THEN /\ done' = done \cup {t}
            /\ UNCHANGED <<rec, phase, out, zeroOff>>
       ELSE /\ zeroOff' = zeroOff \cup ZeroRead(rec, entries)
            /\ IF Stained(rec, ZeroRead(rec, entries))
               THEN RefuseNow /\ UNCHANGED <<rec, done>>
               ELSE /\ done' = done \cup {t}
                    /\ UNCHANGED <<rec, phase, out>>

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
    /\ UNCHANGED <<rec, done, readOff, zeroOff, armRead>>

\* The bounds pass over storage (§4.6): a tag past the writer's arm count
\* refuses out_of_range and the landing is reset.
Bounds ==
    /\ phase = "land"
    /\ IF rec.t1 > 2 \/ (rec.t1 = 1 /\ rec.u0 > 2)
       THEN /\ phase' = "refused"
            /\ out' = NoValue
       ELSE /\ phase' = "accepted"
            /\ UNCHANGED out
    /\ UNCHANGED <<rec, done, readOff, zeroOff, armRead>>

Finished == phase \in {"accepted", "refused"} /\ UNCHANGED vars

Next ==
    \/ CheckCount
    \/ \E i \in ElemTasks : CheckElem(i)
    \/ CheckArm(<<"b", 0>>, BChain, OffU1)
    \/ CheckArm(<<"c", 0>>, CChain, OffU0)
    \/ CheckZero(<<"zo", 0>>, OuterZero)
    \/ CheckZero(<<"zi", 0>>, InnerZero)
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
    /\ zeroOff \subseteq Offsets
    /\ armRead \subseteq {<<"b", 0>>, <<"c", 0>>}

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

\* A dead-zero check reads slack and nothing else: never a live byte, so a
\* live value is never asked the zero rule.
ZeroReadsSlack == zeroOff \subseteq Slack(rec)

\* An arm entry reads its byte only under the arm the meaning selects.
ArmReadsLive ==
    /\ <<"b", 0>> \in armRead => rec.t1 = 1 /\ rec.u0 = 1
    /\ <<"c", 0>> \in armRead => rec.t1 = 2

==============================================================================
