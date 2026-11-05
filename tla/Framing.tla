------------------------------ MODULE Framing --------------------------------
\* A LAYOUT model of the table wire's record scan (docs/LAYERS/1-framing.md;
\* docs/SPEC-TABLES.md §3, §4, §2.9), under THE ONE READ (§6.5): one parent
\* body of N bytes walked by a cursor, one length-framed field at a time. It
\* carries the arithmetic the readers carry and an abstract model cannot see:
\* the field's start, its end `start + L`, the header read INSIDE THE FIELD'S
\* OWN L (§3, THE HEADER INSIDE THE BODY; docs/LAYERS/1-framing.md H2, H3),
\* the elements filling L exactly, and the resume point `end` after a field
\* read whole. DAMAGE DISCARDS THE WHOLE LOAD (§4, D1): a body too short to carry
\* its header, a header that crosses L, and an L past the buffer each end the
\* scan, and nothing resumes, so there is no read-on past damage and no INERT
\* body. It is a model, not a codec: the reference, the kind byte and the
\* element values are folded away, and only offsets remain: the elements are
\* E bytes after the header. Elements that run past L (a count the body
\* cannot cover, H5) and elements that stop short of L, leaving bytes inside
\* it after the count's last element (L6), are each damage and end the load.
\*
\* The byte values a reader would meet are chosen nondeterministically: every
\* L in 0..MaxL for every field, every header width in HeaderBytes (the
\* element kind byte plus a count of one or more LEB128 bytes) and every element
\* span E in 0..MaxL. TLC therefore walks every placement of a header and of
\* the elements against every L and every buffer end.

EXTENDS Naturals, TLC

CONSTANTS N,           \* the parent body's length in bytes
          MaxL,        \* the largest L a field may claim
          HeaderBytes  \* the header widths an array body may carry
ASSUME N \in Nat /\ MaxL \in Nat /\ HeaderBytes \subseteq Nat

\* The fault switches, each a reversed witness of one rule. The positive cfg
\* holds all FALSE; each reversed cfg turns exactly one TRUE and checks TypeOK
\* plus the invariant that names the rule. The first two are the RETIRED
\* tolerant rules (docs/SPEC-TABLES.md §6.5, 2026-11-03), kept as witnesses
\* so the strict rule is shown to exclude them.
CONSTANTS HeaderAgainstParent, \* the retired N3 rule: the header read against
                               \* the PARENT buffer, a header that crosses L
                               \* accepted and its element span clamped at 0
          InertShortBody,      \* the retired H2 rule: a body of fewer than two
                               \* bytes is INERT and the walk reads on past L
          OffByOneAtL,         \* a payload ending one byte past the buffer is
                               \* accepted
          ResumeAfterDamage,   \* damage inside a field is resumed past L: the
                               \* read-on the ruling of 2026-10-31 removed
          TrailingAccepted     \* bytes left inside L after the count's last
                               \* element are stepped over to `end` and the
                               \* field read whole: the reader before L6
ASSUME TrailingAccepted \in BOOLEAN
ASSUME HeaderAgainstParent \in BOOLEAN
ASSUME InertShortBody \in BOOLEAN
ASSUME ResumeAfterDamage \in BOOLEAN
ASSUME OffByOneAtL \in BOOLEAN

VARIABLES off,        \* the parent's cursor: the next byte the scan reads
          done,       \* the scan ended, at the terminator or at damage
          malformed,  \* damage was met: the verdict is "refused"
          fieldStart, \* where the last field's payload began
          fieldEnd,   \* the end the last field was framed by: start + L
          cursor,     \* the furthest byte the last field's read reached
          resumed,    \* where the parent resumed after the last field
          hdrDamage,  \* the last field's count could not be read
          hdrEnd,     \* where the last field's header would have ended
          trailing    \* the parent stepped past a field whose elements
                      \* stopped short of its end

vars == <<off, done, malformed, fieldStart, fieldEnd, cursor, resumed, hdrDamage, hdrEnd, trailing>>

Init ==
    /\ off = 0
    /\ done = FALSE
    /\ malformed = FALSE
    /\ fieldStart = 0
    /\ fieldEnd = 0
    /\ cursor = 0
    /\ resumed = 0
    /\ hdrDamage = FALSE
    /\ hdrEnd = 0
    /\ trailing = FALSE

\* The zero reference at off ends the body (F3, R1).
Terminator ==
    /\ ~done
    /\ off < N
    /\ done' = TRUE
    /\ UNCHANGED <<off, malformed, fieldStart, fieldEnd, cursor, resumed, hdrDamage, hdrEnd, trailing>>

\* No byte left for a reference: the body has no terminator, which is framing
\* damage on this body (L2).
Truncated ==
    /\ ~done
    /\ off >= N
    /\ done' = TRUE
    /\ malformed' = TRUE
    /\ UNCHANGED <<off, fieldStart, fieldEnd, cursor, resumed, hdrDamage, hdrEnd, trailing>>

\* The payload fits when its end is inside the buffer (L4). The reversed
\* witness lets it end one byte past.
LengthFits(end) == IF OffByOneAtL THEN end <= N + 1 ELSE end <= N

