package ci

// THE CAA JOB TAKES A RUNNER ONLY WHEN IT HAS WORK, AND THE SELF-TEST ONLY
// WHEN ITS INPUTS CHANGED. 2026-11-24: 81 of 155 queued schema runs were CAA
// (25 issue_comment, 32 pull_request_target, 24 self-test pull_request), all in
// the queue CI fast waits in. A job that starts and then skips every step still
// holds a self-hosted runner, so cla.yml's job-level if: must admit a comment
// exactly when one of its steps would run, and the self-test's pull_request
// trigger is limited by paths: to what it reads. Both pin the START of the job
// only: the steps, the action, its inputs, the allowlist and the permissions
// are untouched. This is a legal check; pull_request_target always starts it.
// This reads the workflow text only.

import (
	"strings"
	"testing"
)

const caaSignSentence = "I have read the CAA and I hereby sign it, assigning copyright in my contributions to Más Bandwidth LLC."

// workflowStepIf returns the if: of the named step, folded onto one line.
func workflowStepIf(t *testing.T, wf, step string) string {
	t.Helper()
	lines := strings.Split(wf, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: "+step {
			continue
		}
		var cond []string
		in := false
		for _, l2 := range lines[i+1:] {
			trimmed := strings.TrimSpace(l2)
			if strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "- uses:") {
				break
			}
			if rest, ok := strings.CutPrefix(l2, "        if:"); ok {
				rest = strings.TrimSpace(rest)
				if rest == ">-" || rest == ">" {
					in = true
					continue
				}
				return rest
			}
			if in {
				if !strings.HasPrefix(l2, "          ") {
					break
				}
				cond = append(cond, trimmed)
			}
		}
		if len(cond) > 0 {
			return strings.Join(cond, " ")
		}
		t.Fatalf("cla.yml step %q has no if:", step)
	}
	t.Fatalf("cla.yml has no step %q", step)
	return ""
}

func TestCAAJobStartsOnlyWhenAStepWouldRun(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	cla := wfs["cla.yml"]
	check := workflowStepIf(t, cla, "CAA check")
	flag := workflowStepIf(t, cla, "Flag a signature the automation cannot record")

	comment := func(body, user string, bot, isPR bool, issueUser string) ciEvent {
		return ciEvent{Name: "issue_comment", Repo: "mas-bandwidth/schema", CommentBody: body, CommentUser: user, CommentBot: bot, IssueIsPR: isPR, IssueUser: issueUser}
	}
	for _, c := range []struct {
		name string
		ev   ciEvent
		want bool
	}{
		{"plain comment on a PR", comment("looks good to me", "someone", false, true, "author"), false},
		{"plain comment on an issue", comment("thanks, will look", "someone", false, false, "author"), false},
		{"a comment that only mentions recheck", comment("please recheck this", "someone", false, true, "author"), false},
		{"a bot's plain comment on a PR", comment("All contributors have signed the CAA. Thank you.", "github-actions[bot]", true, true, "author"), false},
		{"the sign sentence on a PR", comment(caaSignSentence, "someone", false, true, "author"), true},
		{"recheck on a PR", comment("recheck", "someone", false, true, "author"), true},
		{"recheck on a dependabot PR (the step skips it)", comment("recheck", "someone", false, true, "dependabot[bot]"), false},
		{"the sign sentence on an issue (flagged for hand recording)", comment(caaSignSentence, "someone", false, false, "author"), true},
		{"the sign sentence with extra text on a PR (flagged)", comment(caaSignSentence+" v2", "someone", false, true, "author"), true},
		{"a bot quoting the sign sentence with extra text", comment("sign: "+caaSignSentence, "github-actions[bot]", true, true, "author"), false},
		{"rowan-claude quoting the sign sentence with extra text", comment("> "+caaSignSentence, "rowan-claude", false, false, "author"), false},
	} {
		got, err := jobRunsInWorkflow(cla, "caa", c.ev)
		if err != nil || got != c.want {
			t.Errorf("%s: caa job starts=%v err=%v, want %v", c.name, got, err, c.want)
		}
		// and by construction: the job starts exactly when a step would run
		s1, err1 := evalCIExpr(check, c.ev)
		s2, err2 := evalCIExpr(flag, c.ev)
		if err1 != nil || err2 != nil {
			t.Fatalf("%s: step conditions cannot be evaluated: %v %v", c.name, err1, err2)
		}
		if got != (s1 || s2) {
			t.Errorf("%s: caa job starts=%v but a step would run=%v (CAA check=%v, flag=%v)", c.name, got, s1 || s2, s1, s2)
		}
	}
}

