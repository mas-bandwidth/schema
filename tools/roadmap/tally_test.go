package roadmap

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// ROADMAP.md is generated end to end by tools/roadmap/render.lisp from
// docs/roadmap.sexp and docs/roadmap-evidence/*.sexp. Every table sits between
// a pair of generated-block markers; these tests hold the page to that shape
// where sbcl is absent, and to the renderer's exact output where it is present.

var (
	startMarkerRx   = regexp.MustCompile(`^<!-- nova-work:([a-z0-9-]+):start -->$`)
	endMarkerRx     = regexp.MustCompile(`^<!-- nova-work:([a-z0-9-]+):end -->$`)
	generatedCellRx = regexp.MustCompile(`^(?:✅(?: 100%)?|❌|\d+%|\?(?: \((?:≥ )?\d+%\))?|(?:≥ )?\d+/\d+(?: \((?:≥ )?\d+%\))?)$`)
)

func isValidCell(cell string, inGenerated bool) bool {
	cell = strings.TrimSpace(cell)
	if !inGenerated {
		return cell == "✅" || cell == "❌"
	}
	return generatedCellRx.MatchString(cell)
}

// checkRoadmapText holds every table to its own header's language columns and
// every cell to ✅ or ❌ (or, inside a generated block, the renderer's progress
// grammar). Markers must pair up by id and never nest.
func checkRoadmapText(raw string) error {
	currentColumns, rows := 0, 0
	inGenerated := ""
	for n, line := range strings.Split(raw, "\n") {
		if m := startMarkerRx.FindStringSubmatch(line); m != nil {
			if inGenerated != "" {
				return fmt.Errorf("ROADMAP.md:%d: block %q opens inside block %q", n+1, m[1], inGenerated)
			}
			inGenerated = m[1]
			continue
		}
		if m := endMarkerRx.FindStringSubmatch(line); m != nil {
			if inGenerated != m[1] {
				return fmt.Errorf("ROADMAP.md:%d: block %q closes but %q is open", n+1, m[1], inGenerated)
			}
			inGenerated = ""
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if strings.TrimSpace(cells[0]) == "feature" {
			currentColumns = len(cells) - 1
			if currentColumns <= 0 {
				return fmt.Errorf("table header carries %d language columns; expected > 0", currentColumns)
			}
			continue
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		if len(cells)-1 != currentColumns {
			return fmt.Errorf("row %q carries %d cells and the table header carries %d", strings.TrimSpace(cells[0]), len(cells)-1, currentColumns)
		}
		rows++
		for _, cell := range cells[1:] {
			trimmed := strings.TrimSpace(cell)
			if !isValidCell(trimmed, inGenerated != "") {
				if inGenerated != "" {
					return fmt.Errorf("row %q carries invalid generated progress cell %q", strings.TrimSpace(cells[0]), trimmed)
				}
				return fmt.Errorf("row %q carries a cell %q that is neither ✅ nor ❌", strings.TrimSpace(cells[0]), trimmed)
			}
		}
	}
	if inGenerated != "" {
		return fmt.Errorf("block %q is never closed", inGenerated)
	}
	if rows == 0 || currentColumns == 0 {
		return fmt.Errorf("ROADMAP.md carries no feature table")
	}
	return nil
}

// tablesOutsideBlocks names the first table line that sits outside a generated
// block: a table written by hand, which the renderer would overwrite.
func tablesOutsideBlocks(raw string) error {
	inGenerated := false
	for n, line := range strings.Split(raw, "\n") {
		switch {
		case startMarkerRx.MatchString(line):
			inGenerated = true
		case endMarkerRx.MatchString(line):
			inGenerated = false
		case strings.HasPrefix(line, "|") && !inGenerated:
			return fmt.Errorf("ROADMAP.md:%d: a table outside every nova-work block: %q", n+1, line)
		}
	}
	return nil
}

// generatedBlockIDs lists the block ids in page order.
func generatedBlockIDs(raw string) []string {
	var ids []string
	for line := range strings.SplitSeq(raw, "\n") {
		if m := startMarkerRx.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func readRoadmap(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestTheTablesAreWellFormed holds every row of each table to its own header's
// language columns and every cell to the renderer's grammar.
func TestTheTablesAreWellFormed(t *testing.T) {
	if err := checkRoadmapText(readRoadmap(t)); err != nil {
		t.Fatal(err)
	}
}

// TestEveryTableIsGenerated refuses a table outside a nova-work block: the page
// is rendered from data end to end, so a hand-written table is a drift the
// next render would erase.
func TestEveryTableIsGenerated(t *testing.T) {
	if err := tablesOutsideBlocks(readRoadmap(t)); err != nil {
		t.Fatal(err)
	}
}

// TestThePageCarriesTheThreeTables holds the page to the three roadmaps, in
// order: packet wire, fixed tables, variable tables.
func TestThePageCarriesTheThreeTables(t *testing.T) {
	got := generatedBlockIDs(readRoadmap(t))
	want := []string{"packet-wire", "fixed-tables", "variable-tables"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ROADMAP.md carries blocks %v; want %v", got, want)
	}
}

// TestEveryTableHasEveryLanguage holds every table header to the nine supported
// packet-wire languages (#1861), then the ports on main below parity (goose,
// docs/PORT-GOOSE.md), then the queued ports (luau, lua, squirrel, moo), then
// lobster, in the same order.
func TestEveryTableHasEveryLanguage(t *testing.T) {
	want := "| feature | cpp | c | cs | go | rust | java | js | dart | elixir | goose | luau | lua | squirrel | moo | lobster |"
	headers := 0
	for n, line := range strings.Split(readRoadmap(t), "\n") {
		if strings.HasPrefix(line, "| feature") {
			headers++
			if line != want {
				t.Fatalf("ROADMAP.md:%d: header %q; want %q", n+1, line, want)
			}
		}
	}
	if headers != 3 {
		t.Fatalf("ROADMAP.md carries %d table headers; want 3", headers)
	}
}

// TestNoLanguageIsCountedInProse refuses a sentence such as "C++ at 30 of 31"
// or "340 of 1180 cells" outside the tables: the tables carry the count, and a
// count written by hand is stale the day a cell moves.
func TestNoLanguageIsCountedInProse(t *testing.T) {
	counted := regexp.MustCompile(`\b\d+ of \d+\b`)
	for n, line := range strings.Split(readRoadmap(t), "\n") {
		if strings.HasPrefix(line, "|") {
			continue
		}
		if counted.MatchString(line) {
			t.Fatalf("ROADMAP.md:%d counts by hand: %q", n+1, line)
		}
	}
}

// TestGeneratedCellGrammar verifies that generated cell formats (100%, 50%, ? (≥ 50%), ≥ 1/3)
// are accepted inside generated blocks and rejected outside.
func TestGeneratedCellGrammar(t *testing.T) {
	validGenerated := []string{
		"✅ 100%", "100%", "50%", "0%",
		"?", "? (≥ 0%)", "? (≥ 50%)", "? (≥ 14%)",
		"1/1", "3/3 (100%)", "≥ 1/3", "≥ 1/3 (≥ 33%)", "≥ 0/23 (≥ 0%)",
		"✅", "❌",
	}
	for _, c := range validGenerated {
		if !isValidCell(c, true) {
			t.Errorf("expected %q to be valid inside generated block", c)
		}
	}

	invalidGenerated := []string{"", "foo", "unknown", "progress", "?unknown?"}
	for _, c := range invalidGenerated {
		if isValidCell(c, true) {
			t.Errorf("expected %q to be invalid inside generated block", c)
		}
	}

	// Outside generated block, only ✅ and ❌ are accepted
	for _, c := range []string{"✅", "❌"} {
		if !isValidCell(c, false) {
			t.Errorf("expected %q to be valid outside generated block", c)
		}
	}
	for _, c := range []string{"100%", "50%", "? (≥ 50%)", "≥ 1/3", "foo"} {
		if isValidCell(c, false) {
			t.Errorf("expected %q to be invalid outside generated block", c)
		}
	}
}

// TestTablesWithDifferentAxesPassIndependently verifies that tables with
// different numbers of language columns are each held to their own headers,
// that blocks pair up by id, and that a table outside a block is named.
func TestTablesWithDifferentAxesPassIndependently(t *testing.T) {
	sample := `
## Packet wire

<!-- nova-work:packet-wire:start -->
| feature | c1 | c2 |
|---|---|---|
| f1 | ✅ | ❌ |
| complete | 100% | 0% |
<!-- nova-work:packet-wire:end -->

## Fixed Tables

<!-- nova-work:fixed-tables:start -->
| feature | l1 | l2 | l3 |
|---|---|---|---|
| ft1 | 100% | 50% | ? (≥ 33%) |
| complete | 1/1 | 0/1 | ≥ 0/1 |
<!-- nova-work:fixed-tables:end -->
`
	if err := checkRoadmapText(sample); err != nil {
		t.Fatalf("expected sample with different table axes to pass, got: %v", err)
	}
	if err := tablesOutsideBlocks(sample); err != nil {
		t.Fatalf("expected every table of the sample inside a block, got: %v", err)
	}
	if ids := generatedBlockIDs(sample); strings.Join(ids, ",") != "packet-wire,fixed-tables" {
		t.Fatalf("block ids %v", ids)
	}

	badSample := `
<!-- nova-work:x:start -->
| feature | c1 | c2 |
|---|---|---|
| f1 | ✅ | ❌ | ❌ |
<!-- nova-work:x:end -->
`
	if err := checkRoadmapText(badSample); err == nil {
		t.Fatal("expected row with extra cell to be rejected, but passed")
	}

	handTable := "\n| feature | c1 |\n|---|---|\n| f1 | ✅ |\n"
	if err := tablesOutsideBlocks(handTable); err == nil {
		t.Fatal("expected a table outside every block to be rejected, but passed")
	}

	crossed := "<!-- nova-work:a:start -->\n<!-- nova-work:b:end -->\n| feature | c1 |\n|---|---|\n| f1 | ✅ |\n"
	if err := checkRoadmapText(crossed); err == nil {
		t.Fatal("expected mismatched block markers to be rejected, but passed")
	}
}

func sbclOrSkip(t *testing.T) string {
	t.Helper()
	sbcl, err := exec.LookPath("sbcl")
	if err != nil {
		t.Skip("sbcl not installed on host; skipping the Lisp renderer (make roadmap-check runs it where sbcl is)")
	}
	return sbcl
}

// TestLispRendererSuite invokes the Common Lisp roadmap validation suite if sbcl is installed.
func TestLispRendererSuite(t *testing.T) {
	sbcl := sbclOrSkip(t)
	cmd := exec.Command(sbcl, "--script", "tools/roadmap/test.lisp")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sbcl test.lisp failed: %v\noutput:\n%s", err, string(out))
	}
}

// TestRoadmapIsGeneratedFromTheData holds the committed ROADMAP.md to what
// render.lisp makes of docs/roadmap.sexp and docs/roadmap-evidence/, where
// sbcl is installed (the same check as `make roadmap-check`).
func TestRoadmapIsGeneratedFromTheData(t *testing.T) {
	sbcl := sbclOrSkip(t)
	cmd := exec.Command(sbcl, "--script", "tools/roadmap/render.lisp", "--check", "docs/roadmap.sexp", "ROADMAP.md")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ROADMAP.md is not what the data renders; run: make roadmap\n%s", string(out))
	}
}
