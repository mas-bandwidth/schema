package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// mayCaseLabel is one `case` label of the verdict's may_skip function.
var mayCaseLabel = regexp.MustCompile(`^\s+([a-z0-9|*-]+)\)`)

// ciFastVerdictErrors holds ci-fast.yml's verdict job: main requires no status
// check (2026-11-21: no branch protection; the one ruleset naming a check is
// disabled and targets fixed-table-form), so the fast lane's gates are read
// only through one job that needs every other job of the file, runs whatever
// they did, is red on every result but success, and forgives a skip only for a
// job whose run the plan or the event decides: the gates that always run may
// never skip, and an unknown job's skip is red.
func ciFastVerdictErrors(wf string) []string {
	jobs := yamlJobs(wf)
	v, ok := jobs["verdict"]
	if !ok {
		return []string{"ci-fast.yml has no verdict job: a gate red, cancelled at its cap or skipped reads only on its own row, and main requires no check"}
	}
	var errs []string
	var needs, cond string
	for raw := range strings.SplitSeq(v, "\n") {
		if rest, ok := strings.CutPrefix(raw, "    needs:"); ok {
			needs = strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(raw, "    if:"); ok {
			cond = rest
		}
	}
	if !strings.Contains(cond, "always()") || !strings.Contains(cond, "!cancelled()") {
		errs = append(errs, "ci-fast.yml:verdict does not run always() && !cancelled(): a failed or cancelled gate skips the verdict")
	}
	needed := map[string]bool{}
	for part := range strings.SplitSeq(strings.Trim(needs, "[]"), ",") {
		if p := strings.TrimSpace(part); p != "" {
			needed[p] = true
		}
	}
	if !strings.Contains(v, `select(.value.result != "success")`) {
		errs = append(errs, "ci-fast.yml:verdict does not fail on every result but success")
	}
	cases := map[string]bool{}
	inMay := false
	for raw := range strings.SplitSeq(v, "\n") {
		if strings.Contains(raw, "may_skip() {") {
			inMay = true
			continue
		}
		if inMay && strings.TrimSpace(raw) == "esac" {
			break
		}
		if m := mayCaseLabel.FindStringSubmatch(raw); inMay && m != nil {
			for l := range strings.SplitSeq(m[1], "|") {
				cases[l] = true
			}
		}
	}
	if !cases["*"] || !strings.Contains(v, "*) return 1 ;;") {
		errs = append(errs, "ci-fast.yml:verdict's may_skip has no `*) return 1 ;;`: an unknown job's skip would read green")
	}
	var names []string
	for j := range jobs {
		names = append(names, j)
	}
	sort.Strings(names)
	for _, j := range names {
		if j == "verdict" {
			continue
		}
		if !needed[j] {
			errs = append(errs, "ci-fast.yml:verdict does not need "+j+": that gate red, cancelled or skipped would not turn the verdict red")
		}
		var jobIf string
		for raw := range strings.SplitSeq(jobs[j], "\n") {
			if rest, ok := strings.CutPrefix(raw, "    if:"); ok {
				jobIf = rest
			}
		}
		selected := j == "fork-pull-request" || strings.Contains(jobIf, "needs.plan.outputs") || strings.Contains(jobIf, "workflow_dispatch")
		switch {
		case selected && !cases[j]:
			errs = append(errs, "ci-fast.yml:verdict names no skip rule for "+j+", a job the plan or the event decides: its legitimate skip would read red")
		case !selected && cases[j]:
			errs = append(errs, "ci-fast.yml:verdict forgives a skip of "+j+", a gate that always runs")
		}
	}
	for c := range cases {
		if c != "*" && jobs[c] == "" {
			errs = append(errs, "ci-fast.yml:verdict's may_skip names "+c+", which is no job of the file")
		}
	}
	slices.Sort(errs)
	return errs
}

// TestCIFastVerdictNeedsEveryJob holds the live ci-fast.yml and replays the
// committed corpus testdata/fastverdict: control.yml is accepted, and every
// other file there (no verdict, a need missing, a skip forgiven for a gate
// that always runs, a verdict skipped on red, no default-red case) refused.
func TestCIFastVerdictNeedsEveryJob(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["ci-fast.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/ci-fast.yml")
	}
	for _, e := range ciFastVerdictErrors(wf) {
		t.Error(e)
	}
	files, err := provesran.Glob(filepath.Join("testdata", "fastverdict", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 6 {
		t.Fatalf("the fast-verdict corpus holds %d files, want the control and five red cases", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		errs := ciFastVerdictErrors(string(data))
		if filepath.Base(f) == "control.yml" {
			if len(errs) > 0 {
				t.Errorf("CORPUS CONTROL FAILED: %s: %q", f, errs)
			}
			continue
		}
		if len(errs) == 0 {
			t.Errorf("CORPUS CASE FAILED: %s was accepted", f)
		}
	}
}
