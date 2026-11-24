package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The planted copy carries the canonical wire with its last byte inverted,
// and every other corpus file unchanged; the wire in memory is not touched.
func TestPlantCanonicalInvertsTheLastByte(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	wire := []byte{1, 2, 3}
	if err := os.WriteFile(filepath.Join(src, "canonical.bin"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "canonical.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, planted, err := plantCanonical(src, filepath.Join(t.TempDir(), "planted"), wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(planted, []byte{1, 2, 0xfc}) || !bytes.Equal(wire, []byte{1, 2, 3}) {
		t.Errorf("planted %x from %x", planted, wire)
	}
	got, err := os.ReadFile(filepath.Join(dir, "canonical.bin"))
	if err != nil || !bytes.Equal(got, planted) {
		t.Errorf("the copy's canonical.bin is %x (%v), want %x", got, err, planted)
	}
	if j, err := os.ReadFile(filepath.Join(dir, "canonical.json")); err != nil || string(j) != "{}" {
		t.Errorf("canonical.json changed: %q %v", j, err)
	}
	if _, _, err := plantCanonical(src, filepath.Join(t.TempDir(), "x"), nil); err == nil {
		t.Error("an empty wire was planted")
	}
}
