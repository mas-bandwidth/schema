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
// Measured 2026-10-18 at b1cfd0df380a5164fa573ec79f947635d6a23de4. Each
// ceiling is the measured count rounded up to the next multiple of 250 lines
// (a count already on a step is its own ceiling): the test fails when the
// output grows past the ceiling or shrinks 250 lines below it, so a card edits
// a constant only when its measurement crosses a step and adds no comment line
// for it. The history stays in git. Glenn's contraction item targets 2500 and
// 40000 lines. The ceilings rose when the fixed form's refusal vocabulary
// folded into TableRefuseReason (§6.5): each member adds a constant and an
// Error() case to every emitted Go file.
const (
	// The ceiling is the measured count rounded up to the next multiple of 250
	// lines: a card edits a constant only when its measurement crosses a step,
	// and adds no comment line for it.
	goOneTableLineBudget = 10000
	goExamplesLineBudget = 60000
)

// TestGoGeneratedSizeBudget pins the size of the Go table emitter's output at
// head (docs/SPEC-TABLES.md §8.4): a one-table fixed unit and every schema file
// under tables/examples as one unit, generated with runtime=shared named
// explicitly so the pin does not move when the default runtime does. The
// reversed witness is the budget check itself: each ceiling is shown to refuse
// one line more than was measured and its floor to refuse 250 lines below, so a
// ceiling that silently stopped biting fails.
//
// The standalone one-table unit is smaller than the shared total: for one table
// the embedded runtime is smaller than the shared package, which is only
// amortised by more tables. Standalone is reported, never pinned here.
func TestGoGeneratedSizeBudget(t *testing.T) {
	t.Parallel()
	oneTable := unitFrom(t, "package probe\nfixed table Pos {\n\tx int32\n\ty int32\n}\n")
	examples := loadUnitPaths(t, "../../..", []string{"tables/examples"})

	oneLines := goGeneratedLines(t, oneTable, RuntimeShared)
	exampleLines := goGeneratedLines(t, examples, RuntimeShared)
	standalone := goGeneratedLines(t, oneTable, RuntimeStandalone)
	t.Logf("one-table %d lines (ceiling %d), standalone %d lines, examples %d lines (ceiling %d)",
		oneLines, goOneTableLineBudget, standalone, exampleLines, goExamplesLineBudget)
	for _, c := range []struct {
		name    string
		lines   int
		ceiling int
	}{
		{"one-table", oneLines, goOneTableLineBudget},
		{"examples", exampleLines, goExamplesLineBudget},
	} {
		if verdict := goGeneratedSizeVerdict(c.lines, c.ceiling); verdict != "" {
			t.Fatalf("generated Go is %s the %s ceiling: measured %d lines, ceiling %d, set the ceiling to %d",
				verdict, c.name, c.lines, c.ceiling, roundUp250(c.lines))
		}
		if goGeneratedSizeVerdict(c.ceiling+1, c.ceiling) != "over" {
			t.Fatalf("the %s ceiling (%d lines) accepts %d lines: the ceiling no longer bites",
				c.name, c.ceiling, c.ceiling+1)
		}
		if goGeneratedSizeVerdict(c.ceiling-250, c.ceiling) != "lower" {
			t.Fatalf("the %s floor (%d lines) accepts %d lines: the floor no longer bites",
				c.name, c.ceiling, c.ceiling-250)
		}
	}
}

// goGeneratedSizeVerdict reports "" when a measured count is on the step for
// its ceiling, "over" when it has grown past the ceiling, and "lower" when it
// has shrunk 250 or more lines below it.
func goGeneratedSizeVerdict(lines, ceiling int) string {
	switch {
	case lines > ceiling:
		return "over"
	case ceiling-lines >= 250:
		return "lower"
	default:
		return ""
	}
}

// roundUp250 is the ceiling a measured count sets: the next multiple of 250,
// or the count itself when it is already on a step.
func roundUp250(lines int) int {
	return (lines + 249) / 250 * 250
}

// TestGoGeneratedSizeCeilingSteps pins the stepped ceiling rule: exactly at the
// ceiling, one line over, one line under, 249 under, 250 under, and a count
// that is an exact multiple of 250.
func TestGoGeneratedSizeCeilingSteps(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		lines   int
		ceiling int
		want    string
	}{
		{"exactly at the ceiling", 1000, 1000, ""},
		{"one over", 1001, 1000, "over"},
		{"one under", 999, 1000, ""},
		{"249 under", 751, 1000, ""},
		{"250 under", 750, 1000, "lower"},
		{"exact multiple of 250", 750, 750, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := goGeneratedSizeVerdict(c.lines, c.ceiling); got != c.want {
				t.Fatalf("goGeneratedSizeVerdict(%d, %d) = %q, want %q", c.lines, c.ceiling, got, c.want)
			}
		})
	}
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
