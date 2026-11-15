// TestSpecJsonWideIntegerRefusesNumberToken pins docs/SPEC-TABLES.md §16.2's
// string-only read for wide integers and docs/rulings.json
// `json-wide-integers-refuse-number-token`: an integer field whose kind is
// wider than 32 bits takes the STRING form only; a bare number token there
// refuses `kind_mismatch`.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecJsonWideIntegerRefusesNumberToken(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states the string-only read", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")

		mustState(t, "§16.2 the wide integer reads as the string only",
			section, "reads as that STRING only; a bare number token there refuses `kind_mismatch`")
		mustState(t, "§16.2 the int128 row reads as the string only",
			section, "Reads as that STRING only")
		mustState(t, "§16.2 the number read is scoped to a fixed kind of 32 bits and under",
			section, "A fixed kind of 32 bits and under is written as the shortest EXACT decimal with at least one fractional digit and read from any number token")
		mustNotState(t, "§16.2 no longer reads a wide integer as a number",
			section, "reads as a string or a number")
	})

	t.Run("docs/rulings.json carries the ruling", func(t *testing.T) {
		t.Parallel()
		rulings := readRulings(t)

		r, ok := rulings["json-wide-integers-refuse-number-token"]
		if !ok {
			t.Fatal(`docs/rulings.json carries no ruling "json-wide-integers-refuse-number-token"`)
		}
		if r.Date != "2026-11-10" {
			t.Errorf("json-wide-integers-refuse-number-token date is %q, want 2026-11-10", r.Date)
		}
		const rulingText = "ruled 2026-11-10: a JSON integer field whose kind is wider than 32 bits takes the STRING form only; a bare number token there refuses kind_mismatch"
		if len(r.Quotes) != 1 || r.Quotes[0] != rulingText {
			t.Errorf("json-wide-integers-refuse-number-token quotes are %q, want exactly the ruling text %q", r.Quotes, rulingText)
		}
		if !strings.Contains(r.Source, "strict wins") {
			t.Errorf("json-wide-integers-refuse-number-token source is %q", r.Source)
		}
	})
}
