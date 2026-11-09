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
// (reader VOLD, no older), and both refuse the whole batch out_of_range and
// land nothing (docs/FIXED-FORM-ALGORITHM.md §4.6, strict-only). The unforged
// file reads on both plans, so the refusal is the forge's.
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
check(values == nil and report.reason == "out_of_range", "a value past the writer's set refuses the batch (§4.6): " .. why(report))
check(report.widened == 0 and report.unknown == 0, "a refusal moved a counter: " .. why(report))
local strict: any = {}
check(LOAD("file", strict) == nil and strict.reason == "out_of_range", "the plain read refuses it too: " .. why(strict))
local back: any = {}
check(LOAD("clean", back) ~= nil, "the unforged file reads (the negative control of the forge): " .. why(back))
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_union_append", []string{"VOLD_union_append"}, 0, map[string][]byte{"file": forged, "clean": data}, body)
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VOLD_union_append", nil, 0, map[string][]byte{"file": forged, "clean": data}, body)
	})
}
