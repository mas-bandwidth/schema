package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// proofSnapshotFault reads a workflow's proof job and says what lets its
// proof-snapshot artifact carry something other than the snapshot the run's
// results computed, or "" when nothing does. The write step overwrites the
// checked-out test/proof/proof.json; if it fails (no legs downloaded, a
// refused input), an upload under bare always() would publish the COMMITTED
// file under a name that says it was computed: a stale snapshot passed off
// as fresh. So the write step carries an id, and the upload runs only when
// that step succeeded, and is red when it finds no file.
func proofSnapshotFault(name, workflow string) string {
	job := yamlJobs(workflow)["proof"]
	if job == "" {
		return "no proof job"
	}
	var steps []map[string]string
	var where []string
	for _, l := range lines(name, job) {
		if strings.HasPrefix(l.full, "- ") {
			steps = append(steps, map[string]string{})
			where = append(where, l.where)
		}
		if len(steps) == 0 {
			continue
		}
		key, value, ok := strings.Cut(l.text, ":")
		if ok {
			steps[len(steps)-1][key] = strings.TrimSpace(value)
		}
	}
	writeID := ""
	for i, s := range steps {
		if strings.Contains(s["run"], "tools/matrix proof") && strings.Contains(s["run"], "-write") {
			if s["id"] == "" {
				return where[i] + ": the snapshot write step has no id, so no later step can ask whether it succeeded"
			}
			writeID = s["id"]
			continue
		}
		if s["path"] != "test/proof/proof.json" {
			continue
		}
		if writeID == "" {
			return where[i] + ": the snapshot upload comes before (or without) the step that writes it"
		}
		if want := "always() && steps." + writeID + ".outcome == 'success'"; s["if"] != want {
			return where[i] + ": the snapshot upload runs under if: " + s["if"] + ", not only when the write step succeeded (" + want + "): a failed write would publish the committed proof.json as computed"
		}
		if s["if-no-files-found"] != "error" {
			return where[i] + ": the snapshot upload is green with no file (if-no-files-found: " + s["if-no-files-found"] + ")"
		}
		return ""
	}
	return "no step of the proof job uploads test/proof/proof.json"
}

// TestProofSnapshotUploadOnlyAfterItsWrite holds ci-full.yml's proof job to
// it, and proves the check first: the workflow with the gate taken off the
// upload, or the id taken off the write, is refused.
func TestProofSnapshotUploadOnlyAfterItsWrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRoot(t), ".github", "workflows", "ci-full.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if fault := proofSnapshotFault(path, workflow); fault != "" {
		t.Fatal(fault)
	}
	gate := "if: always() && steps.snapshot.outcome == 'success'"
	if strings.Count(workflow, gate) != 1 {
		t.Fatalf("%s names %q %d times, want once: the controls below would plant nothing", path, gate, strings.Count(workflow, gate))
	}
	for plant, bad := range map[string]string{
		"the upload under bare always()": strings.Replace(workflow, gate, "if: always()", 1),
		"the write step with no id":      strings.Replace(workflow, "        id: snapshot\n", "", 1),
	} {
		if bad == workflow {
			t.Fatalf("%s: the plant changed nothing", plant)
		}
		if proofSnapshotFault(path, bad) == "" {
			t.Errorf("%s: the check passed it", plant)
		}
	}
}
