package githooks

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	agentEmail = "305107455+rowan-claude@users.noreply.github.com"
	zero       = "0000000000000000000000000000000000000000"
)

// git runs git in dir with a fixed identity and returns trimmed stdout.
func git(t *testing.T, dir, name, email string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, name, email, file string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, name, email, "add", file)
	git(t, dir, name, email, "commit", "-q", "-m", file)
	return git(t, dir, name, email, "rev-parse", "HEAD")
}

// runHook runs the tracked hook in dir as 'git push <remote>' would, returning
// its exit code and stderr.
func runHook(t *testing.T, dir, remote, line string) (int, string) {
	t.Helper()
	hook, err := filepath.Abs("../../.githooks/pre-push")
	if err != nil {
		t.Fatal(err)
	}
	// Read the hook here so the go test cache sees it as an input and reruns when it changes.
	if _, err := os.ReadFile(hook); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", hook, remote, "ignored-url")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Stdin = strings.NewReader(line + "\n")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), stderr.String()
	}
	t.Fatal(err)
	return -1, ""
}

func setup(t *testing.T) (work, other string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	other = filepath.Join(root, "other")
	git(t, root, "x", "x@x", "init", "-q", "--bare", "-b", "main", remote)
	git(t, root, "x", "x@x", "clone", "-q", remote, work)
	git(t, root, "x", "x@x", "clone", "-q", remote, other)
	git(t, work, "x", "x@x", "config", "user.name", "Rowan Claude")
	git(t, work, "x", "x@x", "config", "user.email", agentEmail)
	commit(t, work, "Rowan Claude", agentEmail, "base")
	git(t, work, "x", "x@x", "push", "-q", "origin", "main")
	return work, other
}

func TestPrePushMergeFromMainWithGitHubCommits(t *testing.T) {
	work, other := setup(t)
	git(t, work, "x", "x@x", "checkout", "-q", "-b", "feature")
	commit(t, work, "Rowan Claude", agentEmail, "f1")
	git(t, work, "x", "x@x", "push", "-q", "origin", "feature")
	old := git(t, work, "x", "x@x", "rev-parse", "HEAD")

	// main advances with commits committed by GitHub (squash / merge button).
	git(t, other, "x", "x@x", "pull", "-q")
	commit(t, other, "Someone Else", "else@example.com", "m1")
	commit(t, other, "GitHub", "noreply@github.com", "m2")
	git(t, other, "x", "x@x", "push", "-q", "origin", "main")

	git(t, work, "x", "x@x", "fetch", "-q", "origin")
	git(t, work, "Rowan Claude", agentEmail, "merge", "-q", "--no-edit", "origin/main")
	commit(t, work, "Rowan Claude", agentEmail, "f2")
	local := git(t, work, "x", "x@x", "rev-parse", "HEAD")

	code, stderr := runHook(t, work, "origin", "refs/heads/feature "+local+" refs/heads/feature "+old)
	if code != 0 {
		t.Fatalf("merge from main refused (code %d):\n%s", code, stderr)
	}
}

func TestPrePushWrongIdentityRefused(t *testing.T) {
	work, _ := setup(t)
	git(t, work, "x", "x@x", "checkout", "-q", "-b", "feature")
	old := commit(t, work, "Rowan Claude", agentEmail, "f1")
	git(t, work, "x", "x@x", "push", "-q", "origin", "feature")
	local := commit(t, work, "Intruder", "intruder@example.com", "bad")

	code, stderr := runHook(t, work, "origin", "refs/heads/feature "+local+" refs/heads/feature "+old)
	if code != 1 || !strings.Contains(stderr, "Intruder") {
		t.Fatalf("want refusal naming the offender, got code %d:\n%s", code, stderr)
	}
}

func TestPrePushNewBranchAndDelete(t *testing.T) {
	work, other := setup(t)
	git(t, other, "x", "x@x", "pull", "-q")
	commit(t, other, "GitHub", "noreply@github.com", "m1")
	git(t, other, "x", "x@x", "push", "-q", "origin", "main")
	git(t, work, "x", "x@x", "fetch", "-q", "origin")
	git(t, work, "x", "x@x", "checkout", "-q", "-b", "newbranch", "origin/main")
	local := commit(t, work, "Rowan Claude", agentEmail, "n1")

	if code, stderr := runHook(t, work, "origin", "refs/heads/newbranch "+local+" refs/heads/newbranch "+zero); code != 0 {
		t.Fatalf("new branch refused (code %d):\n%s", code, stderr)
	}
	// A bad new commit on a new branch is still refused.
	bad := commit(t, work, "Intruder", "intruder@example.com", "bad")
	if code, _ := runHook(t, work, "origin", "refs/heads/newbranch "+bad+" refs/heads/newbranch "+zero); code != 1 {
		t.Fatalf("bad new-branch commit not refused (code %d)", code)
	}
	// Deleting a ref pushes the zero local sha.
	if code, stderr := runHook(t, work, "origin", "(delete) "+zero+" refs/heads/newbranch "+local); code != 0 {
		t.Fatalf("delete refused (code %d):\n%s", code, stderr)
	}
}
