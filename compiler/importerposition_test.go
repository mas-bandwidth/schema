// A migration importer from .proto and .fbs is a position on the record, not a
// bare dash. Issue #477: `flatc --proto` reads a .proto file and emits .fbs,
// the lowest-effort adoption lever any of the four competitors ships, and
// COMPARISON-TABLES.md's Converters row left schema as a bare dash with no
// reason. The recorded position is that `schema import --proto` (and --fbs),
// emitting a first-draft .schema with the bounds left for the author to
// declare, is not declined and not built, worth doing after 3.0.0 if a team
// asks. This gate holds both halves of the record: COMPETITION.md cites the
// issue for the importer and COMPARISON-TABLES.md's Converters row states the
// reason instead of the dash.
package compiler

import (
	"os"
	"strings"
	"testing"
)

const importerIssueLink = "issues/477"

func TestCompetitionRecordsTheImporterPosition(t *testing.T) {
	competition, err := os.ReadFile("../docs/COMPETITION.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(competition), importerIssueLink) {
		t.Errorf("docs/COMPETITION.md does not cite %s", importerIssueLink)
	}
	if !strings.Contains(string(competition), ".proto") || !strings.Contains(string(competition), ".fbs") {
		t.Errorf("docs/COMPETITION.md cites %s but does not name the .proto/.fbs importer", importerIssueLink)
	}

	comparison, err := os.ReadFile("../docs/COMPARISON-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	cell := convertersSchemaCell(t, string(comparison))
	if cell == "—" {
		t.Errorf("the Converters row's schema cell is a bare dash; it must state the position and cite %s", importerIssueLink)
	}
	if !strings.Contains(cell, "#477") {
		t.Errorf("the Converters row's schema cell does not cite #477: %q", cell)
	}
}

// convertersSchemaCell returns the schema column of the Converters row.
func convertersSchemaCell(t *testing.T, page string) string {
	t.Helper()
	for line := range strings.SplitSeq(page, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "| Converters ") {
			continue
		}
		row := strings.Trim(strings.TrimSpace(line), "|")
		cells := strings.Split(row, "|")
		if len(cells) < 2 {
			t.Fatalf("the Converters row has no schema cell: %q", line)
		}
		return strings.TrimSpace(cells[1])
	}
	t.Fatal("docs/COMPARISON-TABLES.md has no Converters row")
	return ""
}
