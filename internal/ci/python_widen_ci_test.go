package ci

import (
	"encoding/json"
	"regexp"
	"testing"
)

// The slow historical widening parents must run in their own support rows,
// while unrelated tests remain in the automatically discovered package rows.
func TestPythonWidenCIPlan(t *testing.T) {
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
	wanted := []string{"TestPythonScalarWidenHistorical", "TestPythonContainerWidenHistorical", "TestPythonScalarWidenShardPlan", "TestPythonPackageNameCollisions", "TestPythonVtKeyedEnumWriteRefusalsFunctional"}
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
	for _, target := range []string{"python-ci-widen-container", "python-ci-widen-scalar-0", "python-ci-widen-scalar-1", "python-ci-widen-scalar-2", "python-ci-widen-scalar-3", "python-ci-widen-coverage"} {
		if targets[target] != 1 {
			t.Errorf("support target %s appears %d times, want one", target, targets[target])
		}
	}
	for _, name := range wanted {
		want := 1
		if name == "TestPythonScalarWidenHistorical" || name == "TestPythonContainerWidenHistorical" {
			want = 0
		}
		if auto[name] != want {
			t.Errorf("automatic test %s appears %d times, want %d", name, auto[name], want)
		}
	}
}
