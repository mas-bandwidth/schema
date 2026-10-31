package luautable_test

import (
	"encoding/binary"
	"testing"
)

// §5.8 row 9, `refuse_writes_nothing`: the NEW build reads old_nested_append.bin
// with record 0's eight-byte per-record hash bitwise INVERTED and the header's
// hash untouched, so the header still selects the OLD lineage entry — a
// COMPILED plan whose appended Vec.w has a NONZERO default — and §5.3 step 11
// refuses by name, `no_layout`, before anything lands. Every counter is
// asserted EXACTLY 0. On this leg the read builds fresh values and returns them
// only when the whole file read, so "not one destination byte" is the nil the
// read answers: nothing reaches the caller.
func TestLuauFixedVersioningRefuseWritesNothing(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	data := luauCorpusFile(t, corpus, "old_nested_append.bin")
	at0 := 20 + int(binary.LittleEndian.Uint32(data[16:20]))
	if len(data)-at0 <= 8 {
		t.Fatalf("record 0 is too short to forge: %d", len(data)-at0)
	}
	forged := append([]byte(nil), data...)
	binary.LittleEndian.PutUint64(forged[at0:], ^binary.LittleEndian.Uint64(forged[at0:]))
	luauVersionProbe(t, "VNEW_nested_append", []string{"VOLD_nested_append"}, 0, map[string][]byte{"file": forged}, `local report: any = {}
local values = LOAD("file", report)
check(values == nil and report.reason == "no_layout", "a record hash naming no layout owes no_layout: " .. why(report))
check(not report.malformed, "a refusal by name never sets malformed too (the joint answer)")
check(report.clamped == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0,
	"REFUSE is total: no counter moves: " .. why(report))
`)
}
