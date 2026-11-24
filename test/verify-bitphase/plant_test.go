package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The planted copy trades the bytes of a valid vector and a hostile vector of
// the planted class, for every phase, and leaves the rest of the corpus alone.
func TestPlantCorpusSwapsEveryPhasesPair(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	cases := requiredRoster()
	blobs := map[string][]byte{}
	var manifest bytes.Buffer
	for i, c := range cases {
		blobs[c.File] = []byte{byte(i), byte(i >> 8)}
		manifest.WriteString(c.Kind + "\t" + c.Type + "\t" + c.File + "\n")
		if err := os.WriteFile(filepath.Join(src, c.File), blobs[c.File], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.tsv"), manifest.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := map[string][]byte{}
	for k, v := range blobs {
		orig[k] = v
	}
	dir, swapped, err := plantCorpus(src, filepath.Join(t.TempDir(), "planted"), "packet", cases, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if len(swapped) != len(bitphasePhases) {
		t.Errorf("swapped %d pairs, want one per phase (%d): %v", len(swapped), len(bitphasePhases), swapped)
	}
	got, err := os.ReadFile(filepath.Join(dir, "valid_p0_v0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig["hostile_reserved_p0.bin"]) {
		t.Errorf("valid_p0_v0.bin holds %x, want the reserved vector's %x", got, orig["hostile_reserved_p0.bin"])
	}
	if !bytes.Equal(blobs["hostile_reserved_p0.bin"], orig["valid_p0_v0.bin"]) {
		t.Error("the in-memory blobs were not swapped with the files")
	}
	if !bytes.Equal(blobs["hostile_align_p0.bin"], orig["hostile_align_p0.bin"]) {
		t.Error("a vector outside the planted class moved")
	}
	if _, _, err := loadManifest(dir); err != nil {
		t.Errorf("the planted corpus no longer loads, so the control would test the loader, not the legs: %v", err)
	}
	if _, _, err := plantCorpus(src, filepath.Join(t.TempDir(), "x"), "nonsense", cases, blobs); err == nil {
		t.Error("an unknown plant was accepted")
	}
}
