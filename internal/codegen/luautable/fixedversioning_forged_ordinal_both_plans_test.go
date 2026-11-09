package luautable_test

import (
	"bytes"
	"testing"
)

// §5.8 row 12, `forged_ordinal_both_plans`: old_enum_append.bin with r0.tier
// forged from 3 (Gold) to 4, an ordinal PAST the writer's three variants and a
// name the NEW reader does have (Platinum). The SAME bytes are read twice:
// by the NEW build (the COMPILED plan the file's hash selects) and by the OLD
// build (the IDENTITY plan). Both refuse the whole batch out_of_range and land
// nothing (docs/FIXED-FORM-ALGORITHM.md §4.6, strict-only): the bound is the
// WRITER's variant count, never the reader's four. The unforged file reads on
// both plans, so the refusal is the forge's and nothing else's.
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
check(values == nil and report.reason == "out_of_range", "a value past the writer's set refuses the batch (§4.6): " .. why(report))
check(report.widened == 0 and report.unknown == 0, "a refusal moved a counter: " .. why(report))
local strict: any = {}
check(LOAD("file", strict) == nil and strict.reason == "out_of_range", "the plain read refuses it too: " .. why(strict))
local back: any = {}
check(LOAD("clean", back) ~= nil, "the unforged file reads (the negative control of the forge): " .. why(back))
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_enum_append", []string{"VOLD_enum_append"}, 0, map[string][]byte{"file": forged, "clean": data}, body)
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VOLD_enum_append", nil, 0, map[string][]byte{"file": forged, "clean": data}, body)
	})
}
