package roadmap

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestTheTablesAreWellFormed holds every row of the two tables to the header's
// language columns and every cell to ✅ or ❌, so a cell that is half edited or
// a row that gained a column without the header is red on the pull request.
func TestTheTablesAreWellFormed(t *testing.T) {
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	columns, rows := 0, 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if strings.TrimSpace(cells[0]) == "feature" {
			n := len(cells) - 1
			if columns != 0 && n != columns {
				t.Fatalf("the two tables carry %d and %d language columns; they must agree", columns, n)
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
			switch strings.TrimSpace(cell) {
			case "✅", "❌":
			default:
				t.Fatalf("row %q carries a cell %q that is neither ✅ nor ❌", strings.TrimSpace(cells[0]), strings.TrimSpace(cell))
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
