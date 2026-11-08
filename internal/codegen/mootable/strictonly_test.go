package mootable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// TestStrictOnlyMooNoTolerantEntry is the strict-only grep gate for the moo
// leg (docs/plans/strict-only.md layer 4): the generator's output for the
// leg's example schema (tables/examples), the embedded runtime modules, and
// internal/tablenames/moo.go carry none of the retired tolerant names — no
// Tolerant entry point, no loadOpened helper, no clamped or kindMismatch or
// duplicate counter, and no malformed binary verdict field on the read report.
// The one read returns the whole value or refuses the whole load; unknown,
// widened, retained and retainLost stay as evolution diagnostics.
func TestStrictOnlyMooNoTolerantEntry(t *testing.T) {
	t.Parallel()

	retired := []string{
		"Tolerant",
		"tolerant",
		"loadOpened",
		"Clamped",
		"clamped",
		"clampUtf8",
		"clampWide",
		"KindMismatch",
		"kindMismatch",
		"Duplicate",
		"duplicate",
		"Malformed",
		".malformed",
		"malformed:",
		"malformed =",
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := mootable.Generate(u)
	if err != nil {
		t.Fatalf("generate moo tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate moo tables/examples emitted nothing")
	}
	for name, data := range files {
		for _, bad := range retired {
			if strings.Contains(string(data), bad) {
				t.Errorf("generated %s carries retired tolerant name %q", name, bad)
			}
		}
	}
	for name, data := range mootable.Runtime() {
		for _, bad := range retired {
			if strings.Contains(string(data), bad) {
				t.Errorf("runtime %s carries retired tolerant name %q", name, bad)
			}
		}
	}

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "moo.go"))
	if err != nil {
		t.Fatalf("read internal/tablenames/moo.go: %v", err)
	}
	for _, bad := range retired {
		if strings.Contains(string(names), bad) {
			t.Errorf("internal/tablenames/moo.go carries retired tolerant name %q", bad)
		}
	}
}
