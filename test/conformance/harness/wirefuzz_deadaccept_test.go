package main

import "testing"

// relax/dead-bytes-read-<lang> (docs/relaxed.md) leaves a mutant uncompared
// only when its one fault is non-zero dead bytes. Every shape that is not that
// stays compared, and is answered before the oracle is asked again.
func TestDeadByteAcceptedMutantIsNarrow(t *testing.T) {
	t.Parallel()
	data := []byte{1, 2, 3, 4}
	accepted := legReply{loaded: true, saved: []byte{1, 0, 3, 4}}
	refused := oracleAnswer{}
	fixed := &wireRoot{unit: "probe", root: "Root"}
	for _, tc := range []struct {
		name  string
		root  *wireRoot
		reply legReply
		ans   oracleAnswer
	}{
		{"a variable root", &wireRoot{variable: true}, accepted, refused},
		{"the message form", &wireRoot{message: true}, accepted, refused},
		{"the leg refuses", fixed, legReply{loaded: true, report: Counts{Refused: true}}, refused},
		{"the oracle accepts", fixed, accepted, oracleAnswer{accepted: true}},
		{"the leg cannot write back", fixed, legReply{loaded: true, saveFail: true}, refused},
		{"a write-back of another length", fixed, legReply{loaded: true, saved: []byte{1, 0, 3}}, refused},
		{"a write-back equal to the mutant", fixed, legReply{loaded: true, saved: []byte{1, 2, 3, 4}}, refused},
		{"a write-back that changes a byte to non-zero", fixed, legReply{loaded: true, saved: []byte{1, 0, 3, 5}}, refused},
	} {
		if deadByteAcceptedMutant(tc.root, tc.reply, tc.ans, data) {
			t.Errorf("%s: left uncompared", tc.name)
		}
	}
}