func TestCAAPullRequestTargetAlwaysStartsTheRequiredCheck(t *testing.T) {
	t.Parallel()
	cla := workflows(t, repoRoot(t))["cla.yml"]
	if !strings.Contains(cla, "name: caa") && !strings.Contains(cla, "\n  caa:\n") {
		t.Fatal("cla.yml has no caa job: the required check would never report")
	}
	for _, head := range []string{"feature-x", "main-fix", "dependabot/go_modules/x"} {
		for _, headRepo := range []string{"mas-bandwidth/schema", "someone/schema"} {
			ev := ciEvent{Name: "pull_request_target", Branch: "main", HeadRef: head, Repo: "mas-bandwidth/schema", HeadRepo: headRepo}
			got, err := jobRunsInWorkflow(cla, "caa", ev)
			if err != nil || !got {
				t.Errorf("pull_request_target from %s (%s): caa starts=%v err=%v, want true: the required caa check must report", head, headRepo, got, err)
			}
		}
	}
	// the triggers stay opened and synchronize
	for _, want := range []string{"pull_request_target:\n    types: [opened, synchronize]", "issue_comment:\n    types: [created]"} {
		if !strings.Contains(cla, want) {
			t.Errorf("cla.yml lost the trigger %q", want)
		}
	}
}

func TestCAASelfTestPullRequestIsLimitedToItsInputs(t *testing.T) {
	t.Parallel()
	wf := workflows(t, repoRoot(t))["caa-self-test.yml"]
	var paths string
	inPR := false
	for l := range strings.SplitSeq(wf, "\n") {
		switch {
		case l == "  pull_request:":
			inPR = true
		case inPR && strings.HasPrefix(l, "    paths:"):
			paths = l
		case inPR && strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "   "):
			inPR = false
		}
	}
	if paths == "" {
		t.Fatal("caa-self-test.yml's pull_request has no paths: every pull request re-checks main's tip on a schema runner")
	}
	for _, in := range []string{".github/workflows/caa-self-test.yml", ".github/workflows/cla.yml", ".github/caa-self-test.py", "tools/pgroup/**", "go.mod"} {
		if !strings.Contains(paths, "'"+in+"'") {
			t.Errorf("caa-self-test.yml's paths: omits %s, which the test reads or runs: %s", in, paths)
		}
	}
	for _, wide := range []string{"'**'", "'*'", "'.github/**'", "'.github/workflows/**'", "'tools/**'"} {
		if strings.Contains(paths, wide) {
			t.Errorf("caa-self-test.yml's paths: carries %s, which is wider than the test's inputs", wide)
		}
	}
	// the other triggers stay: a push to main and a dispatch re-check the tip
	for _, want := range []string{"  push:\n    branches: [main]\n", "  workflow_dispatch:\n"} {
		if !strings.Contains(wf, want) {
			t.Errorf("caa-self-test.yml lost %q", want)
		}
	}
	// the script reads the allowlist from cla.yml and nothing else on disk
	script := readRepoFile(t, repoRoot(t), ".github", "caa-self-test.py")
	if !strings.Contains(script, `os.path.join(".github", "workflows", "cla.yml")`) || strings.Count(script, " open(") != 1 {
		t.Error(".github/caa-self-test.py reads files other than cla.yml: widen the paths: of caa-self-test.yml to match")
	}
}
