package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// slowGateTableCorpus is the committed corpus of workflows the check below
// reads beside ci-full.yml: one clean control and each way the upload was, or
// could be, missing.
const slowGateTableCorpus = "testdata/slowgate-table-upload"

var pinnedUploadArtifact = regexp.MustCompile(`^uses: actions/upload-artifact@[0-9a-f]{40}$`)

// slowGateTableUploadFault reads a workflow's go-test job and says what keeps
// it from uploading each slow-gate row's coverage table, or "" when nothing
// does. The table is what the accounting prints and promises to upload; the
// rows uploaded only their go test transcripts (the reviewer's F-235). The
// upload must run on every slow-gate row, a red one most of all (always()),
// name the table itself, and be red when the row wrote none.
func slowGateTableUploadFault(name, workflow string) string {
	job := yamlJobs(workflow)["go-test"]
	if job == "" {
		return "no go-test job"
	}
	if !strings.Contains(job, "make slow-gate-coverage-") {
		return "the go-test job runs no slow-gate accounting"
	}
	var steps [][]line
	for _, l := range lines(name, job) {
		if strings.HasPrefix(l.full, "- ") {
			steps = append(steps, nil)
		}
		if len(steps) > 0 {
			steps[len(steps)-1] = append(steps[len(steps)-1], l)
		}
	}
	for _, step := range steps {
		fields := map[string]string{}
		for _, l := range step {
			key, value, ok := strings.Cut(l.text, ":")
			if ok {
				fields[key] = strings.TrimSpace(value)
			}
		}
		if fields["path"] != "build/slowgate/coverage.tsv" {
			continue
		}
		if !pinnedUploadArtifact.MatchString("uses: " + fields["uses"]) {
			return step[0].where + ": the step naming build/slowgate/coverage.tsv is not a pinned actions/upload-artifact"
		}
		if fields["if"] != "always() && startsWith(matrix.group, 'slow-gate-')" {
			return step[0].where + ": the table upload runs under if: " + fields["if"] + ", not on every slow-gate row whatever its verdict"
		}
		if fields["name"] != "slow-gate-coverage-${{ matrix.group }}" {
			return step[0].where + ": the table upload is named " + fields["name"] + ", not one artifact per row"
		}
		if fields["if-no-files-found"] != "error" {
			return step[0].where + ": a slow-gate row that wrote no table uploads nothing and stays green (if-no-files-found: " + fields["if-no-files-found"] + ")"
		}
		return ""
	}
	return "no step of the go-test job uploads build/slowgate/coverage.tsv"
}

// TestSlowGateRowsUploadTheirCoverageTable holds ci-full.yml to the upload
// the slow-gate accounting promises, and proves the check on its committed
// corpus first: the control passes and every broken form is refused.
func TestSlowGateRowsUploadTheirCoverageTable(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(slowGateTableCorpus, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	refused, clean := 0, 0
	for n, row := range strings.Split(string(data), "\n") {
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		f := strings.Split(row, "\t")
		if len(f) != 3 || (f[2] != "ok" && f[2] != "refused") {
			t.Fatalf("cases.tsv line %d: want id, fixture, ok|refused; got %q", n+1, row)
		}
		wf, err := os.ReadFile(filepath.Join(slowGateTableCorpus, f[1]))
		if err != nil {
			t.Fatal(err)
		}
		fault := slowGateTableUploadFault(f[1], string(wf))
		switch {
		case f[2] == "ok" && fault != "":
			t.Errorf("%s: the control is refused: %s", f[0], fault)
		case f[2] == "refused" && fault == "":
			t.Errorf("%s: %s is accepted, want it refused", f[0], f[1])
		}
		if f[2] == "ok" {
			clean++
		} else {
			refused++
		}
	}
	if refused == 0 || clean == 0 {
		t.Fatalf("the corpus holds %d refused and %d clean rows: a refusal is only proven beside a clean control", refused, clean)
	}

	wf, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if fault := slowGateTableUploadFault("ci-full.yml", string(wf)); fault != "" {
		t.Errorf("ci-full.yml: %s", fault)
	}
}
