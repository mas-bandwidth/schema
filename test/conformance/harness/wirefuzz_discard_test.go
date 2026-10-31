package main

import "testing"

// DAMAGE DISCARDS THE WHOLE LOAD (Glenn, 2026-10-31; docs/SPEC-TABLES.md §4):
// on a mutant the oracle reads as damaged, the refusal is the whole of what a
// leg owes. The counters, the retention report and the re-saved bytes are
// undefined there, so a leg that differs from the oracle on any of them is
// green, and a leg that does not say malformed is red. A clean mutant is
// compared in full, as before.
func TestWireVerdictComparesOnlyTheRefusalOnDamage(t *testing.T) {
	root := &wireRoot{retain: true}
	damaged := oracleAnswer{
		report:     Counts{Unknown: 5, KindMismatch: 1, Malformed: true},
		encoded:    make([]byte, 209),
		retained:   3,
		retainLost: 1,
	}
	leg := legReply{
		loaded:     true,
		report:     Counts{Unknown: 2, Malformed: true},
		saved:      make([]byte, 229),
		retained:   7,
		retainLost: 0,
	}
	if v := wireVerdict(root, leg, damaged, 300); v != "" {
		t.Fatalf("a leg that refuses the damaged load is red: %s", v)
	}
	missed := leg
	missed.report = Counts{Unknown: 5, KindMismatch: 1}
	if v := wireVerdict(root, missed, damaged, 300); v == "" {
		t.Fatal("a leg that read a damaged mutant as clean is green")
	}

	variable := &wireRoot{variable: true}
	noRoot := legReply{loaded: false, measure: 64, report: Counts{Malformed: true}}
	if v := wireVerdict(variable, noRoot, oracleAnswer{report: Counts{Malformed: true}, bytes: 64}, 300); v != "" {
		t.Fatalf("a leg that hands back no root on damage is red: %s", v)
	}

	// a builder leg that completes a damaged load still owes only the refusal
	builder := &wireRoot{builder: true}
	if v := wireVerdict(builder, legReply{loaded: true, report: Counts{Malformed: true}}, oracleAnswer{report: Counts{Malformed: true}, builderStopped: true, encFail: true}, 300); v != "" {
		t.Fatalf("a builder leg that refuses the damaged load is red: %s", v)
	}
	if v := wireVerdict(builder, legReply{loaded: true}, oracleAnswer{builderStopped: true, encFail: true}, 300); v == "" {
		t.Fatal("a builder's completion is no longer compared on a clean mutant")
	}

	refused := oracleAnswer{report: Counts{Refused: true}, encoded: []byte{1, 0}}
	if v := wireVerdict(root, legReply{loaded: true, report: Counts{Refused: true}, saved: []byte{1, 2, 3}}, refused, 10); v != "" {
		t.Fatalf("a leg that refuses the form is held to saved defaults: %s", v)
	}

	clean := oracleAnswer{report: Counts{Unknown: 1}, encoded: []byte{1, 0, 0}}
	if v := wireVerdict(root, legReply{loaded: true, report: Counts{Unknown: 1}, saved: []byte{1, 0, 1}}, clean, 10); v == "" {
		t.Fatal("a clean mutant's saved bytes are no longer compared")
	}
	if v := wireVerdict(root, legReply{loaded: true, report: Counts{Unknown: 1}, saved: []byte{1, 0, 0}, retained: 1}, clean, 10); v == "" {
		t.Fatal("a clean mutant's retention report is no longer compared")
	}
}
