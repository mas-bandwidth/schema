package cstable

// kind_moved, the second half of the UNLAWFUL lineage entry: the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30): no plan is laid, the load refuses
// `layout_malformed`, and every row handed in, poisoned first, is back at its
// declared defaults (lead=1, v=0, trail=2; SPEC-TABLES §6.5) on both of two
// loads. Both kinds are four bytes, so a reinterpreting leg lands the bits of
// int32 7 as a float and a counting leg lands 0.0f with the brackets read:
// either loads, and this probe is red.

import (
	"testing"
)

func TestFixedVersioningKindMoved(t *testing.T) {
	corpus := csFixedCorpusFile(t, "old_kind_moved.bin")
	csRunOneVersionProbe(t, corpus, csVersionProbe{
		name:   "kind_moved/new_reads_old",
		reader: "VNEW_kind_moved",
		older:  []string{"VOLD_kind_moved"},
		file:   "old_kind_moved.bin",
		body: csUnlawfulBody(
			`back[k].Lead = 99u; back[k].V = 5.5f; back[k].Trail = 88u;`,
			`back[k].Lead == 1u && back[k].V == 0.0f && back[k].Trail == 2u`),
	})
}
