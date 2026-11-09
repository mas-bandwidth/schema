package main

import (
	"strings"
	"testing"
	"time"
)

func TestWallBudget(t *testing.T) {
	flagWall := 5 * time.Minute
	cases := []struct {
		name      string
		flagWall  time.Duration
		env       string
		wantD     time.Duration
		wantLabel string
		wantCI    bool
		wantErr   bool
	}{
		{"no flag, no env: no wall", 0, "", 0, "", false, false},
		{"flag only", flagWall, "", flagWall, "", false, false},
		{"env only: the per-commit wall", 0, "110s", PerCommitWall, "110s", true, false},
		{"flag beats env", flagWall, "110s", flagWall, "", false, false},
		{"bad env is refused, not ignored", 0, "not-a-duration", 0, "", false, true},
		{"negative env is refused", 0, "-5s", 0, "", false, true},
	}
	for _, c := range cases {
		d, label, ci, err := wallBudget(c.flagWall, c.env)
		if d != c.wantD || label != c.wantLabel || ci != c.wantCI || (err != nil) != c.wantErr {
			t.Errorf("%s: wallBudget(%v, %q) = (%v, %q, %v, %v); want (%v, %q, %v, err=%v)",
				c.name, c.flagWall, c.env, d, label, ci, err, c.wantD, c.wantLabel, c.wantCI, c.wantErr)
		}
	}
}

// TestPerCommitWallWithInjectedClock drives the policy with time as an
// argument, starting at 1000 (never 0): before 110s nothing happens; at the
// budget the step is stopped, the exit code is 124, and the message names
// the CI rule and the step.
func TestPerCommitWallWithInjectedClock(t *testing.T) {
	start := time.Unix(1000, 0)
	if expired(start, start.Add(PerCommitWall-time.Second), PerCommitWall) {
		t.Error("one second before the 110s budget the wall must not fire")
	}
	if !expired(start, start.Add(PerCommitWall), PerCommitWall) {
		t.Fatal("at the 110s budget the wall must fire")
	}
	if expired(start, start.Add(time.Hour), 0) {
		t.Error("no wall (schedule/dispatch) must never expire")
	}

	// The fired wall is the policy's evWall: it reports, terminates and arms
	// grace, and the run exits 124 whatever the command would have returned.
	s, acts := step(state{phase: phaseRunning}, evWall)
	if !s.timedOut || s.phase != phaseStopping {
		t.Errorf("step past the budget = %+v, want stopping with timedOut", s)
	}
	foundReport := false
	for _, a := range acts {
		if a == actReportWall {
			foundReport = true
		}
	}
	if !foundReport {
		t.Errorf("step past the budget actions = %v, want actReportWall among them", acts)
	}
	if got := exitCode(state{phase: phaseDone, timedOut: s.timedOut}, 0); got != 124 {
		t.Errorf("exit code past the budget = %d, want 124", got)
	}

	msg := ciMessage("go test the emitter packages", "110s", PerCommitWall)
	for _, want := range []string{"CI rule", "step 'go test the emitter packages'", "110s", "per-commit jobs finish in 2m", "Split it into rows", "certify.yml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("CI message %q does not name %q", msg, want)
		}
	}
}

func TestStepName(t *testing.T) {
	t.Setenv("PGROUP_STEP_NAME", "")
	t.Setenv("GITHUB_JOB", "")
	if got := stepName([]string{"/bin/sh"}); got != "/bin/sh" {
		t.Errorf("stepName with no env = %q, want the command name", got)
	}
	t.Setenv("GITHUB_JOB", "go-test")
	if got := stepName([]string{"/bin/sh"}); got != "go-test" {
		t.Errorf("stepName with GITHUB_JOB = %q, want the job id", got)
	}
	t.Setenv("PGROUP_STEP_NAME", "go test the emitter packages")
	if got := stepName([]string{"/bin/sh"}); got != "go test the emitter packages" {
		t.Errorf("stepName with PGROUP_STEP_NAME = %q, want the override", got)
	}
}

func TestWallSpec(t *testing.T) {
	if got := wallSpec("110s", PerCommitWall); got != "110s" {
		t.Errorf("wallSpec(110s, ...) = %q, want the PGROUP_WALL label", got)
	}
	if got := wallSpec("", 5*time.Minute); got != "5m0s" {
		t.Errorf("wallSpec(\"\", 5m) = %q, want the duration form", got)
	}
}
