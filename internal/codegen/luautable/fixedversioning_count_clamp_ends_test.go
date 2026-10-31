package luautable_test

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// ---- count_over_bound ends (go/C1 and go/C2, the refusal) -------------------
//
// The two ends of the count bound: the count word of old_array_bounded_grow.bin
// forged to -1 (0xFFFFFFFF read as the wire's little-endian word) and to 9,
// past the writer's 4 AND the reader's 8. The OWNER RULE (2026-10-15) replaced
// the count clamp with a NAMED REFUSAL: a count past the PLAN's bound — the
// WRITER's, carried by the lineage entry (bill §12.5) — refuses
// `count_over_bound` before anything lands, no counter moved, not malformed
// (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3).
func TestLuauFixedVersioningCountClampEnds(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	data := luauCorpusFile(t, corpus, "old_array_bounded_grow.bin")
	// the OLD record is lead 0xAAAAAAAA immediately followed by the count 4
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 || bytes.Contains(data[at+1:], needle) {
		t.Fatal("the lead/count needle is not in the file exactly once, so the search is not a locator")
	}
	for name, count := range map[string]uint32{"negative": 0xFFFFFFFF, "over": 9} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			forged := append([]byte(nil), data...)
			binary.LittleEndian.PutUint32(forged[at+4:], count)
			luauVersionProbe(t, "VNEW_array_bounded_grow", []string{"VOLD_array_bounded_grow"}, 0, map[string][]byte{"file": forged}, `local report: any = {}
local values = LOAD("file", report)
check(values == nil and report.reason == "count_over_bound", "a count past the writer's bound owes count_over_bound: " .. why(report))
check(not report.malformed, "a refusal by name never sets malformed too")
check(report.clamped == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0,
	"REFUSE is total: no counter moves: " .. why(report))
`)
		})
	}
}
