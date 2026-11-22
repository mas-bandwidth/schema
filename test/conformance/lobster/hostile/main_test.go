package main

import (
	"encoding/hex"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

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
