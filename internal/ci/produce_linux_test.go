//go:build linux

package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestAProducedTreeUnpacksOnlyForItsOwnSource holds tools/produce.sh to the
// rule tools/go-ci-corpus.sh keeps for the fixed corpus: a tree carried from
// the produce job stands in for a build only for the exact source and Go
// toolchain it was built from. Unpack stamps what it carries newer than the
// checkout, so make can never see a stale tree as stale; the identity check
// before it is all that stops one. Each refusal is named, and a matching tree
// stamps exactly the entries it holds and nothing beside them.
func TestAProducedTreeUnpacksOnlyForItsOwnSource(t *testing.T) {
	root := repoRoot(t)
	testinputs.Files(t, "tools/produce.sh")
	script := filepath.Join(root, "tools", "produce.sh")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, mode := range []string{"same-tree", "changed-tree", "other-toolchain", "no-identity", "dirty-source", "nothing-to-unpack"} {
		t.Run(mode, func(t *testing.T) {
			checkout := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", checkout, "-c", "user.name=Produce Test", "-c", "user.email=produce@test.invalid", "-c", "commit.gpgsign=false"}, args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(rel, data string) {
				t.Helper()
				p := filepath.Join(checkout, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			produce := func(args ...string) (string, error) {
				cmd := exec.Command("sh", append([]string{script}, args...)...)
				cmd.Dir = checkout
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			git("init", "--quiet")
			write("src.txt", "the source\n")
			git("add", "src.txt")
			git("commit", "--quiet", "-m", "producer merge")
			write("build/leg/bin/tool", "built\n")
			write("build/beside.txt", "not packed\n")
			// the tree carries old, distinct times, as a kept tree does
			for i, p := range []string{"build/leg/bin/tool", "build/leg/bin", "build/leg"} {
				at := old.Add(time.Duration(i) * time.Hour)
				if err := os.Chtimes(filepath.Join(checkout, p), at, at); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := produce("pack", "leg", "build/leg"); err != nil {
				t.Fatalf("pack: %v\n%s", err, out)
			}
			// a consumer merges on its own: another commit, the same tree
			tree := git("rev-parse", "HEAD^{tree}")
			git("commit", "--quiet", "--allow-empty", "-m", "consumer merge, same tree")
			if git("rev-parse", "HEAD^{tree}") != tree {
				t.Fatal("fixture: the consumer's tree must equal the producer's")
			}
			for _, p := range []string{"build/leg/bin/tool", "build/leg/bin", "build/leg", "build/beside.txt"} {
				if err := os.Chtimes(filepath.Join(checkout, p), old, old); err != nil {
					t.Fatal(err)
				}
			}
			dir := "build/produce"
			switch mode {
			case "changed-tree":
				write("src.txt", "main moved\n")
				git("commit", "--quiet", "-am", "a different merge")
			case "other-toolchain":
				write("build/produce/leg.id", tree+" go0.0-other\n")
			case "no-identity":
				if err := os.Remove(filepath.Join(checkout, "build/produce/leg.id")); err != nil {
					t.Fatal(err)
				}
			case "dirty-source":
				write("src.txt", "edited, not committed\n")
			case "nothing-to-unpack":
				dir = "build/empty"
				if err := os.MkdirAll(filepath.Join(checkout, dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			out, err := produce("unpack", dir)
			if (err == nil) != (mode == "same-tree") {
				t.Fatalf("%s: err=%v output=%s", mode, err, out)
			}
			want := map[string]string{
				"same-tree":         "unpacked build/produce/leg.tgz: 3 entries",
				"changed-tree":      "build/produce/leg.tgz was built from " + tree,
				"other-toolchain":   "was built from " + tree + " go0.0-other, this checkout is",
				"no-identity":       "build/produce/leg.tgz has no identity",
				"dirty-source":      "tracked source differs from HEAD",
				"nothing-to-unpack": "build/empty holds no tree to unpack",
			}[mode]
			if !strings.Contains(out, want) {
				t.Fatalf("%s: want %q, got %s", mode, want, out)
			}
			if mode != "same-tree" {
				for _, p := range []string{"build/leg/bin/tool", "build/leg"} {
					if fi, err := os.Stat(filepath.Join(checkout, p)); err != nil || !fi.ModTime().Equal(old) {
						t.Fatalf("%s: a refused unpack touched %s (%v)", mode, p, err)
					}
				}
				return
			}
			src, err := os.Stat(filepath.Join(checkout, "src.txt"))
			if err != nil {
				t.Fatal(err)
			}
			var stamp time.Time
			for _, p := range []string{"build/leg/bin/tool", "build/leg/bin", "build/leg"} {
				fi, err := os.Stat(filepath.Join(checkout, p))
				if err != nil {
					t.Fatal(err)
				}
				if !fi.ModTime().After(src.ModTime()) {
					t.Fatalf("%s is at %v, not after the checkout's source (%v): unpack did not stand it newer", p, fi.ModTime(), src.ModTime())
				}
				if !stamp.IsZero() && !fi.ModTime().Equal(stamp) {
					t.Fatalf("%s is at %v, another entry at %v: the carried tree is not at one time", p, fi.ModTime(), stamp)
				}
				stamp = fi.ModTime()
			}
			if fi, err := os.Stat(filepath.Join(checkout, "build/beside.txt")); err != nil || !fi.ModTime().Equal(old) {
				t.Fatalf("unpack touched build/beside.txt, which the tree does not hold (%v)", err)
			}
		})
	}
}
