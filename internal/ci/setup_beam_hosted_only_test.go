package ci

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"
)

const hostedRunnerCondition = "runner.environment == 'github-hosted'"

// checkEverySetupBeamIsHostedOnly requires every erlef/setup-beam step in
// the workflows it is given to carry the hosted-runner guard. setup-beam maps
// the target OS from ImageOS, which only a GitHub-hosted runner sets; on the
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

// setupBeamExemptSteps names, by workflow and by the line of its `uses:`,
// the erlef/setup-beam steps this card may not change — leg.yml and
// certify.yml are outside its PATHS — but which still predate the
// hosted-runner rule. They are owed the same guard by the streams that own
// them: leg.yml:130 gates only on matrix.otp, and certify.yml:177,414,584,868
// are ungated or gate only on matrix.otp. The exemption is step-scoped, not
// file-scoped (setup_java_test.go's ci-fast/ci-full exemption is the model
// for naming it at all): every other step — in any other workflow, in a
// workflow added later, or a new step in these two files — is still held to
// the guard, so this cannot hide a new ungated step. When an owning stream
// gates one of these steps, delete its line here; the gate fails on a stale
// exemption so the list cannot outlive the steps it names.
var setupBeamExemptSteps = map[string][]int{
	"leg.yml":     {130},
	"certify.yml": {177, 414, 584, 868},
}

// nonExemptSetupBeamErrorsFrom drops only the errors naming an exempt
// step above.
func nonExemptSetupBeamErrorsFrom(all []string) []string {
	var errs []string
	for _, err := range all {
		exempt := false
		for name, lines := range setupBeamExemptSteps {
			for _, line := range lines {
				if strings.HasPrefix(err, fmt.Sprintf("%s:%d:", name, line)) {
					exempt = true
				}
			}
		}
		if !exempt {
			errs = append(errs, err)
		}
	}
	return errs
}

// nonExemptSetupBeamErrors runs the check over every workflow and drops
// only the errors naming an exempt step above.
func nonExemptSetupBeamErrors(wfs map[string]string) []string {
	return nonExemptSetupBeamErrorsFrom(checkEverySetupBeamIsHostedOnly(wfs))
}

// TestEverySetupBeamIsHostedOnly holds the fleet rule across every workflow
// under .github/workflows: every erlef/setup-beam step — in particular
// ci-full.yml's conformance and negative-controls jobs, which run on the
// fleet's self-hosted schema runners — is gated on a hosted runner, so no
// workflow can add an ungated setup-beam again. The only exceptions are the
// steps named in setupBeamExemptSteps, outside this card's PATHS.
func TestEverySetupBeamIsHostedOnly(t *testing.T) {
	t.Parallel()
	all := workflows(t, repoRoot(t))
	steps := 0
	for _, data := range all {
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
	raw := checkEverySetupBeamIsHostedOnly(all)
	for name, lines := range setupBeamExemptSteps {
		for _, line := range lines {
			found := false
			for _, err := range raw {
				if strings.HasPrefix(err, fmt.Sprintf("%s:%d:", name, line)) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s:%d: exempt setup-beam step is no longer ungated — remove it from setupBeamExemptSteps", name, line)
			}
		}
	}
	for _, err := range nonExemptSetupBeamErrors(all) {
		t.Error(err)
	}
	exempt := 0
	for _, lines := range setupBeamExemptSteps {
		exempt += len(lines)
	}
	t.Logf("%d setup-beam steps across %d workflows (%d exempt steps)", steps, len(all), exempt)
}

// TestSetupBeamExemptionIsStepScoped proves the exemption cannot hide what
// it does not name: an ungated step in a new workflow, and an ungated step
// at a new line of an exempt workflow, both still report.
func TestSetupBeamExemptionIsStepScoped(t *testing.T) {
	t.Parallel()
	errs := []string{
		"leg.yml:130: erlef/setup-beam runs without",
		"leg.yml:999: erlef/setup-beam runs without",
		"brand-new.yml:12: erlef/setup-beam runs without",
	}
	kept := nonExemptSetupBeamErrorsFrom(errs)
	if len(kept) != 2 || !strings.HasPrefix(kept[0], "leg.yml:999:") || !strings.HasPrefix(kept[1], "brand-new.yml:12:") {
		t.Errorf("step-scoped exemption kept %v, want leg.yml:999 and brand-new.yml:12", kept)
	}
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
		errs := nonExemptSetupBeamErrors(mutated)
		if len(errs) != 1 || !strings.Contains(errs[0], wf+":") || !strings.Contains(errs[0], "erlef/setup-beam runs without") {
			t.Errorf("%s:%s: removing the guard did not report exactly one ungated setup-beam; it reported %v", wf, job, errs)
		}
	}
}
