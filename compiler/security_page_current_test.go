// TestSecurityPageCurrent pins docs/SECURITY.md's in-scope enum line to the
// wire it describes. An unknown enum value is a packet-wire acceptance bug. On
// the table wire a variable-form enum value from a newer writer reads as None
// by design (SPEC-TABLES.md §4), and the fixed form refuses an enum ordinal
// outside the writer's set (FIXED-FORM-ALGORITHM.md §4.6).
package compiler

import (
	"os"
	"testing"
)

func TestSecurityPageCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	inScope := specTablesSection(t, page, "## In scope", "## Out of scope")

	const row = "docs/SECURITY.md §In scope, the enum line"

	wants := []string{
		"An enum value on the **packet wire** that is not a declared variant being",
		"reads as `None` by design (lawful version evolution",
		"the fixed form refuses an enum ordinal",
		"outside the writer's set (`out_of_range`",
	}
	for _, want := range wants {
		mustState(t, row, inScope, want)
	}

	stales := []string{
		"An enum value that is not a declared variant being accepted.",
		"past the reader's top variant",
	}
	for _, stale := range stales {
		mustNotState(t, row, inScope, stale)
	}
}
