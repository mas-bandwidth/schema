package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPrePushHookNewBranchChecksOnlyUnpushedCommits drives .githooks/pre-push
// over a scratch repository. A new branch based on a remote lane branch (a PR
// branch other PRs merged into) carries the GitHub-committed merge of that
// lane, which is already on a remote and is not the pusher own commit. The
// hook checked origin/main..<sha> for a new branch, so such a push was refused
// for that merge. It must check only the commits on no remote ref, and still
// refuse a foreign commit the push itself carries.
func TestPrePushHookNewBranchChecksOnlyUnpushedCommits(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a POSIX shell script (bash); agent clones run on Linux and macOS")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	hook := filepath.Join(repoRoot(t), ".githooks", "pre-push")
	if _, err := os.Stat(hook); err != nil {
		t.Fatal(err)
	}

	const agentName = "Rowan Claude"
	const agentEmail = "305107455+rowan-claude@users.noreply.github.com"
	dir := t.TempDir()
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	git := func(extra []string, args ...string) string {
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
	as := func(an, ae, cn, ce string) []string {
		return []string{"GIT_AUTHOR_NAME=" + an, "GIT_AUTHOR_EMAIL=" + ae, "GIT_COMMITTER_NAME=" + cn, "GIT_COMMITTER_EMAIL=" + ce}
	}
	agent := as(agentName, agentEmail, agentName, agentEmail)
	githubMerge := as(agentName, "rowan@mas-bandwidth.com", "GitHub", "noreply@github.com")
	commit := func(who []string, msg string) string {
		t.Helper()
		git(who, "commit", "-q", "--allow-empty", "-m", msg)
		return git(nil, "rev-parse", "HEAD")
	}

	git(nil, "init", "-q", "-b", "main")
	git(nil, "config", "user.name", agentName)
	git(nil, "config", "user.email", agentEmail)
	base := commit(agent, "base")
	git(nil, "update-ref", "refs/remotes/origin/main", base)
	git(nil, "checkout", "-q", "-b", "lane")
	commit(agent, "lane work")
	laneMerge := commit(githubMerge, "Merge pull request #1 into lane (GitHub)")
	git(nil, "update-ref", "refs/remotes/origin/lane", laneMerge)
	git(nil, "checkout", "-q", "-b", "child")
	mine := commit(agent, "the child branch, not yet pushed")

	const zero = "0000000000000000000000000000000000000000"
	push := func(lsha string) (string, error) {
		t.Helper()
		cmd := exec.Command("bash", hook, "origin", "unused")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = strings.NewReader("refs/heads/child " + lsha + " refs/heads/child " + zero + "\n")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := push(mine); err != nil {
		t.Errorf("a new branch based on a pushed lane was refused for the GitHub merge on that lane: %v\n%s", err, out)
	}

	foreign := commit(as("x", "x@x", "x", "x@x"), "a placeholder identity")
	out, err := push(foreign)
	if err == nil {
		t.Errorf("a new branch carrying a foreign unpushed commit was pushed\n%s", out)
	} else if !strings.Contains(out, foreign[:12]) {
		t.Errorf("the refusal does not name the foreign commit %s:\n%s", foreign[:12], out)
	}
	if strings.Contains(out, laneMerge[:12]) {
		t.Errorf("the refusal names %s, a merge already on a remote:\n%s", laneMerge[:12], out)
	}
}
