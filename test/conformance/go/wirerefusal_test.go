package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWireRefusalIsAnAnswerNeverAnAbort holds the R13 wire surface contract
// (test/conformance/README.md, review F-089). A present codec writes an
// explicit refusal and the surface still answers later cases. The committed
// refusal and accepted rows exercise both table forms. A missing codec
// writes an empty absence marker.
func TestWireRefusalIsAnAnswerNeverAnAbort(t *testing.T) {
	wire := func(name string) string { return filepath.Join(repoRoot, "testdata/wire/tables", name) }
	lines := []line{
		{"instance", "bounds_refused", "shapes", "Bounds", wire("shapes/bounds_octets_below_min.bin")},
		{"instance", "fixed_refused", "shapes", "FixedBounds", wire("shapes/fixed_octets_below_min.bin")},
		{"instance", "fixed_read", "shapes", "FixedBounds", wire("shapes/fixed_octets_three.bin")},
		{"instance", "blob_refused", "blobdemo", "Catalog", wire("blob_shared.bin")},
		{"instance", "blob_read", "blobdemo", "Catalog", wire("blob_equal_bytes.bin")},
		{"instance", "no_codec", "nosuchunit", "Bounds", wire("shapes/bounds_octets_at_min.bin")},
	}
	out := t.TempDir()
	if err := surfaceWire(lines, out); err != nil {
		t.Fatalf("the surface aborted: %v", err)
	}
	for _, refused := range []string{"bounds_refused", "fixed_refused", "blob_refused"} {
		answer, err := os.ReadFile(filepath.Join(out, refused))
		if err != nil || string(answer) != "refused\n" {
			t.Errorf("%s: want the explicit refusal, got %q (%v)", refused, answer, err)
		}
		if _, err := os.Stat(filepath.Join(out, refused+".absent")); !os.IsNotExist(err) {
			t.Errorf("%s: a present codec must not write an absence marker (%v)", refused, err)
		}
	}
	for _, read := range []string{"fixed_read", "blob_read"} {
		if b, err := os.ReadFile(filepath.Join(out, read)); err != nil || len(b) == 0 || string(b) == "refused\n" {
			t.Errorf("%s: want the written wire, got %d bytes (%v)", read, len(b), err)
		}
		if _, err := os.Stat(filepath.Join(out, read+".absent")); !os.IsNotExist(err) {
			t.Errorf("%s: want no absence marker (%v)", read, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(out, "no_codec.absent")); err != nil || len(b) != 0 {
		t.Errorf("no_codec: want an EMPTY .absent, got %q (%v)", b, err)
	}
	if _, err := os.Stat(filepath.Join(out, "no_codec")); !os.IsNotExist(err) {
		t.Errorf("no_codec: want no answer file (%v)", err)
	}
}
