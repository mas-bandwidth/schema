package main

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestDeadByteAcceptedMutant pins --relax-dead-bytes-accepted's per-mutant rule
// (RELAXED 2026-11-15 legs accept nonzero dead bytes; re-enable:
// relax/dead-bytes-accepted-<lang>): a counted array's count lowered by one
// leaves a nonzero slot past the count, which the engine refuses
// (fixed-dead-bytes-are-zero) and a leg that reads past it accepts and saves
// zeroed. That mutant is relaxed; a leg that refuses, a save that keeps a
// nonzero difference, and a save the oracle refuses stay compared.
func TestDeadByteAcceptedMutant(t *testing.T) {
	root := fuzzFixtureRoot(t, "package probe\n\nfixed table Root\n{\n    list [..3]int32\n    flag bool\n}\n", false)
	encode := func(text string) []byte {
		t.Helper()
		inst := root.model.New(root.def)
		if !root.model.Read(inst, []byte(text), &tabletext.Report{}) {
			t.Fatalf("fixture JSON %s", text)
		}
		wire, err := tablewire.Encode(root.model, inst)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}
	two := encode(`{"list":[5,7]}`)
	one := encode(`{"list":[5]}`)
	if len(one) != len(two) {
		t.Fatal("the fixed form has one size")
	}
	// the mutant: two's bytes with one's count, so the 7 sits past the count
	mutant := append([]byte(nil), two...)
	for i := range one {
		if one[i] != two[i] && one[i] != 0 {
			mutant[i] = one[i]
		}
	}
	ans, err := root.oracle(mutant)
	if err != nil {
		t.Fatal(err)
	}
	if ans.accepted || ans.reason != tablewire.ReasonWireDamaged {
		t.Fatalf("the oracle must refuse a nonzero dead slot as wire_damaged: accepted=%t reason=%q", ans.accepted, ans.reason)
	}
	if !deadByteAcceptedMutant(root, legReply{loaded: true, saved: one}, ans, mutant) {
		t.Fatal("a leg that reads past the dead slot and saves it zero must be relaxed")
	}
	if deadByteAcceptedMutant(root, legReply{loaded: false}, ans, mutant) {
		t.Fatal("a leg that refuses agrees with the oracle and is never relaxed")
	}
	if deadByteAcceptedMutant(root, legReply{loaded: true, saved: mutant}, ans, mutant) {
		t.Fatal("a save that keeps the nonzero dead slot must stay compared")
	}
	flagged := encode(`{"list":[5],"flag":true}`)
	if bytes.Equal(flagged, one) {
		t.Fatal("fixture: the flag must change the wire")
	}
	if deadByteAcceptedMutant(root, legReply{loaded: true, saved: flagged}, ans, mutant) {
		t.Fatal("a save differing in a live nonzero byte must stay compared")
	}
	truncated := one[:len(one)-1]
	if deadByteAcceptedMutant(root, legReply{loaded: true, saved: truncated}, ans, mutant) {
		t.Fatal("a save of another length must stay compared")
	}
}
