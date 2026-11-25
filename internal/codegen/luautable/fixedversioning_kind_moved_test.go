package luautable_test

import "testing"

// kind_moved, the second half of the UNLAWFUL lineage entry: the OLD writer
// carries `v int32`, the NEW reader declares `v float32`, a kind off every
// ladder (§5.1 "ladder"). EMIT refuses a kind that moved rather than
// reinterpreting it or counting it and reading on (docs/FIXED-FORM-ALGORITHM.md
// §5.2 EMIT, §5.4, §5.9 #30): no plan is laid, the load refuses
// `layout_malformed`, and every row handed in is back at its declared defaults
// (SPEC-TABLES §6.5), on two loads. Both kinds are four bytes, so a
// reinterpreting leg lands the bits of int32 7 as a float and a counting leg
// lands 0.0 with the brackets read: either loads, and this probe is red.
func TestLuauFixedVersioningKindMoved(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	luauVersionProbe(t, "VNEW_kind_moved", []string{"VOLD_kind_moved"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "old_kind_moved.bin")},
		luauUnlawfulBody("KindMoved",
			`r.Lead, r.V, r.Trail = 99, 5.5, 88`,
			`num(r.Lead) == 1 and num(r.V) == 0 and num(r.Trail) == 2`))
}
