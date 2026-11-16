package ci

import (
	"os/exec"
	"strings"
	"testing"
)

// The listed support rows must cover the existing local test-python contract;
// discovering Go test names alone cannot carry these make-only controls.
func TestPythonSupportPrerequisiteClosure(t *testing.T) {
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
		t.Fatal("missing test-python contract")
	}
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
	for _, root := range strings.Fields(run("-s", "python-ci-support-list")) {
		walk(root)
	}
	for _, dep := range db["test-python"] {
		if dep != "python-ci-support" && !seen[dep] {
			t.Errorf("local Python prerequisite omitted from CI support roots: %s", dep)
		}
	}
	// An unregistered future direct prerequisite must stay outside the closure.
	if seen["python-future-proof"] {
		t.Fatal("future-prerequisite control is not isolated")
	}
	db["test-python"] = append(db["test-python"], "python-future-proof")
	missing := false
	for _, dep := range db["test-python"] {
		missing = missing || (dep == "python-future-proof" && !seen[dep])
	}
	if !missing {
		t.Fatal("unregistered future prerequisite was silently accepted")
	}
}
