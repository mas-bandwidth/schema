------------------------------ MODULE PGroup ---------------------------------
\* The supervision policy of tools/pgroup (supervise.go's step): a command
\* runs as the leader of its own process group, may start a child that
\* outlives it (the `sq` a dead test leaves behind), and pgroup must leave no
\* member of the group alive once it is done, whichever way the command ends:
\* it exits, pgroup is asked to stop, or the wall clock runs out.
\*
\* The OS is the environment: the leader may die at any time, a forwarded
\* SIGTERM may or may not kill a member (a process may catch it), and only
\* SIGKILL is certain. A grace timer, once armed, fires once.

EXTENDS Naturals, TLC

\* The two reversed witnesses. The positive cfg holds both FALSE.
CONSTANTS KillOnlyLeader,  \* on the leader's exit, kill nothing more (the
                           \* bug the tool exists to fix: a plain exec)
          NoGraceKill      \* the grace timer never escalates to SIGKILL

VARIABLES phase,     \* "running", "stopping", "done" (supervise.go's phase)
          timedOut,  \* the wall ran out
          grace,     \* "unarmed", "armed", "fired"
          leader,    \* "alive", "dead": the command itself
          orphan     \* "none", "alive", "dead": a child the command started

vars == <<phase, timedOut, grace, leader, orphan>>

TypeOK ==
  /\ phase \in {"running", "stopping", "done"}
  /\ timedOut \in BOOLEAN
  /\ grace \in {"unarmed", "armed", "fired"}
  /\ leader \in {"alive", "dead"}
  /\ orphan \in {"none", "alive", "dead"}

Init ==
  /\ phase = "running"
  /\ timedOut = FALSE
  /\ grace = "unarmed"
  /\ leader = "alive"
  /\ orphan = "none"

\* SIGKILL to the group: every member dies.
KillGroup ==
  /\ leader' = "dead"
  /\ orphan' = IF orphan = "alive" THEN "dead" ELSE orphan

\* SIGTERM (or the forwarded stop) to the group: each member may die.
TermGroup ==
  /\ leader' \in {leader, "dead"}
  /\ orphan' \in IF orphan = "alive" THEN {"alive", "dead"} ELSE {orphan}

\* --- the environment

\* the command starts a child, which joins the group
Spawn ==
  /\ leader = "alive" /\ orphan = "none"
  /\ orphan' = "alive"
  /\ UNCHANGED <<phase, timedOut, grace, leader>>

\* the command ends on its own (or a signal it got finally lands)
LeaderDies ==
  /\ leader = "alive"
  /\ leader' = "dead"
  /\ UNCHANGED <<phase, timedOut, grace, orphan>>

\* --- pgroup's events, one per step() call

\* evExit: the leader was reaped
Exit ==
  /\ phase # "done" /\ leader = "dead"
  /\ phase' = "done"
  /\ IF KillOnlyLeader THEN UNCHANGED orphan ELSE KillGroup
  /\ UNCHANGED <<timedOut, grace>>
  /\ leader' = "dead"

\* evStop: SIGINT, SIGTERM, SIGHUP, or the parent went away
Stop ==
  /\ phase = "running"
  /\ phase' = "stopping" /\ grace' = "armed"
  /\ TermGroup
  /\ UNCHANGED timedOut

\* evWall
Wall ==
  /\ phase = "running"
  /\ phase' = "stopping" /\ grace' = "armed" /\ timedOut' = TRUE
  /\ TermGroup

\* evGrace: fires once
Grace ==
  /\ phase = "stopping" /\ grace = "armed"
  /\ grace' = "fired"
  /\ IF NoGraceKill THEN UNCHANGED <<leader, orphan>> ELSE KillGroup
  /\ UNCHANGED <<phase, timedOut>>

\* done is terminal: pgroup has exited and nothing more happens
Done == phase = "done" /\ UNCHANGED vars

Next == Spawn \/ LeaderDies \/ Exit \/ Stop \/ Wall \/ Grace \/ Done

\* pgroup's own events are fair; the environment's are not (a command may
\* run forever until a stop or the wall arrives).
Spec == Init /\ [][Next]_vars /\ WF_vars(Exit) /\ WF_vars(Grace)

\* NO SURVIVOR: once pgroup is done, nothing in the group is alive.
NoSurvivor == phase = "done" => (leader = "dead" /\ orphan # "alive")

\* A STOP ENDS: once stopping (a signal or the wall), pgroup finishes.
StopEnds == (phase = "stopping") ~> (phase = "done")
=============================================================================
