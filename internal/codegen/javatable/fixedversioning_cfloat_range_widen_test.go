package javatable

// `cfloat_range_widen`, §5.8's row, on the java leg: `VOLD_/VNEW_cfloat_range_widen`
// differ only by the `aim` field's range, [-1, 1] -> [-2, 2] — the range WIDENED. A
// compressed float rides as the float32 ITSELF (SPEC §3.4), and on a cross-version read
// it is held to the WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json strict-only-read; docs/FIXED-FORM-ALGORITHM.md §4.6).
//
// Three reads of the corpus: `old_` lands its written 0.5 whole (the control: a writer
// range NARROWER than the reader's lands what it wrote); `hostile_` forges 1.5, outside
// the old writer's range but inside the new reader's, and the strict read REFUSES the
// whole load as outOfRange — the old writer could not have written it; `past_` forges
// 5.0, past both ranges, and refuses the same way.

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

	// THE BITS, FROM THE MANIFEST (schema#1164): `old_`'s value is the
	// manifest's `values=r0.aim`. `hostile_` (1.5, past the WRITER's max 1.0)
	// and `past_` (5.0) land nothing: each refuses the whole load.
	for _, tc := range []struct {
		file    string
		aim     uint32
		refuses bool
	}{
		{"old_cfloat_range_widen.bin", manifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"), false},
		{"hostile_cfloat_range_widen.bin", 0, true},
		{"past_cfloat_range_widen.bin", 0, true},
	} {
		r := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, tc.file))
		if tc.refuses {
			// past the WRITER's declared range: the strict read refuses the
			// whole load as outOfRange and lands nothing.
			requireNamedRefusal(t, tc.file, r, "outOfRange")
			continue
		}

		if r.n != 1 {
			t.Errorf("%s: n=%d, want 1", tc.file, r.n)
		}
		if r.refused || r.damaged {
			t.Errorf("%s: a clean read refused: refused=%v reason=%s malformed=%v", tc.file, r.refused, r.reason, r.damaged)
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
		if r.unknown != 0 || r.widened != 0 {
			t.Errorf("%s: counters moved on a clean read: unknown=%d widened=%d, want all 0",
				tc.file, r.unknown, r.widened)
		}
	}
}
