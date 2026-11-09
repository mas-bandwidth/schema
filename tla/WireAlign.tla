------------------------------ MODULE WireAlign ------------------------------
\* A LAYOUT model of the packet wire's static byte-alignment analysis
\* (ir/wire.go: wirePos, AlignedFixedByteArrays, walkAligned, afterField,
\* unionAlignExit; docs/SPEC.md §4.3, §4.4, §4.7, §4.8, §4.12). The analysis
\* walks a struct's items carrying the wire bit position MODULO 8, or UNKNOWN,
\* and marks a fixed [N]uint8 field when its first element provably begins on
\* a byte boundary; a backend then bulk-copies that field (align, then memcpy),
\* which is the same wire only when the align contributes zero bits.
\*
\* The model carries both sides of that claim:
\*
\*   the analysis   Walk(items, pos, mark), the code's rules over the same
\*                  modulo-8 arithmetic: ((pos + bits) % 8), align and text
\*                  reset to 0, a branch is known only when both sides agree,
\*                  a union only when every arm and None agree
\*   the wire       a cursor over the items with the ABSOLUTE bit offset the
\*                  writer reaches, entered at any offset 0..7 (a struct may be
\*                  embedded anywhere), choosing every legal width: a string's
\*                  length and its pad, a wide string's length, a counted
\*                  array's count, the branch side taken, the union arm
\*                  selected or None
\*
\* and checks that every field the analysis marks is reached on a byte
\* boundary, and that a known exit is the exit the wire reaches. It is a
\* model, not a codec: field values are folded away and only widths remain.

EXTENDS Naturals, Sequences, FiniteSets, TLC

CONSTANTS Widths,    \* fixed-width field widths in bits: {1, 7, 8}
          Elems,     \* counted-array element widths in bits: {7, 8}
          TextSize,  \* N of string(N) / wstring(N): 1
          Bound,     \* N of a counted array [..N]T: 3
          MaxLen     \* items before the closing [1]uint8 field: 2
ASSUME Widths \subseteq 1..16 /\ Elems \subseteq 1..16
ASSUME TextSize \in 1..3 /\ Bound \in 1..7 /\ MaxLen \in 0..3

\* The fault switch. The positive cfg holds it FALSE; the reversed cfg turns it
\* TRUE.
CONSTANT VariableAsFixed  \* a union field, whose wire is its tag and the ONE
                          \* arm selected, advances the position as though it
                          \* were fixed at its widest arm (tag + max arm bits)
ASSUME VariableAsFixed \in BOOLEAN

U == 8                                          \* the unknown position
Add(p, b) == IF p = U THEN U ELSE (p + b) % 8   \* wirePos.add
Pad(b) == b + ((8 - (b % 8)) % 8)               \* the writer's align

\* BitsRequired(0, n): the bits of a tag, a length or a count prefix.
Bits(n) == CHOOSE b \in 0..8 : 2^b > n /\ (b = 0 \/ 2^(b - 1) <= n)

\* ------------------------------------------------------------- the items
\* One record shape for every item, so TLC compares them as values.

Item(k, w, t, e, arms) == [k |-> k, w |-> w, t |-> t, e |-> e, arms |-> arms, m |-> FALSE]
FixedI(w)      == Item("fixed", w, <<>>, <<>>, <<>>)    \* a scalar, enum, flags, fixed-width struct
AlignI         == Item("align", 0, <<>>, <<>>, <<>>)    \* the `align` item
TextI          == Item("text", 0, <<>>, <<>>, <<>>)     \* string(N) / bytes(N): prefix, align, bytes
WideI          == Item("wide", 0, <<>>, <<>>, <<>>)     \* wstring(N): prefix, 32-bit groups
CountedI(e)    == Item("counted", e, <<>>, <<>>, <<>>)  \* [..N]T: count prefix, count elements
DataI          == Item("data", 8, <<>>, <<>>, <<>>)     \* the [1]uint8 field the analysis may mark
BranchI(t, e)  == Item("branch", 0, t, e, <<>>)         \* if c { t } else { e }
UnionI(arms)   == Item("union", 0, <<>>, <<>>, arms)    \* a union field; <<>> is a payload-free arm

SimpleItems == {FixedI(w) : w \in Widths} \cup {AlignI, DataI}
Sides       == {<<>>} \cup {<<s>> : s \in SimpleItems}
ArmChoices  == {<<>>} \cup {<<FixedI(w)>> : w \in Widths} \cup {<<TextI>>}
Unions      == {UnionI(<<a>>) : a \in ArmChoices} \cup {UnionI(<<a, b>>) : a, b \in ArmChoices}
Alphabet    == {FixedI(w) : w \in Widths} \cup {AlignI, DataI, TextI, WideI}
                 \cup {CountedI(e) : e \in Elems}
                 \cup {BranchI(t, e) : t, e \in Sides}
                 \cup Unions
Programs    == {p \o <<DataI>> : p \in UNION {[1..n -> Alphabet] : n \in 0..MaxLen}}

