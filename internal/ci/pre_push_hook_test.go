package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPrePushHookSkipsCommitsAlreadyOnMain drives .githooks/pre-push over a
// scratch repository. Bringing a branch current is a merge of origin/main into
// it, and main's own commits carry GitHub (or a maintainer) as committer. The
// hook checked rsha..lsha for an existing branch, so every such push was
// refused for commits that were already on main and not the pusher's. The hook
// must still refuse a foreign commit that is not on main.
func TestPrePushHookSkipsCommitsAlreadyOnMain(t *testing.T) {
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
	git := func(env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	as := func(name, email string) []string {
		return []string{
			"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email,
			"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email,
		}
	}
	agent := as(agentName, agentEmail)
	github := as("GitHub", "noreply@github.com")
	commit := func(env []string, msg string) string {
		t.Helper()
		git(env, "commit", "-q", "--allow-empty", "-m", msg)
		return git(nil, "rev-parse", "HEAD")
	}

	git(nil, "init", "-q", "-b", "main")
	git(nil, "config", "user.name", agentName)
	git(nil, "config", "user.email", agentEmail)
	commit(github, "base")
	git(nil, "checkout", "-q", "-b", "work")
	pushed := commit(agent, "work, already pushed")
	git(nil, "checkout", "-q", "main")
	onMain := commit(github, "a later main merge")
	git(nil, "update-ref", "refs/remotes/origin/main", onMain)
	git(nil, "checkout", "-q", "work")
	git(agent, "merge", "-q", "--no-edit", "origin/main")
	merged := git(nil, "rev-parse", "HEAD")

	push := func(lsha, rsha string) (string, error) {
		t.Helper()
		cmd := exec.Command("bash", hook, "origin", "unused")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		cmd.Stdin = strings.NewReader("refs/heads/work " + lsha + " refs/heads/work " + rsha + "\n")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := push(merged, pushed); err != nil {
		t.Errorf("a branch brought current by merging origin/main was refused for main's commits: %v\n%s", err, out)
	}

	foreign := commit(as("x", "x@x"), "a placeholder identity")
	out, err := push(foreign, merged)
	if err == nil {
		t.Errorf("a foreign commit not on main was pushed: the hook let it through\n%s", out)
	} else if !strings.Contains(out, foreign[:12]) {
		t.Errorf("the refusal does not name the foreign commit %s:\n%s", foreign[:12], out)
	}
	if strings.Contains(out, onMain[:12]) {
		t.Errorf("the refusal names %s, a commit already on main:\n%s", onMain[:12], out)
	}
}
