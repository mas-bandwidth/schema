package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestElixirBenchStartsItsLongestGatesFirst: make -j starts prerequisites in
// the order they are listed, and the per-leg rules elsewhere append theirs
// after _verify-elixir-bench's own, so the row's two longest gates (the
// tablegen compile and the timed quick run) are named first, or they start
// last and end the row past the cap on a loaded runner (#4530). That each
// Elixir table unit is listed once is elixir_table_units_once_test.go (#4597).
func TestElixirBenchStartsItsLongestGatesFirst(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "verify-compiled.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n_verify-elixir-bench: verify-tablegen-elixir _verify-elixir-bench-quick ") {
		t.Error("make/verify-compiled.mk _verify-elixir-bench does not name verify-tablegen-elixir and _verify-elixir-bench-quick first")
	}
}
