------------------------------ MODULE FixedBatch ------------------------------
\* A LAYOUT model of the fixed form's LOAD over a whole BATCH
\* (docs/FIXED-FORM-ALGORITHM.md §5.3, TOTAL OVER THE BATCH; docs/SPEC-TABLES.md
\* §3.4). tla/FixedLive holds one record's check loop; this module holds the
\* loop over records, with the arithmetic the Squirrel reader carries
\* (internal/codegen/squirreltable/fixedform.go, emitFixedRead):
\*
\*   the file      H header bytes, then `count` records of R bytes each, then a
\*                 ragged tail of 0..R-1 bytes; record k's value byte sits at
\*                 offset H + k*R (0-based), the rest of the record is filler
\*   the frame     rest = len - H; rest % R # 0 refuses (truncated);
\*                 count = rest \div R; count > the caller's rows refuses
\*                 (batch_too_large)
\*   the decode    record k decodes into scratch[k]; a bad record refuses
\*   the commit    only after the last record decoded: out[k] := scratch[k]
\*                 for k in 0..count-1
\*
\* The caller hands M = N + 1 rows, each POISONED, so a write past count is
\* seen. A record's value is 1 or 2 (it decodes) or 0 (one of the body
\* conditions: a count, length, text, bool or present byte, float or range;
\* which one does not matter to the batch). The ORACLE is written from the
\* file's MEANING: refused when the tail is ragged or any record is bad, and
\* otherwise rows 0..n-1 hold the records' values and every other row its
\* poison. It is a model, not a codec.

EXTENDS Naturals, Sequences, FiniteSets, TLC

CONSTANTS N,   \* the most records a file holds: 3
          R,   \* the record size in bytes: 2
          H    \* the header size in bytes: 1
ASSUME N \in 1..4 /\ R \in 2..3 /\ H \in 1..2

\* The fault switches. The positive cfg holds all FALSE; each reversed cfg
\* turns exactly one TRUE.
CONSTANTS LandDuringDecode, \* the interleaved loop: record k decodes straight
                            \* into out[k], so a refusal at k leaves 0..k-1
                            \* landed
          CommitShort,      \* the commit loop stops at count - 1: the last
                            \* record is decoded and never lands
          StrideOff,        \* record k addressed at H + k*(R-1): the stride
                            \* drops a byte and reads filler as a value
          NoTailCheck       \* count = rest \div R with no rest % R check: a
                            \* ragged tail is read as though it were whole
ASSUME \A s \in {LandDuringDecode, CommitShort, StrideOff, NoTailCheck} : s \in BOOLEAN

M      == N + 1             \* the caller's rows
Poison == 7
Filler == 9
Values == 0..2              \* 0 is a bad record

\* The file's bytes, 1-based as TLA sequences are.
Img(vals, tail) ==
    LET n == Len(vals)
        len == H + n * R + tail
    IN [o \in 1..len |->
          IF o <= H THEN Filler
          ELSE IF o <= H + n * R
               THEN (IF (o - H - 1) % R = 0 THEN vals[((o - H - 1) \div R) + 1] ELSE Filler)
               ELSE Filler]

Stride == IF StrideOff THEN R - 1 ELSE R
Off(k)  == H + k * Stride + 1                 \* record k's value byte, 1-based
At(img, o) == IF o \in 1..Len(img) THEN img[o] ELSE Filler

\* ------------------------------------------------------------- the oracle

OracleRefuses(vals, tail) == tail # 0 \/ \E i \in 1..Len(vals) : vals[i] = 0
Untouched == [i \in 1..M |-> Poison]
OracleRows(vals) == [i \in 1..M |-> IF i <= Len(vals) THEN vals[i] ELSE Poison]

\* ------------------------------------------------------------- the reader

VARIABLES vals, tail, img, phase, count, k, scratch, out, readOff
vars == <<vals, tail, img, phase, count, k, scratch, out, readOff>>

Init ==
    /\ \E n \in 0..N : vals \in [1..n -> Values]
    /\ tail \in 0..(R - 1)
    /\ img = Img(vals, tail)
    /\ phase = "frame"
    /\ count = 0
    /\ k = 0
    /\ scratch = [i \in 1..M |-> Poison]
    /\ out = Untouched
    /\ readOff = {}

Frame ==
    /\ phase = "frame"
    /\ LET rest == Len(img) - H IN
       IF ~NoTailCheck /\ rest % R # 0
       THEN phase' = "refused" /\ UNCHANGED count
       ELSE IF rest \div R > M
            THEN phase' = "refused" /\ UNCHANGED count
            ELSE phase' = "decode" /\ count' = rest \div R
    /\ UNCHANGED <<vals, tail, img, k, scratch, out, readOff>>

Decode ==
    /\ phase = "decode"
    /\ k < count
    /\ LET x == At(img, Off(k)) IN
       /\ readOff' = readOff \cup {Off(k)}
       /\ IF x \notin 1..2
          THEN /\ phase' = "refused"
               /\ UNCHANGED <<k, scratch, out>>
          ELSE /\ k' = k + 1
               /\ IF LandDuringDecode
                  THEN out' = [out EXCEPT ![k + 1] = x] /\ UNCHANGED scratch
                  ELSE scratch' = [scratch EXCEPT ![k + 1] = x] /\ UNCHANGED out
               /\ UNCHANGED phase
    /\ UNCHANGED <<vals, tail, img, count>>

Decoded ==
    /\ phase = "decode"
    /\ k = count
    /\ phase' = "commit"
    /\ k' = 0
    /\ UNCHANGED <<vals, tail, img, count, scratch, out, readOff>>

CommitTo == IF CommitShort /\ count > 0 THEN count - 1 ELSE count

Commit ==
    /\ phase = "commit"
    /\ IF k < CommitTo
       THEN /\ out' = IF LandDuringDecode THEN out ELSE [out EXCEPT ![k + 1] = scratch[k + 1]]
            /\ k' = k + 1
            /\ UNCHANGED phase
       ELSE /\ phase' = "accepted"
            /\ UNCHANGED <<k, out>>
    /\ UNCHANGED <<vals, tail, img, count, scratch, readOff>>

Finished == phase \in {"accepted", "refused"} /\ UNCHANGED vars

Next == Frame \/ Decode \/ Decoded \/ Commit \/ Finished

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- properties

TypeOK ==
    /\ phase \in {"frame", "decode", "commit", "accepted", "refused"}
    /\ count \in 0..M
    /\ k \in 0..M
    /\ scratch \in [1..M -> Values \cup {Poison}]
    /\ out \in [1..M -> Values \cup {Poison}]

\* THE CONTRACT: the read refuses exactly the files the meaning refuses, with
\* every one of the caller's rows untouched, the rows before the bad record as
\* much as the bad record's own; and otherwise lands exactly the meaning, with
\* every row past the batch untouched.
OneVerdict ==
    phase \in {"accepted", "refused"} =>
        /\ (phase = "refused") = OracleRefuses(vals, tail)
        /\ phase = "refused" => out = Untouched
        /\ phase = "accepted" => out = OracleRows(vals)

\* Every byte the decode reads is a record's value byte, inside the records.
ReadsValueBytes ==
    readOff \subseteq {H + j * R + 1 : j \in 0..(Len(vals) - 1)}

==============================================================================
