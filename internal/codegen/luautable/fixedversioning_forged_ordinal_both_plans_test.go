package luautable_test

import (
	"bytes"
	"testing"
)

// §5.8 row 12, `forged_ordinal_both_plans`: old_enum_append.bin with r0.tier
// forged from 3 (Gold) to 4, an ordinal PAST the writer's three variants and a
// name the NEW reader does have (Platinum). The SAME bytes are read twice:
// by the NEW build (the COMPILED plan the file's hash selects) and by the OLD
// build (the IDENTITY plan). Both land None and count `clamped == 1` exactly:
// the bound is the WRITER's variant count, never the reader's four, and a leg
// that counts in the remap and again in the bounds pass lands 2.
func TestLuauFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	data := luauCorpusFile(t, corpus, "old_enum_append.bin")
	needle := []byte{3, 9, 0, 0, 0}
	at := bytes.Index(data, needle)
	if at < 0 || bytes.Contains(data[at+1:], needle) {
		t.Fatal("tier=3 followed by seq=9 is not in the file exactly once, so the search is not a locator")
	}
	forged := append([]byte(nil), data...)
	forged[at] = 4
	body := `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 1, "the forged file carries one record: " .. why(report))
if values == nil then return end
local strict: any = {}
check(LOAD("file", strict) == nil and strict.reason == "clamped", "the plain read is STRICT and refuses the clamp by name (§6.5): " .. why(strict))
check(report.reason == nil and not report.malformed, "a forged ordinal is not a refusal and not malformed: " .. why(report))
check(num(values[1].Tier) == 0, "a forged ordinal lands None, never Platinum: " .. tostring(values[1].Tier))
check(num(values[1].Seq) == 9, "the forged ordinal moved its neighbour")
check(report.clamped == 1, "the forged ordinal is counted exactly once: " .. why(report))
check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0, "no other counter moved: " .. why(report))
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, map[string][]byte{"file": forged}, body)
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VOLD_enum_append", nil, 0, map[string][]byte{"file": forged}, body)
	})
}
