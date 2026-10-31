----------------------------- MODULE NodeExtent -------------------------------
\* A LAYOUT model of the NODE EXTENT (docs/SPEC-TABLES.md §2.8, §2.9, §7.2,
\* §7.6): where a cooked node's maps and lists lay their arrays. It carries the
\* arithmetic every writer carries (internal/tablecook/extent.go, the C++
\* reference's ExtentAt and ExtentPack, the Rust carve in
\* internal/codegen/rusttable/cookcarve.go) and that an abstract model of "the
\* entries are somewhere in the node" cannot see: the node's base, the record's
\* storage rounded to the region's alignment floor, the running offset, each
\* array's start rounded to its element's alignment, `count x size`, and the
\* PRE-ORDER rule — a container's WHOLE array first, then, element by element,
\* the arrays its elements hold.
\*
\* One node, two top-level containers (A then B, in field order); each element
\* of A may hold one child container of its own (a list of tables that hold
\* lists, a map whose entries hold maps). Every shape is chosen
\* nondeterministically at Init — the node's alignment and base, the record's
\* size, each container's element size and alignment, every count, every
\* child count — and the layout the walk computes is checked against the rules.
\* A map's entries are a list's elements with the generated `{ key, value }`
\* entry as the element; the key order is not index arithmetic and is not here.

EXTENDS Naturals, Sequences, TLC

CONSTANTS MaxCount,   \* the largest count a container may hold
          Elems,      \* the element shapes: [size, align], size a multiple of align
          RecAligns,  \* the alignments a record may have
          MaxRecUnits,\* a record's size is k x its alignment, k in 1..MaxRecUnits
          Floor       \* the region's alignment floor: eight

\* The three fault switches, each a reversed witness of one rule. The positive
\* cfg holds all three FALSE; each reversed cfg turns exactly one TRUE.
CONSTANTS OldRule,         \* the node aligned to its record alone and the extent
                           \* begun at the floor, whatever its elements need
          SkipAlign,       \* an array is laid at the running offset, unaligned
          ChildrenFirst,   \* an element's child array is laid before the
                           \* parent's whole array is reserved
          ExtentOverRecord \* the extent begins at the node's base, over the
                           \* record's own storage
ASSUME OldRule \in BOOLEAN /\ SkipAlign \in BOOLEAN /\ ChildrenFirst \in BOOLEAN /\ ExtentOverRecord \in BOOLEAN

Max(x, y) == IF x > y THEN x ELSE y

Up(x, a) == IF SkipAlign THEN x ELSE ((x + a - 1) \div a) * a

VARIABLES shape, layout
vars == <<shape, layout>>

