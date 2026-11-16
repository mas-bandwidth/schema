package ci

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
	var rows []struct{ Target string }
	if err := json.Unmarshal(run("sh", "tools/python-ci-rows.sh"), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) > 256 {
		t.Fatalf("Python CI matrix has %d rows; GitHub permits at most 256", len(rows))
	}
	var got []string
	shapeRows := map[string]int{}
	for _, row := range rows {
		if strings.HasPrefix(row.Target, "python-ci-required-shape-") {
			shapeRows[row.Target]++
		}
		if strings.HasPrefix(row.Target, "python-ci-compiler-shard-") {
			got = append(got, strings.Split(strings.TrimPrefix(row.Target, "python-ci-compiler-shard-"), "+")...)
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

func compilerShardCoverage(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	a, b := slices.Clone(want), slices.Clone(got)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
