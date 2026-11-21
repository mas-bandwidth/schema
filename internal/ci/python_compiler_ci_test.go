package ci

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// The per-commit rows must cover the same namespace as the unsplit aggregate,
// exactly once. Deleting a row is a control for the coverage check itself.
func TestPythonCompilerCIShardCoverage(t *testing.T) {
	slowtest.Gate(t, "Go compiler tests and Python CI planner")
	root := repoRoot(t)
	run := func(name string, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return out
	}
	listed := strings.Split(string(run("go", "test", "-tags", "schema_leg_python", "-list", "Python", "./compiler")), "\n")
	var wanted []string
	for _, name := range listed {
		if regexp.MustCompile(`^(Test|Example|Fuzz)[A-Za-z0-9_]*$`).MatchString(name) {
			wanted = append(wanted, name)
		}
	}
	plan := pythonCIPlan(t)
	var rows []struct{ Target string }
	if err := json.Unmarshal(plan, &rows); err != nil {
		t.Fatal(err)
	}
	var original, combined []json.RawMessage
	if err := json.Unmarshal(plan, &original); err != nil {
		t.Fatal(err)
	}
	seenRows := map[string]bool{}
	for _, row := range original {
		key := string(row)
		if seenRows[key] {
			t.Fatalf("duplicate row in Python CI plan: %s", key)
		}
		seenRows[key] = true
	}
	for _, part := range []string{"0", "1"} {
		cmd := exec.Command("sh", "tools/python-ci-partition.sh", part)
		cmd.Dir = root
		cmd.Stdin = bytes.NewReader(plan)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("partition %s: %v\n%s", part, err, out)
		}
		var shard []json.RawMessage
		if err := json.Unmarshal(out, &shard); err != nil {
			t.Fatal(err)
		}
		if len(shard) == 0 || len(shard) > 256 {
			t.Fatalf("partition %s has %d rows", part, len(shard))
		}
		combined = append(combined, shard...)
	}
	if !pythonPlanCoverage(original, combined) {
		t.Fatal("matrix partitions lose or duplicate plan rows")
	}
	if pythonPlanCoverage(original, combined[1:]) {
		t.Fatal("missing matrix row escaped coverage control")
	}
	if pythonPlanCoverage(original, append(slices.Clone(combined), combined[0])) {
		t.Fatal("duplicate matrix row escaped coverage control")
	}
	if pythonPlanCoverage(original, append(slices.Clone(combined[1:]), combined[1])) {
		t.Fatal("substituting a duplicate for a missing row escaped coverage control")
	}
	// Exercise the actual limit, rather than only today's smaller plan.
	synthetic := make([]int, 513)
	for i := range synthetic {
		synthetic[i] = i
	}
	for _, size := range []int{512, 513} {
		payload, err := json.Marshal(synthetic[:size])
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"0", "1"} {
			cmd := exec.Command("sh", "tools/python-ci-partition.sh", part)
			cmd.Dir = root
			cmd.Stdin = bytes.NewReader(payload)
			out, err := cmd.CombinedOutput()
			if size == 513 {
				if err == nil {
					t.Fatal("oversized matrix plan was accepted")
				}
				continue
			}
			var shard []int
			if err != nil || json.Unmarshal(out, &shard) != nil || len(shard) != 256 {
				t.Fatalf("512-row plan partition %s: %v %s", part, err, out)
			}
		}
	}
	wf, err := os.ReadFile(root + "/.github/workflows/python-leg.yml")
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(wf))
	for _, required := range []string{"rows0: ${{ steps.rows.outputs.rows0 }}", "rows1: ${{ steps.rows.outputs.rows1 }}", "sh tools/python-ci-partition.sh 0", "sh tools/python-ci-partition.sh 1"} {
		if !strings.Contains(jobs["plan"], required) {
			t.Errorf("Python plan lacks %q", required)
		}
	}
	first, second := jobs["test"], jobs["test-second"]
	firstBody := strings.TrimSpace(strings.TrimPrefix(first, "  test:"))
	secondBody := strings.TrimSpace(strings.TrimPrefix(second, "  test-second:"))
	if first == "" || second == "" || !strings.Contains(first, "needs.plan.outputs.rows0") || !strings.Contains(second, "needs.plan.outputs.rows1") || !strings.Contains(first, "max-parallel: 4") || strings.Replace(firstBody, "needs.plan.outputs.rows0", "needs.plan.outputs.rows1", 1) != secondBody {
		t.Fatal("Python matrix jobs differ in setup, execution, or partition wiring")
	}
	if !pythonMatrixGoConcurrency(jobs) {
		t.Fatal("both Python matrix consumers must cap Go concurrency at 2")
	}
	capLine := "\n    env:\n      GOMAXPROCS: '2'\n"
	for _, name := range []string{"test", "test-second"} {
		mutated := map[string]string{"test": first, "test-second": second}
		mutated[name] = strings.Replace(mutated[name], capLine, "\n", 1)
		if pythonMatrixGoConcurrency(mutated) {
			t.Fatalf("missing Go concurrency cap in %s escaped guard", name)
		}
	}
	mutated := map[string]string{"test": strings.Replace(first, capLine, "\n", 1), "test-second": strings.Replace(second, capLine, "\n", 1)}
	if pythonMatrixGoConcurrency(mutated) {
		t.Fatal("missing Go concurrency cap in both matrix jobs escaped guard")
	}
	if jobs["fixed-corpus"] != "" && !strings.Contains(first, "needs: [plan, fixed-corpus]") {
		t.Fatal("Python matrix jobs do not wait for the fixed corpus producer")
	}
	var got []string
	shapeRows := map[string]int{}
	for _, row := range rows {
		if strings.HasPrefix(row.Target, "python-ci-required-shape-") {
			shapeRows[row.Target]++
		}
		if names, ok := strings.CutPrefix(row.Target, "python-ci-compiler-shard-"); ok {
			got = append(got, strings.Split(names, "+")...)
		}
		if row.Target == "python-ci-compiler" {
			t.Error("unbounded compiler aggregate remains in matrix")
		}
	}
	if len(shapeRows) != 0 {
		if len(shapeRows) != 16 {
			t.Fatalf("required public shape partition has %d rows, want 16", len(shapeRows))
		}
		for n := range 16 {
			if shapeRows["python-ci-required-shape-"+strconv.Itoa(n)] != 1 {
				t.Fatalf("missing or duplicate required public shape shard %d", n)
			}
		}
		wanted = slices.DeleteFunc(wanted, func(name string) bool { return name == "TestPythonRequiredPublicShapes" })
	}
	if len(wanted) == 0 || !compilerShardCoverage(wanted, got) {
		t.Fatalf("compiler shards do not match discovered tests: wanted %v, got %v", wanted, got)
	}
	if compilerShardCoverage(wanted, got[1:]) {
		t.Fatal("deleted compiler row escaped coverage control")
	}
	if compilerShardCoverage(wanted, append(slices.Clone(got), got[0])) {
		t.Fatal("duplicate compiler row escaped coverage control")
	}
}

func pythonMatrixGoConcurrency(jobs map[string]string) bool {
	capLine := "\n    env:\n      GOMAXPROCS: '2'\n"
	return strings.Count(jobs["test"], capLine) == 1 && strings.Count(jobs["test-second"], capLine) == 1
}

func pythonPlanCoverage(want, got []json.RawMessage) bool {
	if len(want) != len(got) {
		return false
	}
	a, b := make([]string, len(want)), make([]string, len(got))
	for i := range want {
		a[i] = string(want[i])
		b[i] = string(got[i])
	}
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func compilerShardCoverage(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	a, b := slices.Clone(want), slices.Clone(got)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
