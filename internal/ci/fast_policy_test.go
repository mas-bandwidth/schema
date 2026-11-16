package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The policy checks must run with the same admission as the fast lint job,
// and remain in a separate bounded job so all checks can finish.
func TestCIFastPolicyJobCoverage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	if errors := fastPolicyErrors(wf); len(errors) != 0 {
		t.Fatal(strings.Join(errors, "\n"))
	}
	for _, needle := range []string{
		"        run: make slow-gate-scan",
		"        run: go test ./internal/ci/",
		"        run: bash -n bench/run.sh",
		"  ci-policy:\n",
	} {
		if !strings.Contains(wf, needle) {
			t.Fatalf("missing mutation target %q", needle)
		}
		if len(fastPolicyErrors(strings.Replace(wf, needle, "", 1))) == 0 {
			t.Errorf("removing %q escaped policy guard", needle)
		}
	}
}

func fastPolicyErrors(wf string) []string {
	jobs := yamlJobs(wf)
	lint, policy := jobs["lint-fast"], jobs["ci-policy"]
	var errs []string
	if lint == "" || policy == "" || jobIfCondition(wf, "ci-policy") != jobIfCondition(wf, "lint-fast") {
		errs = append(errs, "policy job must have the same admission as lint-fast")
	}
	for _, command := range []string{"run: make slow-gate-scan", "run: go test ./internal/ci/", "run: bash -n bench/run.sh"} {
		if strings.Count(policy, command) != 1 || strings.Contains(lint, command) {
			errs = append(errs, "policy command must occur once in its own job: "+command)
		}
	}
	for _, command := range []string{"gofmt -l .", "go mod tidy -diff", "modernize ./..."} {
		if strings.Count(lint, command) != 1 {
			errs = append(errs, "lint-fast must retain "+command)
		}
	}
	return errs
}
