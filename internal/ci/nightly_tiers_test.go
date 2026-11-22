package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nightlyrows"
	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// THE NIGHTLY-ONLY FOOTGUN (2026-11-22). The strict Ruby gate ran only in
// ruby-leg.yml and certify.yml, which run on a schedule or a dispatch, so no
// push or pull request ran it and a RuboCop offense reached main. Every
// workflow that a push or a pull request does not start is a gate the pull
// request tier never sees, unless something in that tier catches what it
// catches. docs/CI-TIERS.md decides that for each one, and this test holds
// the decision to the tree:
//
//   fast       the gate has a pull-request form: rows of
//              .github/nightly-fast-rows.txt, run by ci-fast.yml nightly-fast
//   covered    named ci-fast.yml jobs (or rows) already catch what it catches
//   held       a fast form is written but held off the PR tier, and the row
//              names the red run that holds it
//   not-a-gate a release, a tag, or a reusable workflow, not a check
//
// A new nightly-only workflow with no row in docs/CI-TIERS.md is red here by
// name, and so is a row naming a job, a row or a make target that is not there.

var tiersRow = regexp.MustCompile("^\\| `([a-z0-9._-]+\\.yml)` \\| ([a-z-]+) \\|(.*)\\|(.*)\\|$")

// makeTargetWord is a make command argument that names a target, not a
// flag, a variable or a piece of a shell expansion.
var makeTargetWord = regexp.MustCompile("^[a-z0-9][a-z0-9._-]*$")

var backticked = regexp.MustCompile("`([^`]+)`")

// workflowStartsOnPushOrPR reports whether a workflow file names push,
// pull_request or pull_request_target among its triggers.
func workflowStartsOnPushOrPR(text string) bool {
	in := false
	for line := range strings.SplitSeq(text, "\n") {
		if line == "on:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			break
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
			(strings.HasPrefix(t, "push:") || strings.HasPrefix(t, "pull_request:") || strings.HasPrefix(t, "pull_request_target:")) {
			return true
		}
	}
	return false
}

