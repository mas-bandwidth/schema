package ci

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCISupportCoversEveryPrerequisite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make (Python support prerequisite closure)")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("make", args...)
		cmd.Dir = repoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	db := map[string][]string{}
	for line := range strings.SplitSeq(run("-pn", "python-ci-support-list"), "\n") {
		if line == "" || strings.ContainsAny(line[:1], "#\t ") {
			continue
		}
		name, deps, ok := strings.Cut(line, ":")
		if ok && !strings.ContainsAny(name, " =") {
			db[name] = strings.Fields(deps)
		}
	}
	if len(db["test-python"]) == 0 {
		t.Fatal("make database contains no test-python prerequisites")
	}
	roots := strings.Fields(run("-s", "python-ci-support-list"))
	if missing := pythonSupportMissing(db, roots); len(missing) != 0 {
		t.Fatalf("test-python prerequisites absent from CI support rows: %v", missing)
	}
	// A new direct prerequisite must not silently fall outside the matrix.
	db["test-python"] = append(db["test-python"], "python-future-proof")
	if missing := pythonSupportMissing(db, roots); !slices.Contains(missing, "python-future-proof") {
		t.Fatalf("unregistered prerequisite escaped coverage: %v", missing)
	}
}

func pythonSupportMissing(db map[string][]string, roots []string) []string {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		for _, dep := range db[name] {
			walk(dep)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	var missing []string
	for _, dep := range db["test-python"] {
		if dep != "python-ci-support" && !seen[dep] {
			missing = append(missing, dep)
		}
	}
	return missing
}

func TestPythonCIPlannerProvisionsFixedCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Go package listing (Python CI matrix)")
	cmd := exec.Command("sh", "tools/python-ci-rows.sh")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan Python rows: %v\n%s", err, out)
	}
	var rows []struct {
		Target, Package string
		Corpus          bool
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	fixed, packages := 0, 0
	for _, row := range rows {
		if strings.HasPrefix(row.Target, "python-fixed-") || strings.Contains(row.Package, "/pythontable") {
			if !row.Corpus {
				t.Errorf("fixed row has no reference corpus: %+v", row)
			}
			if row.Target != "" {
				fixed++
			} else {
				packages++
			}
		}
	}
	if fixed < 19 || packages == 0 {
		t.Fatalf("fixed support=%d, table package rows=%d; want all fixed support and package rows", fixed, packages)
	}
	wf, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/python-leg.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(wf))
	for _, want := range []string{"timeout-minutes: 2", "sh tools/python-ci-corpus.sh build", "python-fixed-corpus-${{ github.sha }}"} {
		if !strings.Contains(jobs["fixed-corpus"], want) {
			t.Errorf("corpus producer lacks %q", want)
		}
	}
	consumer := jobs["test"]
	for _, want := range []string{"needs: [plan, fixed-corpus]", "sh tools/python-ci-corpus.sh restore", "if: matrix.corpus", "--old-file=build/fixedform-corpus/.stamp"} {
		if !strings.Contains(consumer, want) {
			t.Errorf("consumer lacks %q", want)
		}
	}
	if strings.Index(consumer, "sh tools/python-ci-corpus.sh restore") > strings.Index(consumer, "run the selected complete-leg slice") {
		t.Fatal("reference corpus is restored after the tests")
	}
}

func TestPythonCICorpusProvenanceAndDamageControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "shell (offline corpus artifact and refusal controls)")
	script := filepath.Join(repoRoot(t), "tools/python-ci-corpus.sh")
	for _, name := range []string{"round trip", "wrong source", "wrong runtime pin", "wrong producer runtime", "dirty source", "missing archive", "damaged bytes"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write := func(path, body string, mode os.FileMode) {
				t.Helper()
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			const pin = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			write("SIBLINGS", "../serialize example v1 "+pin+"\n", 0o600)
			write("bin/git", `#!/bin/sh
if [ "$1" = -C ]; then
  case "$3" in rev-parse) echo "${TEST_CPP_HEAD:-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}" ;; status) : ;; esac
else
  case "$1" in rev-parse) echo "${TEST_SOURCE_HEAD:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" ;; status) printf %s "${TEST_SOURCE_DIRTY:-}" ;; esac
fi
`, 0o700)
			write("bin/make", "#!/bin/sh\nmkdir -p build/fixedform-corpus\nprintf wire > build/fixedform-corpus/old_int_widen.bin\nprintf manifest > build/fixedform-corpus/manifest.txt\ntouch build/fixedform-corpus/.stamp\n", 0o700)
			run := func(mode string, vars ...string) ([]byte, error) {
				cmd := exec.Command("sh", script, mode)
				cmd.Dir = root
				cmd.Env = append(stripEnv(os.Environ(), "PATH"), "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))
				cmd.Env = append(cmd.Env, vars...)
				return cmd.CombinedOutput()
			}
			if out, err := run("build"); err != nil {
				t.Fatalf("build fixture artifact: %v\n%s", err, out)
			}
			var vars []string
			mode, want := "restore", ""
			switch name {
			case "wrong source":
				vars, want = []string{"TEST_SOURCE_HEAD=cccccccccccccccccccccccccccccccccccccccc"}, "source SHA mismatch"
			case "wrong runtime pin":
				write("SIBLINGS", "../serialize example v2 cccccccccccccccccccccccccccccccccccccccc\n", 0o600)
				want = "runtime pin mismatch"
			case "wrong producer runtime":
				mode, vars, want = "build", []string{"TEST_CPP_HEAD=cccccccccccccccccccccccccccccccccccccccc"}, "runtime does not match"
			case "dirty source":
				mode, vars, want = "build", []string{"TEST_SOURCE_DIRTY= M compiler/source.go"}, "source checkout is dirty"
			case "missing archive":
				if err := os.Remove(filepath.Join(root, "build/python-ci-fixed-corpus.tar")); err != nil {
					t.Fatal(err)
				}
				want = "Cannot open"
			case "damaged bytes":
				cmd := exec.Command("sh", "-ec", "mkdir unpack; tar -xf build/python-ci-fixed-corpus.tar -C unpack; printf damaged > unpack/fixedform-corpus/old_int_widen.bin; tar -cf build/python-ci-fixed-corpus.tar -C unpack .")
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("damage fixture: %v\n%s", err, out)
				}
				want = "FAILED"
			}
			out, err := run(mode, vars...)
			if want == "" {
				if err != nil {
					t.Fatalf("restore valid artifact: %v\n%s", err, out)
				}
			} else if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("%s did not refuse by %q: err=%v\n%s", name, want, err, out)
			}
		})
	}
}
