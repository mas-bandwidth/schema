// TestSpecJsonWideIntegerRefusesNumberToken pins docs/SPEC-TABLES.md §16.2's
// read rule for a JSON integer field whose kind is wider than 32 bits
// (docs/rulings.json `json-wide-integers-refuse-number-token`): such a field
// takes the STRING form only, and a bare number token there refuses
// `kind_mismatch`. It tightens the 2026-10-22 read half that such a field
// reads "a string or a number".
package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSpecJsonWideIntegerRefusesNumberToken(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 reads a wide integer as a string only", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")

		mustState(t, "§16.2 the wide-integer row",
			section,
			"is written as a JSON string of decimal digits and reads as a string only; a bare number token there refuses `kind_mismatch`")
		mustState(t, "§16.2 the wide fixed row",
			section,
			"is written as a string of digits and reads as a string only; a bare number token there refuses `kind_mismatch`")
		mustState(t, "§16.2 the int128 row",
			section,
			"Reads as a string only; a bare number token there refuses `kind_mismatch`")
		mustState(t, "§16.2 the wide kinds prose",
			section,
			"reads as a string only")

		mustNotState(t, "§16.2 no longer reads a wide integer as a number",
			section, "reads as a string or a number")
	})

	t.Run("docs/rulings.json carries json-wide-integers-refuse-number-token", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/rulings.json")
		if err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		var file struct {
			Rulings []struct {
				ID         string   `json:"id"`
				Date       string   `json:"date"`
				Quotes     []string `json:"quotes"`
				Source     string   `json:"source"`
				Supersedes []string `json:"supersedes"`
			} `json:"rulings"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		const rulingText = "ruled 2026-11-10: a JSON integer field whose kind is wider than 32 bits takes the STRING form only; a bare number token there refuses"
		found := false
		for _, r := range file.Rulings {
			if r.ID != "json-wide-integers-refuse-number-token" {
				continue
			}
			found = true
			if r.Date != "2026-11-10" {
				t.Errorf("json-wide-integers-refuse-number-token: date %q, want 2026-11-10", r.Date)
			}
			quotes := strings.Join(r.Quotes, "\n")
			if !strings.Contains(quotes, rulingText) {
				t.Errorf("json-wide-integers-refuse-number-token: quotes do not carry the ruling text %q", rulingText)
			}
			if !strings.Contains(r.Source, "strict wins") || !strings.Contains(r.Source, "coordinator") {
				t.Errorf("json-wide-integers-refuse-number-token: source does not state the standing rule and the coordinator: %q", r.Source)
			}
			supersedes := strings.Join(r.Supersedes, "\n")
			if !strings.Contains(supersedes, "2026-10-22") || !strings.Contains(supersedes, "a string or a number") {
				t.Errorf("json-wide-integers-refuse-number-token: supersedes does not name the 2026-10-22 read half: %q", supersedes)
			}
		}
		if !found {
			t.Error("docs/rulings.json carries no ruling json-wide-integers-refuse-number-token")
		}
	})
}
