package roadmap

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheTallyFollowsTheTables recomputes the roadmap's tally from its two
// tables and holds the sentence to it. A row is a table line whose first cell
// is a feature; the header row and the separator row are not rows. Every row
// carries one cell per language column, and a cell is done when it is ✅.
func TestTheTallyFollowsTheTables(t *testing.T) {
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	var columns, rows, done int
	for line := range strings.SplitSeq(page, "\n") {
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
			case "✅":
				done++
			case "❌":
			default:
				t.Fatalf("row %q carries a cell %q that is neither ✅ nor ❌", strings.TrimSpace(cells[0]), strings.TrimSpace(cell))
			}
		}
	}
	total := rows * columns
	sentence := regexp.MustCompile(`(?m)^(\d+) of (\d+) cells are done\.`)
	m := sentence.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("ROADMAP.md carries no sentence of the form \"N of M cells are done.\"")
	}
	said, _ := strconv.Atoi(m[1])
	saidTotal, _ := strconv.Atoi(m[2])
	if said != done || saidTotal != total {
		t.Fatalf("the sentence says %d of %d cells are done and the tables say %d of %d: edit the sentence beside the cell", said, saidTotal, done, total)
	}
}

// TestNoLanguageIsCountedInProse refuses a sentence such as "C++ at 30 of 31"
// outside the tables: a per-language count written by hand is the kind of
// line that goes stale, and the tables already carry the count.
func TestNoLanguageIsCountedInProse(t *testing.T) {
	raw, err := os.ReadFile("../../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	counted := regexp.MustCompile(`\b[A-Za-z+#]+ at \d+ of \d+\b`)
	n := 0
	for line := range strings.SplitSeq(string(raw), "\n") {
		n++
		if strings.HasPrefix(line, "|") {
			continue
		}
		if counted.MatchString(line) {
			t.Fatalf("ROADMAP.md:%d counts a language by hand: %q", n, line)
		}
	}
}
