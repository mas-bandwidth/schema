package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWireFuzzSkippedVectorKeepsPriorSeedMutable(t *testing.T) {
	m, _, _ := corpus(t)
	all, roots, index, _, err := buildWireFuzzRoster(m, wireFuzzOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var excluded *wireSeed
	for _, s := range all {
		r := roots[index[s]]
		if !r.variable || r.message {
			excluded = s
			break
		}
	}
	if excluded == nil {
		t.Fatal("full corpus has no outside-arm seed")
	}
	dir := t.TempDir()
	wire := filepath.Join(dir, "outside.bin")
	vectors := filepath.Join(dir, "INDEX.txt")
	if err = os.WriteFile(wire, excluded.wire, 0600); err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if excluded.message {
		suffix = " message"
	}
	if err = os.WriteFile(vectors, fmt.Appendf(nil, "outside %s %s %s%s\n", excluded.unit, excluded.root, wire, suffix), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts wireFuzzOptions
	}{
		{"builder", wireFuzzOptions{builder: true}},
		{"retain", wireFuzzOptions{retain: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _, _, skipped, err := buildWireFuzzRoster(m, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			tc.opts.vectors = vectors
			after, _, _, gotSkipped, err := buildWireFuzzRoster(m, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) || gotSkipped != skipped+1 {
				t.Fatalf("outside-arm vector changed seed count: %d/%d skipped %d/%d", len(after), len(before), gotSkipped, skipped)
			}
			for i, s := range after {
				if s.name != before[i].name || s.vector {
					t.Fatalf("excluded vector changed live seed %q into vector=%v", s.name, s.vector)
				}
			}
		})
	}
}
