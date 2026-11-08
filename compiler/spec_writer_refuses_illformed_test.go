// TestSpecTextWriterRefusesIllFormedText pins docs/SPEC-TABLES.md §16.3's
// writer rule for text that is not text, and the §16.2 wstring row that cites
// it: a narrow field's ill-formed UTF-8 and a wide field's unpaired surrogate
// are refused `invalid_value`, never replaced with U+FFFD, and every reader
// refuses ill-formed text by name. The retired replacement unit is a stale
// sentence the page must not state.
package compiler

import (
	"os"
	"testing"
)

func TestSpecTextWriterRefusesIllFormedText(t *testing.T) {
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
				"`invalid_value`, naming the field, and nothing is replaced",
				"the READ REFUSES IT BY NAME at every string site",
				"`string_not_utf8: <key>` or `string_lone_surrogate: <key>`",
				"A silent\nreplacement would make two different texts one value",
			},
			stales: []string{
				"A MAXIMAL SUBPART is the unit of that replacement",
				"one per maximal subpart",
				"one `U+FFFD` per ill-formed unit",
			},
		},
		{
			row:  "§16.2 the wstring row that cites §16.3",
			text: mapping,
			wants: []string{
				"ill-formed UTF-8 is REFUSED BY NAME on read (`string_not_utf8`, §16.3), never transcoded to `U+FFFD`",
				"the writer refuses it `invalid_value` (§16.3)",
			},
			stales: []string{
				"§16.3's maximal-subpart rule writes it as `U+FFFD`",
				"and §16.3 writes it as `U+FFFD`",
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
