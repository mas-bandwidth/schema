package ci

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoFixedCorpusArtifactRejectsWrongSourceOrBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux CI artifact contract")
	}
	root := repoRoot(t)
	revision, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
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
	for _, mode := range []string{"valid", "wrong-revision", "wrong-runtime", "changed-wire", "extra-wire", "missing-manifest"} {
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
			write(filepath.Join(dir, "go-fixed-corpus.revision"), revision)
			write(filepath.Join(dir, "go-fixed-corpus.runtime"), []byte(pin+"\n"))
			write(filepath.Join(dir, "go-fixed-corpus.sha256"), []byte(fmt.Sprintf("%x  ./manifest.txt\n%x  ./one.bin\n", sha256.Sum256(manifest), sha256.Sum256(wire))))
			switch mode {
			case "wrong-revision":
				write(filepath.Join(dir, "go-fixed-corpus.revision"), []byte(strings.Repeat("0", 40)+"\n"))
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
			cmd := exec.Command("sh", filepath.Join(root, "tools/go-ci-corpus.sh"), "verify", dir)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: err=%v output=%s", mode, err, out)
			}
		})
	}
}

func TestGoFixedCorpusPipelineKeepsCompleteTestRecipe(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(data))
	producer, tests := jobs["go-fixed-corpus"], jobs["go-test-touched"]
	for _, want := range []string{"tools/go-ci-corpus.sh produce", "actions/upload-artifact@", "go-fixed-corpus-${{ github.run_id }}-${{ github.sha }}", "timeout-minutes: 2"} {
		if !strings.Contains(producer, want) {
			t.Errorf("producer misses %s", want)
		}
	}
	for _, want := range []string{"needs: [plan, go-fixed-corpus, go-generated-support]", "needs.go-fixed-corpus.result == 'skipped'", "actions/download-artifact@", "matrix.make == 'tables-go-fixedform'", "tools/go-ci-corpus.sh verify", "make -o tables-fixedform-corpus tables-go-fixedform"} {
		if !strings.Contains(tests, want) {
			t.Errorf("test pipeline misses %s", want)
		}
	}
	if strings.Index(tests, "tools/go-ci-corpus.sh verify") > strings.Index(tests, "make -o tables-fixedform-corpus tables-go-fixedform") {
		t.Error("test recipe can run before artifact verification")
	}
	if !makeTargetSelectsCardAdded(t) {
		t.Fatal("the complete fixed-form recipe no longer selects the required case")
	}
}

func TestGoFixedCorpusDependencyStates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	event := ciEvent{Name: "pull_request", Branch: "dev", HeadRef: "stella/ci-proof", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema", PlanPkgs: "./internal/codegen/gotable"}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.GoCorpusResult = state
		got, err := jobRunsInWorkflow(string(data), "go-test-touched", event)
		want := state == "success"
		if err != nil || got != want {
			t.Errorf("producer %s: got%v want%v err%v", state, got, want, err)
		}
	}
	event.GoCorpusResult = "skipped"
	event.PlanRows = "[]"
	event.PlanPkgs = "./compiler"
	if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || !got {
		t.Fatalf("unrelated test row blocked by skipped producer: %v %v", got, err)
	}
	event.PlanRows = `[{"make":"tables-go-fixedform"}]`
	event.PlanPkgs = "./internal/codegen/gotable"
	event.GoCorpusResult = "success"
	event.PlanResult = "failure"
	if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || got {
		t.Fatalf("failed plan admitted tests: %v %v", got, err)
	}
	event.PlanResult = "success"
	event.PlanRows = `[{"make":"tables-go-fixedform"}]`
	if got, err := jobRunsInWorkflow(string(data), "go-fixed-corpus", event); err != nil || !got {
		t.Fatalf("selected producer missing: %v %v", got, err)
	}
	event.PlanRows = "[]"
	if got, err := jobRunsInWorkflow(string(data), "go-fixed-corpus", event); err != nil || got {
		t.Fatalf("unneeded producer admitted: %v %v", got, err)
	}
}
