package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

// THE MESSAGE SKIP DEPTH LIMIT (docs/SPEC-TABLES.md §3.3, message_skip_depth =
// 128), pinned on both sides by two shared wire-fuzz vectors
// (testdata/wire/tables/fuzz-vectors/message_skip_depth_{at,over}_limit.bin).
// The vectors are a batch of one LoginRequest body whose only field is the
// announced union arm Payload.login (a kind-13 entry LoginRequest does not
// declare), each skipped body holding that entry again. The field's skip is
// depth 0 and the Nth nested body is depth N.
//
// The test rebuilds both batches from the unit's own announcement, so the
// pinned files cannot drift from what they claim to be, and holds the oracle
// to the answer the C++ reference gives under strict-only: 128 nested bodies
// read with one unknown, 129 are refused.
func TestLimitMessageSkipDepthVectors(t *testing.T) {
	_, _, u := corpus(t)
	root, err := newWireRoot(u, "backenddemo", "LoginRequest", true, false)
	if err != nil {
		t.Fatal(err)
	}
	const login = 22 // Payload.login's 1-based position in the announcement
	entries := root.vocabulary.Entries()
	if len(entries) < login || entries[login-1].Id != 0x03c75db6e18f29d2 || entries[login-1].Kind != 13 {
		t.Fatalf("announcement entry %d is no longer Payload.login (kind 13): the vectors must be rebuilt", login)
	}
	for _, tc := range []struct {
		name     string
		levels   int
		accepted bool
		want     Counts
	}{
		{"message_skip_depth_at_limit", 128, true, Counts{Unknown: 1}},
		{"message_skip_depth_over_limit", 129, false, Counts{Unknown: 1, Refused: true}},
	} {
		built := skipDepthBatch(tc.levels, login, root.vocabulary.RefBits())
		pinned := pinnedWire(t, filepath.Join("testdata", "wire", "tables", "fuzz-vectors", tc.name+".bin"), built)
		if !bytes.Equal(built, pinned) {
			t.Errorf("%s: the pinned file is not %d nested bodies of entry %d", tc.name, tc.levels, login)
		}
		answer, err := root.oracle(pinned)
		if err != nil {
			t.Fatal(err)
		}
		if answer.accepted != tc.accepted {
			t.Errorf("%s: the oracle accepted=%v, want %v", tc.name, answer.accepted, tc.accepted)
		}
		if answer.report != tc.want {
			t.Errorf("%s: the oracle answers %v, the reference answers %v", tc.name, answer.report, tc.want)
		}
	}
}

func TestMessageSkipDepthVectors(t *testing.T) {
	TestLimitMessageSkipDepthVectors(t)
}

// skipDepthBatch is a form-2 batch of one body: the batch count M-1 = 0 in
// eight bits, then `levels` references to entry `ref` (each opening one more
// nested body), then levels+1 zero references closing every body and the
// root, padded with zero bits to the byte.
func skipDepthBatch(levels int, ref uint64, refBits int) []byte {
	var bits []bool
	put := func(v uint64, n int) {
		for i := range n {
			bits = append(bits, v>>uint(i)&1 == 1)
		}
	}
	put(0, 8)
	for range levels {
		put(ref, refBits)
	}
	for range levels + 1 {
		put(0, refBits)
	}
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b {
			out[i/8] |= 1 << uint(i%8)
		}
	}
	return append([]byte{2}, out...)
}
