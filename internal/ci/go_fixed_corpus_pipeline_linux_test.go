//go:build linux

package ci

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

func TestGoFixedCorpusArtifactRejectsWrongSourceOrBytes(t *testing.T) {
	root := repoRoot(t)
	pins, err := os.ReadFile(filepath.Join(root, "SIBLINGS"))
	if err != nil {
		t.Fatal(err)
	}
	pin := ""
	for line := range strings.SplitSeq(string(pins), "\n") {
		f := strings.Fields(line)
		if len(f) == 4 && f[0] == "../serialize" {
			pin = f[3]
		}
	}
	if pin == "" {
		t.Fatal("missing expected runtime pin")
	}
	checkout := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", checkout, "-c", "user.name=Corpus Test", "-c", "user.email=corpus@test.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "--quiet")
	if err := os.WriteFile(filepath.Join(checkout, "SIBLINGS"), pins, 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "SIBLINGS")
	git("commit", "--quiet", "-m", "producer merge")
	producer := git("rev-parse", "HEAD")
	tree := git("rev-parse", "HEAD^{tree}")
	git("commit", "--quiet", "--allow-empty", "-m", "distinct consumer merge, identical tree")
	if git("rev-parse", "HEAD") == producer || git("rev-parse", "HEAD^{tree}") != tree {
		t.Fatal("fixture must use distinct commits with identical trees")
	}
	for _, mode := range []string{"same-tree", "wrong-runtime", "changed-wire", "extra-wire", "missing-manifest", "changed-tree"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			corpus := filepath.Join(dir, "fixedform-corpus")
			if err := os.MkdirAll(corpus, 0700); err != nil {
				t.Fatal(err)
			}
			wire, manifest := []byte("actual producer bytes\n"), []byte("file=one.bin root=One\n")
			write := func(path string, data []byte) {
				t.Helper()
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(corpus, "one.bin"), wire)
			write(filepath.Join(corpus, "manifest.txt"), manifest)
			write(filepath.Join(dir, "go-fixed-corpus.revision"), []byte(tree))
			write(filepath.Join(dir, "go-fixed-corpus.runtime"), []byte(pin+"\n"))
			write(filepath.Join(dir, "go-fixed-corpus.sha256"), []byte(fmt.Sprintf("%x  ./manifest.txt\n%x  ./one.bin\n", sha256.Sum256(manifest), sha256.Sum256(wire))))
			switch mode {
			case "changed-tree":
				write(filepath.Join(checkout, "different-source.txt"), []byte("a different merged tree\n"))
				git("add", "different-source.txt")
				git("commit", "--quiet", "-m", "a genuinely different merge")
				if git("rev-parse", "HEAD^{tree}") == tree {
					t.Fatal("fixture must change the consumer tree")
				}
			case "wrong-runtime":
				write(filepath.Join(dir, "go-fixed-corpus.runtime"), []byte(strings.Repeat("0", 40)+"\n"))
			case "changed-wire":
				write(filepath.Join(corpus, "one.bin"), []byte("changed\n"))
			case "extra-wire":
				write(filepath.Join(corpus, "extra.bin"), wire)
			case "missing-manifest":
				if err := os.Remove(filepath.Join(corpus, "manifest.txt")); err != nil {
					t.Fatal(err)
				}
			}
			testinputs.Files(t, "tools/go-ci-corpus.sh")
			cmd := exec.Command("sh", filepath.Join(root, "tools/go-ci-corpus.sh"), "verify", dir)
			cmd.Dir = checkout
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "same-tree") {
				t.Fatalf("%s: err=%v output=%s", mode, err, out)
			}
			want := map[string]string{
				"wrong-runtime":    "fixed corpus runtime pin mismatch",
				"changed-wire":     "fixed corpus inventory or bytes differ from its producer",
				"extra-wire":       "fixed corpus inventory or bytes differ from its producer",
				"missing-manifest": "fixed corpus manifest is missing",
				"changed-tree":     "fixed corpus source tree does not match this checkout",
			}[mode]
			if !strings.Contains(string(out), want) {
				t.Fatalf("%s: want %q, got %s", mode, want, out)
			}
		})
	}
}
