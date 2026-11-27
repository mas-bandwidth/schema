package ci

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
)

type pythonPlanRow struct {
	Target  string `json:"target"`
	Package string `json:"package"`
	Test    string `json:"test"`
}

const pythonTablePackage = "github.com/mas-bandwidth/schema/v3/internal/codegen/pythontable"

// Strict mypy over the reflection units must run once per shard in its own
// support rows, while the other reflection tests stay in the automatically
// discovered package rows.
func TestPythonReflectionCIPlan(t *testing.T) {
	var rows []pythonPlanRow
	if err := json.Unmarshal(pythonCIPlan(t), &rows); err != nil {
		t.Fatalf("parse Python CI plan: %v", err)
	}
	if issue := pythonReflectionPlanIssue(rows); issue != "" {
		t.Fatal(issue)
	}
	// Controls: a dropped shard row and a strict parent left in a package
	// row must each be named.
	var dropped []pythonPlanRow
	for _, row := range rows {
		if row.Target != "python-ci-reflection-strict-7" {
			dropped = append(dropped, row)
		}
	}
	if issue := pythonReflectionPlanIssue(dropped); issue != "support target python-ci-reflection-strict-7 appears 0 times, want 1" {
		t.Fatalf("dropped strict shard escaped: %q", issue)
	}
	leaked := append(append([]pythonPlanRow{}, rows...), pythonPlanRow{Package: pythonTablePackage, Test: "^(TestPythonReflectionStrictTypes)$"})
	if issue := pythonReflectionPlanIssue(leaked); issue != "automatic test TestPythonReflectionStrictTypes appears 1 times, want 0" {
		t.Fatalf("strict parent in a package row escaped: %q", issue)
	}
}

func pythonReflectionPlanIssue(rows []pythonPlanRow) string {
	targets := map[string]int{}
	auto := map[string]int{}
	wanted := []string{"TestPythonReflectionStrictTypes", "TestPythonReflectionStrictShardPlan", "TestPythonReflectionCorpus", "TestPythonReflectionCorpusNegativeControl"}
	for _, row := range rows {
		targets[row.Target]++
		if row.Package != pythonTablePackage || row.Target != "" {
			continue
		}
		r, err := regexp.Compile(row.Test)
		if err != nil {
			return fmt.Sprintf("invalid package selector %q: %v", row.Test, err)
		}
		for _, name := range wanted {
			if r.MatchString(name) {
				auto[name]++
			}
		}
	}
	for shard := range 24 {
		target := fmt.Sprintf("python-ci-reflection-strict-%d", shard)
		if targets[target] != 1 {
			return fmt.Sprintf("support target %s appears %d times, want 1", target, targets[target])
		}
	}
	if targets["python-ci-reflection"] != 1 {
		return fmt.Sprintf("support target python-ci-reflection appears %d times, want 1", targets["python-ci-reflection"])
	}
	for _, name := range wanted {
		want := 1
		if name == "TestPythonReflectionStrictTypes" {
			want = 0
		}
		if auto[name] != want {
			return fmt.Sprintf("automatic test %s appears %d times, want %d", name, auto[name], want)
		}
	}
	return ""
}
