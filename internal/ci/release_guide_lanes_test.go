package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseGuideOldPromise is the sentence docs/VERSIONING.md carried at v3.0.5,
// whitespace collapsed: it promised full certification before every release
// while release.yml's default lane ran CI fast only (F-170).
const releaseGuideOldPromise = "including the full certification run across every language, before the release is dispatched"

// releaseGuideFastLaneSkips are the workflows a fast-lane release runs none of
// before its tag; the guide's fast-lane paragraph names each.
var releaseGuideFastLaneSkips = []string{"ci-full.yml", "certify.yml", "nightly-conformance.yml", "tla-nightly.yml"}

// releaseGuideErrors holds the release guide to release.yml: each lane's
// guarantee is stated, the fast lane says what it does not run before the
// tag, and the lane the guide calls the default is the one the workflow
// defaults to.
func releaseGuideErrors(guide, releaseWF string) []string {
	g := strings.Join(strings.Fields(guide), " ")
	var errs []string
	if strings.Contains(g, releaseGuideOldPromise) {
		errs = append(errs, "the release guide promises full certification across every language before every release, and release.yml's fast lane runs CI fast only (F-170)")
	}
	full := strings.Contains(guide, "**The full lane**")
	fast := strings.Index(guide, "**The fast lane**")
	if !full || fast < 0 {
		errs = append(errs, "the release guide does not state both lanes' guarantees (**The full lane**, **The fast lane**)")
		return errs
	}
	// the fast lane's own list item: to the next item or the paragraph's end
	fastPara := guide[fast:]
	if end := strings.Index(fastPara, "\n\n"); end >= 0 {
		fastPara = fastPara[:end]
	}
	if end := strings.Index(fastPara, "\n  - "); end >= 0 {
		fastPara = fastPara[:end]
	}
	fastPara = strings.Join(strings.Fields(fastPara), " ")
	if !strings.Contains(fastPara, "does NOT run") {
		errs = append(errs, "the release guide's fast-lane paragraph does not say what a fast-lane release does NOT run before its tag")
	}
	for _, wf := range releaseGuideFastLaneSkips {
		if !strings.Contains(fastPara, "`"+wf+"`") {
			errs = append(errs, "the release guide's fast-lane paragraph does not name `"+wf+"` among what it does not run")
		}
	}
	if !strings.Contains(fastPara, "ci-fast.yml") {
		errs = append(errs, "the release guide's fast-lane paragraph does not name the CI fast run that locks it")
	}
	if strings.Contains(releaseWF, "        default: fast\n") != strings.Contains(fastPara, "the workflow's default") {
		errs = append(errs, "the release guide and release.yml disagree on which lane is the default")
	}
	return errs
}

// TestReleaseGuidePromisesWhatEachLaneRuns is F-170's regression (the
// reviewer, 2026-11-21): v3.0.5 was released on the fast lane with zero
// ci-full or certify runs at its commit while docs/VERSIONING.md promised
// full certification before each release. It holds the live guide against
// the live release.yml, and replays the committed corpus: testdata/f170's
// v3.0.5 paragraph must be refused, and the by-lane paragraph accepted.
func TestReleaseGuidePromisesWhatEachLaneRuns(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf, ok := workflows(t, root)["release.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/release.yml")
	}
	guide, err := os.ReadFile(filepath.Join(root, "docs", "VERSIONING.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range releaseGuideErrors(string(guide), wf) {
		t.Error(e)
	}

	for _, c := range []struct {
		file string
		red  bool
	}{
		{"release-tags-v3.0.5.md", true},
		{"release-tags-by-lane.md", false},
	} {
		data, err := os.ReadFile(filepath.Join("testdata", "f170", c.file))
		if err != nil {
			t.Fatalf("the F-170 corpus lost %s: %v", c.file, err)
		}
		errs := releaseGuideErrors(string(data), wf)
		switch {
		case c.red && len(errs) == 0:
			t.Errorf("CORPUS CASE FAILED: %s promises full certification for every release and the gate accepted it", c.file)
		case !c.red && len(errs) > 0:
			t.Errorf("CORPUS CONTROL FAILED: %s: %q", c.file, errs)
		}
	}

	g := string(guide)
	for _, c := range []struct {
		name string
		g    string
		wf   string
		want string
	}{
		{"the fast lane's list of what it skips dropped", strings.Replace(g, "A fast-lane release does NOT run", "A fast-lane release runs", 1), wf, "does NOT run"},
		{"certify.yml dropped from the list", strings.Replace(g, "`certify.yml` (every", "(every", 1), wf, "does not name `certify.yml`"},
		{"the workflow's default moved to full", g, strings.Replace(wf, "        default: fast\n", "        default: full\n", 1), "disagree on which lane is the default"},
	} {
		if c.g == g && c.wf == wf {
			t.Fatalf("control %q did not plant", c.name)
		}
		errs := strings.Join(releaseGuideErrors(c.g, c.wf), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %q: got %q, want an error containing %q", c.name, errs, c.want)
		}
	}
}
