// TestSpecReadsRefuseNonCanonical pins the 2026-11-10 rule
// (docs/rulings.json reads-refuse-non-canonical): a read of the VARIABLE form
// (form 1) or the MESSAGE form (form 2) refuses a file or batch that decodes
// to a good value but that no conforming writer of a lawful lineage version
// would produce. The seven refusal cases are named, the three lawful evolutions
// stay lawful, the JSON read still accepts any field order, and the stale
// sentences that named field order and id-table order as reader permissions
// are gone.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	section3 := specTablesSection(t, page, "## 3. The wire", "## 4. Versioning")
	section4 := specTablesSection(t, page, "## 4. Versioning, and the one read", "## 5. Identity")
	flat3 := strings.Join(strings.Fields(section3), " ")
	flat4 := strings.Join(strings.Fields(section4), " ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§3 the variable form refuses the non-canonical",
			text: flat3,
			wants: []string{
				"fields out of the writer's order",
				"a field stored at its declared default",
				"an explicit null pointer",
				"an id-table entry that is unused, or out of first-use order",
				"an empty node table, or one out of its place",
				"a union written with arm reference `0`",
				"a message body that omits a field its own announcement declares",
			},
			stales: []string{},
		},
		{
			row:  "§4 the refusal list carries the rule and the lawful evolutions",
			text: flat4,
			wants: []string{
				"A NON-CANONICAL SPELLING OF A GOOD VALUE",
				"fields out of the writer's order",
				"a field stored at its declared default",
				"an explicit null pointer",
				"an id-table entry that is unused, or out of first-use order",
				"an empty node table, or one out of its place",
				"a union written with arm reference `0`",
				"a message body that omits a field its own announcement declares",
				"Absent fields, unknown fields from a newer writer and widened fields stay lawful",
				"a JSON read accepts any field order",
			},
			stales: []string{},
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

	flatPage := strings.Join(strings.Fields(page), " ")
	for _, stale := range []string{
		"FIRST-USE ORDER IS A WRITER'S RULE",
		"is not part of the contract",
		"not part of the READ contract",
	} {
		mustNotState(t, "docs/SPEC-TABLES.md", flatPage, stale)
	}

	rulings := readRulings(t)
	r, ok := rulings["reads-refuse-non-canonical"]
	if !ok {
		t.Fatalf("docs/rulings.json carries no ruling reads-refuse-non-canonical")
	}
	if r.Date != "2026-11-10" {
		t.Errorf("reads-refuse-non-canonical date %q, want 2026-11-10", r.Date)
	}
	if len(r.Quotes) == 0 {
		t.Error("reads-refuse-non-canonical quotes nobody")
	}
	found := false
	for _, q := range r.Quotes {
		if q == "strict. refuse non-canonical." {
			found = true
		}
	}
	if !found {
		t.Errorf("reads-refuse-non-canonical does not quote \"strict. refuse non-canonical.\": %q", r.Quotes)
	}
	if r.Source == "" {
		t.Error("reads-refuse-non-canonical carries no source")
	}
}
