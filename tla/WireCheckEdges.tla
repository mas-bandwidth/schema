--------------------------- MODULE WireCheckEdges ---------------------------
(***************************************************************************)
(* THE LAYOUT OF THE LUA WIRE CHECK'S POINTER LIST (wire_check in          *)
(* internal/codegen/luatable/runtime/load.lua, a port of                   *)
(* internal/tablewire/wirecheck.go). The abstract rules the check holds    *)
(* are ReadOrder.tla's and Framing.tla's; this model carries the port's    *)
(* own index arithmetic, which those cannot see.                           *)
(*                                                                         *)
(* The Go walk keeps one slice of pointers per node. The Lua port keeps    *)
(* ONE flat array (`edges`) that the loader state owns and reuses across   *)
(* checks without clearing it, a count `ne` of the entries this check      *)
(* wrote, and per node k the range first[k] .. last[k] of its pointers.    *)
(* The root is node 1 and record r is node r + 1. Each check opens node k  *)
(* at first[k] = ne + 1, appends its pointers, and closes it at            *)
(* last[k] = ne; the graph then counts names over edges[1 .. ne] and walks *)
(* node k's pointers over its range.                                       *)
(*                                                                         *)
(* The array starts with stale entries, and each later check reuses what   *)
(* the one before it left, so a reader that runs past `ne` reads pointers  *)
(* this file does not hold.                                                *)
(*                                                                         *)
(* Invariants:                                                             *)
(*   RangesHoldTheNodes - once laid, each node's range holds exactly the   *)
(*     pointers its body carries, in wire order;                           *)
(*   NamesCountThisFile - the name count over the array equals the count   *)
(*     of every pointer the file carries.                                  *)
(*                                                                         *)
(* Reversed witnesses (each one fault TRUE, each must find a trace):       *)
(*   FirstOffByOne - a node opens at ne instead of ne + 1;                 *)
(*   StaleTail     - names are counted over the whole array, not to ne.    *)
(***************************************************************************)
EXTENDS Naturals, Sequences

CONSTANTS MaxNodes, MaxOut, FirstOffByOne, StaleTail

Cap == MaxNodes * MaxOut
\* a pointer's wire index: 0 is null, 1 .. MaxNodes a node, one past the table
Index == 0 .. (MaxNodes + 1)
OutSeqs == UNION {[1 .. m -> Index] : m \in 0 .. MaxOut}

VARIABLES
  edges,  \* the reused flat array, 1 .. Cap
  ne,     \* the entries this check wrote
  first,  \* node -> the first entry of its range
  last,   \* node -> the last entry of its range
  kids,   \* the abstract pointers of each node, in wire order
  nn,     \* the nodes this check lays
  k,      \* the node being laid (0 before the first)
  e,      \* the pointers of node k laid so far
  phase   \* "closed" (between nodes), "open" (laying node k) or "judge"

vars == <<edges, ne, first, last, kids, nn, k, e, phase>>

TypeOK ==
  /\ edges \in [1 .. Cap -> Index]
  /\ ne \in 0 .. Cap
  /\ first \in [1 .. MaxNodes -> 0 .. (Cap + 1)]
  /\ last \in [1 .. MaxNodes -> 0 .. Cap]
  /\ nn \in 1 .. MaxNodes
  /\ k \in 0 .. MaxNodes
  /\ e \in 0 .. MaxOut
  /\ phase \in {"closed", "open", "judge"}

\* a fresh check over the storage it is handed: the array keeps what it held
Fresh(stale, n, ks) ==
  /\ edges = stale
  /\ ne = 0
  /\ first = [j \in 1 .. MaxNodes |-> 0]
  /\ last = [j \in 1 .. MaxNodes |-> 0]
  /\ kids = [j \in 1 .. MaxNodes |-> IF j <= n THEN ks[j] ELSE << >>]
  /\ nn = n
  /\ k = 0
  /\ e = 0
  /\ phase = "closed"

\* the first check's stale entries name the root; every later check finds what
\* the one before it left
Init ==
  \E n \in 1 .. MaxNodes, ks \in [1 .. MaxNodes -> OutSeqs] :
    Fresh([i \in 1 .. Cap |-> 1], n, ks)

\* open the next node: first[k] = ne + 1
Open ==
  /\ phase = "closed"
  /\ k < nn
  /\ k' = k + 1
  /\ e' = 0
  /\ first' = [first EXCEPT ![k + 1] = IF FirstOffByOne THEN ne ELSE ne + 1]
  /\ phase' = "open"
  /\ UNCHANGED <<edges, ne, last, kids, nn>>

\* append node k's next pointer at ne + 1
Push ==
  /\ phase = "open"
  /\ e < Len(kids[k])
  /\ edges' = [edges EXCEPT ![ne + 1] = kids[k][e + 1]]
  /\ ne' = ne + 1
  /\ e' = e + 1
  /\ UNCHANGED <<first, last, kids, nn, k, phase>>

\* close node k at the last entry it wrote: last[k] = ne
Close ==
  /\ phase = "open"
  /\ e = Len(kids[k])
  /\ last' = [last EXCEPT ![k] = ne]
  /\ phase' = "closed"
  /\ UNCHANGED <<edges, ne, first, kids, nn, k, e>>

Judge ==
  /\ phase = "closed"
  /\ k = nn
  /\ phase' = "judge"
  /\ UNCHANGED <<edges, ne, first, last, kids, nn, k, e>>

\* the next check reuses the array as this one left it
Again ==
  /\ phase = "judge"
  /\ \E n \in 1 .. MaxNodes, ks \in [1 .. MaxNodes -> OutSeqs] :
       /\ edges' = edges
       /\ ne' = 0
       /\ first' = [j \in 1 .. MaxNodes |-> 0]
       /\ last' = [j \in 1 .. MaxNodes |-> 0]
       /\ kids' = [j \in 1 .. MaxNodes |-> IF j <= n THEN ks[j] ELSE << >>]
       /\ nn' = n
       /\ k' = 0
       /\ e' = 0
       /\ phase' = "closed"

Next == Open \/ Push \/ Close \/ Judge \/ Again

Spec == Init /\ [][Next]_vars

\* the range of node j, read the way wc_graph reads it
RangeOf(j) ==
  IF last[j] < first[j] THEN << >>
  ELSE [i \in 1 .. (last[j] - first[j] + 1) |->
          IF first[j] + i - 1 \in 1 .. Cap THEN edges[first[j] + i - 1] ELSE "outside"]

RangesHoldTheNodes ==
  phase = "judge" => \A j \in 1 .. nn : RangeOf(j) = kids[j]

RECURSIVE CountIn(_, _, _)
CountIn(s, x, i) ==
  IF i > Len(s) THEN 0 ELSE (IF s[i] = x THEN 1 ELSE 0) + CountIn(s, x, i + 1)

RECURSIVE SumOver(_, _, _)
SumOver(x, j, n) == IF j > n THEN 0 ELSE CountIn(kids[j], x, 1) + SumOver(x, j + 1, n)

Top == IF StaleTail THEN Cap ELSE ne

NamesCountThisFile ==
  phase = "judge" =>
    \A x \in 1 .. MaxNodes :
      Len(SelectSeq([i \in 1 .. Top |-> edges[i]], LAMBDA y : y = x)) = SumOver(x, 1, nn)
=============================================================================
