package main

import (
	"encoding/hex"
	"testing"
)

// THE ORACLE'S ANSWER ON A MAP WHOSE HEADER CROSSES ITS L, pinned so the side
// every leg is compared against cannot drift. A red of the Dart wire fuzzer
// on ci-full run 41205503830 (seed 24845619678, corpus seed list_of_maps,
// random pass #5292); the leg was wrong and the oracle right, by
// docs/SPEC-TABLES.md §2.8 and §3.
//
// The second Squad's roster arrives as `02 0e 02 | 0e 83`: kind 14, L = 2,
// then an element kind of 14 where a map declares 13, and a count LEB128 whose
// first byte continues past the L. Every reader takes an array's header from
// the ENCLOSING body (internal/tablewire mapField), so the count reads, the
// element kind is judged, and the map is the ordinary array kind mismatch:
// one kind_mismatch, and the parent reads on past the L. A leg that bounded
// the header by the L found the count truncated, called it framing damage,
// and reported kind_mismatch 0.
func TestWireFuzzMapHeaderCrossingItsLengthIsAKindMismatch(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "listdemo", "Army", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString("01010e580d022a020e200d020e030602040d0705041401000000000e030609040d0705045a0000000000060432000000002a020e020e830603040d0705041500000000000e030608040d0705045b0000000000060433000000000704080000000026a9020c9b014878424f4f300d39841cec105b36094ac93dea0ce83094fde47c74a279448ee2e5b1861b638ebaadbc90e9ea716f0f0182bf0700000000000000")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if answer.report != (Counts{Unknown: 7, KindMismatch: 1, Duplicate: 1, Malformed: true}) {
		t.Fatalf("map header across L: the oracle says %s, want 7,1,0,0,1,true", answer.report)
	}
}
