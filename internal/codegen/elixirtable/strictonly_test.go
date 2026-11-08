package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictOnlyElixirNoTolerantEntry is the strict-only grep gate for the
// Elixir leg (docs/plans/strict-only.md layer 4): the generator's output for
// the leg's example schema and internal/tablenames/elixir.go carry none of
// the retired tolerant names — no Tolerant entry point, no clamped or
// kind_mismatch counter, no duplicate counter, and no malformed binary
// verdict field. The one read returns the whole value or refuses the whole
// load; unknown, widened, retained and retain_lost stay as diagnostics.
func TestStrictOnlyElixirNoTolerantEntry(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(root, "tables", "examples", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples schema: %v %v", paths, err)
	}
	u := loadUnit(t, paths...)
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	retired := []string{
		"Tolerant",
		"tolerant",
		"kind_mismatch",
		"clamped",
		"duplicate:",
		":duplicate",
		"malformed:",
	}
	for name, data := range out {
		for _, bad := range retired {
			if strings.Contains(string(data), bad) {
				t.Errorf("generated %s carries retired tolerant name %q", name, bad)
			}
		}
	}
	names, err := os.ReadFile(filepath.Join(root, "internal", "tablenames", "elixir.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range retired {
		if strings.Contains(string(names), bad) {
			t.Errorf("internal/tablenames/elixir.go carries retired tolerant name %q", bad)
		}
	}
}
