package ci

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// pgroupShell is the default shell every self-hosted job runs its steps
// under: tools/pgroup puts the step in a process group of its own and kills
// the whole group when the step ends, so a child a test left running (an
// `sq` outliving `go test -timeout`) cannot keep the step, and the runner,
// busy. `bash -e {0}` is what GitHub runs when a step names no shell.
const pgroupShell = "go run ./tools/pgroup -- bash -e {0}"

// maxFleetMinutes is the largest timeout a fleet job may carry. Every bound
// in the tree is sized from measured durations and is well under it; a job
// that needs more is a job to split, not a cap to raise.
const maxFleetMinutes = 60

// checkSelfHostedBounds holds every job that can run on a self-hosted runner
// to the two rules that keep a runner from being held: an explicit, bounded
// `timeout-minutes`, and steps that run under pgroupShell. A step may name
// its own shell only before the checkout (tools/pgroup is not there yet) or
// when it runs only on a hosted runner.
func checkSelfHostedBounds(wfs map[string]string) []string {
	var errs []string
	for _, sh := range enumerateSelfHostedJobs(wfs) {
		body := yamlJobs(wfs[sh.Workflow])[sh.Job]
		minutes, hasTimeout := "", false
		shell := ""
		inDefaults, inRun := false, false
		for raw := range strings.SplitSeq(body, "\n") {
			line := raw
			if before, _, ok := strings.Cut(line, " #"); ok {
				line = before
			}
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			indent := len(line) - len(strings.TrimLeft(line, " "))
			trimmed := strings.TrimSpace(line)
			if indent == 4 {
				inDefaults = trimmed == "defaults:"
				inRun = false
				if v, ok := strings.CutPrefix(trimmed, "timeout-minutes:"); ok {
					minutes, hasTimeout = strings.TrimSpace(v), true
				}
				continue
			}
			if inDefaults && indent == 6 {
				inRun = trimmed == "run:"
				continue
			}
			if inDefaults && inRun && indent == 8 {
				if v, ok := strings.CutPrefix(trimmed, "shell:"); ok {
					shell = strings.TrimSpace(v)
				}
			}
		}
		switch n, err := strconv.Atoi(minutes); {
		case !hasTimeout:
			errs = append(errs, fmt.Sprintf("%s has no timeout-minutes: a hung step holds its runner for up to six hours, and longer when the runner loses the job", sh.Key()))
		case err != nil:
			errs = append(errs, fmt.Sprintf("%s timeout-minutes %q is not a literal number of minutes", sh.Key(), minutes))
		case n <= 0 || n > maxFleetMinutes:
			errs = append(errs, fmt.Sprintf("%s timeout-minutes %d is outside 1..%d", sh.Key(), n, maxFleetMinutes))
		}
		if shell != pgroupShell {
			errs = append(errs, fmt.Sprintf("%s runs its steps under %q, not defaults.run.shell %q: a child its step leaves running outlives the step", sh.Key(), shell, pgroupShell))
		}
		errs = append(errs, shellOverrides(sh.Key(), body)...)
	}
	sort.Strings(errs)
	return errs
}

// shellOverrides names each step of a self-hosted job that names its own
// shell after the checkout without being hosted-only.
func shellOverrides(key, body string) []string {
	var errs []string
	type stepInfo struct {
		name, shell, cond string
		checkout          bool
	}
	var steps []stepInfo
	for raw := range strings.SplitSeq(body, "\n") {
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		trimmed := strings.TrimSpace(raw)
		if indent == 6 && strings.HasPrefix(trimmed, "- ") {
			steps = append(steps, stepInfo{})
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			indent = 8
		}
		if len(steps) == 0 || indent != 8 {
			continue
		}
		s := &steps[len(steps)-1]
		switch {
		case strings.HasPrefix(trimmed, "uses: actions/checkout@"):
			s.checkout = true
		case strings.HasPrefix(trimmed, "name:"):
			s.name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		case strings.HasPrefix(trimmed, "shell:"):
			s.shell = strings.TrimSpace(strings.TrimPrefix(trimmed, "shell:"))
		case strings.HasPrefix(trimmed, "if:"):
			s.cond = trimmed
		}
	}
	checkedOut := false
	for i, s := range steps {
		if s.shell != "" && s.shell != pgroupShell && checkedOut && !strings.Contains(s.cond, "runner.environment == 'github-hosted'") {
			errs = append(errs, fmt.Sprintf("%s step %d (%s) names shell %q after the checkout: it runs outside pgroup", key, i+1, s.name, s.shell))
		}
		if s.checkout {
			checkedOut = true
		}
	}
	return errs
}

// TestEverySelfHostedJobIsBounded: every job that can land on a self-hosted
// runner carries an explicit timeout and runs its steps in a process group
// pgroup kills, so no hung job or leftover child holds a fleet runner.
func TestEverySelfHostedJobIsBounded(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	jobs := enumerateSelfHostedJobs(wfs)
	if len(jobs) == 0 {
		t.Fatal("no self-hosted job found: this gate would pass over an empty set")
	}
	for _, e := range checkSelfHostedBounds(wfs) {
		t.Error(e)
	}
	t.Logf("%d self-hosted jobs bounded", len(jobs))
}

// TestSelfHostedBoundsNegativeControls breaks each rule in a copy of a real
// workflow and requires the check to name it.
func TestSelfHostedBoundsNegativeControls(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	const wf = "schema-squirrel-nightly-fuzz.yml"
	base, ok := wfs[wf]
	if !ok {
		t.Fatalf("%s is the workflow these controls mutate", wf)
	}
	cases := []struct {
		name, old, new, want string
	}{
		{"timeout removed", "    timeout-minutes: 20\n", "", "schema-squirrel-nightly-fuzz.yml:fuzz has no timeout-minutes"},
		{"timeout from an expression", "    timeout-minutes: 20\n", "    timeout-minutes: ${{ inputs.minutes }}\n", "is not a literal number of minutes"},
		{"timeout past the cap", "    timeout-minutes: 20\n", "    timeout-minutes: 600\n", "timeout-minutes 600 is outside"},
		{"default shell removed", "    timeout-minutes: 20\n    defaults:\n      run:\n        shell: " + pgroupShell + "\n", "    timeout-minutes: 20\n", "schema-squirrel-nightly-fuzz.yml:fuzz runs its steps under \"\""},
		{"a step opts out after checkout", "      - run: make schema-squirrel-nightly-fuzz\n", "      - run: make schema-squirrel-nightly-fuzz\n        shell: bash\n", "names shell \"bash\" after the checkout"},
	}
	for _, c := range cases {
		if !strings.Contains(base, c.old) {
			t.Fatalf("%s: %s no longer carries %q", c.name, wf, c.old)
		}
		mutated := maps.Clone(wfs)
		mutated[wf] = strings.Replace(base, c.old, c.new, 1)
		errs := strings.Join(checkSelfHostedBounds(mutated), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("%s: the check did not report %q; it reported:\n%s", c.name, c.want, errs)
		}
	}
}
