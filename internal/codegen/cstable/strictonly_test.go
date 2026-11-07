package cstable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
)

// TestStrictOnlyCsNoTolerantEntry is the cs leg's strict-only grep gate
// (docs/plans/strict-only.md layer 4): the generator's output for the leg's
// example schema (tables/examples) and internal/tablenames/cs.go carry none
// of the retired tolerant names — no Tolerant entry point, no Clamped or
// KindMismatch or Duplicate counter, and no Malformed binary verdict on the
// read report. The one read returns the whole value or refuses the whole
// load; the surviving counters are the evolution diagnostics only.
func TestStrictOnlyCsNoTolerantEntry(t *testing.T) {
	t.Parallel()

	retired := []string{"Tolerant", "KindMismatch", "Clamped", "Duplicate", "Malformed"}

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := cstable.Generate(u)
	if err != nil {
		t.Fatalf("generate cs tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate cs tables/examples emitted nothing")
	}
	for name, data := range files {
		for _, word := range retired {
			if strings.Contains(string(data), word) {
				t.Errorf("generated %s retains retired tolerant name %q", name, word)
			}
		}
	}

	names, err := os.ReadFile(filepath.Join("..", "..", "tablenames", "cs.go"))
	if err != nil {
		t.Fatalf("read internal/tablenames/cs.go: %v", err)
	}
	for _, word := range retired {
		if strings.Contains(string(names), word) {
			t.Errorf("internal/tablenames/cs.go retains retired tolerant name %q", word)
		}
	}
}
