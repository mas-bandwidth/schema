package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWireRefusalIsAnAnswerNeverAnAbort holds the wire surface to the driver
// contract (test/conformance/README.md; review F-089, found in this driver
// too): a present codec's refusal of a row's wire is that row's answer, a
// .absent holding the read's reason, whatever the reason, and the rows after
// it are still answered. On the committed corpus: shapes Bounds and
// FixedBounds below their minimum count (count_over_bound) and blobdemo's
// shared blob (blob_shared) are refused; their legal twins on the same roots
// are written; a unit with no codec is an EMPTY .absent.
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
		reason, err := os.ReadFile(filepath.Join(out, refused+".absent"))
		if err != nil || len(reason) == 0 {
			t.Errorf("%s: want a .absent holding the read's reason, got %q (%v)", refused, reason, err)
		}
	}
	for _, read := range []string{"fixed_read", "blob_read"} {
		if b, err := os.ReadFile(filepath.Join(out, read)); err != nil || len(b) == 0 {
			t.Errorf("%s: want the written wire, got %d bytes (%v)", read, len(b), err)
		}
		if _, err := os.Stat(filepath.Join(out, read+".absent")); err == nil {
			t.Errorf("%s: a legal row answered .absent", read)
		}
	}
	if b, err := os.ReadFile(filepath.Join(out, "no_codec.absent")); err != nil || len(b) != 0 {
		t.Errorf("no_codec: want an EMPTY .absent, got %q (%v)", b, err)
	}
}
