package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// THE CONFORMANCE ROWS RUN NIGHTLY, EVERY LEG, AND FAIL LOUDLY (2026-11-24).
// Every leg's rows job (make conformance-rows-<leg>, tools/conformancerows)
// took 80-203 s on a loaded bench against the per-PR two minutes, which are
// never raised, so the rows moved out of ci-fast.yml into ci-full.yml's
// nightly `conformance-rows` job, which has no time limit. The move must drop
// nothing, so this test holds:
//
//   - no workflow a push or a pull request starts runs a conformance-rows
//     target or job (a leg comes back per-PR only by changing this test,
//     once it fits in 60 s on a CI runner); the ten rows the per-PR verify
//     targets ran before still run there, through make, never named in a
//     workflow (conformance_rows_pr_test.go);
//   - ci-full.yml's conformance-rows matrix is exactly the legs with a
//     test/conformance/<leg>/rows directory, Go's aside (its rows are TestRow
//     functions go test runs);
//   - the job runs `make conformance-rows-<leg>` for its row, is not
//     report-only (no continue-on-error, no `|| true`), and the verdict
//     needs it.
//
// The reversed witness puts each breach into a copy of the workflows and holds
// that each one is named.

var conformanceRowsMatrixLeg = regexp.MustCompile(`(?m)^        leg: \[([^\]]*)\]\s*$`)

// conformanceRowsLegs are the legs whose rows the runner runs: every
// test/conformance/<leg>/rows directory but Go's.
func conformanceRowsLegs(t *testing.T, root string) []string {
	t.Helper()
	dirs, err := provesran.Glob(filepath.Join(root, "test", "conformance", "*", "rows"))
	if err != nil {
		t.Fatal(err)
	}
	var legs []string
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			if leg := filepath.Base(filepath.Dir(d)); leg != "go" {
				legs = append(legs, leg)
			}
		}
	}
	sort.Strings(legs)
	if len(legs) == 0 {
		t.Fatal("no test/conformance/<leg>/rows directory: this gate would pass over an empty set")
	}
	return legs
}

// uncommented drops a workflow line's comment.
func uncommented(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	if before, _, ok := strings.Cut(line, " #"); ok {
		return before
	}
	return line
}

func conformanceRowsLaneErrors(wfs map[string]string, legs []string) []string {
	var errs []string
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !workflowStartsOnPushOrPR(wfs[name]) {
			continue
		}
		for i, raw := range strings.Split(wfs[name], "\n") {
			if strings.Contains(uncommented(raw), "conformance-rows") {
				errs = append(errs, fmt.Sprintf("%s:%d runs the conformance rows on a push or a pull request (%s): they run nightly in ci-full.yml until a leg fits in 60 s on a CI runner", name, i+1, strings.TrimSpace(raw)))
			}
		}
	}
	full, ok := wfs["ci-full.yml"]
	if !ok {
		return append(errs, "ci-full.yml is missing: the conformance rows have no nightly job")
	}
	if workflowStartsOnPushOrPR(full) {
		errs = append(errs, "ci-full.yml starts on a push or a pull request: its conformance-rows job would run per-PR past the two minutes")
	}
	job, ok := yamlJobs(full)["conformance-rows"]
	if !ok {
		return append(errs, "ci-full.yml has no conformance-rows job: the rows of "+strings.Join(legs, ", ")+" would run nowhere")
	}
	m := conformanceRowsMatrixLeg.FindStringSubmatch(job)
	if m == nil {
		errs = append(errs, "ci-full.yml conformance-rows has no `leg: [...]` matrix")
	} else {
		var got []string
		for part := range strings.SplitSeq(m[1], ",") {
			if p := strings.TrimSpace(part); p != "" {
				got = append(got, p)
			}
		}
		sort.Strings(got)
		for _, leg := range legs {
			if !slices.Contains(got, leg) {
				errs = append(errs, fmt.Sprintf("ci-full.yml conformance-rows does not run the %s rows (test/conformance/%s/rows is on disk)", leg, leg))
			}
		}
		for _, leg := range got {
			if !slices.Contains(legs, leg) {
				errs = append(errs, fmt.Sprintf("ci-full.yml conformance-rows names %s, which has no test/conformance/%s/rows directory (or is Go's, run by go test)", leg, leg))
			}
		}
	}
	runs := false
	for i, raw := range strings.Split(job, "\n") {
		line := uncommented(raw)
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "continue-on-error:") && !strings.HasSuffix(t, "false") {
			errs = append(errs, fmt.Sprintf("ci-full.yml conformance-rows line %d is %q: a red row would read green", i+1, t))
		}
		if strings.Contains(line, "|| true") || strings.Contains(line, "|| :") {
			errs = append(errs, fmt.Sprintf("ci-full.yml conformance-rows line %d swallows a failure: %q", i+1, t))
		}
		if strings.Contains(line, "make ") && strings.Contains(line, "conformance-rows-") {
			runs = true
		}
	}
	if !runs {
		errs = append(errs, "ci-full.yml conformance-rows runs no `make conformance-rows-<leg>`")
	}
	if v, ok := yamlJobs(full)["verdict"]; !ok || !slices.Contains(jobNeeds(v), "conformance-rows") {
		errs = append(errs, "ci-full.yml verdict does not need conformance-rows: a red row would not fail the run")
	}
	return errs
}

