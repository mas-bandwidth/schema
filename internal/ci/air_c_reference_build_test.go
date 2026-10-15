package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAirCFixedTableReferenceBuildRecord holds the darwin/arm64 C
// fixed-table reference build record to its claims (issue #1115). The
// record is a build result only from the air bench (Apple M2 MacBook Air,
// darwin/arm64) inside a sandboxed card: generator, generated C sources,
// and one C fixed-table reference link. Without this gate the markdown
// file can be dropped or hollowed out and no `go test` goes red — the
// fleet's only non-linux/x64 data point for the C leg's `-Werror`
// surface under Apple clang silently vanishes.
func TestAirCFixedTableReferenceBuildRecord(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "measurements", "2026-09-18-air-c-reference-build.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("air C reference build record missing at %s (issue #1115): %v", path, err)
	}
	doc := string(data)

	// One row per build step: the red line names the step, the green line
	// is the record carrying it.
	for _, step := range []string{
		"`make bin/schema`",
		"`make build/tables-generated-c/.stamp`",
		"`make build/schema_test_c_variable`",
	} {
		if !strings.Contains(doc, step) {
			t.Errorf("record does not carry the %s build step (issue #1115)", step)
		}
	}

	// The bench identity: Apple clang on darwin/arm64, Mach-O arm64 artefact.
	for _, marker := range []string{
		"Apple clang version 21.0.0",
		"darwin/arm64",
		"Mach-O 64-bit executable arm64",
	} {
		if !strings.Contains(doc, marker) {
			t.Errorf("record does not carry the bench marker %q (issue #1115)", marker)
		}
	}

	// The honest build record reproduces both generator warnings.
	for _, warning := range []string{
		"RenderFrame",
		"PartFrame",
	} {
		if !strings.Contains(doc, warning) {
			t.Errorf("record does not reproduce the %s generator warning (issue #1115)", warning)
		}
	}

	// It is a build result only: no benchmark, no executed test binary.
	if !strings.Contains(doc, "build result only") {
		t.Errorf("record does not state it is a build result only (issue #1115)")
	}

	// The tip moved under the prior diff, so the record's references must
	// resolve here: the link target and the feature-tested flag still in
	// the C leg's make include, the §3.4 bounds still in SPEC-TABLES.
	cmk, err := os.ReadFile(filepath.Join(root, "make", "c.mk"))
	if err != nil {
		t.Fatalf("read make/c.mk: %v", err)
	}
	if !strings.Contains(string(cmk), "build/schema_test_c_variable") {
		t.Errorf("make/c.mk no longer defines build/schema_test_c_variable — the record's third step names a target this tip does not build (issue #1115)")
	}
	if !strings.Contains(string(cmk), "-Wtautological-type-limit-compare") {
		t.Errorf("make/c.mk no longer feature-tests -Wtautological-type-limit-compare — the record's warning-surface claim does not hold on this tip (issue #1115)")
	}
	spec, err := os.ReadFile(filepath.Join(root, "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("read docs/SPEC-TABLES.md: %v", err)
	}
	if !strings.Contains(string(spec), "65536") || !strings.Contains(string(spec), "4096") {
		t.Errorf("docs/SPEC-TABLES.md no longer carries the 4096/65536 §3.4 bounds — the record's warning citations do not resolve on this tip (issue #1115)")
	}
}
