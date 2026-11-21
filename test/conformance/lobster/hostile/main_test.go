package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAFindingCarriesTheAnswerItRecords is F-079 on the Lobster runner: a
// finding names the answer the port gives today and carries that answer and no
// other. Before, any accept or refusal was carried under any finding. The
// committed carry controls (internal/ci/hostilecorpus/testdata/carry/
// findings-lobster) are read here as the Lobster make target reads them; the
// controls that carry are the answer the finding records, and a refusal under
// `refuse` whatever its reason.
func TestAFindingCarriesTheAnswerItRecords(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "internal", "ci", "hostilecorpus", "testdata", "carry", "findings-lobster")
	read := func(name string) carriedFinding {
		t.Helper()
		f, err := readFindings(filepath.Join(dir, name+".tsv"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, ok := f["wire-valid"]
		if !ok {
			t.Fatalf("%s carries no wire-valid finding", name)
		}
		return got
	}
	accept := "accept c319fe4163a51643f37f62a2cc18dd6c3bfea204a6e2f3313de8cc9dc0a37a30"
	for _, c := range []struct {
		control, got string
		want         bool
	}{
		{"answer-matches", accept, true},
		{"answer-matches", "refuse damaged", false},
		{"answer-refuse", "refuse damaged", true},
		{"answer-refuse", "refuse value_out_of_range", true},
		{"answer-refuse", accept, false},
		{"answer-other-digest", accept, false},
		{"answer-other-digest", "refuse damaged", false},
	} {
		if carried := read(c.control).carries(c.got); carried != c.want {
			t.Errorf("%s carries %q: %v, want %v", c.control, c.got, carried, c.want)
		}
	}
	if _, err := readFindings(filepath.Join(dir, "no-answer.tsv")); err == nil || !strings.Contains(err.Error(), "answer") {
		t.Errorf("a finding that records no answer was read (it would carry any answer): %v", err)
	}
}
