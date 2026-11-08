package main

// The supervision policy, as a pure transition function over the events the
// OS loop delivers. It reads no clock and sends no signal: the loop in
// group_unix.go and group_windows.go turns each returned action into a
// syscall, and supervise_test.go walks every transition without either. The
// state machine is tla/PGroup.tla; its NoSurvivor invariant is the reason
// every path into phaseDone carries actKillGroup.

// event is one thing the OS loop observed.
type event int

const (
	// evExit: the command (the group's leader) exited and was reaped.
	evExit event = iota
	// evStop: pgroup was asked to stop — SIGINT, SIGTERM or SIGHUP arrived,
	// or the process that started pgroup went away.
	evStop
	// evWall: the -wall budget ran out.
	evWall
	// evGrace: the grace period after a stop or a wall ran out.
	evGrace
)

// action is one thing the OS loop must do, in order.
type action int

const (
	// actForward: send the stop signal to the whole group.
	actForward action = iota
	// actTerm: send SIGTERM to the whole group.
	actTerm
	// actStartGrace: arm the grace timer.
	actStartGrace
	// actKillGroup: SIGKILL the whole group.
	actKillGroup
	// actReportWall: name the budget that ran out on stderr.
	actReportWall
)

type phase int

const (
	phaseRunning phase = iota
	phaseStopping
	phaseDone
)

// state is everything the policy remembers.
type state struct {
	phase    phase
	timedOut bool
}

// step is the whole policy. Every path into phaseDone kills the group: the
// leader exiting is exactly the moment a child it started (an `sq` the test
// spawned, a compiler a make recipe left behind) becomes an orphan, and an
// orphan holding the job's output pipe keeps the runner busy. Once
// stopping, a second stop or the wall changes nothing; the grace timer is
// what escalates.
func step(s state, e event) (state, []action) {
	switch s.phase {
	case phaseRunning:
		switch e {
		case evExit:
			return state{phase: phaseDone, timedOut: s.timedOut}, []action{actKillGroup}
		case evStop:
			return state{phase: phaseStopping, timedOut: s.timedOut}, []action{actForward, actStartGrace}
		case evWall:
			return state{phase: phaseStopping, timedOut: true}, []action{actReportWall, actTerm, actStartGrace}
		case evGrace:
			// no grace timer is armed while running; nothing to do
			return s, nil
		}
	case phaseStopping:
		switch e {
		case evExit:
			return state{phase: phaseDone, timedOut: s.timedOut}, []action{actKillGroup}
		case evGrace:
			return s, []action{actKillGroup}
		case evStop, evWall:
			return s, nil
		}
	}
	return s, nil
}

// exitCode is what pgroup exits with: 124 when the wall ran out (the
// convention timeout(1) set), else the command's own status.
func exitCode(s state, command int) int {
	if s.timedOut {
		return 124
	}
	return command
}
