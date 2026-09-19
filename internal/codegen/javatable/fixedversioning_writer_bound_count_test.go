package javatable

// `writer_bound_count`, §5.8 row 4 of docs/FIXED-FORM-VERSIONING-TESTS.md, on the
// java leg. `VOLD_/VNEW_array_bounded_grow` as they stand — `[..4]int32` →
// `[..8]int32`, the READER's bound wider than the WRITER's — plus a forged
// `old_array_bounded_grow.bin` whose count word is set to 7, a value BETWEEN the
// writer's 4 and the reader's 8.
//
// The NEW build reads the forged OLD file through its lineage, so the plan is a
// COMPILED one, and the bound the count op clamps against is the WRITER's,
// carried by the plan: `valsCount` stays 4, never the reader's 8 and never the
// forged 7. `clamped` is asserted EXACTLY == 1 and never >= 1: the BOUNDS pass
// counts once per entry per record (§5.4), and a leg that also counted inside
// some other op would land 2 and be wrong. This is the java twin of the C row
// landed in #1126.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningWriterBoundCount(t *testing.T) {
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
	if bytes.Index(raw[at+1:], needle) >= 0 {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	forged := append([]byte(nil), raw...)
	forged[at+4] = 0x07 // the forge: 4 -> 7, between the writer's bound and the reader's
	hostile := filepath.Join(t.TempDir(), "hostile_array_bounded_grow.bin")
	if err := os.WriteFile(hostile, forged, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "array_bounded_grow", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema"}},
		{key: "refuses", schema: "VOLD_array_bounded_grow.schema"},
	})

	r := runProbe(t, classes, "Probe_reads", hostile)

	if r.n != 1 {
		t.Errorf("n=%d, want 1", r.n)
	}
	if r.refused || r.malformed {
		t.Errorf("a clean hostile read refused: refused=%v reason=%s malformed=%v", r.refused, r.reason, r.malformed)
	}
	if got := r.value["valsCount"]; got != "4" {
		t.Errorf("valsCount=%s, want the WRITER's 4 — never the reader's 8 and never the forged 7", got)
	}
	if got := r.value["vals"]; got != "list:1000,1001,1002,1003,0,0,0,0" {
		t.Errorf("vals=%s, want list:1000,1001,1002,1003 then the reader's declared default 0 in the grown slots", got)
	}
	if got := r.value["lead"]; got != "-1431655766" {
		t.Errorf("lead=%s, want 0xAAAAAAAA", got)
	}
	if got := r.value["trail"]; got != "-1145324613" {
		t.Errorf("trail=%s, want 0xBBBBBBBB", got)
	}
	if r.clamped != 1 {
		t.Errorf("clamped=%d, want 1 exactly — the bounds pass counts once per entry per record (§5.4)", r.clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("counters moved on a clean hostile read: unknown=%d kindMismatch=%d widened=%d, want all 0",
			r.unknown, r.kindMismatch, r.widened)
	}
}
