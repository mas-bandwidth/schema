package javatable

// `writer_bound_count` with TWO lineage endpoints (§5.8 row 4 of
// docs/FIXED-FORM-VERSIONING-TESTS.md), on the java leg. The reader
// VNEW_array_bounded_grow ([..8]int32) is handed TWO older endpoints with DIFFERENT
// bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE the file, and
// VMID_array_bounded_grow ([..6]int32), the distractor, which did not. The count
// word is forged to 7 exactly as the landed single-endpoint test forges it.
//
// The number that makes this test worth having is 6. Under the strict read
// (docs/plans/strict-only.md) a count past the bound REFUSES the load, so a
// second forge sets the count to 5: a reader that holds its own bound (8) or
// whichever endpoint it saw last (6) reads it, and only a plan that carries
// the WRITING endpoint's bound (4) refuses it as countOverBound.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningWriterBoundCountTwoRemotes(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: the count word is located by its neighbour `lead` = 0xAAAAAAAA
	// and its own lawful value 4 (the manifest's numbers for this writer). The
	// needle must occur exactly once or the forge has no single locator.
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	forged := append([]byte(nil), raw...)
	forged[at+4] = 0x07 // the forge: 4 -> 7, between the writer's bound and the reader's
	hostile := filepath.Join(t.TempDir(), "hostile_array_bounded_grow.bin")
	if err := os.WriteFile(hostile, forged, 0o644); err != nil {
		t.Fatal(err)
	}
	// THE DISCRIMINATING FORGE: 4 -> 5, inside the distractor's 6 and the
	// reader's 8, past only the WRITING endpoint's 4.
	five := append([]byte(nil), raw...)
	five[at+4] = 0x05
	hostileFive := filepath.Join(t.TempDir(), "hostile5_array_bounded_grow.bin")
	if err := os.WriteFile(hostileFive, five, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "array_bounded_grow", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema", "VMID_array_bounded_grow.schema"}},
		{key: "refuses", schema: "VOLD_array_bounded_grow.schema"},
	})

	// 7 is past the writer's 4 and the distractor's 6: the load refuses.
	requireNamedRefusal(t, "count 7", runProbe(t, classes, "Probe_reads", hostile), "countOverBound")
	// 5 refuses ONLY against the writing endpoint's 4: a plan that held the
	// distractor's 6 or the reader's own 8 would read it.
	requireNamedRefusal(t, "count 5", runProbe(t, classes, "Probe_reads", hostileFive), "countOverBound")
}
