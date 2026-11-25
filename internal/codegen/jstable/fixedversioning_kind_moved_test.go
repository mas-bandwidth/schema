package jstable

// kind_moved, the second half of the UNLAWFUL lineage entry
// (docs/FIXED-FORM-VERSIONING-TESTS.md row `kind_moved`): the OLD writer carries
// `v int32`, the NEW reader declares `v float32`, a kind off every ladder (§5.1
// "ladder"). EMIT refuses a kind that moved rather than reinterpreting it or
// counting it and reading on (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT, §5.4, §5.9
// #30): no plan is laid, the load refuses `layout_malformed` with every counter
// at zero, and every row handed in is back at its declared defaults
// (SPEC-TABLES §6.5: lead = 1, v = 0, trail = 2), on a first load and a second.
// Both kinds are four bytes, so a reinterpreting leg lands the bits of int32 7
// as a float and a counting leg lands 0 with the brackets read: either loads,
// and this probe is red.

import (
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningKindMoved(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_kind_moved")
	older := jsReadSchema(t, "VOLD_kind_moved")
	table := jsFixedRootName(t, older)
	src := jsUnlawfulProbe(filepath.Join(corpus, "old_kind_moved.bin"), "kind_moved",
		`back[k].lead = 99; back[k].v = 5.5; back[k].trail = 88;`,
		`back[k].lead === 1 && back[k].v === 0 && back[k].trail === 2`)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("NEW-READS-OLD kind_moved: %v\n%s", err, out)
	}
}
