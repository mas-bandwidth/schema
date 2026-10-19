package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCsConformanceRunsUnderCheckedArithmetic pins the C# checked-arithmetic
// lens (schema-cs-conformance-checked-overflow). The lens is
// CheckForOverflowUnderflow (/checked+) over the generated C# and the serialize.cs
// runtime: an integer overflow that would silently wrap becomes an
// OverflowException, so overflow bugs in the generated readers/writers and the
// runtime surface over the conformance corpus. The gate is the make target
// tables-cs-checked-overflow-conformance; this test holds the property and the
// target that make it a lens rather than a wish, so removing either goes red
// here before any dotnet build needs to run.
func TestCsConformanceRunsUnderCheckedArithmetic(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	projects := []struct {
		path string
	}{
		{"test/conformance/cs/schemaconformance.csproj"},
		{"test/cs/schematest.csproj"},
	}
	checked := 0
	for _, p := range projects {
		data, err := os.ReadFile(filepath.Join(root, p.path))
		if err != nil {
			t.Fatalf("%s: %v", p.path, err)
		}
		if !strings.Contains(string(data), "<CheckForOverflowUnderflow>true</CheckForOverflowUnderflow>") {
			t.Errorf("%s does not set CheckForOverflowUnderflow — the checked-arithmetic lens is off", p.path)
			continue
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no C# project carries CheckForOverflowUnderflow")
	}

	// The gate target must live in the C# make fragment so the Makefile
	// includes it. A target that no makefile defines cannot gate anything.
	csMk, err := os.ReadFile(filepath.Join(root, "make", "cs.mk"))
	if err != nil {
		t.Fatalf("make/cs.mk: %v", err)
	}
	if !strings.Contains(string(csMk), "tables-cs-checked-overflow-conformance") {
		t.Error("make/cs.mk does not define tables-cs-checked-overflow-conformance — the checked-arithmetic gate is not wired into make")
	}
}
