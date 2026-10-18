// TestSpecGoRefusalEnumTable pins the one closed refusal enum of
// docs/SPEC-TABLES.md §6.5. The table there is bracketed by the
// refusal-names markers and lists one member per row, its snake_case name in
// the first column in backticks, so the cross-check card
// schema-go-spec-refusal-names-cross-check can parse it. The page is found the
// same way compiler/docs_test.go finds it: from this package's directory.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// The table's row names, parsed between the markers.
type refusalRows struct {
	names []string
	set   map[string]bool
}

// theFiveLineageCandidates are the parse-rule names §6.5 examined for
// retirement under the lineage rule (a reader never parses a stranger's
// layout). The report KEEPS all five: Go's one remaining parse, the lock-init
// path at internal/codegen/gotable/fixedruntime.go:1675-1697 (the call at
// :1681), still reaches each of them through tableFixedParseLayout and its
// c.fail literals. So each must be a row here.
var theFiveLineageCandidates = []string{
	"layout_count_mismatch",
	"layout_kind_unknown",
	"layout_kind_invalid",
	"layout_size_mismatch",
	"layout_tree_unclosed",
}

// retiredNames are the §6.5 names the report retired. None: every candidate
// above is still reachable at the lock-init parse, so each is kept and named
// there. A later card that removes the last reachable path moves its name
// here and out of the table.
var retiredNames = []string{}

func TestSpecGoRefusalEnumTable(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	const begin = "<!-- refusal-names:begin -->"
	const end = "<!-- refusal-names:end -->"
	b := strings.Index(page, begin)
	if b < 0 {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the %q marker is missing", begin)
	}
	rest := page[b+len(begin):]
	e := strings.Index(rest, end)
	if e < 0 {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the %q marker is missing after the begin marker", end)
	}
	block := rest[:e]
	if strings.Contains(block, begin) {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the begin marker appears twice before the end marker")
	}

	rows := parseRefusalRows(t, block)
	if len(rows.names) < 40 {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the refusal-names table lists %d members, want the whole closed enum (at least 40): %v", len(rows.names), rows.names)
	}

	// The three names the card pins by name: the fixed form's lineage answer,
	// the form-registry answer, and the message-form prose made a name.
	for _, name := range []string{"layout_unknown", "newer_form", "message_form_as_file"} {
		if !rows.set[name] {
			t.Errorf("docs/SPEC-TABLES.md §6.5: %q is not a refusal-names row", name)
		}
	}

	// The five lineage candidates are kept, because the lock-init parse still
	// reaches each. Each is a row and its row names the reachable line.
	for _, name := range theFiveLineageCandidates {
		if !rows.set[name] {
			t.Errorf("docs/SPEC-TABLES.md §6.5: %q was kept by the report but is not a row", name)
		}
	}

	// A retired name is absent from the table.
	for _, name := range retiredNames {
		if rows.set[name] {
			t.Errorf("docs/SPEC-TABLES.md §6.5: %q was retired but is still a row", name)
		}
	}
}

// parseRefusalRows reads the markdown table in block: a header row, a
// separator row, then one member per row with its name in the first column in
// backticks. It fails on a block that holds no table or a row whose first
// column is not exactly one backticked snake_case name.
func parseRefusalRows(t *testing.T, block string) refusalRows {
	t.Helper()
	out := refusalRows{set: map[string]bool{}}
	header, separator, rows := false, false, 0
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		if !header {
			header = true
			continue
		}
		if !separator {
			if !strings.Contains(trimmed, "---") {
				t.Fatalf("docs/SPEC-TABLES.md §6.5: the refusal-names header is not followed by a table separator: %q", trimmed)
			}
			separator = true
			continue
		}
		rows++
		cell := trimmed[1:]
		if i := strings.Index(cell, "|"); i >= 0 {
			cell = cell[:i]
		}
		cell = strings.TrimSpace(cell)
		if len(cell) < 3 || cell[0] != '`' || cell[len(cell)-1] != '`' {
			t.Fatalf("docs/SPEC-TABLES.md §6.5: a refusal-names row's first column is not one backticked name: %q", trimmed)
		}
		name := cell[1 : len(cell)-1]
		if !isRefusalName(name) {
			t.Fatalf("docs/SPEC-TABLES.md §6.5: %q is not a snake_case refusal name", name)
		}
		if out.set[name] {
			t.Fatalf("docs/SPEC-TABLES.md §6.5: %q appears twice in the refusal-names table", name)
		}
		out.set[name] = true
		out.names = append(out.names, name)
	}
	if !header || !separator {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the refusal-names markers do not bracket a table")
	}
	if rows == 0 {
		t.Fatalf("docs/SPEC-TABLES.md §6.5: the refusal-names table has no member rows")
	}
	return out
}

func isRefusalName(name string) bool {
	if name == "" || strings.HasPrefix(name, "_") || strings.HasSuffix(name, "_") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
