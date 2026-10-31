------------------------------ MODULE Framing --------------------------------
\* A LAYOUT model of the table wire's record scan (docs/LAYERS/1-framing.md;
\* docs/SPEC-TABLES.md §3, §4, §2.9): one parent body of N bytes walked by a
\* cursor, one length-framed field at a time. It carries the arithmetic the
\* readers carry and an abstract model cannot see: the field's start, its end
\* `start + L`, the header that must lie inside [start, end), and the resume
\* point, which is `end`. It also carries the one identity fact framing owns:
\* each field's reference, and the set this body has already met.
\*
\* INPUT NO WRITER PRODUCES IS DAMAGE (docs/SPEC-TABLES.md §4; Glenn, #391,
\* 2026-09-03: "Table reads are not trusted, they will come over the network.
\* Table reads are exactly equivalent to type reads in security posture."): a
\* body too short for its header, a header that runs past L, and a reference
\* the body already carried are each damage. DAMAGE DISCARDS THE WHOLE LOAD
\* (§4): the scan ends at the first damage and nothing resumes. It is a model,
\* not a codec: the kind byte and the element values are folded away, and only
\* offsets and references remain.
\*
\* The byte values a reader would meet are chosen nondeterministically: every
\* L in 0..MaxL for every field, every header width in HeaderBytes (the element
\* kind byte plus a count of one or more LEB128 bytes) and every reference in
\* Refs. TLC therefore walks every placement of a header against every L and
\* every buffer end, and every order of references.

EXTENDS Naturals, TLC

CONSTANTS N,           \* the parent body's length in bytes
          MaxL,        \* the largest L a field may claim
          HeaderBytes, \* the header widths an array body may carry
          Refs         \* the references a field may carry
ASSUME N \in Nat /\ MaxL \in Nat /\ HeaderBytes \subseteq Nat /\ Refs # {}

\* The fault switches, each a reversed witness of one rule. The positive cfg
\* holds all of them FALSE; each reversed cfg turns exactly one TRUE and checks
\* TypeOK plus the invariant that names the rule.
CONSTANTS HeaderAgainstParent, \* the header read against the parent buffer,
                               \* so a count spelled past L is completed from
                               \* the bytes after it (the retired reading)
          ShortBodyInert,      \* a body of fewer than two bytes stepped over
                               \* as inert (the retired reading)
          RepeatLastWins,      \* a reference the body already carried is
                               \* accepted (the retired last-wins reading)
          OffByOneAtL,         \* a payload ending one byte past the buffer
                               \* is accepted
          ResumeAfterDamage    \* damage inside a field is resumed past L, the
                               \* partial read the ruling of 2026-10-31 removed
ASSUME HeaderAgainstParent \in BOOLEAN
ASSUME ShortBodyInert \in BOOLEAN
ASSUME RepeatLastWins \in BOOLEAN
ASSUME OffByOneAtL \in BOOLEAN
ASSUME ResumeAfterDamage \in BOOLEAN

VARIABLES off,        \* the parent's cursor: the next byte the scan reads
          done,       \* the scan ended, at the terminator or at damage
          malformed,  \* the report's flag
          fieldEnd,   \* the end the last field was framed by: start + L
          cursor,     \* the furthest byte the last field's read reached
          resumed,    \* where the parent resumed after the last field
          accShort,   \* the last ACCEPTED field was shorter than its header
          accCross,   \* the last ACCEPTED field's header ran past its L
          seen,       \* the references this body has carried
          repeated    \* a field was ACCEPTED under a reference already seen

vars == <<off, done, malformed, fieldEnd, cursor, resumed, accShort, accCross, seen, repeated>>

Init ==
    /\ off = 0
    /\ done = FALSE
    /\ malformed = FALSE
    /\ fieldEnd = 0
    /\ cursor = 0
    /\ resumed = 0
    /\ accShort = FALSE
    /\ accCross = FALSE
    /\ seen = {}
    /\ repeated = FALSE

\* The zero reference at off ends the body (F3, R1).
Terminator ==
    /\ ~done
    /\ off < N
    /\ done' = TRUE
    /\ UNCHANGED <<off, malformed, fieldEnd, cursor, resumed, accShort, accCross, seen, repeated>>

\* No byte left for a reference: the body has no terminator, which is damage
\* (L2).
Truncated ==
    /\ ~done
    /\ off >= N
    /\ done' = TRUE
    /\ malformed' = TRUE
    /\ UNCHANGED <<off, fieldEnd, cursor, resumed, accShort, accCross, seen, repeated>>

\* The payload fits when its end is inside the buffer (L4). The reversed
\* witness lets it end one byte past.
LengthFits(end) == IF OffByOneAtL THEN end <= N + 1 ELSE end <= N

\* Damage at a field: the load ends there (§4), or, under the reversed witness,
\* resumes past L.
Damage(end, reach) ==
    /\ malformed' = TRUE
    /\ fieldEnd' = end
    /\ cursor' = reach
    /\ UNCHANGED <<accShort, accCross, seen, repeated>>
    /\ IF ResumeAfterDamage
       THEN /\ off' = end
            /\ resumed' = end
            /\ UNCHANGED done
       ELSE /\ done' = TRUE
            /\ UNCHANGED <<off, resumed>>

\* One length-framed field whose payload is an array body: the reference R
\* and the kind are folded into the byte at off, so start is off + 1 and the
\* payload is [start, end). H is the header's width, the element kind byte and
\* the count's bytes.
Field(R, L, H) ==
    /\ ~done
    /\ off < N
    /\ LET start == off + 1
           end == start + L
           hend == start + H
           bound == IF HeaderAgainstParent THEN N ELSE end
       IN IF ~LengthFits(end)
          THEN \* L4: the length runs past the body carrying it
               Damage(end, start)
          ELSE IF R \in seen /\ ~RepeatLastWins
          THEN \* R5: one id twice in one body (D3)
               Damage(end, start)
          ELSE IF L < 2 /\ ~ShortBodyInert
          THEN \* H2: too short to carry the header (D3)
               Damage(end, start)
          ELSE IF L >= 2 /\ hend > bound
          THEN \* H3: the header runs past its bound (D3)
               Damage(end, IF hend > N THEN N ELSE hend)
          ELSE \* accepted: the elements bounded by L, the parent resumes at
               \* end (L1)
               LET reach == IF L < 2 THEN start ELSE IF hend > end THEN hend ELSE end
               IN /\ off' = end
                  /\ resumed' = end
                  /\ fieldEnd' = end
                  /\ cursor' = reach
                  /\ accShort' = (L < 2)
                  /\ accCross' = (L >= 2 /\ hend > end)
                  /\ repeated' = (repeated \/ R \in seen)
                  /\ seen' = seen \cup {R}
                  /\ UNCHANGED <<done, malformed>>

Finished == done /\ UNCHANGED vars

Next ==
    \/ Terminator
    \/ Truncated
    \/ \E R \in Refs, L \in 0..MaxL, H \in HeaderBytes : Field(R, L, H)
    \/ Finished

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- properties

TypeOK ==
    /\ off \in 0..(N + MaxL + 1)
    /\ done \in BOOLEAN
    /\ malformed \in BOOLEAN
    /\ fieldEnd \in 0..(N + MaxL + 1)
    /\ cursor \in 0..(N + MaxL + 1)
    /\ resumed \in 0..(N + MaxL + 1)
    /\ accShort \in BOOLEAN
    /\ accCross \in BOOLEAN
    /\ seen \subseteq Refs
    /\ repeated \in BOOLEAN

\* No field's read reaches past the buffer (L4, H5): a neighbour's bytes are
\* never read as elements, and a payload past the end is damage, not data.
NoReadPastBuffer == cursor <= N

\* The parent resumes exactly at end after every field it did not stop on (L1).
ResumeAtEnd == ~done => resumed = fieldEnd

\* DAMAGE DISCARDS THE WHOLE LOAD (§4): once the flag is set the scan is over,
\* so no field is read after damage.
DamageEndsLoad == malformed => done

\* THE HEADER LIES INSIDE THE BODY (H2, H3): no accepted field is shorter than
\* its header, and no accepted field's header runs past its L.
HeaderInsideBody == ~accShort /\ ~accCross

\* ONE ID RIDES ONCE PER BODY (R5): no field is accepted under a reference the
\* body already carried.
OneIdOncePerBody == ~repeated

==============================================================================
