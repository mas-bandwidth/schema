// TestSpecTextWriterRefusesWhatIsNotText pins docs/SPEC-TABLES.md §16.3's
// writer rule and the §16.2 wstring row that cites it: storage a program built
// holding ill-formed UTF-8 or an unpaired surrogate is REFUSED by the writer,
// invalid_value, and nothing is replaced (§16.14, W0). The readers refuse the
// same text by name (string_not_utf8), so no text form turns storage that is
// not text into text.
package compiler

import (
	"os"
	"testing"
)

func TestSpecTextWriterRefusesWhatIsNotText(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	mapping := specTablesSection(t, page,
		"### 16.2 The mapping, field kind by field kind", "### 16.3 ")
	writer := specTablesSection(t, page,
		"### 16.3 What the writer refuses", "### 16.4 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§16.3 the writer refuses what is not text",
			text: writer,
			wants: []string{
				"THE WRITER REFUSES WHAT IS NOT TEXT, AND ESCAPES WHAT IS",
				"the writer REFUSES them",
				"`invalid_value`",
				"nothing is replaced",
				"**Storage that is not text** — `invalid_value`",
			},
			stales: []string{
				"A MAXIMAL SUBPART is the unit of that replacement",
				"one per maximal subpart",
				"is written as\none `U+FFFD`",
			},
		},
		{
			row:  "§16.2 the wstring row that cites §16.3",
			text: mapping,
			wants: []string{
				"the writer refuses it `invalid_value` (§16.3)",
			},
			stales: []string{
				"maximal-subpart rule writes it as `U+FFFD`",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			mustState(t, r.row, r.text, want)
		}
		for _, stale := range r.stales {
			mustNotState(t, r.row, r.text, stale)
		}
	}
}
