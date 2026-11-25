package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// THE LANE MERGE (footgun 2026-11-22). A pull request into a lane
// (rowan/fix-ledger-1122) was checked out as the PR merged into the LANE, so
// no ci-fast gate ever saw current main and a drifted lane stayed green until
// it landed. .github/actions/merge-main/merge-main.sh merges current main into
// a lane PR's checkout, and fails by name when the lane conflicts with main.

// mergeMainRepo builds, in a scratch directory and with no network, an origin
// holding main, a lane forked from it and a pull request into the lane, the
// way GitHub serves them: refs/pull/1/merge is the PR merged into the lane.
// edit(main, lane) runs after the fork to make main and the lane diverge. It
// returns the origin's file:// URL.
func mergeMainRepo(t *testing.T, edit func(git func(...string) string, write func(name, body string))) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required: %v", err)
	}
	root := t.TempDir()
	work := filepath.Join(root, "author")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		return mergeMainGit(t, work, nil, args...)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", name)
	}
	git("init", "-q", "-b", "main")
	write("shared.txt", "one\n")
	git("commit", "-q", "-m", "base")
	git("branch", "lane")
	edit(git, write)
	git("checkout", "-q", "lane")
	git("checkout", "-q", "-b", "pr")
	write("pr.txt", "the pull request\n")
	git("commit", "-q", "-m", "the pull request")
	git("checkout", "-q", "--detach", "lane")
	git("merge", "-q", "--no-ff", "--no-edit", "pr")
	git("update-ref", "refs/pull/1/merge", "HEAD")

	origin := filepath.Join(root, "origin.git")
	mergeMainGit(t, root, nil, "clone", "-q", "--bare", work, origin)
	mergeMainGit(t, origin, nil, "fetch", "-q", work, "+refs/pull/1/merge:refs/pull/1/merge")
	mergeMainGit(t, origin, nil, "config", "uploadpack.allowFilter", "true")
	return "file://" + origin
}

func mergeMainGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.invalid"), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// mergeMainCheckout does what actions/checkout does for a pull request: a
// one-commit-deep fetch of refs/pull/1/merge into a fresh repository.
func mergeMainCheckout(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	mergeMainGit(t, dir, nil, "init", "-q")
	mergeMainGit(t, dir, nil, "remote", "add", "origin", origin)
	mergeMainGit(t, dir, nil, "fetch", "-q", "--no-tags", "--depth=1", "origin", "+refs/pull/1/merge:refs/remotes/pull/1/merge")
	mergeMainGit(t, dir, nil, "checkout", "-q", "refs/remotes/pull/1/merge")
	return dir
}

func runMergeMain(t *testing.T, dir, event, base string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRoot(t), ".github", "actions", "merge-main", "merge-main.sh")
	testinputs.Files(t, script)
	cmd := exec.Command("bash", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GITHUB_EVENT_NAME="+event, "GITHUB_BASE_REF="+base)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestEveryFastCheckoutMergesMain holds every job of ci-fast.yml to the lane
// merge: the step right after each actions/checkout is merge-main. A new job
// that forgets it is red here by line number.
func TestEveryFastCheckoutMergesMain(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	const step = "      - "
	const merge = "      - uses: ./.github/actions/merge-main"
	checkouts := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, step+"uses: actions/checkout@") {
			continue
		}
		checkouts++
		next := ""
		for _, l := range lines[i+1:] {
			if strings.HasPrefix(l, step) || (strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "    ")) {
				next = l
				break
			}
		}
		if strings.TrimRight(next, " ") != merge {
			t.Errorf("ci-fast.yml:%d: the step after actions/checkout is %q, want %q: a lane pull request would be checked without current main", i+1, strings.TrimSpace(next), strings.TrimSpace(merge))
		}
	}
	if checkouts == 0 {
		t.Fatal("ci-fast.yml has no actions/checkout step: the scan matched nothing")
	}
}
