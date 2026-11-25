//go:build !windows

package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestPrePushHookExistingBranchSkipsCommitsOnAnyRemote drives
// .githooks/pre-push over a scratch clone with a local bare repository as its
// remote (no network). An existing branch that merges a remote branch (a
// staging lane) takes in that lane commits, one of which carries another
// identity but is already pushed. Those commits are not this push, so the hook
// must pass; a new foreign commit the push adds must still be refused, named,
// without naming the pushed one; and a new branch keeps checking only the
// commits on no remote ref.
func TestPrePushHookExistingBranchSkipsCommitsOnAnyRemote(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	hook := filepath.Join(repoRoot(t), ".githooks", "pre-push")
	if _, err := os.Stat(hook); err != nil {
		t.Fatal(err)
	}

	const agentName = "Rowan Claude"
	const agentEmail = "305107455+rowan-claude@users.noreply.github.com"
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	run := func(dir string, extra []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(append(os.Environ(), env...), extra...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git := func(extra []string, args ...string) string { t.Helper(); return run(work, extra, args...) }
	as := func(an, ae, cn, ce string) []string {
		return []string{"GIT_AUTHOR_NAME=" + an, "GIT_AUTHOR_EMAIL=" + ae, "GIT_COMMITTER_NAME=" + cn, "GIT_COMMITTER_EMAIL=" + ce}
	}
	agent := as(agentName, agentEmail, agentName, agentEmail)
	other := as("Someone Else", "else@example.com", "GitHub", "noreply@github.com")
	commit := func(who []string, msg string) string {
		t.Helper()
		git(who, "commit", "-q", "--allow-empty", "-m", msg)
		return git(nil, "rev-parse", "HEAD")
	}

	run(root, nil, "init", "-q", "--bare", "-b", "main", bare)
	run(root, nil, "init", "-q", "-b", "main", work)
	git(nil, "config", "user.name", agentName)
	git(nil, "config", "user.email", agentEmail)
	git(nil, "remote", "add", "origin", bare)
	commit(agent, "base")
	git(nil, "push", "-q", "origin", "main")

	// The old branch, pushed, before the staging lane has the other commit.
	git(nil, "checkout", "-q", "-b", "old")
	commit(agent, "old work")
	git(nil, "push", "-q", "origin", "old")

	// The staging lane holds an other-identity commit and is pushed.
	git(nil, "checkout", "-q", "-b", "staging", "main")
	commit(agent, "staging work")
	pushed := commit(other, "other identity, already pushed")
	git(nil, "push", "-q", "origin", "staging")
	git(nil, "fetch", "-q", "origin")

	// The old branch merges staging (agent identity on the merge itself).
	git(nil, "checkout", "-q", "old")
	git(agent, "merge", "-q", "--no-ff", "-m", "merge staging", "staging")
	merge := git(nil, "rev-parse", "HEAD")
	oldRemote := git(nil, "rev-parse", "origin/old")

	const zero = "0000000000000000000000000000000000000000"
	push := func(ref, lsha, rsha string) (string, error) {
		t.Helper()
		testinputs.Files(t, hook)
		cmd := exec.Command("bash", hook, "origin", bare)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = strings.NewReader("refs/heads/" + ref + " " + lsha + " refs/heads/" + ref + " " + rsha + "\n")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := push("old", merge, oldRemote); err != nil {
		t.Errorf("an existing branch merging a pushed remote branch was refused for a commit already on the remote: %v\n%s", err, out)
	}

	foreign := commit(other, "a new foreign commit this push adds")
	out, err := push("old", foreign, oldRemote)
	if err == nil {
		t.Errorf("an existing branch adding a foreign commit was pushed\n%s", out)
	} else {
		if !strings.Contains(out, foreign[:12]) {
			t.Errorf("the refusal does not name the new foreign commit %s:\n%s", foreign[:12], out)
		}
		if strings.Contains(out, pushed[:12]) {
			t.Errorf("the refusal names %s, a commit already on a remote:\n%s", pushed[:12], out)
		}
	}

	// A new branch: the pushed other-identity commit is not its own, a new one is.
	git(nil, "checkout", "-q", "-b", "fresh", "staging")
	mine := commit(agent, "fresh branch work")
	if out, err := push("fresh", mine, zero); err != nil {
		t.Errorf("a new branch on a pushed lane was refused: %v\n%s", err, out)
	}
	newForeign := commit(other, "a new foreign commit on a new branch")
	out, err = push("fresh", newForeign, zero)
	if err == nil {
		t.Errorf("a new branch carrying a foreign unpushed commit was pushed\n%s", out)
	} else if !strings.Contains(out, newForeign[:12]) || strings.Contains(out, pushed[:12]) {
		t.Errorf("the new-branch refusal must name %s and not %s:\n%s", newForeign[:12], pushed[:12], out)
	}
}

// prePushScratch builds a scratch agent clone and returns a function that runs
// the hook on one pushed ref line, plus helpers to make commits.
func prePushScratch(t *testing.T) (commit func(foreign bool, msg string) string, push func(lsha, rsha string) (string, error)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	hook := filepath.Join(repoRoot(t), ".githooks", "pre-push")
	if _, err := os.Stat(hook); err != nil {
		t.Fatal(err)
	}
	const agentName = "Rowan Claude"
	const agentEmail = "305107455+rowan-claude@users.noreply.github.com"
	work := t.TempDir()
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	git := func(extra []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(append(os.Environ(), env...), extra...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(nil, "init", "-q", "-b", "main")
	git(nil, "config", "user.name", agentName)
	git(nil, "config", "user.email", agentEmail)
	commit = func(foreign bool, msg string) string {
		t.Helper()
		who := []string{"GIT_AUTHOR_NAME=" + agentName, "GIT_AUTHOR_EMAIL=" + agentEmail, "GIT_COMMITTER_NAME=" + agentName, "GIT_COMMITTER_EMAIL=" + agentEmail}
		if foreign {
			who = []string{"GIT_AUTHOR_NAME=x", "GIT_AUTHOR_EMAIL=x@x", "GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL=x@x"}
		}
		git(who, "commit", "-q", "--allow-empty", "-m", msg)
		return git(nil, "rev-parse", "HEAD")
	}
	push = func(lsha, rsha string) (string, error) {
		t.Helper()
		testinputs.Files(t, hook)
		cmd := exec.Command("bash", hook, "origin", "unused")
		cmd.Dir = work
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = strings.NewReader("refs/heads/b " + lsha + " refs/heads/b " + rsha + "\n")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return commit, push
}

// TestPrePushHookMissingRemoteTipChecksUnpushedCommits: when the remote tip
// (rsha) is not in the clone, rsha..lsha cannot be computed. The hook used to
// ignore that git error and exit 0 unchecked. It must check the commits on no
// remote ref instead, and refuse a foreign one, naming it.
func TestPrePushHookMissingRemoteTipChecksUnpushedCommits(t *testing.T) {
	t.Parallel()
	commit, push := prePushScratch(t)
	const missing = "1234567890123456789012345678901234567890"
	good := commit(false, "base")
	if out, err := push(good, missing); err != nil {
		t.Errorf("a clean push with a remote tip missing locally was refused: %v\n%s", err, out)
	}
	foreign := commit(true, "foreign commit")
	out, err := push(foreign, missing)
	if err == nil {
		t.Errorf("a foreign commit was pushed when the remote tip was missing locally\n%s", out)
	} else if !strings.Contains(out, foreign[:12]) {
		t.Errorf("the refusal does not name %s:\n%s", foreign[:12], out)
	}
}

// TestPrePushHookGitLogFailureRefuses: a git log that fails (here the pushed
// sha is no object in the clone) must refuse the push loudly with git's cause;
// the hook used to ignore the failure and exit 0 with nothing checked.
func TestPrePushHookGitLogFailureRefuses(t *testing.T) {
	t.Parallel()
	commit, push := prePushScratch(t)
	base := commit(false, "base")
	const bogus = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	out, err := push(bogus, base)
	if err == nil {
		t.Fatalf("a push whose git log failed was passed unchecked\n%s", out)
	}
	if !strings.Contains(out, "could not be checked") || !strings.Contains(out, "abcdef") {
		t.Errorf("the refusal does not give the failed log and its cause:\n%s", out)
	}
}