func TestConformanceRowsRunNightlyOnEveryLeg(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, e := range conformanceRowsLaneErrors(workflows(t, root), conformanceRowsLegs(t, root)) {
		t.Error(e)
	}
}

func TestConformanceRowsRunNightlyOnEveryLegNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	legs := conformanceRowsLegs(t, root)
	if errs := conformanceRowsLaneErrors(wfs, legs); len(errs) > 0 {
		t.Fatalf("the tree is not clean, so the witnesses below prove nothing: %v", errs)
	}
	full := wfs["ci-full.yml"]
	m := conformanceRowsMatrixLeg.FindString(full)
	if m == "" {
		t.Fatal("no conformance-rows matrix in ci-full.yml")
	}
	const step = "      - name: make conformance-rows-${{ matrix.leg }}\n        run: |\n"
	if strings.Count(full, step) != 1 {
		t.Fatalf("the conformance-rows make step does not stand once in ci-full.yml:\n%s", step)
	}
	cases := []struct {
		name string
		wf   string
		text string
		want string
	}{
		{"a per-PR rows step", "ci-fast.yml",
			strings.Replace(wfs["ci-fast.yml"], "\njobs:\n", "\njobs:\n  rows-again:\n    runs-on: ubuntu-latest\n    timeout-minutes: 2\n    steps:\n      - run: make conformance-rows-c\n", 1),
			"ci-fast.yml:"},
		{"a leg dropped", "ci-full.yml", strings.Replace(full, m, strings.Replace(m, " js,", "", 1), 1),
			"does not run the js rows"},
		{"report-only", "ci-full.yml", strings.Replace(full, step, step[:strings.Index(step, "\n")+1]+"        continue-on-error: true\n"+step[strings.Index(step, "\n")+1:], 1),
			"continue-on-error: true"},
		{"a swallowed failure", "ci-full.yml", strings.Replace(full, `make -j"$(nproc)" conformance-rows-js NODE=node ;;`, `make -j"$(nproc)" conformance-rows-js NODE=node || true ;;`, 1),
			"swallows a failure"},
		{"off the verdict", "ci-full.yml", strings.Replace(full, " conformance-rows, proof,", " proof,", 1),
			"verdict does not need conformance-rows"},
	}
	for _, tc := range cases {
		if tc.text == wfs[tc.wf] {
			t.Errorf("%s: the sabotage did not change %s", tc.name, tc.wf)
			continue
		}
		sab := map[string]string{}
		for k, v := range wfs {
			sab[k] = v
		}
		sab[tc.wf] = tc.text
		errs := conformanceRowsLaneErrors(sab, legs)
		if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, tc.want) }) {
			t.Errorf("%s: no error names %q; got %v", tc.name, tc.want, errs)
		}
	}
}
