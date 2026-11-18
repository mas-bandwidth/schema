package ci

import (
	"encoding/json"
	"regexp"
	"testing"
)

// The map benchmark pins must run in their own support rows,
// while unrelated tests remain in the automatically discovered package rows.
func TestPythonMapBenchCIPlan(t *testing.T) {
	out := pythonCIPlan(t)
	var rows []struct {
		Target  string `json:"target"`
		Package string `json:"package"`
		Test    string `json:"test"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("parse Python CI plan: %v\n%s", err, out)
	}
	targets := map[string]int{}
	auto := map[string]int{}
	const tablePackage = "github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	wanted := []string{"TestPythonMapShapeBench", "TestPythonMapBenchPinPlan", "TestPythonVtEvolution", "TestPythonTableCoreFixedUnion", "TestPythonVtKeyedEnumWriteRefusalsFunctional"}
	for _, row := range rows {
		targets[row.Target]++
		if row.Package != tablePackage || row.Target != "" {
			continue
		}
		r, err := regexp.Compile(row.Test)
		if err != nil {
			t.Fatalf("invalid package selector %q: %v", row.Test, err)
		}
		for _, name := range wanted {
			if r.MatchString(name) {
				auto[name]++
			}
		}
	}
	for _, target := range []string{"python-ci-map-bench-current", "python-ci-map-bench-floor", "python-ci-map-bench-coverage"} {
		if targets[target] != 1 {
			t.Errorf("support target %s appears %d times, want one", target, targets[target])
		}
	}
	for _, name := range wanted {
		want := 1
		if name == "TestPythonMapShapeBench" || name == "TestPythonVtEvolution" {
			want = 0
		}
		if auto[name] != want {
			t.Errorf("automatic test %s appears %d times, want %d", name, auto[name], want)
		}
	}
}
