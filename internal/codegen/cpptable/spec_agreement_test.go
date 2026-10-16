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

// TestSpecFieldInfoColumnNames pins §8's map-and-list rule to the column names
// of the emitted TableFieldInfo: every snake_case column the rule names is a
// member of the descriptor the C++ reference writes.
func TestSpecFieldInfoColumnNames(t *testing.T) {
	t.Parallel()
	out := specEmitAll(t, `package p
enum Slot { A, B }
table Root {
    m map[int32]int32
    l []int32
    k [Slot]int32
}
`)
	i := strings.Index(out, "struct TableFieldInfo\n{")
	if i < 0 {
		t.Fatal("no struct TableFieldInfo in the emitted runtime")
	}
	body := out[i:]
	body = body[:strings.Index(body, "\n};")]
	members := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+[^/\n]*?\b(\w+)\s*;`).FindAllStringSubmatch(body, -1) {
		members[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`\(\s*\*\s*(\w+)\s*\)`).FindAllStringSubmatch(body, -1) {
		members[m[1]] = true
	}
	rule := specTablesBetween(t, "**A MAP AND AN UNBOUNDED ARRAY ARE ARRAY FIELDS", "**The public currency is the KEY")
	named := regexp.MustCompile("`([a-z]+(?:_[a-z]+)+)`").FindAllStringSubmatch(rule, -1)
	if len(named) == 0 {
		t.Fatal("§8's map-and-list rule names no column")
	}
	for _, n := range named {
		if !members[n[1]] {
			t.Errorf("§8's map-and-list rule names the column `%s`, which the emitted TableFieldInfo does not have", n[1])
		}
	}
}

// TestSpecBlockOpenClauses pins §19.2's BlockOpen paragraph to the emitted
// check: a block shorter than its PROJECTION (not the 24-byte prologue)
// answers truncated, and an offset_of below the projection or off the array's
// start alignment is bad_layout — two clauses the paragraph has to state.
func TestSpecBlockOpenClauses(t *testing.T) {
	t.Parallel()
	out := specEmitAll(t, `package p
fixed table Row {
    v uint32
}
fixed table Frame {
    stamp uint64
    rows  [..8]Row
}
`)
	// Frame carries the out-of-line array, so its check carries the per-array
	// clauses; the projection size is read off its offset_of floor
	m := regexp.MustCompile(`if \( offset_of < (\d+) \|\| \( offset_of % 64 \) != 0 \) \{ return TableCookRefuse\( reason, bad_layout \)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the emitted BlockOpen check lost its offset_of floor and alignment clause")
	}
	if m[1] == "24" {
		t.Fatalf("expected the projection to be larger than the 24-byte prologue; got %s", m[1])
	}
	if !strings.Contains(out, "if ( bytes < "+m[1]+" ) { return TableCookRefuse( reason, truncated )") {
		t.Errorf("the emitted BlockOpen check's short-buffer clause does not compare against the projection (%s bytes)", m[1])
	}
	para := foldSpace(specTablesBetween(t, "- **`BlockOpen` checks once and points", "**THE READ SIDE TAKES A CONST OVERLOAD"))
	for _, bad := range []string{"fewer than its prologue"} {
		if strings.Contains(para, bad) {
			t.Errorf("§19.2 says %q; the emitted check compares against the projection (%s bytes here)", bad, m[1])
		}
	}
	for _, want := range []string{"fewer than its projection", "past the end of the projection", "max( 64, alignof( element ) )"} {
		if !strings.Contains(para, want) {
			t.Errorf("§19.2's BlockOpen paragraph must state the emitted clause (missing %q)", want)
		}
	}
}
