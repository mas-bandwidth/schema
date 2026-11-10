// TestSpecTextWriterRefusesIllFormedText pins docs/SPEC-TABLES.md §16.3's
// writer rule for text that is not text: a field's ill-formed UTF-8 is refused
// `invalid_value`, never replaced with U+FFFD, and every reader refuses
// ill-formed text by name. The retired replacement unit is a stale sentence
// the page must not state. Wide text is retired (SPEC.md §4.12).
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
				"the writer REFUSES it",
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