\* ------------------------------------------------------------- the analysis

RECURSIVE MaxBits(_)
\* maxBitsStruct over an arm's items: the longest wire.
MaxBits(items) ==
    IF items = <<>> THEN 0
    ELSE LET it == Head(items)
             w  == CASE it.k = "fixed" -> it.w
                     [] it.k = "text"  -> Bits(TextSize) + 7 + 8 * TextSize
                     [] OTHER          -> 0
         IN w + MaxBits(Tail(items))

MaxArm(arms) == LET ws == {MaxBits(arms[i]) : i \in 1..Len(arms)}
                IN CHOOSE x \in ws : \A y \in ws : y <= x

RECURSIVE Walk(_, _, _), StepPos(_, _, _)

\* Walk(items, pos, mark) == << the items with each data field's mark, exit >>.
\* mark is FALSE inside a nested walk (a union arm), which marks nothing.
Walk(items, pos, mark) ==
    IF items = <<>> THEN << <<>>, pos >>
    ELSE LET one  == StepPos(Head(items), pos, mark)
             rest == Walk(Tail(items), one[2], mark)
         IN << <<one[1]>> \o rest[1], rest[2] >>

StepPos(it, pos, mark) ==
    CASE it.k = "fixed"   -> << it, Add(pos, it.w) >>
      [] it.k = "align"   -> << it, 0 >>
      [] it.k = "text"    -> << it, 0 >>
      [] it.k = "wide"    -> << it, Add(pos, Bits(TextSize)) >>
      [] it.k = "counted" -> << it, IF it.w % 8 # 0 THEN U ELSE Add(pos, Bits(Bound)) >>
      [] it.k = "data"    -> << [it EXCEPT !.m = mark /\ pos = 0], Add(pos, it.w) >>
      [] it.k = "branch"  ->
            LET t == Walk(it.t, pos, mark)
                e == Walk(it.e, pos, mark)
            IN << [it EXCEPT !.t = t[1], !.e = e[1]],
                  IF t[2] # U /\ t[2] = e[2] THEN t[2] ELSE U >>
      [] it.k = "union"   ->
            LET tagged == Add(pos, Bits(Len(it.arms)))
                exits  == {Walk(it.arms[i], tagged, FALSE)[2] : i \in 1..Len(it.arms)}
            IN << it,
                  IF VariableAsFixed
                  THEN Add(pos, Bits(Len(it.arms)) + MaxArm(it.arms))
                  ELSE IF tagged # U /\ exits \subseteq {tagged} THEN tagged ELSE U >>

\* ------------------------------------------------------------- the wire

VARIABLES prog, entry, exit, todo, bit
vars == <<prog, entry, exit, todo, bit>>

Init ==
    /\ prog \in Programs
    /\ entry \in 0..7
    /\ todo = Walk(prog, U, TRUE)[1]     \* AlignedFixedByteArrays: entry unknown
    /\ exit = Walk(prog, U, TRUE)[2]
    /\ bit = entry

Write ==
    /\ todo # <<>>
    /\ LET it   == Head(todo)
           rest == Tail(todo)
       IN CASE it.k = "fixed"   -> bit' = bit + it.w /\ todo' = rest
            [] it.k = "align"   -> bit' = Pad(bit) /\ todo' = rest
            [] it.k = "data"    -> bit' = bit + it.w /\ todo' = rest
            [] it.k = "text"    -> \E n \in 0..TextSize :
                                     bit' = Pad(bit + Bits(TextSize)) + 8 * n /\ todo' = rest
            [] it.k = "wide"    -> \E n \in 0..TextSize :
                                     bit' = bit + Bits(TextSize) + 32 * n /\ todo' = rest
            [] it.k = "counted" -> \E c \in 0..Bound :
                                     bit' = bit + Bits(Bound) + c * it.w /\ todo' = rest
            [] it.k = "branch"  -> \E side \in {it.t, it.e} :
                                     bit' = bit /\ todo' = side \o rest
            [] it.k = "union"   -> \E i \in 0..Len(it.arms) :
                                     /\ bit' = bit + Bits(Len(it.arms))
                                     /\ todo' = (IF i = 0 THEN <<>> ELSE it.arms[i]) \o rest
    /\ UNCHANGED <<prog, entry, exit>>

Finished == todo = <<>> /\ UNCHANGED vars

Next == Write \/ Finished

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- properties

TypeOK ==
    /\ entry \in 0..7
    /\ exit \in 0..U
    /\ bit \in Nat
    /\ DOMAIN todo = 1..Len(todo)

\* Every field the analysis marks begins on a byte boundary on every wire.
AlignedWhereMarked ==
    (todo # <<>> /\ Head(todo).k = "data" /\ Head(todo).m) => bit % 8 = 0

\* A known exit is the position every wire leaves the struct at.
ExitSound ==
    (todo = <<>> /\ exit # U) => bit % 8 = exit

==============================================================================
