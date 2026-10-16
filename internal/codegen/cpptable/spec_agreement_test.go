// Spec agreement pins for the C++ table reference (docs/CPP-REFERENCE-REVIEW.md,
// part 1). Each test emits a small unit, reads what the emitter actually
// writes, and holds docs/SPEC-TABLES.md to the same fact, so a sentence that
// describes the reference cannot drift from it: change the emitter and the
// sentence goes red, change the sentence and the emitter does.
package cpptable

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// specTablesRaw is docs/SPEC-TABLES.md as it sits in the tree.
func specTablesRaw(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatalf("read docs/SPEC-TABLES.md: %v", err)
	}
	return string(raw)
}

// specTablesBetween returns the page from the first occurrence of start up to
// the first later occurrence of end, unfolded (line structure kept).
func specTablesBetween(t *testing.T, start, end string) string {
	t.Helper()
	text := specTablesRaw(t)
	i := strings.Index(text, start)
	if i < 0 {
		t.Fatalf("docs/SPEC-TABLES.md: anchor %q not found", start)
	}
	rest := text[i:]
	if j := strings.Index(rest[len(start):], end); j >= 0 {
		rest = rest[:len(start)+j]
	}
	return rest
}

func foldSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// specEmitAll generates one source's C++ table tree and returns every file's
// text concatenated in sorted name order.
func specEmitAll(t *testing.T, src string) string {
	t.Helper()
	files, err := GenerateLineage(cppUnitFrom(t, src), nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString("// FILE " + n + "\n")
		b.Write(files[n])
	}
	return b.String()
}

// TestSpecCookSpellingCppColumn pins §7's per-backend spelling table to the
// C++ cook surface: the open, the descriptors and §7.1's facts cells are the
// spellings the emitter writes, with X standing for the root, and the column
// names no XCook handle the C++ reference does not have.
func TestSpecCookSpellingCppColumn(t *testing.T) {
	t.Parallel()
	out := specEmitAll(t, `package p
table Scene {
    x int32
}
`)
	table := specTablesBetween(t, "**THE SPELLING, PER BACKEND.**", "**THE GENERATED STRUCT IS THE COOKED RECORD**")
	cells := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 4 || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells[strings.TrimSpace(parts[1])] = strings.TrimSpace(parts[2])
	}
	for _, row := range []string{"open", "the handle", "the root", "deref", "the record", "the descriptors", "§7.1's facts"} {
		cell, ok := cells[row]
		if !ok {
			t.Errorf("§7's spelling table lost its %q row", row)
			continue
		}
		if strings.Contains(cell, "XCook") {
			t.Errorf("§7's C++ cell for %q names an XCook the C++ reference does not emit: %s", row, cell)
		}
	}
	code := regexp.MustCompile("`([^`]+)`")
	for _, row := range []string{"open", "the descriptors"} {
		spans := code.FindAllStringSubmatch(cells[row], -1)
		if len(spans) == 0 {
			t.Errorf("§7's C++ cell for %q spells nothing: %q", row, cells[row])
			continue
		}
		for _, s := range spans {
			want := strings.ReplaceAll(s[1], "X", "Scene")
			if !strings.Contains(out, want) {
				t.Errorf("§7's C++ cell for %q spells %q, which the emitter does not write (as %q)", row, s[1], want)
			}
		}
	}
}
