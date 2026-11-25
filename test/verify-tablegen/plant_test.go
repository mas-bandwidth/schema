package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Every case with an expected answer has that answer's last byte inverted in
// the planted pack; a case without one, and the caller's cases, are untouched.
func TestPlantTablegenInvertsEveryExpectedAnswer(t *testing.T) {
	t.Parallel()
	cases := []CorpusCase{
		{ID: "a", Want1: []byte{1, 2}},
		{ID: "b"},
		{ID: "c", Want1: []byte{9}},
	}
	dir, planted, n, err := plantTablegen(cases, filepath.Join(t.TempDir(), "planted"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !bytes.Equal(planted[0].Want1, []byte{1, 0xfd}) || len(planted[1].Want1) != 0 || !bytes.Equal(planted[2].Want1, []byte{0xf6}) {
		t.Errorf("planted %d: %+v", n, planted)
	}
	if !bytes.Equal(cases[0].Want1, []byte{1, 2}) {
		t.Error("the caller's cases were changed")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "corpus.pack"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnpackCases(raw)
	if err != nil || len(back) != 3 || !bytes.Equal(back[2].Want1, []byte{0xf6}) {
		t.Errorf("the planted pack reads back as %+v (%v)", back, err)
	}
	if _, _, _, err := plantTablegen([]CorpusCase{{ID: "x"}}, t.TempDir()); err == nil {
		t.Error("a corpus with no expected answer was planted")
	}
}
