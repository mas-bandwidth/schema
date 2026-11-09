package ci

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"
)

const hostedRunnerCondition = "runner.environment == 'github-hosted'"

// checkEverySetupBeamIsHostedOnly requires every erlef/setup-beam step under
// .github/workflows to carry the hosted-runner guard. setup-beam maps the
// target OS from ImageOS, which only a GitHub-hosted runner sets; on the
// fleet's schema runners the step dies during setup, before any control or
// conformance row runs. Those runners already carry OTP and Elixir on PATH,
// so the guard both fixes the fleet and leaves hosted rows unchanged.
func checkEverySetupBeamIsHostedOnly(wfs map[string]string) []string {
	var errs []string
	for name, data := range wfs {
		lines := strings.Split(data, "\n")
		for i, raw := range lines {
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			step := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if !strings.HasPrefix(step, "uses: erlef/setup-beam@") {
				continue
			}

			// Walk back to the step's opening dash so its `if:` is read from
			// this step and not from a neighbouring one.
			start := i
			for j := i; j >= 0; j-- {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "#") {
					continue
				}
				indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
				if indent == 6 && strings.HasPrefix(t, "- ") {
					start = j
					break
				}
				if indent < 6 {
					break
				}
			}

			cond := ""
			for j := start; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if j > start {
					indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
					if strings.HasPrefix(t, "- ") && indent <= 6 {
						break
					}
					if indent < 6 && t != "" {
						break
					}
				}
				if rest, ok := strings.CutPrefix(t, "if:"); ok {
					cond = strings.TrimSpace(rest)
					break
				}
			}
			if !strings.Contains(cond, hostedRunnerCondition) {
				errs = append(errs, fmt.Sprintf("%s:%d: erlef/setup-beam runs without if: %s (got %q): setup-beam needs ImageOS, which only a hosted runner sets", name, i+1, hostedRunnerCondition, cond))
			}
		}
	}
	sort.Strings(errs)
	return errs
}

// TestEverySetupBeamIsHostedOnly holds the fleet rule across every workflow:
// an erlef/setup-beam step is never allowed to run on a self-hosted runner.
func TestEverySetupBeamIsHostedOnly(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	steps := 0
	for _, data := range wfs {
		for raw := range strings.SplitSeq(data, "\n") {
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), "uses: erlef/setup-beam@") {
				steps++
			}
		}
	}
	if steps == 0 {
		t.Fatal("no erlef/setup-beam step under .github/workflows — this gate would pass over an empty set")
	}
	for _, err := range checkEverySetupBeamIsHostedOnly(wfs) {
		t.Error(err)
	}
	t.Logf("%d setup-beam steps gated", steps)
}

// TestSetupBeamHostedGateNegativeControls removes the guard from each of
// ci-full.yml's two formerly-ungated jobs and requires the check to name the
// step. A check that cannot fail on the exact shape this card fixes is not a
// gate.
func TestSetupBeamHostedGateNegativeControls(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	const wf = "ci-full.yml"
	base, ok := wfs[wf]
	if !ok {
		t.Fatalf("%s is the workflow these controls mutate", wf)
	}
	jobs := yamlJobs(base)
	for _, job := range []string{"conformance", "negative-controls"} {
		body, ok := jobs[job]
		if !ok {
			t.Fatalf("%s no longer carries the %s job", wf, job)
		}
		const gated = "        if: matrix.otp != '' && runner.environment == 'github-hosted'\n"
		if !strings.Contains(body, gated) {
			t.Fatalf("%s:%s no longer carries the hosted guard %q", wf, job, gated)
		}
		ungated := strings.Replace(body, gated, "        if: matrix.otp != ''\n", 1)
		mutated := maps.Clone(wfs)
		mutated[wf] = strings.Replace(base, body, ungated, 1)
		errs := checkEverySetupBeamIsHostedOnly(mutated)
		if len(errs) != 1 || !strings.Contains(errs[0], wf+":") || !strings.Contains(errs[0], "erlef/setup-beam runs without") {
			t.Errorf("%s:%s: removing the guard did not report exactly one ungated setup-beam; it reported %v", wf, job, errs)
		}
	}
}