\* The node: its alignment is its record's, and its base is any multiple of
\* that inside the region (the region's base is aligned to the greatest node
\* alignment, which is at least the node's). A record that holds a container
\* holds its sixteen-byte slot at eight (ir/blocklayout.go), so its alignment
\* is at least eight: RecAligns is {8, 16}. An element may be aligned sixteen
\* (a 128-bit list element) inside a record aligned eight: the node's own
\* alignment covers it (NodeAlign, below).
Shapes == [ recAlign : RecAligns,
            recUnits : 1..MaxRecUnits,
            baseUnits : 0..2,
            a : Elems, b : Elems, child : Elems,
            na : 0..MaxCount, nb : 0..MaxCount,
            kids : [1..MaxCount -> 0..MaxCount] ]

\* The walk, as the writers take it. Positions are RELATIVE to the extent's
\* base; an array is a record [start, count, size, align].
Arr(start, n, e) == [start |-> start, count |-> n, size |-> e.size, align |-> e.align]

\* The children of A, one array per element, laid from offset `at`, in index
\* order; answers the arrays and the offset after them.
RECURSIVE KidsFrom(_, _, _)
KidsFrom(s, i, at) ==
    IF i > s.na THEN <<<<>>, at>>
    ELSE LET st == Up(at, s.child.align)
             here == Arr(st, s.kids[i], s.child)
             rest == KidsFrom(s, i + 1, st + s.kids[i] * s.child.size)
         IN  <<<<here>> \o rest[1], rest[2]>>

Walk(s) ==
    IF ChildrenFirst
    THEN \* the reversed witness: the children are laid where A's array begins,
         \* before A's own bytes are reserved
         LET kids == KidsFrom(s, 1, 0)
             aSt  == Up(0, s.a.align)
             aEnd == aSt + s.na * s.a.size
             bSt  == Up(kids[2], s.b.align)
         IN  [a |-> Arr(aSt, s.na, s.a), kids |-> kids[1], b |-> Arr(bSt, s.nb, s.b),
              extent |-> bSt + s.nb * s.b.size]
    ELSE LET aSt  == Up(0, s.a.align)
             aEnd == aSt + s.na * s.a.size
             kids == KidsFrom(s, 1, aEnd)
             bSt  == Up(kids[2], s.b.align)
         IN  [a |-> Arr(aSt, s.na, s.a), kids |-> kids[1], b |-> Arr(bSt, s.nb, s.b),
              extent |-> bSt + s.nb * s.b.size]

\* THE NODE'S ALIGNMENT IS DERIVED FROM THE TYPES, never from the values: the
\* greater of its record's and every element's its extent can hold, however
\* many of them the value carries (a count of zero included). The extent then
\* begins at the record's size rounded to the greater of the floor and that
\* element alignment, so an array aligned relative to the extent is aligned in
\* the region too. OldRule is the reversed witness: the node at its record's
\* alignment alone and the extent at the floor.
ElemAlign(s) == Max(s.a.align, Max(s.b.align, s.child.align))
NodeAlign(s) == IF OldRule THEN s.recAlign ELSE Max(s.recAlign, ElemAlign(s))
RecSize(s)  == s.recAlign * s.recUnits
Base(s)     == NodeAlign(s) * s.baseUnits
ExtentAt(s) == IF ExtentOverRecord THEN 0
               ELSE Up(RecSize(s), IF OldRule THEN Floor ELSE Max(Floor, ElemAlign(s)))

Init == /\ shape \in Shapes
        /\ layout = Walk(shape)
Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

\* ---- the rules ----

Arrays == <<layout.a, layout.b>> \o layout.kids
NonEmpty == { i \in 1..Len(Arrays) : Arrays[i].count > 0 }
End(r) == r.start + r.count * r.size
Abs(r) == Base(shape) + ExtentAt(shape) + r.start

TypeOK == /\ shape \in Shapes
          /\ layout.extent \in Nat

\* EVERY ARRAY SITS AT ITS ELEMENT'S ALIGNMENT, in the region, where a reader
\* points a typed pointer at it: the node's base and the extent's start are
\* both multiples of every element alignment the node's types can hold.
AlignedInRegion ==
    \A i \in NonEmpty : Abs(Arrays[i]) % Arrays[i].align = 0

\* NO TWO ARRAYS SHARE A BYTE: lists and maps are one population in the extent.
NoOverlap ==
    \A i, j \in NonEmpty : i # j =>
        End(Arrays[i]) <= Arrays[j].start \/ End(Arrays[j]) <= Arrays[i].start

\* EVERY ARRAY IS INSIDE THE EXTENT THE LAYOUT RESERVED, which begins after the
\* record's own storage: the measure and the write are one walk.
Contained ==
    \A i \in NonEmpty : End(Arrays[i]) <= layout.extent

\* NO ARRAY TOUCHES THE RECORD'S OWN STORAGE.
OutsideRecord ==
    \A i \in NonEmpty : Abs(Arrays[i]) >= Base(shape) + RecSize(shape)

\* PRE-ORDER: a container's whole array before the arrays its elements hold.
PreOrder ==
    \A i \in 3..Len(Arrays) : Arrays[i].count > 0 /\ layout.a.count > 0 =>
        Arrays[i].start >= End(layout.a)

\* THE SLOT RESOLVES: element i's child slot lives inside A's array (at the
\* element's start), and its self-relative delta lands on the child's array.
SlotResolves ==
    \A i \in 1..Len(layout.kids) : layout.kids[i].count > 0 =>
        LET slot  == Abs(layout.a) + (i - 1) * layout.a.size
            delta == Abs(layout.kids[i]) - slot
        IN  slot + delta = Abs(layout.kids[i]) /\ slot < Abs(layout.a) + layout.a.count * layout.a.size

=============================================================================
