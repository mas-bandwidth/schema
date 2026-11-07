package luautable_test

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// writer_bound_count is §5.8 row 4: the array_bounded_grow pair, [..4]int32 on
// the writer and [..8]int32 on the reader, the old file's count word forged
// from 4 to 7 — between the writer's 4 and the reader's 8. The NEW build reads
// it through its lineage, so the plan is COMPILED and the bound it carries is
// the WRITER's 4: the forged count refuses `count_over_bound` (the owner rule
// of 2026-10-15) and never lands seven slots the old writer could not write.
func TestLuauFixedVersioningWriterBoundCount(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	forged := luauForgeCount(t, luauCorpusFile(t, corpus, "old_array_bounded_grow.bin"), 7)
	luauVersionProbe(t, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, map[string][]byte{"file": forged}, luauWriterBoundBody)
}

const luauWriterBoundBody = `local report: any = {}
local values = LOAD("file", report)
check(values == nil and report.reason == "count_over_bound", "a count past the WRITER's 4 owes count_over_bound, never the reader's 8: " .. why(report))
check(not report.malformed, "a refusal by name never sets malformed too")
check(report.clamped == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0,
	"REFUSE is total: no counter moves: " .. why(report))
`

// luauForgeCount is old_array_bounded_grow.bin with its count word, located by
// the lead it follows, set to count.
func luauForgeCount(t *testing.T, data []byte, count uint32) []byte {
	t.Helper()
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 || bytes.Contains(data[at+1:], needle) {
		t.Fatal("the lead/count needle is not in the file exactly once, so the search is not a locator")
	}
	forged := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(forged[at+4:], count)
	return forged
}
