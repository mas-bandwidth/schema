package javatable

// `cfloat_res_refine`, §5.8's row, on the java leg: `VOLD_/VNEW_cfloat_res_refine`
// differ only by the `aim` field's resolution, 0.1 -> 0.01 — the step REFINED, this
// form's widening. The compressed float rides as the float32 ITSELF (SPEC §3.4), so
// the resolution is a DEFINITION in the digest and the wire hash moves, not the record.
// This row takes NO forged file (the page says so in as many words).
//
// `clamped` is asserted EXACTLY == 0 and never >= 1: the old writer quantized to 0.1
// and 0.1 is a whole multiple of this reader's 0.01, so nothing requantizes — a reader
// that counted a clamp on this lawful refinement is exactly the read the row exists to catch.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_cfloat_res_refine.bin")
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatalf("the corpus has no old_cfloat_res_refine.bin: run `make tables-fixedform-corpus`")
	}

	_, classes := buildRow(t, t.TempDir(), "cfloat_res_refine", []sideSpec{
		{key: "reads", schema: "VNEW_cfloat_res_refine.schema", older: []string{"VOLD_cfloat_res_refine.schema"}},
	})

	r := runProbe(t, classes, "Probe_reads", oldFile)

	if r.n != 1 {
		t.Errorf("n=%d, want 1", r.n)
	}
	if r.refused || r.malformed {
		t.Errorf("a clean NEW-READS-OLD refused: refused=%v reason=%s malformed=%v", r.refused, r.reason, r.malformed)
	}
	if got := r.value["lead"]; got != "1" {
		t.Errorf("lead=%s, want 1", got)
	}
	// THE BITS, AND FROM THE MANIFEST (schema#1164). This was `got != "0.3"` —
	// a DECIMAL STRING, the weakest of the nine legs' comparisons, and "0.3" is
	// the shortest decimal that round-trips for more than one float32, so a
	// reader that requantized onto its own grid could land a neighbouring float
	// and this row would have called it equal.
	wantAim := manifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")
	if got := parseDumpedFloatBits(t, r.value["aim"]); got != wantAim {
		t.Errorf("aim=%#x, want %#x from the corpus manifest — the old writer's 0.1 grid is a whole multiple of the reader's 0.01, so the float rides whole and bit-exact", got, wantAim)
	}
	if got := r.value["trail"]; got != "2" {
		t.Errorf("trail=%s, want 2", got)
	}
	if r.clamped != 0 {
		t.Errorf("clamped=%d, want 0 exactly — the refinement requantizes nothing (§5.8)", r.clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("counters moved on a clean read: unknown=%d kindMismatch=%d widened=%d, want all 0",
			r.unknown, r.kindMismatch, r.widened)
	}
}
