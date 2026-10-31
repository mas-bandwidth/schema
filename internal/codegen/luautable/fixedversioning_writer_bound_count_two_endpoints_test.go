package luautable_test

import "testing"

// writer_bound_count, two lineage endpoints (§5.8 row 4): the reader is
// VNEW_array_bounded_grow ([..8]int32), handed TWO lineage entries with
// different bounds — VOLD ([..4]int32), which WROTE the file, and VMID
// ([..6]int32), the distractor, which did not. The count forged to 7 refuses
// `count_over_bound` against the bound of the entry the file's HASH selected,
// the writer's 4: a reader that took the distractor's 6, or the newest older
// bound, would refuse for the wrong reason only by luck, and one that took the
// reader's 8 would land it.
func TestLuauFixedVersioningWriterBoundCountTwoEndpoints(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	forged := luauForgeCount(t, luauCorpusFile(t, corpus, "old_array_bounded_grow.bin"), 5)
	// 5 is past the writer's 4 and INSIDE the distractor's 6: only the
	// writer's own bound refuses it
	luauVersionProbe(t, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow", "VMID_array_bounded_grow"}, 0,
		map[string][]byte{"file": forged}, luauWriterBoundBody)
	clean := luauCorpusFile(t, corpus, "old_array_bounded_grow.bin")
	luauVersionProbe(t, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow", "VMID_array_bounded_grow"}, 0,
		map[string][]byte{"file": clean}, `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and report.reason == nil, "the writer's own file reads through the two-endpoint lineage: " .. why(report))
if values ~= nil then check(values[1].ValsCount == 4 and num(values[1].Vals[4]) == 1003, "the writer's four values did not land") end
`)
}
