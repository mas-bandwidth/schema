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
//     range too, clamps to 2.0 and counts `clamped == 1` exactly, which is the
//     file that proves the bounds pass runs at all.
func TestLuauFixedVersioningCfloatRangeWiden(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	reader := luauVersionUnit(t, "VNEW_cfloat_range_widen")
	root := luauVersionRoot(t, reader)
	cases := []struct {
		file, oracle, clamped, extra string
	}{
		{"old_cfloat_range_widen.bin", "old_cfloat_range_widen.bin", "0", ""},
		{"hostile_cfloat_range_widen.bin", "", "0", `check(f32bits(v.Aim) == 0x3FC00000, "the hostile 1.5 inside the reader's range did not land whole: " .. tostring(v.Aim))`},
		{"past_cfloat_range_widen.bin", "", "1", `check(v.Aim == 2.0, "the past 5.0 did not clamp to the reader's 2.0: " .. tostring(v.Aim))`},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			checks := c.extra
			if c.oracle != "" {
				checks = luauManifestChecks(t, corpus, c.oracle, root)
			}
			// the tolerant read by its name: the past file's clamp is the
			// strict read's refusal (§6.5)
			body := `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 1, "the file carries one record: " .. why(report))
if values == nil then return end
local v = values[1]
check(report.reason == nil and not report.malformed, "not a refusal: " .. why(report))
check(report.clamped == ` + c.clamped + `, "clamped " .. report.clamped .. ", want exactly ` + c.clamped + `")
check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0, "no other counter moved: " .. why(report))
check(num(v.Lead) == 1 and num(v.Trail) == 2, "the brackets moved")
` + checks + "\n"
			luauVersionProbe(t, "VNEW_cfloat_range_widen", []string{"VOLD_cfloat_range_widen"}, 0,
				map[string][]byte{"file": luauCorpusFile(t, corpus, c.file)}, body)
		})
	}
}
