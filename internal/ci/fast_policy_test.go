package ci

import (
	"os"
	"os/exec"
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
		"    needs: ci-policy\n",
		"always() && !cancelled() && ",
		"          if [ \"$POLICY_RESULT\" != success ]; then\n",
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
	const statusPrefix = "always() && !cancelled() && "
	if lint == "" || policy == "" || strings.Replace(jobIfCondition(wf, "lint-fast"), statusPrefix, "", 1) != jobIfCondition(wf, "ci-policy") || !strings.Contains(jobIfCondition(wf, "lint-fast"), statusPrefix) {
		errs = append(errs, "policy and lint jobs must have the same admission, with status-aware lint")
	}
	const gate = `      - name: require CI policy success
        env:
          POLICY_RESULT: ${{ needs.ci-policy.result }}
        run: |
          if [ "$POLICY_RESULT" != success ]; then
            echo "::error::CI policy result: $POLICY_RESULT"
            exit 1
          fi
`
	if !strings.Contains(lint, "    needs: ci-policy\n") || strings.Count(lint, gate) != 1 {
		errs = append(errs, "lint-fast must depend on and enforce the policy result")
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

// The shell gate must accept only a successful dependency, including when the
// policy job was skipped or cancelled rather than reporting an ordinary failure.
func TestCIFastPolicyResultGate(t *testing.T) {
	const script = `if [ "$POLICY_RESULT" != success ]; then
  echo "::error::CI policy result: $POLICY_RESULT"
  exit 1
fi`
	for _, result := range []string{"success", "failure", "skipped", "cancelled", ""} {
		cmd := exec.Command("bash", "-e", "-c", script)
		cmd.Env = append(os.Environ(), "POLICY_RESULT="+result)
		err := cmd.Run()
		if (err == nil) != (result == "success") {
			t.Errorf("policy result %q: unexpected shell result %v", result, err)
		}
	}
}
