package main

import (
	"strings"
	"testing"
)

// A PRIOR-WRITER DRIFT ROW is written as a prior-connection and a
// prior-message line (run.go, md.Prior). The opt-in filter knows both kinds:
// a leg that omits the row's unit is handed neither line, and a leg that names
// it is handed both. Before, either kind was refused as an unknown line and
// every leg's manifest failed to derive.
func TestOptInFilterCarriesPriorWriterRows(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"connection plain_inside tbl 0x01 testdata/a.bin",
		"message plain_inside plain_inside Root a.bin b.bin",
		"prior-connection prior_inside tblprior 0x02 testdata/p.bin",
		"prior-message prior_inside prior_inside Prior p.bin q.bin",
	}, "\n")
	kept, dropped, err := optInLines(text, map[string]bool{"tblprior": true})
	if err != nil {
		t.Fatalf("the filter refused a prior-writer row: %v", err)
	}
	if strings.Contains(kept, "prior_inside") {
		t.Errorf("a leg that omits tblprior was handed its prior-writer row:\n%s", kept)
	}
	if !dropped["prior_inside"] || !strings.Contains(kept, "plain_inside") {
		t.Errorf("dropped %v, kept:\n%s", dropped, kept)
	}
	kept, _, err = optInLines(text, map[string]bool{"other": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(kept, "prior_inside") != 3 {
		t.Errorf("a leg that names tblprior lost part of its prior-writer row:\n%s", kept)
	}
	if err := checkLegRows(kept, notAsked{}); err != nil {
		t.Errorf("the kept prior-writer row does not check: %v", err)
	}
}
