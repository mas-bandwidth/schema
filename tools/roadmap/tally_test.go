package roadmap

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var generatedCellRx = regexp.MustCompile(`^(?:✅|❌|\d+%|\?(?: \((?:≥ )?\d+(?:%|/\d+)?\))?|≥ \d+(?:%|/\d+)|\d+/\d+)$`)

func isValidCell(cell string, inGenerated bool) bool {
	cell = strings.TrimSpace(cell)
	if !inGenerated {
		return cell == "✅" || cell == "❌"
	}
	return generatedCellRx.MatchString(cell)
}

// TestTheTablesAreWellFormed holds every row of the tables to the header's
// language columns and every cell outside generated progress blocks to ✅ or ❌.
// Inside the fixed-tables generated block, explicit progress and unknown formats
// are accepted.
func TestTheTablesAreWellFormed(t *testing.T) {
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	columns, rows := 0, 0
	inGenerated := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.Contains(line, "<!-- nova-work:fixed-tables:start -->") {
			inGenerated = true
		}
		if strings.Contains(line, "<!-- nova-work:fixed-tables:end -->") {
			inGenerated = false
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if strings.TrimSpace(cells[0]) == "feature" {
			n := len(cells) - 1
			if columns != 0 && n != columns {
				t.Fatalf("the tables carry %d and %d language columns; they must agree", columns, n)
			}
			columns = n
			continue
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		if len(cells)-1 != columns {
			t.Fatalf("row %q carries %d cells and the header carries %d", strings.TrimSpace(cells[0]), len(cells)-1, columns)
		}
		rows++
		for _, cell := range cells[1:] {
			trimmed := strings.TrimSpace(cell)
			if !isValidCell(trimmed, inGenerated) {
				if inGenerated {
					t.Fatalf("row %q carries invalid generated progress cell %q", strings.TrimSpace(cells[0]), trimmed)
				}
				t.Fatalf("row %q carries a cell %q that is neither ✅ nor ❌", strings.TrimSpace(cells[0]), trimmed)
			}
		}
	}
	if rows == 0 || columns == 0 {
		t.Fatal("ROADMAP.md carries no feature table")
	}
}

// TestNoLanguageIsCountedInProse refuses a sentence such as "C++ at 30 of 31"
// or "340 of 1180 cells" outside the tables: the tables carry the count, and a
// count written by hand is stale the day a cell moves.
func TestNoLanguageIsCountedInProse(t *testing.T) {
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	counted := regexp.MustCompile(`\b\d+ of \d+\b`)
	n := 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		n++
		if strings.HasPrefix(line, "|") {
			continue
		}
		if counted.MatchString(line) {
			t.Fatalf("ROADMAP.md:%d counts by hand: %q", n, line)
		}
	}
}

// TestGeneratedCellGrammar verifies that generated cell formats (100%, 50%, ? (≥ 50%), ≥ 1/3)
// are accepted inside generated blocks and rejected outside.
func TestGeneratedCellGrammar(t *testing.T) {
	validGenerated := []string{"100%", "50%", "0%", "? (≥ 50%)", "? (≥ 0%)", "≥ 1/3", "3/3", "✅", "❌"}
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

// TestLispRendererSuite invokes the Common Lisp roadmap validation suite if sbcl is installed.
func TestLispRendererSuite(t *testing.T) {
	sbcl, err := exec.LookPath("sbcl")
	if err != nil {
		t.Skip("sbcl not installed on host; skipping Lisp validation suite")
	}
	cmd := exec.Command(sbcl, "--script", "tools/roadmap/test.lisp")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sbcl test.lisp failed: %v\noutput:\n%s", err, string(out))
	}
}
