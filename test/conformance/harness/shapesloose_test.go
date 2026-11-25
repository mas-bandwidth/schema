package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// THE LOOSE SHAPES UNIT IS WHERE bounds_octets_below_min.bin COMES FROM
// (testdata/conformance/tables/MANIFEST.txt, the shapes block, whose line
// says written-by=testdata/conformance/tables/shapes-loose): the image is
// `bin/schema pack --root Bounds` of {"octets": [7]} against
// testdata/conformance/tables/shapes-loose, the shapes unit with every Min
// dropped, so the count of 1 rides as written. The pack against the loose
// unit must write the committed image byte for byte, and the same pack
// against the shapes unit itself must not write it, which is why the loose
// unit exists.
func TestShapesLooseWritesTheBelowMinImage(t *testing.T) {
	m, _, u := corpus(t)
	inst, err := m.LookupInstance("shapes_bounds_octets_below_min")
	if err != nil {
		t.Fatal(err)
	}
	if inst.WrittenBy != "testdata/conformance/tables/shapes-loose" {
		t.Fatalf("shapes_bounds_octets_below_min names written-by=%q, want the loose shapes unit", inst.WrittenBy)
	}
	want, err := os.ReadFile(inst.Wire)
	if err != nil {
		t.Fatal(err)
	}
	one := t.TempDir()
	if err := os.WriteFile(filepath.Join(one, "Bounds.json"), []byte(`{"octets": [7]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pack := func(dir string) ([]byte, bool, error) {
		paths, err := compiler.GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		u.c.FormatInPlace = false
		unit, err := u.c.Load(paths)
		if err != nil {
			t.Fatalf("loading %s: %v", dir, err)
		}
		back, _, rep, err := u.c.Pack(unit, "Bounds", one)
		return back, rep.Silent(), err
	}
	got, silent, err := pack(inst.WrittenBy)
	if err != nil || !silent {
		t.Fatalf("pack against shapes-loose: err=%v silent=%t", err, silent)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("pack against shapes-loose wrote %d bytes, not the %d of bounds_octets_below_min.bin", len(got), len(want))
	}
	strict, silent, err := pack("test/tables/shapes")
	if err == nil && silent && bytes.Equal(strict, want) {
		t.Error("the shapes unit itself writes the below-min image, so the loose unit is not needed: retire it and say so in the manifest")
	}
}
