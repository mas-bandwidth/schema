---------------------------- MODULE CsReadReportIso ----------------------------
\* Generated C# read/report isolation (docs/security/audit-2026-11-05/cs.md;
\* internal/codegen/cstable/region.go, the shared Ignored report reset; the
\* packet form at internal/codegen/csharp/functions.go). One read walks a
\* cursor over a finite buffer. A damage or refusal condition ends the read:
\* a refusal publishes no value, damage keeps the partial destination the
\* table report documents, and an acceptance publishes the whole value. The
\* next read starts with a zeroed report, so no earlier read's counters or
\* verdict leak into it. It is a model, not a codec, and it does not prove
\* any reader.
\*
\* The fault switch BoundCheckRemoved deletes the cursor's bound check;
\* MCCsReadReportIsoBroken.cfg turns it TRUE and TLC must reject the model
\* with a CursorBounded counterexample.

EXTENDS Naturals, TLC

CONSTANTS MaxLen,            \* the largest buffer length in the instance
          BoundCheckRemoved  \* the fault switch

ASSUME MaxLen \in 1..4
ASSUME BoundCheckRemoved \in BOOLEAN

VARIABLES phase,    \* "idle", "reading" or "done"
          len,      \* the current buffer's length
          cursor,   \* the read cursor
          dmgAt,    \* the offset of the damage/refusal condition, or MaxLen
          dest,     \* "empty", "partial" or "whole": what the caller holds
          rep,      \* the report's unknown-field counter, capped
          verdict   \* "none", "ok", "refused" or "damaged"

vars == <<phase, len, cursor, dmgAt, dest, rep, verdict>>

Init ==
    /\ phase = "idle"
    /\ len = 0
    /\ cursor = 0
    /\ dmgAt = MaxLen
    /\ dest = "empty"
    /\ rep = 0
    /\ verdict = "none"

Start ==
    /\ phase = "idle"
    /\ \E n \in 0..MaxLen, d \in 0..MaxLen :
         /\ len' = n
         /\ dmgAt' = d
    /\ phase' = "reading"
    /\ cursor' = 0
    /\ dest' = "empty"
    /\ rep' = 0
    /\ verdict' = "none"

Step ==
    /\ phase = "reading"
    /\ cursor < len
    /\ cursor # dmgAt
    /\ cursor' = cursor + 1
    /\ dest' = "partial"
    /\ rep' = IF rep < 2 THEN rep + 1 ELSE rep
    /\ UNCHANGED <<phase, len, dmgAt, verdict>>

StepUnbounded ==
    /\ BoundCheckRemoved
    /\ phase = "reading"
    /\ cursor' = cursor + 1
    /\ UNCHANGED <<phase, len, dmgAt, dest, rep, verdict>>

HitRefusal ==
    /\ phase = "reading"
    /\ cursor < len
    /\ cursor = dmgAt
    /\ phase' = "done"
    /\ verdict' = "refused"
    /\ dest' = "empty"
    /\ UNCHANGED <<len, cursor, dmgAt, rep>>

HitDamage ==
    /\ phase = "reading"
    /\ cursor < len
    /\ cursor = dmgAt
    /\ phase' = "done"
    /\ verdict' = "damaged"
    /\ UNCHANGED <<len, cursor, dmgAt, dest, rep>>

Accept ==
    /\ phase = "reading"
    /\ cursor = len
    /\ phase' = "done"
    /\ verdict' = "ok"
    /\ dest' = "whole"
    /\ UNCHANGED <<len, cursor, dmgAt, rep>>

NextRead ==
    /\ phase = "done"
    /\ phase' = "idle"
    /\ cursor' = 0
    /\ dest' = "empty"
    /\ rep' = 0
    /\ verdict' = "none"
    /\ UNCHANGED <<len, dmgAt>>

Finished == phase = "done" /\ UNCHANGED vars

Next ==
    \/ Start
    \/ Step
    \/ StepUnbounded
    \/ HitRefusal
    \/ HitDamage
    \/ Accept
    \/ NextRead
    \/ Finished

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"idle", "reading", "done"}
    /\ len \in 0..MaxLen
    /\ cursor \in 0..(MaxLen + 1)
    /\ dmgAt \in 0..MaxLen
    /\ dest \in {"empty", "partial", "whole"}
    /\ rep \in 0..2
    /\ verdict \in {"none", "ok", "refused", "damaged"}

CursorBounded == cursor <= len \/ phase = "idle"

NoValueOnRefusal == verdict = "refused" => dest = "empty"

AcceptedIsWhole == verdict = "ok" => (dest = "whole" /\ cursor = len)

ReportIsolated == phase = "idle" => (rep = 0 /\ verdict = "none")

==============================================================================
