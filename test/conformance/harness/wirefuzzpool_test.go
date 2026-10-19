package main

import (
	"fmt"
	"testing"
)

// The random pass draws from the manifest seeds alone. A leg whose only live
// roots are pinned vectors has an empty pool, and the run must name that
// outcome rather than index [0] of nothing (test/conformance/README.md,
// "The wire-fuzz driver"; docs/SPEC-TABLES.md §4.2).
func TestWireFuzzRandomPassWithNoManifestSeed(t *testing.T) {
	t.Parallel()
	const seed = uint64(24845619678)

	var drew []*wireMutant
	line := randomPass(nil, 3, seed, 1000, func(m *wireMutant) {
		drew = append(drew, m)
	})
	if len(drew) != 0 {
		t.Fatalf("empty pool drew %d mutants, want 0", len(drew))
	}
	wantLine := "wire-fuzz: random pass skipped: no manifest seed is live (3 pinned vectors only)"
	if line != wantLine {
		t.Fatalf("empty pool line = %q, want %q", line, wantLine)
	}
	if m := randomMutant(nil, seed, 0); m != nil {
		t.Fatalf("randomMutant over an empty list = %+v, want nil", m)
	}

	s := &wireSeed{wire: []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}, frame: &wireFrame{}}
	// The first three mutants a one-seed pool draws at this seed, as the
	// random pass drew them before the guard.
	want := []string{
		"00010203040d060708490a0b0c0d1e0f103978131415161718191a1b1c1d1e1f",
		"00010203",
		"000102030405060708090a0b0c0d0e0f1011121314151617182c1a1b1c1d1e1f",
	}
	var got []*wireMutant
	if line := randomPass([]*wireSeed{s}, 0, seed, 3, func(m *wireMutant) {
		got = append(got, m)
	}); line != "" {
		t.Fatalf("one-seed pool line = %q, want none", line)
	}
	if len(got) != len(want) {
		t.Fatalf("one-seed pool drew %d mutants, want %d", len(got), len(want))
	}
	for i, m := range got {
		if h := fmt.Sprintf("%x", m.data); h != want[i] {
			t.Fatalf("mutant %d = %s, want %s", i, h, want[i])
		}
		if m.seed != s || m.pass != "random" || m.index != i {
			t.Fatalf("mutant %d identity = %+v, want seed, random, index %d", i, m, i)
		}
	}
}
