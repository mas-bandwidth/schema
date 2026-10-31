package squirreltable

import (
	"os"
	"strings"
	"testing"
)

// TestRuntimeFileIsABuildInput pins the cause the wire pin gate exists to
// catch: squirreltable.go embeds runtime/*.nut, but the Makefile's GO_SOURCES
// names only *.go, so a runtime edit alone must still name those files as a
// prerequisite of the tagged compiler AND of the corpus it generates. Without
// both the corpus keeps the previous runtime and the wire surface compares
// saves the source no longer produces (docs/PORT-SQUIRREL.md, THE HARNESS;
// docs/SPEC-TABLES.md §3).
func TestRuntimeFileIsABuildInput(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../make/squirrel-conformance.mk")
	if err != nil {
		t.Fatalf("read make/squirrel-conformance.mk: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	for _, target := range []string{"build/schema-squirrel:", "build/conformance-squirrel:"} {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, target) &&
				strings.Contains(line, "internal/codegen/squirreltable/runtime/*.nut") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s does not depend on internal/codegen/squirreltable/runtime/*.nut", target)
		}
	}
}
