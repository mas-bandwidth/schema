package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestPinsBaseRefIsTheCommitTheChangeMergesInto: the base the
// not-yet-generated list is held to comes from the event ci-fast.yml runs
// on: a pull request's target branch (main, or an integration branch), a
// push's previous tip, main for a dispatch. A pull request with no target, a
// push with no previous tip (a new branch's zero sha, a payload without one
// or one that does not read) and any other event are errors, never a base.
func TestPinsBaseRefIsTheCommitTheChangeMergesInto(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("ab", 20)
	for _, c := range []struct {
		name, event, baseRef, payload, want, says string
	}{
		{"a pull request to main", "pull_request", "main", "", "main", ""},
		{"a pull request to an integration branch", "pull_request", "rowan/test-registry-staging", "", "rowan/test-registry-staging", ""},
		{"a push to main", "push", "", `{"before":"` + sha + `"}`, sha, ""},
		{"a dispatch", "workflow_dispatch", "", "", "main", ""},
		{"a pull request with no target", "pull_request", "", "", "", "no GITHUB_BASE_REF"},
		{"a push that makes a branch", "push", "", `{"before":"` + strings.Repeat("0", 40) + `"}`, "", "names no previous tip"},
		{"a push with no before", "push", "", `{}`, "", "names no previous tip"},
		{"a push whose before is no sha", "push", "", `{"before":"main"}`, "", "names no previous tip"},
		{"a push payload that does not read", "push", "", `{`, "", "does not read"},
		{"a scheduled run", "schedule", "", "", "", `event "schedule"`},
		{"no event", "", "", "", "", `event ""`},
	} {
		got, err := pinsBaseRef(c.event, c.baseRef, []byte(c.payload))
		switch {
		case c.says == "" && (err != nil || got != c.want):
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		case c.says != "" && (err == nil || !strings.Contains(err.Error(), c.says) || got != ""):
			t.Errorf("%s: got %q, %v; want the error %q", c.name, got, err, c.says)
		}
	}
}

// TestCIPrintsThePinsBaseRefFromTheEvent: `schema-test ci --pins-base-ref`
// reads the event from the runner's environment and its payload file, and
// prints the base; a payload file that is not there is an error. Not
// parallel: it sets the environment.
func TestCIPrintsThePinsBaseRefFromTheEvent(t *testing.T) {
	sha := strings.Repeat("cd", 20)
	payload := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(payload, []byte(`{"before":"`+sha+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_BASE_REF", "")
	t.Setenv("GITHUB_EVENT_PATH", payload)
	var out bytes.Buffer
	if err := ciCmd([]string{"--pins-base-ref"}, &out); err != nil || out.String() != sha+"\n" {
		t.Errorf("a push: %q, %v; want %s", out.String(), err, sha)
	}
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_BASE_REF", "rowan/test-registry-staging")
	out.Reset()
	if err := ciCmd([]string{"--pins-base-ref"}, &out); err != nil || out.String() != "rowan/test-registry-staging\n" {
		t.Errorf("a pull request: %q, %v", out.String(), err)
	}
	t.Setenv("GITHUB_EVENT_PATH", filepath.Join(t.TempDir(), "absent.json"))
	if err := ciCmd([]string{"--pins-base-ref"}, &out); err == nil || !strings.Contains(err.Error(), "the event payload") {
		t.Errorf("a payload that is not there: %v", err)
	}
}

// mainTreeBaseProblems names what keeps the main-tree action from exporting
// the base pinsBaseRef names: the ref computed some other way, not fetched,
// or the tree exported from another commit.
func mainTreeBaseProblems(action string) []string {
	var bad []string
	for _, want := range []string{
		`ref=$(go run ./tools/schema-test ci --pins-base-ref)`,
		`if ! git fetch --quiet --depth 1 origin "$ref"; then`,
		`base_sha=$(git rev-parse FETCH_HEAD)`,
		`git archive "$base_sha" | tar -x -C "$base"`,
		`echo "base=$base" >> "$GITHUB_OUTPUT"`,
	} {
		if strings.Count(action, want) != 1 {
			bad = append(bad, "the main-tree action does not hold, once: "+want)
		}
	}
	if strings.Count(action, "ref=") != 1 {
		bad = append(bad, "the main-tree action sets the base ref more than once: only schema-test ci --pins-base-ref names it")
	}
	return bad
}

// TestTheMainTreeActionExportsTheBaseTheEventNames: the action asks the
// driver for the base (pinsBaseRef), fetches that ref, and exports the tree
// of what it fetched as the base output that ci-fast.yml passes to extract
// --pins-base (TestCICheckAndExtractHoldThePins). The plants: the ref chosen
// in the shell, the ref overwritten after the driver names it, and the tree
// exported from main.
func TestTheMainTreeActionExportsTheBaseTheEventNames(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot, ".github/actions/main-tree/action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	action := string(data)
	if bad := mainTreeBaseProblems(action); len(bad) > 0 {
		t.Fatalf("the main-tree action does not export the base:\n  %s", strings.Join(bad, "\n  "))
	}
	for _, p := range []struct{ name, old, new string }{
		{"the ref chosen in the shell", `ref=$(go run ./tools/schema-test ci --pins-base-ref)`, `ref=main`},
		{"the ref overwritten", `ref=$(go run ./tools/schema-test ci --pins-base-ref)` + "\n", `ref=$(go run ./tools/schema-test ci --pins-base-ref)` + "\n        ref=main\n"},
		{"the tree exported from main", `git archive "$base_sha" | tar -x -C "$base"`, `git archive "$main" | tar -x -C "$base"`},
	} {
		if !strings.Contains(action, p.old) {
			t.Errorf("%s: the plant's anchor is not there", p.name)
			continue
		}
		if len(mainTreeBaseProblems(strings.Replace(action, p.old, p.new, 1))) == 0 {
			t.Errorf("PLANTED PATTERN %q left the check green", p.name)
		}
	}
}

// TestTheBaseLeavesOutExactlyTheGeneratedWorkflows: the workflows a base
// from before ci-pins.tsv does not count as not yet generated
// (generatedWorkflowFiles) are exactly the ones schema-test ci writes, so
// the two lists cannot drift apart.
func TestTheBaseLeavesOutExactlyTheGeneratedWorkflows(t *testing.T) {
	t.Parallel()
	files, err := ciFiles(loadRepoRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	var got, want []string
	for name := range generatedWorkflows(files) {
		got = append(got, ".github/workflows/"+name)
	}
	want = append(want, generatedWorkflowFiles...)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("schema-test ci writes %v; generatedWorkflowFiles names %v", got, want)
	}
}
