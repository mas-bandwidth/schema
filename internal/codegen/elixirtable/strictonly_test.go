package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The refusal reason kind_mismatch is valid. These patterns name the retired
// tolerant entry points and report fields, not diagnostic string values.
var strictOnlyElixirRetired = []string{
	"Tolerant", "tolerant", "kind_mismatch:", ":kind_mismatch =>",
	"report.kind_mismatch", "Map.put(report, :kind_mismatch,",
	"clamped", "duplicate:", ":duplicate", "malformed:",
}

func strictOnlyElixirViolation(text string) string {
	for _, bad := range strictOnlyElixirRetired {
		if strings.Contains(text, bad) {
			return bad
		}
	}
	return ""
}

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
	for name, data := range out {
		if bad := strictOnlyElixirViolation(string(data)); bad != "" {
			t.Errorf("generated %s carries retired tolerant name %q", name, bad)
		}
	}
	names, err := os.ReadFile(filepath.Join(root, "internal", "tablenames", "elixir.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bad := strictOnlyElixirViolation(string(names)); bad != "" {
		t.Errorf("internal/tablenames/elixir.go carries retired tolerant name %q", bad)
	}
}

// The guard catches each retired form while accepting the strict refusal reason.
func TestStrictOnlyElixirNoTolerantEntryNegativeControl(t *testing.T) {
	t.Parallel()
	if bad := strictOnlyElixirViolation(`{:error, :kind_mismatch, %{reason: "kind_mismatch"}}`); bad != "" {
		t.Fatalf("strict refusal reason matched %q", bad)
	}
	for _, retired := range strictOnlyElixirRetired {
		if got := strictOnlyElixirViolation("generated source " + retired); got == "" {
			t.Errorf("retired form %q escaped the guard", retired)
		}
	}
}