\* One length-framed field whose payload is an array body: the reference and
\* the kind are folded into the L byte at off, so start is off + 1 and the
\* payload is [start, end). H is the header's width, the element kind byte and
\* the count's bytes; E is the bytes the count's elements cover after it.
Field(L, H, E) ==
    /\ ~done
    /\ off < N
    /\ LET start == off + 1
           end == start + L
       IN /\ fieldStart' = start
          /\ IF ~LengthFits(end)
             THEN \* L4: the length runs past the body carrying it: damage,
                  \* and the load ends
                  /\ done' = TRUE
                  /\ malformed' = TRUE
                  /\ fieldEnd' = end
                  /\ cursor' = start
                  /\ hdrDamage' = FALSE
                  /\ hdrEnd' = start
                  /\ trailing' = FALSE
                  /\ UNCHANGED <<off, resumed>>
             ELSE IF L < 2
             THEN \* H2: too short to carry its header: damage, and the load
                  \* ends. The retired rule (InertShortBody) stepped over it
                  \* and read on.
                  /\ fieldEnd' = end
                  /\ cursor' = start
                  /\ hdrDamage' = FALSE
                  /\ hdrEnd' = start
                  /\ trailing' = FALSE
                  /\ IF InertShortBody
                     THEN /\ off' = end
                          /\ resumed' = end
                          /\ UNCHANGED <<done, malformed>>
                     ELSE /\ done' = TRUE
                          /\ malformed' = TRUE
                          /\ UNCHANGED <<off, resumed>>
             ELSE LET bound == IF HeaderAgainstParent THEN N ELSE end
                      hend == start + H
                  IN IF hend > bound
                     THEN \* H3: the header crosses the field's own L (or, under
                          \* the retired rule, the parent): damage inside the
                          \* field, and the whole load ends (§4). The reversed
                          \* witness ResumeAfterDamage resumes past L instead.
                          /\ malformed' = TRUE
                          /\ fieldEnd' = end
                          /\ cursor' = IF hend > N THEN N ELSE hend
                          /\ hdrDamage' = TRUE
                          /\ hdrEnd' = hend
                          /\ trailing' = FALSE
                          /\ IF ResumeAfterDamage
                             THEN /\ off' = end
                                  /\ resumed' = end
                                  /\ UNCHANGED done
                             ELSE /\ done' = TRUE
                                  /\ UNCHANGED <<off, resumed>>
                     ELSE \* the header inside L. Only the retired parent rule
                          \* reaches a header past end, whose element span it
                          \* clamped at zero.
                          LET span == IF end >= hend THEN end - hend ELSE 0
                          IN /\ fieldEnd' = end
                             /\ hdrDamage' = FALSE
                             /\ hdrEnd' = hend
                             /\ IF E > span \/ (E < span /\ ~TrailingAccepted)
                                THEN \* H5: elements past L; L6: bytes left in
                                     \* L after them. Damage inside the field,
                                     \* and the whole load ends (§4), or, under
                                     \* ResumeAfterDamage, resumes past L.
                                     /\ malformed' = TRUE
                                     /\ cursor' = IF E > span THEN end ELSE hend + E
                                     /\ trailing' = FALSE
                                     /\ IF ResumeAfterDamage
                                        THEN /\ off' = end
                                             /\ resumed' = end
                                             /\ UNCHANGED done
                                        ELSE /\ done' = TRUE
                                             /\ UNCHANGED <<off, resumed>>
                                ELSE \* the elements fill L (or, under
                                     \* TrailingAccepted, stop short and the
                                     \* rest is stepped over); the parent
                                     \* resumes at end (L1)
                                     /\ off' = end
                                     /\ resumed' = end
                                     /\ cursor' = hend + E
                                     /\ trailing' = (E < span)
                                     /\ UNCHANGED <<done, malformed>>

Finished == done /\ UNCHANGED vars

Next ==
    \/ Terminator
    \/ Truncated
    \/ \E L \in 0..MaxL, H \in HeaderBytes, E \in 0..MaxL : Field(L, H, E)
    \/ Finished

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- properties

TypeOK ==
    /\ off \in 0..(N + MaxL + 1)
    /\ done \in BOOLEAN
    /\ malformed \in BOOLEAN
    /\ fieldStart \in 0..(N + MaxL + 1)
    /\ fieldEnd \in 0..(N + MaxL + 1)
    /\ cursor \in 0..(N + MaxL + 1)
    /\ resumed \in 0..(N + MaxL + 1)
    /\ hdrDamage \in BOOLEAN
    /\ hdrEnd \in 0..(N + MaxL + 1)
    /\ trailing \in BOOLEAN

\* No field's read reaches past the buffer (L4, H5): a neighbour's bytes are
\* never read as elements, and a payload past the end is damage, not data.
NoReadPastBuffer == cursor <= N

\* The parent resumes exactly at end after every field it did not stop on
\* (L1): a count that crossed L does not move the resume point.
ResumeAtEnd == ~done => resumed = fieldEnd

\* DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): once the flag is
\* set the scan is over, so no field is read after damage and nothing a later
\* field held can stand beside it.
DamageEndsLoad == malformed => done

\* THE HEADER INSIDE THE BODY (H2, H3): every field the parent read whole and
\* stepped past carried a whole header of at least two bytes, inside its own
\* L. A short body read on (the retired INERT rule) and a header past L read
\* against the parent (the retired N3 rule) each break it.
HeaderInsideBody ==
    (~done /\ off > 0) => (fieldStart + 2 <= hdrEnd /\ hdrEnd <= fieldEnd)

\* The elements decode inside the field's own L (H5): no field the parent
\* stepped past was read beyond its end.
ElementsInsideBody == (~done /\ off > 0) => cursor <= fieldEnd

\* Header damage is exactly a header that crosses the field's own L.
HeaderDamageIsCrossing == hdrDamage => hdrEnd > fieldEnd

\* BYTES LEFT IN AN ARRAY'S L AFTER ITS ELEMENTS ARE DAMAGE (L6; §4, §6.5):
\* no field the parent stepped past held bytes inside its L after the count's
\* last element. MCFramingTrailingElements.cfg reverses it (TrailingAccepted).
EveryCaseRuled == ~trailing

==============================================================================
