package main

import (
	"encoding/hex"
	"testing"
)

// Historical hostile vector: the complete read must refuse.
func TestWireFuzzMapHeaderCrossingItsLengthRefuses(t *testing.T) {
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
	if answer.accepted || len(answer.encoded) != 0 {
		t.Fatalf("hostile wire produced a value: %+v", answer)
	}
}
