// TestSpecTextFffdIsPerMaximalSubpart pins docs/SPEC-TABLES.md §16.3's unit
// for "one U+FFFD per ill-formed sequence", and the §16.2 wstring reader row
// that cites it. The standard must state the unit first: the Unicode maximal
// subpart, with the worked examples, so the Go writers can be held to it.
//
// The card's evidence read at d808c3f54: both Go text writers replaced one
// U+FFFD per bad byte (internal/codegen/gotable/json.go:846 and
// internal/tabletext/write.go:682), the generated walk's reader did the same
// (json.go:1447, utf8.DecodeRune n=1), and the pack text reader took a lead
// byte plus its continuations (internal/tabletext/read.go:334-358). Both Go
// readers have since been landed to REFUSE ill-formed text by name
// (`string_not_utf8`, `string_lone_surrogate`), which §16.3 states; this pin
// holds the replacement unit that remains, the writers' maximal subpart.
package compiler

import (
	"os"
	"testing"
)

func TestSpecTextFffdIsPerMaximalSubpart(t *testing.T) {
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
			// §16.3: the replacement unit is the Unicode maximal subpart,
			// with the card's six worked examples.
			row:  "§16.3 the maximal-subpart unit",
			text: writer,
			wants: []string{
				"A MAXIMAL SUBPART is the unit of that replacement",
				"The Unicode Standard, chapter 3",
				"U+FFFD substitution of maximal",
				"one per maximal subpart",
				"beginning at the first unconvertible byte",
				"or, when no such prefix exists, that one byte",
				"`E2 82 41` writes `U+FFFD` then `A`",
				"`F0 80 80 80` writes four `U+FFFD`",
				"`ED A0 80` (a surrogate) writes three",
				"`C0 AF` writes two",
				"`F4 90 80 80` writes four",
				"a lone `FF` writes one",
			},
			stales: []string{
				"one per sequence",
				"one `U+FFFD` per ill-formed unit",
			},
		},
		{
			// §16.2: the wstring row that cites §16.3 now names the
			// maximal-subpart rule by reference, never restating it.
			row:  "§16.2 the reader row that cites §16.3",
			text: mapping,
			wants: []string{
				"§16.3's maximal-subpart rule writes it as `U+FFFD`",
			},
			stales: []string{
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
