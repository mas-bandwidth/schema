package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestOrphanCycleVectorPinned builds a cyclic node directory with no root
// pointer. An absent pointer is the canonical spelling of null.
func TestOrphanCycleVectorPinned(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	v := framingVector{
		unit: "graphdemo", root: "Scene",
		ids:  []string{"node-table", "type:ListNode", "next"},
		body: b(1, 12, 10, 2, 2, 4, 3, 17, 2, 0, 2, 1, 0, 0),
	}
	want, err := v.bytes(newUnits(m))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/pointer_orphan_cycle.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("orphan cycle vector differs from its canonical generator")
	}
}
