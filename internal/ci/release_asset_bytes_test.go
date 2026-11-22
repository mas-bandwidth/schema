package ci

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleasePublishesOnlyTheCurrentBuild is F-246's regression (the reviewer,
// 2026-11-21, with her patch): the publication step compared the draft's asset
// NAMES with the build's and then checked the downloaded archives against the
// downloaded SHA256SUMS, so a draft still carrying an earlier run's
// same-named archive and its own matching SHA256SUMS published. It runs the
// actual step from release.yml in bash against a stub gh (the committed corpus
// testdata/release-asset-replay: the stub, this run's archive and an earlier
// run's), inside t.TempDir, with no network: the draft holding this run's
// bytes publishes, and the coherent earlier pair is refused.
func TestReleasePublishesOnlyTheCurrentBuild(t *testing.T) {
	t.Parallel()
	wf := workflows(t, repoRoot(t))["release.yml"]
	_, rest, ok := strings.Cut(wf, "- name: check the draft's assets, then publish it as latest\n")
	if !ok {
		t.Fatal("no asset check and publication step")
	}
	_, script, ok := strings.Cut(rest, "\n        run: |\n")
	if !ok {
		t.Fatal("no asset check script")
	}
	fixtures, err := filepath.Abs(filepath.Join("testdata", "release-asset-replay"))
	if err != nil {
		t.Fatal(err)
	}
	const archive = "schema_v0.0.0_linux.tar.gz"
	for _, c := range []struct {
		name, remoteFile string
		publish          bool
	}{
		{"matching assets", "current-archive.txt", true},
		{"coherent earlier assets", "earlier-archive.txt", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			work := t.TempDir()
			for _, pair := range []struct{ dir, file string }{
				{filepath.Join(work, "build", "release-binaries"), "current-archive.txt"},
				{filepath.Join(work, "remote"), c.remoteFile},
			} {
				if err := os.MkdirAll(pair.dir, 0o755); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(fixtures, pair.file))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(pair.dir, archive), data, 0o644); err != nil {
					t.Fatal(err)
				}
				// SHA256SUMS as tools/releasebin writes it: "<hex>  <name>"
				sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), archive)
				if err := os.WriteFile(filepath.Join(pair.dir, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			published := filepath.Join(work, "published")
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "VERSION=v0.0.0", "RUNNER_TEMP="+work,
				"PATH="+fixtures+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ASSET_CORPUS_REMOTE="+filepath.Join(work, "remote"), "ASSET_CORPUS_PUBLISHED="+published)
			output, err := cmd.CombinedOutput()
			if c.publish && err != nil || !c.publish && err == nil {
				t.Errorf("publish=%v: the step's error=%v\n%s", c.publish, err, output)
			}
			if _, err := os.Stat(published); c.publish && err != nil || !c.publish && !os.IsNotExist(err) {
				t.Errorf("publish=%v: the stub's publication marker: %v", c.publish, err)
			}
		})
	}
}
