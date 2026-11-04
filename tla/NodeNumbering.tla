---------------------------- MODULE NodeNumbering ----------------------------
\* The node NUMBERING of the write algorithm (docs/SPEC-TABLES.md §3.1, §22.3):
\* the first-visit order of one depth-first pre-order walk from the root over
\* pointer edges, each body's slots in declaration order, a node numbered the
\* moment it is first reached and descended into before the walk takes the
\* next slot of the body it came from, a share taking the index it already
\* has, and a pointer to a node still OPEN on the walk's path refused as a
\* cycle.
\*
\* The walk is modelled the way a port writes it, as a loop over an explicit
\* stack of (node, next slot) frames, and checked against the RECURSIVE
\* definition the spec states. Every graph over Nodes with Slots ordered slots
\* a node is chosen at Init, so the check covers every shape of that size:
\* trees, diamonds, shares reached from either side, self loops, cycles, and
\* nodes the root never reaches.
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    N,            \* nodes are 1..N, and node 1 is the root
    Slots,        \* each node's pointer slots, in declaration order
    Reverse,      \* WITNESS: a body's slots taken last to first
    BreadthFirst  \* WITNESS: the oldest frame taken first, a queue for a stack

Nodes == 1..N
Null == 0

VARIABLES
    children,  \* children[n][i]: the node slot i of n names, or Null
    stack,     \* the walk's frames, <<node, next slot>>, the top LAST
    num,       \* num[n]: n's index, 0 while unnumbered
    next,      \* the next index the walk hands out
    open,      \* the nodes on the walk's path
    status     \* "run", "done" or "cycle"

vars == <<children, stack, num, next, open, status>>

SlotOrder(i) == IF Reverse THEN Slots + 1 - i ELSE i

Init ==
    /\ children \in [Nodes -> [1..Slots -> Nodes \cup {Null}]]
    /\ stack = << <<1, 1>> >>
    /\ num = [n \in Nodes |-> IF n = 1 THEN 1 ELSE 0]
    /\ next = 2
    /\ open = {1}
    /\ status = "run"

\* the frame the walk works on: the top of the stack, or for the witness the
\* bottom, which turns the stack into a queue and the walk breadth first
At == IF BreadthFirst THEN 1 ELSE Len(stack)

Finish ==
    /\ status = "run"
    /\ stack = << >>
    /\ status' = "done"
    /\ UNCHANGED <<children, stack, num, next, open>>

\* a body whose slots are all taken: its node closes
Close ==
    /\ status = "run"
    /\ stack # << >>
    /\ stack[At][2] > Slots
    /\ stack' = [k \in 1..Len(stack) - 1 |-> IF k < At THEN stack[k] ELSE stack[k + 1]]
    /\ open' = open \ {stack[At][1]}
    /\ UNCHANGED <<children, num, next, status>>

\* take the frame's next slot
Take ==
    /\ status = "run"
    /\ stack # << >>
    /\ stack[At][2] <= Slots
    /\ LET n == stack[At][1]
           i == stack[At][2]
           c == children[n][SlotOrder(i)]
           advanced == [stack EXCEPT ![At] = <<n, i + 1>>]
       IN  CASE c = Null ->
                    /\ stack' = advanced
                    /\ UNCHANGED <<num, next, open, status>>
             [] c \in open ->
                    \* a pointer to an OPEN node is a cycle, refused at save
                    /\ status' = "cycle"
                    /\ stack' = advanced
                    /\ UNCHANGED <<num, next, open>>
             [] num[c] # 0 ->
                    \* a SHARE: the index it already has, and no descent
                    /\ stack' = advanced
                    /\ UNCHANGED <<num, next, open, status>>
             [] OTHER ->
                    \* first visit: number it NOW, then descend into it
                    /\ num' = [num EXCEPT ![c] = next]
                    /\ next' = next + 1
                    /\ open' = open \cup {c}
                    /\ stack' = Append(advanced, <<c, 1>>)
                    /\ UNCHANGED status
    /\ UNCHANGED children

Next == Finish \/ Close \/ Take

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

-----------------------------------------------------------------------------
\* THE DEFINITION the spec states, recursively: visit a node's slots in
\* declaration order, and number and descend into each node at its first
\* reaching. Shares and back edges take nothing (a cycle is the refusal
\* below, and the recursion terminates because a numbered node is never
\* entered twice).
MaxIndex(f) == CHOOSE m \in {f[n] : n \in Nodes} : \A n \in Nodes : f[n] <= m

RECURSIVE VisitSlots(_, _, _)
VisitSlots(n, i, f) ==
    IF i > Slots THEN f
    ELSE LET c == children[n][i]
         IN  IF c = Null \/ f[c] # 0
             THEN VisitSlots(n, i + 1, f)
             ELSE VisitSlots(n, i + 1,
                      VisitSlots(c, 1, [f EXCEPT ![c] = MaxIndex(f) + 1]))

PreOrder == VisitSlots(1, 1, [n \in Nodes |-> IF n = 1 THEN 1 ELSE 0])

\* the nodes the root reaches over pointer edges, and whether a cycle is among
\* them (a reached node that reaches itself)
Step(S) == S \cup ({children[n][i] : n \in S, i \in 1..Slots} \ {Null})
RECURSIVE Closure(_, _)
Closure(S, k) == IF k = 0 THEN S ELSE Closure(Step(S), k - 1)
Reach(n) == Closure({n}, N)
Reached == Reach(1)
ReachesItself(n) == n \in Closure({children[n][i] : i \in 1..Slots} \ {Null}, N)
HasCycle == \E n \in Reached : ReachesItself(n)

-----------------------------------------------------------------------------
TypeOK ==
    /\ num \in [Nodes -> 0..N]
    /\ next \in 2..N + 1
    /\ open \subseteq Nodes
    /\ status \in {"run", "done", "cycle"}

\* the walk numbers exactly the definition's order
MatchesPreOrder == status = "done" => num = PreOrder

\* one index, one node: the indices are 1..next-1, each once, and exactly the
\* reached nodes hold one
OneIndexOneNode ==
    status = "done" =>
        /\ {num[n] : n \in {m \in Nodes : num[m] # 0}} = 1..next - 1
        /\ \A a, b \in Nodes : num[a] # 0 /\ num[a] = num[b] => a = b
        /\ {n \in Nodes : num[n] # 0} = Reached

\* a save finishes exactly when the graph it reaches is acyclic
CycleIsRefused ==
    /\ status = "done" => ~HasCycle
    /\ status = "cycle" => HasCycle

\* every walk ends: done or refused, never stuck
Ends == <>(status # "run")
=============================================================================
