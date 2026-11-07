package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS4S5ReportAndIdentity(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	section4 := specTablesSection(t, page, "## 4. Versioning, and the one read", "## 5. Identity")
	section5 := specTablesSection(t, page, "## 5. Identity", "## 6. The two lives of a table")

	// the refusals that were tolerant-read counts are named as refusals, and
	// the report is the verdict and the diagnostic counters alone
	mustState(t, "§4 missing_required", section4, "A REQUIRED FIELD ABSENT — `missing_required`")
	mustState(t, "§4 blob_shared", section4, "FROM TWO SLOTS — `blob_shared`")
	mustState(t, "§4 non_finite_float", section4, "`non_finite_float`")
	mustState(t, "§4 no other counter", section4, "**No other counter exists**")
	mustState(t, "§4 retired counters", section4, "`duplicate`, `blob_shared` and `missing_required` were tolerant-read counts and\nare now refusal reasons")
	mustNotState(t, "§4's complete counter list", section4, "whose counters are `unknown`")
	mustNotState(t, "§4 the title", section4, "Versioning is wire tolerance")

	// a map whose key kind widens counts one `widened`, and no second counter
	mustState(t, "§4 a map's one counter", section4, "a map ONE for the map")
	mustNotState(t, "§4 the two-counter map sentence", section4, "both counters move for one map")

	mustState(t, "§5 was on a reached type's field", section5, "`was` is legal on a field of a")
	mustNotState(t, "§5 the outside-a-table-body sentence", section5, "`was` outside a table body is refused")

	mustState(t, "§5 the projection's wire name", section5, "the projection carries the wire name")
	mustNotState(t, "§5 a bare type rename's blanket id move", section5, "`type` rename MOVES THE PROTOCOL ID")
}

func specTablesSection(t *testing.T, page, start, end string) string {
	t.Helper()
	i := strings.Index(page, start)
	if i < 0 {
		t.Fatalf("docs/SPEC-TABLES.md no longer holds the section heading %q", start)
	}
	rest := page[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		t.Fatalf("docs/SPEC-TABLES.md no longer holds the section heading %q after %q", end, start)
	}
	return rest[:len(start)+j]
}

func mustState(t *testing.T, what, section, literal string) {
	t.Helper()
	if !strings.Contains(section, literal) {
		t.Errorf("%s: the section does not hold %q", what, literal)
	}
}

func mustNotState(t *testing.T, what, section, literal string) {
	t.Helper()
	if strings.Contains(section, literal) {
		t.Errorf("%s: the section still holds the stale %q", what, literal)
	}
}
