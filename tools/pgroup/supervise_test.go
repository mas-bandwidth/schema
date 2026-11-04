package main

import (
	"slices"
	"testing"
)

// TestStep walks every (phase, event) pair of the policy. It reads no clock
// and starts no process: the events are values.
func TestStep(t *testing.T) {
	running := state{phase: phaseRunning}
	stopping := state{phase: phaseStopping}
	walled := state{phase: phaseStopping, timedOut: true}
	done := state{phase: phaseDone}
	cases := []struct {
		name string
		from state
		ev   event
		to   state
		acts []action
	}{
		{"exit while running kills the group", running, evExit, done, []action{actKillGroup}},
		{"a stop is forwarded and arms grace", running, evStop, stopping, []action{actForward, actStartGrace}},
		{"the wall is named, terminates and arms grace", running, evWall, walled, []action{actReportWall, actTerm, actStartGrace}},
		{"no grace is armed while running", running, evGrace, running, nil},
		{"exit while stopping kills the group", stopping, evExit, done, []action{actKillGroup}},
		{"exit after the wall keeps the timeout", walled, evExit, state{phase: phaseDone, timedOut: true}, []action{actKillGroup}},
		{"grace escalates to SIGKILL", stopping, evGrace, stopping, []action{actKillGroup}},
		{"a second stop changes nothing", stopping, evStop, stopping, nil},
		{"the wall while stopping changes nothing", stopping, evWall, stopping, nil},
		{"done is terminal", done, evStop, done, nil},
	}
	for _, c := range cases {
		to, acts := step(c.from, c.ev)
		if to != c.to || !slices.Equal(acts, c.acts) {
			t.Errorf("%s: step(%+v, %d) = %+v, %v; want %+v, %v", c.name, c.from, c.ev, to, acts, c.to, c.acts)
		}
	}
}

// TestEveryWayOutKillsTheGroup is tla/PGroup.tla's NoSurvivor over the Go
// function: from every reachable state, every event sequence that reaches
// phaseDone does so through an actKillGroup on the transition into it.
func TestEveryWayOutKillsTheGroup(t *testing.T) {
	events := []event{evExit, evStop, evWall, evGrace}
	seen := map[state]bool{}
	frontier := []state{{phase: phaseRunning}}
	for len(frontier) > 0 {
		s := frontier[0]
		frontier = frontier[1:]
		if seen[s] {
			continue
		}
		seen[s] = true
		for _, e := range events {
			to, acts := step(s, e)
			if s.phase != phaseDone && to.phase == phaseDone && !slices.Contains(acts, actKillGroup) {
				t.Errorf("step(%+v, %d) reaches done without killing the group: %v", s, e, acts)
			}
			frontier = append(frontier, to)
		}
	}
	if len(seen) != 5 {
		t.Errorf("reachable states = %d, want 5 (running, stopping, stopping after the wall, done, done after the wall)", len(seen))
	}
}

func TestExitCode(t *testing.T) {
	if got := exitCode(state{phase: phaseDone}, 3); got != 3 {
		t.Errorf("exit code = %d, want the command's 3", got)
	}
	if got := exitCode(state{phase: phaseDone, timedOut: true}, 143); got != 124 {
		t.Errorf("exit code after the wall = %d, want 124", got)
	}
}
