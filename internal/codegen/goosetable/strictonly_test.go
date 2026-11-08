package goosetable_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// TestStrictOnlyGooseNoTolerantEntry is the Goose leg's strict-only grep gate
// (docs/plans/strict-only.md layer 4): the generator's output for the leg's
// example schema (tables/examples) and the Goose table runtime carry none of
// the retired tolerant names — no Tolerant entry point, no clamped (renamed
// out_of_range), no kind_mismatch counter, no duplicate counter, and no
// malformed binary verdict field on any read report. kind_mismatch and
// malformed survive only as refusal reasons.
func TestStrictOnlyGooseNoTolerantEntry(t *testing.T) {
	t.Parallel()

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("generate goose tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate goose tables/examples emitted nothing")
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	retired := []string{
		"Tolerant",
		"tolerant",
		"clamped",
		"Clamped",
		"duplicate",
		"Duplicate",
		"kind_mismatch:",
		".kind_mismatch",
		"kind_mismatch +=",
		"malformed:",
		".malformed",
		"malformed =",
	}

	for _, name := range names {
		text := string(files[name])
		for _, bad := range retired {
			if strings.Contains(text, bad) {
				t.Errorf("generated %s retains retired tolerant name %q", name, bad)
			}
		}
	}
}
