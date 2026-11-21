package ci

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"
)

func pythonLegDispatchOnlyErrors(content string) []string {
	triggers := workflowTriggerBranches(content)
	if len(triggers) == 0 {
		return []string{"python-leg.yml declares no workflow triggers"}
	}
	var names []string
	for ev := range triggers {
		names = append(names, ev)
	}
	sort.Strings(names)
	var errs []string
	for _, ev := range names {
		if ev != "workflow_dispatch" {
			errs = append(errs, fmt.Sprintf("python-leg.yml triggers on %s; ci-fast.yml covers per-PR/push fast gates and certify.yml/ci-full.yml/python-leg-nightly.yml cover full Python certification, so python-leg.yml must be workflow_dispatch only", ev))
		}
	}
	if _, ok := triggers["workflow_dispatch"]; !ok {
		errs = append(errs, "python-leg.yml must keep workflow_dispatch for manual partition inspection")
	}
	return errs
}

// TestPythonLegWorkflowIsDispatchOnly prevents python-leg.yml from spawning
// 450 duplicate jobs on every pull_request or push to main.
func TestPythonLegWorkflowIsDispatchOnly(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	wf, ok := wfs["python-leg.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/python-leg.yml")
	}
	if errs := pythonLegDispatchOnlyErrors(wf); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}

	for _, tc := range []struct {
		name    string
		mutated string
	}{
		{
			name:    "pull_request trigger reintroduced",
			mutated: strings.Replace(wf, "\non:\n  workflow_dispatch:\n", "\non:\n  pull_request:\n  workflow_dispatch:\n", 1),
		},
		{
			name:    "push trigger reintroduced",
			mutated: strings.Replace(wf, "\non:\n  workflow_dispatch:\n", "\non:\n  push:\n    branches: [ main ]\n  workflow_dispatch:\n", 1),
		},
		{
			name:    "schedule trigger reintroduced",
			mutated: strings.Replace(wf, "\non:\n  workflow_dispatch:\n", "\non:\n  schedule:\n    - cron: '0 0 * * *'\n  workflow_dispatch:\n", 1),
		},
	} {
		if tc.mutated == wf {
			t.Fatalf("negative control %q did not mutate python-leg.yml", tc.name)
		}
		if errs := pythonLegDispatchOnlyErrors(tc.mutated); len(errs) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %q left pythonLegDispatchOnlyErrors green", tc.name)
		}
		wfsCopy := maps.Clone(wfs)
		wfsCopy["python-leg.yml"] = tc.mutated
		if tc.name != "schedule trigger reintroduced" && len(nightlyOnlyErrors(wfsCopy)) == 0 {
			t.Errorf("NEGATIVE CONTROL FAILED: %q left nightlyOnlyErrors green", tc.name)
		}
	}
}
