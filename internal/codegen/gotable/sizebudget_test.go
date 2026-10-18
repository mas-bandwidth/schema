package gotable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The generated Go size budget is measured, not guessed (docs/SPEC-TABLES.md
// §8.4, GO's shared runtime): both counts are the wc -l of every emitted .go
// file with the shared runtime package counted once. Measured 2026-10-18 at
// BASE 5dbe8c20097ef5cb625a5d91a9d6ae286ec4dfd1. They are pins, no slack: a
// later cut lowers them. Glenn's contraction item targets 2500 and 40000 lines.
const (
	goOneTableLineBudget = 9271
	goExamplesLineBudget = 63716
)

// TestGoGeneratedSizeBudget pins the size of the Go table emitter's output at
// head (docs/SPEC-TABLES.md §8.4): a one-table fixed unit and every schema file
// under tables/examples as one unit, generated with runtime=shared named
// explicitly so the pin does not move when the default runtime does. The
// standalone one-table unit is the reversed witness: its embedded runtime makes
// it larger than the shared budget, so the pin is seen to bite.
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
	if standalone <= goOneTableLineBudget {
		t.Fatalf("the standalone one-table unit is %d lines, not over the %d-line budget: the budget no longer bites",
			standalone, goOneTableLineBudget)
	}
}

// goGeneratedLines counts wc -l over the generated .go files of one unit,
// written in a t.TempDir(), with the shared runtime package counted once in
// every total. A standalone unit carries the same machinery embedded as well,
// which is what makes the reversed witness larger.
func goGeneratedLines(t *testing.T, u *ir.Unit, rt Runtime) int {
	t.Helper()
	files, err := GenerateRuntime(u, nil, rt)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	shared, err := SharedRuntimeFiles()
	if err != nil {
		t.Fatalf("shared runtime: %v", err)
	}
	for name, data := range shared {
		files[name] = data
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
