package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCsConformanceRunsUnderCheckedArithmetic(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "schema-cs-conformance-checked-overflow.mk")
	mkBytes, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read checked-overflow make fragment: %v", err)
	}
	mk := string(mkBytes)
	for _, want := range []string{
		"tables-cs-checked-overflow-conformance:",
		"-p:CheckForOverflowUnderflow=true",
		"build/tables-generated-cs/.stamp",
		"build/conformance-harness",
		"build-cs-cook",
		"$(DOTNET) build test/cs-cook/schemacook.csproj",
		"-p:CheckForOverflowUnderflow=true",
		"./build/conformance-harness run --only cs",
		"schemaconformance.dll",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not contain %q", mkPath, want)
		}
	}

	// The red half uses the same checked build against a scratch copy of the
	// generated corpus and requires the planted addition to raise overflow.
	if got := strings.Count(mk, "-p:CheckForOverflowUnderflow=true"); got < 4 {
		t.Errorf("checked conformance and both overflow plants need four checked builds; got %d", got)
	}
	for _, want := range []string{
		"tables-cs-checked-overflow-negative-control:",
		"CheckedOverflowPlant.cs",
		"OverflowException",
		"TablesGeneratedDir=",
		"CheckedOverflowPlant=$(abspath $(CS_CHECKED_DIR)/negative/cs-cook/CheckedOverflowPlant.cs)",
		"schemacooktest.dll",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not pin negative-control token %q", mkPath, want)
		}
	}

	workflowPath := filepath.Join(root, ".github", "workflows", "schema-cs-conformance-checked-overflow.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read checked-overflow workflow: %v", err)
	}
	// The cs-cook project only includes the negative-control source when its
	// explicit property is set; ordinary builds remain unchanged.
	cookProjectPath := filepath.Join(root, "test", "cs-cook", "schemacook.csproj")
	cookProject, err := os.ReadFile(cookProjectPath)
	if err != nil {
		t.Fatalf("read cs-cook project: %v", err)
	}
	if !strings.Contains(string(cookProject), `Compile Include="$(CheckedOverflowPlant)" Condition="'$(CheckedOverflowPlant)' != ''"`) {
		t.Errorf("%s does not conditionally include the planted overflow source", cookProjectPath)
	}
	text := string(workflow)
	for _, target := range []string{"tables-cs-checked-overflow-conformance", "tables-cs-checked-overflow-negative-control"} {
		if !strings.Contains(text, "make "+target) {
			t.Errorf("checked-overflow workflow does not invoke %s", target)
		}
	}
	if strings.Contains(text, "pull_request:") || strings.Contains(text, "push:") {
		t.Error("checked-overflow dotnet gate is heavy and must remain nightly/manual")
	}
}
