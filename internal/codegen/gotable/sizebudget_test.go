package gotable

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The generated Go size budget is measured, not guessed (docs/SPEC-TABLES.md
// §8.4, GO's shared runtime): both counts are the wc -l of every emitted .go
// file with the shared runtime package counted once in the shared total.
// Measured 2026-10-18 at b1cfd0df380a5164fa573ec79f947635d6a23de4. They
// are pins, no slack: a later cut lowers them. Glenn's contraction item targets
// 2500 and 40000 lines. The pins rose when the fixed form's refusal vocabulary
// folded into TableRefuseReason (§6.5): each member adds a constant and an
// Error() case to every emitted Go file.
const (
	// +9 lines from this card's DEPRECATED SLOT default image (§3.4), measured at sprint/schema-go ac4dc34.
	// +4: the variable reader names the form byte on its refusal (schema-go-prose-reasons-become-names)
	// +22: a truncated fixed load answers malformed by class and truncated by name (schema-go-fixed-load-truncated-refusal-matches-strict-test, 703169488)
	// -13: the emitted loops range over their counts and the clamps call min/max (modernize-clean generated Go)
	// +2: the open's bounded-work cap and the unsigned-overflow ceiling clamp (schema-shared landing, SPEC-TABLES §3, schema#611)
	// +19: table floats are finite only (SPEC-TABLES §3.3, Glenn 2026-10-27): the NonFiniteFloat report slot, the refusal's two runtime helpers and Err() arm, the cook writer's and the retain capture's raw-bits tests (tables-finite/go)
	// -128: wide text is not part of schema (kind 33 retired), so its UTF-16 runtime is gone from every unit
	goOneTableLineBudget = 9381
	// +369: the strict measure's damage checks in the emitted runtime (schema-go-fuzz-measure-accepts-malformed)
	// +109: the variable reader names the form byte on its refusal (schema-go-prose-reasons-become-names)
	// +22: a truncated fixed load answers malformed by class and truncated by name (schema-go-fixed-load-truncated-refusal-matches-strict-test, 703169488)
	// -31: the emitted loops range over their counts and the clamps call min/max (modernize-clean generated Go)
	// +2: the open's bounded-work cap and the unsigned-overflow ceiling clamp (schema-shared landing, SPEC-TABLES §3, schema#611)
	// +821: table floats are finite only (SPEC-TABLES §3.3, Glenn 2026-10-27): every float read tests its raw bits, every tolerant load and message batch refuses a non-finite one, and every fixed type holding a float gains its finite pass and its writer clause (tables-finite/go)
	// -133: wide text is not part of schema (kind 33 retired), so its UTF-16 runtime is gone from every unit
	goExamplesLineBudget = 65925
)

// TestGoGeneratedSizeBudget pins the size of the Go table emitter's output at
// head (docs/SPEC-TABLES.md §8.4): a one-table fixed unit and every schema file
// under tables/examples as one unit, generated with runtime=shared named
// explicitly so the pin does not move when the default runtime does. The
// reversed witness is the budget check itself: each pin is shown to refuse one
// line more than was measured, so a pin that silently stopped biting fails.
//
// The standalone one-table unit is smaller than the shared total (8915 vs 9271
// lines at this BASE): for one table the embedded runtime is smaller than the
// shared package, which is only amortised by more tables. Standalone is
// reported, never pinned here.
func TestGoGeneratedSizeBudget(t *testing.T) {
	t.Parallel()
	oneTable := unitFrom(t, "package probe\nfixed table Pos {\n\tx int32\n\ty int32\n}\n")
	examples := loadUnitPaths(t, "../../..", []string{"tables/examples"})

	oneLines := goGeneratedLines(t, oneTable, RuntimeShared)
	exampleLines := goGeneratedLines(t, examples, RuntimeShared)
	standalone := goGeneratedLines(t, oneTable, RuntimeStandalone)
	t.Logf("one-table %d lines (budget %d), standalone %d lines, examples %d lines (budget %d)",
		oneLines, goOneTableLineBudget, standalone, exampleLines, goExamplesLineBudget)
	if oneLines > goOneTableLineBudget || exampleLines > goExamplesLineBudget {
		t.Fatalf("generated Go is over budget: one-table %d lines (budget %d), examples %d lines (budget %d)",
			oneLines, goOneTableLineBudget, exampleLines, goExamplesLineBudget)
	}
	for _, c := range []struct {
		name   string
		lines  int
		budget int
	}{
		{"one-table", oneLines, goOneTableLineBudget},
		{"examples", exampleLines, goExamplesLineBudget},
	} {
		if !goOverGeneratedSizeBudget(c.lines+1, c.budget) {
			t.Fatalf("the %s pin (%d lines) accepts %d lines: the budget no longer bites",
				c.name, c.budget, c.lines+1)
		}
	}
}

// goOverGeneratedSizeBudget is the budget check: a unit is over when its
// measured line count exceeds the pin.
func goOverGeneratedSizeBudget(lines, budget int) bool {
	return lines > budget
}

// goGeneratedLines counts wc -l over the generated .go files of one unit,
// written in a t.TempDir(). The shared runtime package is counted once, and
// only for runtime=shared: a standalone unit carries its own machinery
// embedded and does not use the shared package.
func goGeneratedLines(t *testing.T, u *ir.Unit, rt Runtime) int {
	t.Helper()
	files, err := GenerateRuntime(u, nil, rt)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rt == RuntimeShared {
		shared, err := SharedRuntimeFiles()
		if err != nil {
			t.Fatalf("shared runtime: %v", err)
		}
		maps.Copy(files, shared)
	}
	dir := t.TempDir()
	lines := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		lines += bytes.Count(data, []byte("\n"))
	}
	return lines
}
