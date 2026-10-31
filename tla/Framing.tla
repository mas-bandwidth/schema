------------------------------ MODULE Framing --------------------------------
\* A LAYOUT model of the table wire's record scan (docs/LAYERS/1-framing.md;
\* docs/SPEC-TABLES.md §3, §4, §2.9): one parent body of N bytes walked by a
\* cursor, one length-framed field at a time. It carries the arithmetic the
\* readers carry and an abstract model cannot see: the field's start, its end
\* `start + L`, the header read AGAINST THE PARENT buffer (N) and not against
\* the field's own end, the element span clamped at zero when the header
\* crossed L, and the resume point, which is `end` whatever the payload turned
\* out to be. It is a model, not a codec: the reference, the kind byte and the
\* element values are folded away, and only offsets remain.
\*
\* The byte values a reader would meet are chosen nondeterministically: every
\* L in 0..MaxL for every field, and every header width in HeaderBytes (the
\* element kind byte plus a count of one or more LEB128 bytes). TLC therefore
\* walks every placement of a header against every L and every buffer end.

EXTENDS Naturals, TLC

CONSTANTS N,           \* the parent body's length in bytes
          MaxL,        \* the largest L a field may claim
          HeaderBytes  \* the header widths an array body may carry
ASSUME N \in Nat /\ MaxL \in Nat /\ HeaderBytes \subseteq Nat

\* The three fault switches, each a reversed witness of one rule. The positive
\* cfg holds all three FALSE; each reversed cfg turns exactly one TRUE and
\* checks TypeOK plus the invariant that names the rule.
CONSTANTS HeaderByChildL,  \* the header is bounded by the field's own L
                           \* (the #2400 bug: Dart, then Rust, Java, Luau,
                           \* Squirrel)
          OffByOneAtL,     \* a payload ending one byte past the buffer is
                           \* accepted
          ResumeAtCursor   \* the walk resumes where the payload's read
                           \* stopped rather than at end
ASSUME HeaderByChildL \in BOOLEAN
ASSUME OffByOneAtL \in BOOLEAN
ASSUME ResumeAtCursor \in BOOLEAN

VARIABLES off,        \* the parent's cursor: the next byte the scan reads
          done,       \* the scan ended, at the terminator or at damage
          malformed,  \* the report's flag
          fieldEnd,   \* the end the last field was framed by: start + L
          cursor,     \* the furthest byte the last field's read reached
          resumed,    \* where the parent resumed after the last field
          hdrDamage,  \* the last field's count could not be read
          hdrEnd      \* where the last field's header would have ended

vars == <<off, done, malformed, fieldEnd, cursor, resumed, hdrDamage, hdrEnd>>

Init ==
    /\ off = 0
    /\ done = FALSE
    /\ malformed = FALSE
    /\ fieldEnd = 0
    /\ cursor = 0
    /\ resumed = 0
    /\ hdrDamage = FALSE
    /\ hdrEnd = 0

\* The zero reference at off ends the body (F3, R1).
Terminator ==
    /\ ~done
    /\ off < N
    /\ done' = TRUE
    /\ UNCHANGED <<off, malformed, fieldEnd, cursor, resumed, hdrDamage, hdrEnd>>

\* No byte left for a reference: the body has no terminator, which is framing
\* damage on this body (L2).
Truncated ==
    /\ ~done
    /\ off >= N
    /\ done' = TRUE
    /\ malformed' = TRUE
    /\ UNCHANGED <<off, fieldEnd, cursor, resumed, hdrDamage, hdrEnd>>

\* The payload fits when its end is inside the buffer (L4). The reversed
\* witness lets it end one byte past.
LengthFits(end) == IF OffByOneAtL THEN end <= N + 1 ELSE end <= N

\* One length-framed field whose payload is an array body: the reference and
\* the kind are folded into the L byte at off, so start is off + 1 and the
\* payload is [start, end). H is the header's width, the element kind byte and
\* the count's bytes.
Field(L, H) ==
    /\ ~done
    /\ off < N
    /\ LET start == off + 1
           end == start + L
       IN IF ~LengthFits(end)
          THEN \* L4: the length runs past the body carrying it; this body
               \* stops, with what it decoded standing
               /\ done' = TRUE
               /\ malformed' = TRUE
               /\ fieldEnd' = end
               /\ cursor' = start
               /\ hdrDamage' = FALSE
               /\ hdrEnd' = start
               /\ UNCHANGED <<off, resumed>>
          ELSE IF L < 2
          THEN \* H2: too short for a header, inert; the walk continues past L
               /\ off' = end
               /\ resumed' = end
               /\ fieldEnd' = end
               /\ cursor' = start
               /\ hdrDamage' = FALSE
               /\ hdrEnd' = start
               /\ UNCHANGED <<done, malformed>>
          ELSE LET bound == IF HeaderByChildL THEN end ELSE N
                   hend == start + H
               IN IF hend > bound
                  THEN \* the count cannot complete: damage inside the field,
                       \* the field defaults, the parent resumes past L
                       /\ malformed' = TRUE
                       /\ off' = end
                       /\ resumed' = end
                       /\ fieldEnd' = end
                       /\ cursor' = IF hend > N THEN N ELSE hend
                       /\ hdrDamage' = TRUE
                       /\ hdrEnd' = hend
                       /\ UNCHANGED done
                  ELSE \* H3: the header read against the parent; the elements
                       \* bounded by L, a span clamped at zero when the header
                       \* crossed end; the parent resumes at end (L1)
                       LET span == IF end >= hend THEN end - hend ELSE 0
                           after == hend + span
                           resume == IF ResumeAtCursor THEN after ELSE end
                       IN /\ off' = resume
                          /\ resumed' = resume
                          /\ fieldEnd' = end
                          /\ cursor' = after
                          /\ hdrDamage' = FALSE
                          /\ hdrEnd' = hend
                          /\ UNCHANGED <<done, malformed>>

Finished == done /\ UNCHANGED vars

Next ==
    \/ Terminator
    \/ Truncated
    \/ \E L \in 0..MaxL, H \in HeaderBytes : Field(L, H)
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
    /\ hdrDamage \in BOOLEAN
    /\ hdrEnd \in 0..(N + MaxL + 1)

\* No field's read reaches past the buffer (L4, H5): a neighbour's bytes are
\* never read as elements, and a payload past the end is damage, not data.
NoReadPastBuffer == cursor <= N

\* The parent resumes exactly at end after every field it did not stop on
\* (L1): a count that crossed L does not move the resume point.
ResumeAtEnd == ~done => resumed = fieldEnd

\* A count is damage only when the PARENT cannot complete it (H3): a header
\* the buffer holds is read whether or not the field's own L covers it.
HeaderAgainstParent == hdrDamage => hdrEnd > N

==============================================================================
