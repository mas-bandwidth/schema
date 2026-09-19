package javatable

// `cfloat_range_widen`, §5.8's row, on the java leg: `VOLD_/VNEW_cfloat_range_widen`
// differ only by the `aim` field's range, [-1, 1] -> [-2, 2] — the range WIDENED. A
// compressed float rides as the float32 ITSELF (SPEC §3.4), so min, max and resolution
// are DEFINITIONS in the digest and widening is lawful here where it is not in the
// variable form.
//
// Three reads of the corpus: `old_` lands its written 0.5 whole (clamp 0); `hostile_`
// forges 1.5, outside the old writer's range but inside the new reader's, and lands it
// WHOLE with clamp 0 — the pass is the READER's, not the writer's; `past_` forges 5.0
// and clamps to the READER's own max 2.0 with clamp EXACTLY 1. `clamped` is asserted
// exactly, never >= 1: this row exists to catch a reader that counts twice or counts
// the wrong read. The `hostile_` read alone is not worth having — with the emitter's
// float bounds pass deleted outright it would STAY GREEN and only `past_` would go red.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := corpusDir(t)
	for _, name := range []string{
		"old_cfloat_range_widen.bin",
		"hostile_cfloat_range_widen.bin",
		"past_cfloat_range_widen.bin",
	} {
		if _, err := os.Stat(filepath.Join(corpus, name)); err != nil {
			t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", name)
		}
	}

	_, classes := buildRow(t, t.TempDir(), "cfloat_range_widen", []sideSpec{
		{key: "reads", schema: "VNEW_cfloat_range_widen.schema", older: []string{"VOLD_cfloat_range_widen.schema"}},
	})

	// THE BITS, AND TWO OF THE THREE FROM THE MANIFEST (schema#1164). These
	// were the decimal strings "0.5"/"1.5"/"2.0" — the weakest comparison of
	// the nine legs. `old_`'s value is the manifest's `values=r0.aim`;
	// `hostile_`'s is the manifest's own `forged=r0.aim@104`, the value the
	// reference wrote into those bytes. `past_` is NOT a manifest value and is
	// not dressed as one: the file carries 5.0 and what lands is THIS READER'S
	// OWN DECLARED MAX, 2.0 = 0x40000000, which is the whole content of that
	// column.
	for _, tc := range []struct {
		file    string
		aim     uint32
		clamped int
	}{
		{"old_cfloat_range_widen.bin", manifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"), 0},
		{"hostile_cfloat_range_widen.bin", manifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"), 0},
		{"past_cfloat_range_widen.bin", 0x40000000, 1},
	} {
		r := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, tc.file))

		if r.n != 1 {
			t.Errorf("%s: n=%d, want 1", tc.file, r.n)
		}
		if r.refused || r.malformed {
			t.Errorf("%s: a clean read refused: refused=%v reason=%s malformed=%v", tc.file, r.refused, r.reason, r.malformed)
		}
		if got := r.value["lead"]; got != "1" {
			t.Errorf("%s: lead=%s, want 1", tc.file, got)
		}
		if got := parseDumpedFloatBits(t, r.value["aim"]); got != tc.aim {
			t.Errorf("%s: aim=%#x, want %#x", tc.file, got, tc.aim)
		}
		if got := r.value["trail"]; got != "2" {
			t.Errorf("%s: trail=%s, want 2", tc.file, got)
		}
		if r.clamped != tc.clamped {
			t.Errorf("%s: clamped=%d, want %d exactly", tc.file, r.clamped, tc.clamped)
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
			t.Errorf("%s: counters moved on a clean read: unknown=%d kindMismatch=%d widened=%d, want all 0",
				tc.file, r.unknown, r.kindMismatch, r.widened)
		}
	}
}
