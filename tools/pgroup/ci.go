package main

import (
	"fmt"
	"os"
	"time"
)

// PerCommitWall is the per-step budget a per-commit (push or pull_request)
// CI run gives every step: per-commit jobs finish in two minutes, so a step
// gets 110s and the remaining seconds are the runner's own overhead. A
// schedule or workflow_dispatch run sets no PGROUP_WALL and keeps the long
// budget its job timeout-minutes names.
const PerCommitWall = 110 * time.Second

// wallBudget resolves the effective wall: the -wall flag when set, else the
// PGROUP_WALL environment value CI sets. ciPolicy reports whether the budget
// came from the environment (a CI-configured per-step wall): only then does
// the wall message name the CI rule. label is the PGROUP_WALL string, which
// reads better in the message ("110s") than the duration's String ("1m50s").
func wallBudget(flagWall time.Duration, env string) (d time.Duration, label string, ciPolicy bool, err error) {
	if flagWall != 0 {
		return flagWall, "", false, nil
	}
	if env == "" {
		return 0, "", false, nil
	}
	parsed, err := time.ParseDuration(env)
	if err != nil {
		return 0, "", false, fmt.Errorf("bad PGROUP_WALL %q: %w", env, err)
	}
	if parsed < 0 {
		return 0, "", false, fmt.Errorf("bad PGROUP_WALL %q: negative", env)
	}
	return parsed, env, true, nil
}

// expired is the wall decision with time as an argument: it reports whether
// now has reached start+wall. A non-positive wall never expires. The OS loop
// arms a timer for the same decision; this pure form is what the tests drive
// with an injected clock.
func expired(start, now time.Time, wall time.Duration) bool {
	if wall <= 0 {
		return false
	}
	return !now.Before(start.Add(wall))
}

// stepName is what the CI-rule message calls the step that outran its wall:
// the PGROUP_STEP_NAME override when set, else the GitHub job id
// (GITHUB_JOB), else the command's own name.
func stepName(argv []string) string {
	if name := os.Getenv("PGROUP_STEP_NAME"); name != "" {
		return name
	}
	if job := os.Getenv("GITHUB_JOB"); job != "" {
		return job
	}
	if len(argv) > 0 {
		return argv[0]
	}
	return "?"
}

// ciMessage is the actionable line a CI wall prints: it names the rule, the
// step and the budget, and says what to do instead.
func ciMessage(step, label string, wall time.Duration) string {
	return fmt.Sprintf("::error::CI rule: step '%s' ran past %s; per-commit jobs finish in 2m. Split it into rows, cache it, or move it to the nightly (certify.yml).", step, wallSpec(label, wall))
}

// wallSpec is the human-readable budget the wall message names.
func wallSpec(label string, wall time.Duration) string {
	if label != "" {
		return label
	}
	return wall.String()
}
