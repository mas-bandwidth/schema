package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The conformance legs used to each rebuild bin/schema, the harness and the
// generated tree from a wiped checkout. That build, not the run, was the
// 129-150s kill. conformance-matrix builds the three once and hands them
// over; a leg only freshen their timestamps (bin/schema, then the harness,
// then the stamp, so the stamp stays the newest) and builds its own driver.
// An empty checkout is newer than an artifact that kept the build job's
// clocks, so a leg that skips the freshening rebuilds the shared tree.

const conformanceSharedTouch = "" +
	"touch bin/schema\n" +
	"          touch build/conformance-harness\n" +
	"          touch build/tables-generated/.stamp"

func checkConformanceShared(wf, makefile string) []string {
	jobs := yamlJobs(wf)
	matrix, okM := jobs["conformance-matrix"]
	leg, okL := jobs["conformance"]
	if !okM || !okL {
		return []string{"ci-full.yml: conformance-matrix and conformance are the jobs that share the build"}
	}
	var errs []string
	if !strings.Contains(matrix, "make -j\"$(nproc)\" conformance-shared") {
		errs = append(errs, "conformance-matrix does not build conformance-shared once")
	}
	if !strings.Contains(matrix, "name: conformance-shared") || !strings.Contains(matrix, "bin/schema") ||
		!strings.Contains(matrix, "build/conformance-harness") || !strings.Contains(matrix, "build/tables-generated") {
		errs = append(errs, "conformance-matrix does not hand bin/schema, the harness and the generated tree to the legs")
	}
	if strings.Contains(matrix, "actions/cache/save@") {
		errs = append(errs, "conformance-matrix saves the Go cache, which ran past the job cap and skipped the artifact upload")
	}
	if cacheRestoreIsHostedOnly(matrix) {
		errs = append(errs, "conformance-matrix restores the Go build cache only on a github-hosted runner, so a fleet runner starts cold")
	}
	if !strings.Contains(leg, "actions/download-artifact@") || !strings.Contains(leg, "name: conformance-shared") {
		errs = append(errs, "a conformance leg does not download conformance-shared, so it rebuilds bin/schema, the harness and the generated tree from an empty checkout")
	}
	if !strings.Contains(leg, "chmod +x bin/schema build/conformance-harness") {
		errs = append(errs, "a conformance leg does not restore the executable bit the artifact drops")
	}
	if !strings.Contains(leg, conformanceSharedTouch) {
		errs = append(errs, "a conformance leg does not touch bin/schema, then the harness, then the stamp, so make rebuilds the shared tree over the empty checkout")
	}
	dl := strings.Index(leg, "actions/download-artifact@")
	touch := strings.Index(leg, conformanceSharedTouch)
	build := strings.Index(leg, "make -j\"$(nproc)\" ${{ matrix.targets }} ${{ matrix.env }}")
	if dl < 0 || touch < 0 || build < 0 || !(dl < touch && touch < build) {
		errs = append(errs, "a conformance leg does not freshen the shared inputs before building only its own driver")
	}
	if strings.Contains(leg, "build the harness and the") {
		errs = append(errs, "a conformance leg still builds the harness with its driver")
	}
	if cacheRestoreIsHostedOnly(leg) {
		errs = append(errs, "a conformance leg restores the Go build cache only on a github-hosted runner")
	}
	const target = "conformance-shared: bin/schema build/conformance-harness build/tables-generated/.stamp\n"
	if !strings.Contains(makefile, target) {
		errs = append(errs, "Makefile conformance-shared does not build bin/schema, the harness and the generated stamp")
	}
	return errs
}

// cacheRestoreIsHostedOnly reports whether this job's Go cache restore is
// skipped on a fleet runner. The if sits on the step, after the uses line.
func cacheRestoreIsHostedOnly(job string) bool {
	const needle = "actions/cache/restore@"
	i := strings.Index(job, needle)
	if i < 0 {
		return true
	}
	rest := job[i:]
	lines := strings.Split(rest, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "      - ") {
			break
		}
		b.WriteString("\n")
		b.WriteString(l)
	}
	return strings.Contains(b.String(), "runner.environment == 'github-hosted'")
}

func TestConformanceSharedInputsAreBuiltOnce(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	wf, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is the workflow this gate reads")
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range checkConformanceShared(wf, string(makefile)) {
		t.Error(e)
	}
}

// TestConformanceSharedInputsNegativeControls puts back each way a leg
// rebuilds the shared tree from an empty checkout, and requires the check
// to name it.
func TestConformanceSharedInputsNegativeControls(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	wf, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is the workflow these controls mutate")
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(makefile)
	cases := []struct {
		name, old, new, want string
	}{
		{
			name: "leg no longer downloads the shared tree",
			old:  "      - uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2\n        with:\n          name: conformance-shared\n          path: .\n",
			new:  "",
			want: "does not download conformance-shared",
		},
		{
			name: "stamp is not freshened",
			old:  conformanceSharedTouch,
			new:  "touch bin/schema\n          touch build/conformance-harness",
			want: "does not touch bin/schema, then the harness, then the stamp",
		},
		{
			name: "leg builds the harness again",
			old:  "      - name: build the ${{ matrix.lang }} driver\n",
			new:  "      - name: build the harness and the ${{ matrix.lang }} driver\n",
			want: "still builds the harness with its driver",
		},
		{
			name: "fleet cache restore gated off",
			old:  "      - uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0\n        with:\n",
			new:  "      - uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0\n        if: runner.environment == 'github-hosted'\n        with:\n",
			want: "only on a github-hosted runner",
		},
	}
	for _, c := range cases {
		if !strings.Contains(wf, c.old) {
			t.Fatalf("%s: ci-full.yml no longer carries %q", c.name, c.old)
		}
		// The fleet-cache control matches both jobs. Gate the leg's restore,
		// which is the second one, so the matrix job is not what the check names.
		got := wf
		if c.name == "fleet cache restore gated off" {
			jobs := yamlJobs(wf)
			leg := jobs["conformance"]
			if !strings.Contains(leg, c.old) {
				t.Fatalf("%s: the conformance job no longer carries the restore step", c.name)
			}
			got = strings.Replace(wf, leg, strings.Replace(leg, c.old, c.new, 1), 1)
		} else {
			got = strings.Replace(wf, c.old, c.new, 1)
		}
		errs := strings.Join(checkConformanceShared(got, mk), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("%s: the check did not report %q; it reported:\n%s", c.name, c.want, errs)
		}
	}
}
