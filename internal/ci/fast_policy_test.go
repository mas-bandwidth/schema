package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The CI policy Go tests are registry rows the schema-test-pr shards run
// (docs/TEST-REGISTRY.md); the policy checks that are not Go tests run in
// the ci-policy job. lint-fast is the required check that carries their
// results: it is admitted exactly as ci-policy is, status-aware, and fails
// unless ci-policy, schema-test and schema-test-pr all succeeded.
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
		"        run: bash -n bench/run.sh",
		"        run: make python-tooling-tests",
		"          go run ./tools/schema-test run --group pr --lang none --shard \"$SHARD\"\n",
		"  ci-policy:\n",
		"    needs: [ci-policy, schema-test, schema-test-pr]\n",
		"always() && !cancelled() && ",
		"          if [ \"$POLICY_RESULT\" != success ] || [ \"$REGISTRY_RESULT\" != success ] || [ \"$SHARDS_RESULT\" != success ]; then\n",
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
	lint, policy, shards := jobs["lint-fast"], jobs["ci-policy"], jobs["schema-test-pr"]
	var errs []string
	// lint-fast is admitted exactly as ci-policy is, and status-aware so it
	// reports when a job it needs fails. No branch-name prefix on either
	// (TestNoJobRunsOnABranchName).
	policyCond := jobIfCondition(wf, "ci-policy")
	wantLint := "(always() && !cancelled() && " + policyCond + ")"
	if lint == "" || policy == "" || policyCond == "" || jobIfCondition(wf, "lint-fast") != wantLint {
		errs = append(errs, "policy and lint jobs must have the same admission, with status-aware lint")
	}
	const gate = `      - name: require CI policy and the registry's pr group success
        env:
          POLICY_RESULT: ${{ needs.ci-policy.result }}
          REGISTRY_RESULT: ${{ needs.schema-test.result }}
          SHARDS_RESULT: ${{ needs.schema-test-pr.result }}
        run: |
          if [ "$POLICY_RESULT" != success ] || [ "$REGISTRY_RESULT" != success ] || [ "$SHARDS_RESULT" != success ]; then
            echo "::error::results: ci-policy $POLICY_RESULT, schema-test $REGISTRY_RESULT, schema-test-pr $SHARDS_RESULT"
            exit 1
          fi
`
	if !strings.Contains(lint, "    needs: [ci-policy, schema-test, schema-test-pr]\n") || strings.Count(lint, gate) != 1 {
		errs = append(errs, "lint-fast must depend on and enforce the policy and registry results")
	}
	if strings.Count(shards, `go run ./tools/schema-test run --group pr --lang none --shard "$SHARD"`) != 1 {
		errs = append(errs, "schema-test-pr must run the registry's pr group's language-free part by shard")
	}
	for _, command := range []string{"run: bash -n bench/run.sh", "run: make python-tooling-tests"} {
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

// The shell gate must accept only three successful dependencies, including
// when one was skipped or cancelled rather than reporting an ordinary
// failure.
func TestCIFastPolicyResultGate(t *testing.T) {
	const script = `if [ "$POLICY_RESULT" != success ] || [ "$REGISTRY_RESULT" != success ] || [ "$SHARDS_RESULT" != success ]; then
  echo "::error::results: ci-policy $POLICY_RESULT, schema-test $REGISTRY_RESULT, schema-test-pr $SHARDS_RESULT"
  exit 1
fi`
	results := []string{"success", "failure", "skipped", "cancelled", ""}
	for _, policy := range results {
		for _, registry := range results {
			for _, shards := range results {
				// testinputs:none — runs the script constant above; reads no file
				cmd := exec.Command("bash", "-e", "-c", script)
				cmd.Env = append(os.Environ(), "POLICY_RESULT="+policy, "REGISTRY_RESULT="+registry, "SHARDS_RESULT="+shards)
				err := cmd.Run()
				if (err == nil) != (policy == "success" && registry == "success" && shards == "success") {
					t.Errorf("policy %q, registry %q, shards %q: unexpected shell result %v", policy, registry, shards, err)
				}
			}
		}
	}
}
