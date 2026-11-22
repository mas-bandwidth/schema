package main

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
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

// TestScorerOutcomes grades the shared scorer outcome cases
// (tools/hostilecorpusexec/testdata/scorer-outcomes.tsv) through the answer,
// expectation and comparison the corpus run uses: a refusal with another name
// or none passes, a changed verdict either way, a different accepted value or
// a worker that answered nothing fails. It runs no Lobster.
func TestScorerOutcomes(t *testing.T) {
	outcomes, err := hostilecorpus.LoadScorerOutcomes("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		var a []string
		ok := true
		if name, refused := o.Refused(); refused {
			a = []string{hostilecorpus.Refuse, name}
		} else if text, accepted := o.Accepted(); accepted {
			a = []string{hostilecorpus.Accept, hex.EncodeToString(text)}
		} else {
			ok = false
		}
		c := hostilecorpus.Case{Name: o.Name, Carrier: "text", Verdict: o.Verdict, Reason: o.Reason, Digest: o.Digest}
		got, want := answerOf(a, ok, false), expected(c)
		if pass := agrees(got, want); pass != o.Pass {
			t.Errorf("%s: got %q want %q graded pass=%t, the fixture says pass=%t", o.Name, got, want, pass, o.Pass)
		}
	}
}