func TestEveryNightlyOnlyGateHasAPRTierForm(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nightlyOnly []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !workflowStartsOnPushOrPR(string(raw)) {
			nightlyOnly = append(nightlyOnly, e.Name())
		}
	}
	if len(nightlyOnly) == 0 {
		t.Fatal("no workflow parsed as nightly-only: the trigger scan matched nothing")
	}

	fast, err := os.ReadFile(filepath.Join(dir, "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := map[string]bool{}
	for line := range strings.SplitSeq(string(fast), "\n") {
		if m := regexp.MustCompile("^  ([a-z0-9-]+):$").FindStringSubmatch(line); m != nil {
			jobs[m[1]] = true
		}
	}
	if !jobs["nightly-fast"] {
		t.Fatal("ci-fast.yml has no nightly-fast job: the nightly gates' fast forms run nowhere")
	}

	f, err := os.Open(filepath.Join(root, filepath.FromSlash(nightlyrows.RegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rowNames := map[string]bool{}
	for _, r := range rows {
		rowNames[r.Name] = true
	}

	doc, err := os.ReadFile(filepath.Join(root, "docs", "CI-TIERS.md"))
	if err != nil {
		t.Fatal(err)
	}
	decided := map[string]string{}
	usedRows := map[string]bool{}
	for i, line := range strings.Split(string(doc), "\n") {
		m := tiersRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		wf, decision, form := m[1], m[2], m[3]
		where := fmt.Sprintf("docs/CI-TIERS.md:%d", i+1)
		if prev, dup := decided[wf]; dup {
			t.Errorf("%s: %s is decided twice (%s, %s)", where, wf, prev, decision)
		}
		decided[wf] = decision
		names := backticked.FindAllStringSubmatch(form, -1)
		switch decision {
		case "fast", "covered":
			if len(names) == 0 {
				t.Errorf("%s: %s is %s but names no row or job", where, wf, decision)
			}
			rowsNamed := 0
			for _, n := range names {
				if rowNames[n[1]] {
					rowsNamed++
				}
			}
			if decision == "fast" && rowsNamed == 0 {
				t.Errorf("%s: %s is fast but names no row of %s", where, wf, nightlyrows.RegistryPath)
			}
			for _, n := range names {
				switch {
				case rowNames[n[1]]:
					usedRows[n[1]] = true
				case jobs[n[1]]:
				default:
					t.Errorf("%s: %s names %q, which is no row of %s and no ci-fast.yml job", where, wf, n[1], nightlyrows.RegistryPath)
				}
			}
		case "held":
			if !regexp.MustCompile("run [0-9]{8,}").MatchString(form + m[4]) {
				t.Errorf("%s: %s is held but names no red run (\"run <id>\") that holds it", where, wf)
			}
		case "not-a-gate":
		default:
			t.Errorf("%s: %s has decision %q; want fast, covered, held or not-a-gate", where, wf, decision)
		}
	}
	for _, wf := range nightlyOnly {
		if _, ok := decided[wf]; !ok {
			t.Errorf("%s runs only on a schedule or a dispatch, and docs/CI-TIERS.md does not decide it: give it a row in %s (fast) or name the ci-fast.yml jobs that catch what it catches (covered)", wf, nightlyrows.RegistryPath)
		}
	}
	for wf := range decided {
		if !slices.Contains(nightlyOnly, wf) {
			t.Errorf("docs/CI-TIERS.md decides %s, which is not a nightly-only workflow in .github/workflows", wf)
		}
	}
	for _, r := range rows {
		if !usedRows[r.Name] {
			t.Errorf("%s:%d: row %q stands for no workflow in docs/CI-TIERS.md", nightlyrows.RegistryPath, r.Line, r.Name)
		}
	}
}

// TestNightlyFastRowsNameDefinedTargets holds every make target a row of
// .github/nightly-fast-rows.txt runs to a rule in the tree, and every leg to
// one the nightly-fast job sets up: a row that names nothing would be a green
// row that ran nothing.
func TestNightlyFastRowsNameDefinedTargets(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var makeText strings.Builder
	for _, glob := range []string{"Makefile", "make/*.mk", "make/checks/*.mk"} {
		paths, err := provesran.Glob(filepath.Join(root, filepath.FromSlash(glob)))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			makeText.Write(raw)
			makeText.WriteByte('\n')
		}
	}
	mk := makeText.String()
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(nightlyrows.RegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	legs := map[string]bool{"-": true}
	for _, l := range strings.Fields("c cpp cs unity rust js java dart elixir go lua luau lobster moo python ruby squirrel goose") {
		legs[l] = true
	}
	for _, r := range rows {
		if !legs[r.Leg] {
			t.Errorf("%s:%d: row %q names leg %q, which the nightly-fast job does not set up", nightlyrows.RegistryPath, r.Line, r.Name, r.Leg)
		}
		fields := strings.Fields(r.Run)
		if len(fields) < 2 || fields[0] != "make" {
			t.Errorf("%s:%d: row %q runs %q; a row runs one make command", nightlyrows.RegistryPath, r.Line, r.Name, r.Run)
			continue
		}
		targets := 0
		for _, a := range fields[1:] {
			if !makeTargetWord.MatchString(a) {
				continue
			}
			targets++
			if !regexp.MustCompile("(?m)^" + regexp.QuoteMeta(a) + ":").MatchString(mk) {
				t.Errorf("%s:%d: row %q runs make target %q, which no rule in Makefile or make/ defines", nightlyrows.RegistryPath, r.Line, r.Name, a)
			}
		}
		if targets == 0 {
			t.Errorf("%s:%d: row %q names no make target", nightlyrows.RegistryPath, r.Line, r.Name)
		}
	}
}

// TestNightlyFastRunsTheRubyStrictFormOnARubyDiff is the footgun, replayed on
// the real registry: a diff to the Ruby emitter (the leg the plan names ruby)
// selects the strict gate fast form. Before 2026-11-22 no pull-request job
// selected any form of schema-ruby-generated-strict-gate.
func TestNightlyFastRunsTheRubyStrictFormOnARubyDiff(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join(repoRoot(t), filepath.FromSlash(nightlyrows.RegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range nightlyrows.Select(rows, []string{"internal/codegen/ruby/emit.go"}, []string{"ruby"}, false) {
		got = append(got, r.Name)
	}
	for _, want := range []string{"strict-ruby", "hostile-ruby", "bounds-ruby"} {
		if !slices.Contains(got, want) {
			t.Errorf("a Ruby emitter diff selects %v, without %s", got, want)
		}
	}
	if fast := nightlyrows.Select(rows, []string{"docs/FAQ.md"}, nil, false); len(fast) != 0 {
		t.Errorf("a docs-only diff selects %d nightly-fast rows, want none", len(fast))
	}
}
