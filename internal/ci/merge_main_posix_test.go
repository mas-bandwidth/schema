//go:build !windows

package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeMainChecksALanePullRequestAgainstCurrentMain(t *testing.T) {
	t.Parallel()
	origin := mergeMainRepo(t, func(git func(...string) string, write func(string, string)) {
		write("main-only.txt", "landed on main after the lane forked\n")
		git("commit", "-q", "-m", "main moves on")
	})

	t.Run("drifted lane: the checkout gains main", func(t *testing.T) {
		t.Parallel()
		dir := mergeMainCheckout(t, origin)
		// THE FOOTGUN, planted: what actions/checkout hands a lane PR has the
		// PR and the lane and none of main's newer commits.
		if _, err := os.Stat(filepath.Join(dir, "main-only.txt")); err == nil {
			t.Fatal("the planted lane already holds main's commit: the plant does not reproduce the drift")
		}
		out, err := runMergeMain(t, dir, "pull_request", "lane")
		if err != nil {
			t.Fatalf("merge-main failed on a lane that merges cleanly: %v\n%s", err, out)
		}
		for _, name := range []string{"main-only.txt", "pr.txt", "shared.txt"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("after merge-main the checkout lacks %s: %v\n%s", name, err, out)
			}
		}
		if !strings.Contains(out, "the lane lane, which is 1 commit(s) behind main") {
			t.Errorf("merge-main does not say how far the lane is behind main:\n%s", out)
		}
	})

	t.Run("pull request into main: untouched", func(t *testing.T) {
		t.Parallel()
		dir := mergeMainCheckout(t, origin)
		before := mergeMainGit(t, dir, nil, "rev-parse", "HEAD")
		out, err := runMergeMain(t, dir, "pull_request", "main")
		if err != nil {
			t.Fatalf("merge-main failed on a pull request into main: %v\n%s", err, out)
		}
		if after := mergeMainGit(t, dir, nil, "rev-parse", "HEAD"); after != before {
			t.Fatalf("merge-main moved HEAD on a pull request into main: %s -> %s", before, after)
		}
	})

	t.Run("push: untouched", func(t *testing.T) {
		t.Parallel()
		dir := mergeMainCheckout(t, origin)
		before := mergeMainGit(t, dir, nil, "rev-parse", "HEAD")
		out, err := runMergeMain(t, dir, "push", "")
		if err != nil {
			t.Fatalf("merge-main failed on a push: %v\n%s", err, out)
		}
		if after := mergeMainGit(t, dir, nil, "rev-parse", "HEAD"); after != before {
			t.Fatalf("merge-main moved HEAD on a push: %s -> %s", before, after)
		}
	})

	t.Run("pull request with no base: loud", func(t *testing.T) {
		t.Parallel()
		dir := mergeMainCheckout(t, origin)
		out, err := runMergeMain(t, dir, "pull_request", "")
		if err == nil || !strings.Contains(out, "no GITHUB_BASE_REF") {
			t.Fatalf("merge-main passed a pull request it could not place: err=%v\n%s", err, out)
		}
	})
}

func TestMergeMainFailsALaneThatConflictsWithMain(t *testing.T) {
	t.Parallel()
	origin := mergeMainRepo(t, func(git func(...string) string, write func(string, string)) {
		write("shared.txt", "main's line\n")
		git("commit", "-q", "-m", "main edits shared.txt")
		git("checkout", "-q", "lane")
		write("shared.txt", "the lane's line\n")
		git("commit", "-q", "-m", "the lane edits shared.txt")
		git("checkout", "-q", "main")
	})
	dir := mergeMainCheckout(t, origin)
	out, err := runMergeMain(t, dir, "pull_request", "lane")
	if err == nil {
		t.Fatalf("merge-main passed a lane that conflicts with main:\n%s", out)
	}
	for _, want := range []string{"::error title=lane does not merge with main::", "the lane lane does not merge cleanly with main", "shared.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, out)
		}
	}
}
