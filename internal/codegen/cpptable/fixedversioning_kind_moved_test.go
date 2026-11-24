package cpptable

// kind_moved, the second half of the UNLAWFUL lineage entry: the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30): no plan is laid, the load refuses
// `layout_malformed`, and every row handed in is back at its declared defaults
// (SPEC-TABLES §6.5). Both kinds are four bytes, so a reinterpreting leg lands
// the bits of int32 7 as a float and a counting leg lands 0.0f with the
// brackets read: either loads, and this probe is red.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningKindMoved(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := cppUnlawfulBody("kind_moved", "KindMoved",
		`back[k].lead = 99u; back[k].v = 5.5f; back[k].trail = 88u;`,
		`back[k].lead == 1u && back[k].v == 0.0f && back[k].trail == 2u`)
	out, err := cppRunVersionProbe(t, "kind_moved",
		filepath.Join(corpus, "old_kind_moved.bin"), body, "")
	if err != nil {
		t.Fatalf("kind_moved: %v\n%s", err, out)
	}
}
