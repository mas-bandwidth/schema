package ci

// A WORKFLOW'S OR A JOB'S `env:` MAY NAME ONLY THE CONTEXTS GITHUB ALLOWS THERE.
// GitHub checks this when it parses the file: a job-level env naming
// ${{ runner.temp }} made the whole of ci-full.yml invalid, so the run failed
// in 0 s with a workflow file issue and no job started (PR #4291, 2026-11-16).
// A nightly broken that way never runs at all, and nothing reads red but the
// run list. This reads the workflow text only (actionlint is not pinned in the
// tree), against GitHub's context availability table:
//
//	env (workflow)     github, inputs, secrets, vars
//	jobs.<id>.env      github, inputs, matrix, needs, secrets, strategy, vars
//
// A step's env may name runner, env, job and steps too; it is not checked here.

import (
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	workflowEnvContexts = map[string]bool{"github": true, "inputs": true, "secrets": true, "vars": true}
	jobEnvContexts      = map[string]bool{"github": true, "inputs": true, "matrix": true, "needs": true, "secrets": true, "strategy": true, "vars": true}

	expression   = regexp.MustCompile(`\$\{\{(.*?)\}\}`)
	contextUse   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.'"-])(github|inputs|secrets|vars|matrix|needs|strategy|runner|job|jobs|steps|env)\s*(?:\.|\[)`)
	quotedInExpr = regexp.MustCompile(`'(?:[^']|'')*'`)
)

func envContextErrors(wfs map[string]string) []string {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []string
	for _, name := range names {
		inJobs := false
		envIndent := -1 // the indent of the env: key whose block we are in
		var allowed map[string]bool
		where := ""
		for i, raw := range strings.Split(wfs[name], "\n") {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			indent := len(raw) - len(strings.TrimLeft(raw, " "))
			if indent == 0 {
				inJobs = trimmed == "jobs:"
			}
			if envIndent >= 0 && indent <= envIndent {
				envIndent = -1
			}
			switch {
			case indent == 0 && trimmed == "env:":
				envIndent, allowed, where = 0, workflowEnvContexts, "the workflow's env"
				continue
			case inJobs && indent == 4 && trimmed == "env:":
				envIndent, allowed, where = 4, jobEnvContexts, "a job's env"
				continue
			}
			if envIndent < 0 {
				continue
			}
			for _, e := range expression.FindAllStringSubmatch(raw, -1) {
				body := quotedInExpr.ReplaceAllString(e[1], "''")
				for _, m := range contextUse.FindAllStringSubmatch(body, -1) {
					if !allowed[m[1]] {
						errs = append(errs, fmt.Sprintf("%s:%d: %s names the %s context, which GitHub does not allow there; the workflow fails to parse and no job runs (set it from a step: echo NAME=... >> \"$GITHUB_ENV\")", name, i+1, where, m[1]))
					}
				}
			}
		}
	}
	return errs
}

func TestWorkflowAndJobEnvNameOnlyAllowedContexts(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	if len(wfs) == 0 {
		t.Fatal("no workflows read: this gate would pass over an empty set")
	}
	for _, e := range envContextErrors(wfs) {
		t.Error(e)
	}

	plant := func(name, from, to string) map[string]string {
		m := maps.Clone(wfs)
		if !strings.Contains(m[name], from) {
			t.Fatalf("control cannot plant: %s does not contain %q", name, from)
		}
		m[name] = strings.Replace(m[name], from, to, 1)
		return m
	}
	for _, c := range []struct {
		name string
		wfs  map[string]string
	}{
		{"the runner context in a job's env (the #4291 break)", plant("ci-full.yml", "    env:\n      PGROUP_WALL: ''\n", "    env:\n      PGROUP_WALL: ''\n      PGROUP_GO_TEST_JSON: ${{ runner.temp }}/go-test-json\n")},
		{"the steps context in the workflow's env", plant("certify.yml", "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n", "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n  X: ${{ steps.a.outputs.b }}\n")},
		{"the matrix context in the workflow's env", plant("certify.yml", "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n", "\n  PGROUP_GO_TEST_TIMEOUT: '0'\n  X: ${{ matrix.os }}\n")},
	} {
		if len(envContextErrors(c.wfs)) == 0 {
			t.Errorf("PLANTED PATTERN %q left the gate green", c.name)
		}
	}
	// and the forms GitHub allows stay green
	ok := map[string]string{"ok.yml": "on:\n  workflow_dispatch:\nenv:\n  A: ${{ github.sha }}\n  B: ${{ vars.X }}\njobs:\n  j:\n    env:\n      C: ${{ matrix.row }}-${{ needs.a.outputs.b }}\n      D: ${{ 'runner.temp' }}\n    steps:\n      - run: echo\n        env:\n          E: ${{ runner.temp }}\n"}
	if errs := envContextErrors(ok); len(errs) != 0 {
		t.Errorf("allowed contexts refused: %v", errs)
	}
}
