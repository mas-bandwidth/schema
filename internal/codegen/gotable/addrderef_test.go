package gotable

import (
	"bytes"
	"regexp"
	"testing"
)

// TestGeneratedGoHasNoAddressOfDereference pins the staticcheck SA4001 fix:
// the message form used to spell &(*p) around a union arm's value expression,
// which is the same pointer as p. The check loads the real bench corpus and
// fails on the emitted text, so restoring the round trip is red here before
// any analyzer runs.
func TestGeneratedGoHasNoAddressOfDereference(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"})
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("the bench corpus generated no files")
	}
	roundTrip := regexp.MustCompile(`&\(\*([A-Za-z_][A-Za-z0-9_]*)\)([^.[A-Za-z0-9_(])`)
	for name, data := range out {
		if loc := roundTrip.FindSubmatchIndex(data); loc != nil {
			line := bytes.Count(data[:loc[0]], []byte("\n")) + 1
			t.Errorf("%s:%d emits %s, which staticcheck names SA4001: the address of a dereference is the pointer itself", name, line, data[loc[0]:loc[1]])
		}
	}
}
