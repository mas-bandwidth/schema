package luautable_test

import "testing"

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1164,
// statement B): a compressed float RIDES AS THE FLOAT in this form, so its
// bound is the READER's and the plan carries none. Three files, every value
// from the manifest:
//
//   - old_cfloat_range_widen.bin: the old writer's 0.5 lands exactly, nothing
//     counted;
//   - hostile_cfloat_range_widen.bin: aim forged to 1.5, outside the old
//     writer's [-1, 1] and inside the new reader's [-2, 2], lands WHOLE and
//     UNCOUNTED — the price the widening states;
//   - past_cfloat_range_widen.bin: aim forged to 5.0, outside the reader's
//     range too, refuses the batch out_of_range and lands nothing, which is
//     the file that proves the bounds pass runs at all (strict-only).
//
// hostile_ lands as the Go engine lands it (gotable's
// TestFixedVersioningCfloatRangeWiden): the plan carries no compressed-float
// writer range yet on any leg (docs/plans/strict-only.md decision 8 is owed).
func TestLuauFixedVersioningCfloatRangeWiden(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	reader := luauVersionUnit(t, "VNEW_cfloat_range_widen")
	root := luauVersionRoot(t, reader)
	cases := []struct {
		file, oracle, refused, extra string
	}{
		{"old_cfloat_range_widen.bin", "old_cfloat_range_widen.bin", "0", ""},
		{"hostile_cfloat_range_widen.bin", "", "0", `check(f32bits(v.Aim) == 0x3FC00000, "the hostile 1.5 inside the reader's range did not land whole: " .. tostring(v.Aim))`},
		{"past_cfloat_range_widen.bin", "", "1", ""},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			checks := c.extra
			if c.oracle != "" {
				checks = luauManifestChecks(t, corpus, c.oracle, root)
			}
			if c.refused == "1" {
				body := `local report: any = {}
local values = LOADT("file", report)
check(values == nil and report.reason == "out_of_range", "a value past the reader's range refuses the batch: " .. why(report))
check(report.widened == 0 and report.unknown == 0, "a refusal moved a counter: " .. why(report))
`
				luauVersionProbe(t, "VNEW_cfloat_range_widen", []string{"VOLD_cfloat_range_widen"}, 0,
					map[string][]byte{"file": luauCorpusFile(t, corpus, c.file)}, body)
				return
			}
			body := `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 1, "the file carries one record: " .. why(report))
if values == nil then return end
local v = values[1]
check(report.reason == nil, "not a refusal: " .. why(report))
check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0, "no other counter moved: " .. why(report))
check(num(v.Lead) == 1 and num(v.Trail) == 2, "the brackets moved")
` + checks + "\n"
			luauVersionProbe(t, "VNEW_cfloat_range_widen", []string{"VOLD_cfloat_range_widen"}, 0,
				map[string][]byte{"file": luauCorpusFile(t, corpus, c.file)}, body)
		})
	}
}
