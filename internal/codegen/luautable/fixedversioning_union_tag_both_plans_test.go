package luautable_test

import (
	"bytes"
	"testing"
)

// union_tag_both_plans forges a union TAG: the ONE byte that carries
// old_union_append.bin's r0.pick.type, set to 9 — past the OLD writer's two
// arms AND the NEW reader's three. The nine-byte needle (tag 1, alpha.m = 7,
// seq = 15) must occur exactly once. The SAME forged bytes are read through
// BOTH plans, the COMPILED one (reader VNEW, older VOLD) and the IDENTITY one
// (reader VOLD, no older), and both land the tag as None (0), count
// `clamped == 1` exactly, and leave the neighbour alone.
func TestLuauFixedVersioningUnionTagBothPlans(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	data := luauCorpusFile(t, corpus, "old_union_append.bin")
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 || bytes.Contains(data[at+1:], needle) {
		t.Fatal("pick.type=1, alpha.m=7, seq=15 is not in the file exactly once, so the search is not a locator")
	}
	forged := append([]byte(nil), data...)
	forged[at] = 9
	body := `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 1, "the forged file carries one record: " .. why(report))
if values == nil then return end
local strict: any = {}
check(LOAD("file", strict) == nil and strict.reason == "clamped", "the plain read is STRICT and refuses the clamp by name (§6.5): " .. why(strict))
check(report.reason == nil and not report.malformed, "a forged tag is not a refusal and not malformed: " .. why(report))
check(values[1].Pick.Type == 0, "a tag past the arm set lands None (0): " .. tostring(values[1].Pick.Type))
check(num(values[1].Seq) == 15, "the tag that ran past its arm set moved its neighbour: " .. tostring(values[1].Seq))
check(report.clamped == 1, "the forged tag is clamped exactly once: " .. why(report))
check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0, "no other counter moved: " .. why(report))
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_union_append", []string{"VOLD_union_append"}, 0, map[string][]byte{"file": forged}, body)
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VOLD_union_append", nil, 0, map[string][]byte{"file": forged}, body)
	})
}
