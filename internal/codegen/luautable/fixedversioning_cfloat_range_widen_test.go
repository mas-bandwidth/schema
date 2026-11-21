package luautable_test

import "testing"

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md "Hostile rows";
// docs/rulings.json strict-only-read, which overturned schema#1164 statement
// B; docs/FIXED-FORM-ALGORITHM.md §4.6): a compressed float is held to the
// WRITER's range the lineage entry carries, like every ranged kind. Three
// files, every value from the manifest:
//
//   - old_cfloat_range_widen.bin: the old writer's 0.5 lands exactly, nothing
//     counted (the control: a writer range NARROWER than the reader's lands
//     what lies inside it);
//   - hostile_cfloat_range_widen.bin: aim forged to 1.5, outside the old
//     writer's [-1, 1] and inside the new reader's [-2, 2], refuses the whole
//     load out_of_range and lands nothing;
//   - past_cfloat_range_widen.bin: aim forged to 5.0, outside the reader's
//     range too, refuses the whole load out_of_range and lands nothing.
func TestLuauFixedVersioningCfloatRangeWiden(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	reader := luauVersionUnit(t, "VNEW_cfloat_range_widen")
	root := luauVersionRoot(t, reader)
	cases := []struct {
		file    string
		refused bool
	}{
		{"old_cfloat_range_widen.bin", false},
		{"hostile_cfloat_range_widen.bin", true},
		{"past_cfloat_range_widen.bin", true},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			if c.refused {
				body := `local report: any = {}
local values = LOADT("file", report)
check(values == nil and report.reason == "out_of_range", "a value past the writer's range refuses the whole load: " .. why(report))
check(report.widened == 0 and report.unknown == 0, "a refusal moved a counter: " .. why(report))
`
				luauVersionProbe(t, "VNEW_cfloat_range_widen", []string{"VOLD_cfloat_range_widen"}, 0,
					map[string][]byte{"file": luauCorpusFile(t, corpus, c.file)}, body)
				return
			}
			checks := luauManifestChecks(t, corpus, c.file, root)
			body := `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 1, "the file carries one record: " .. why(report))
if values == nil then return end
local v = values[1]
check(report.reason == nil, "not a refusal: " .. why(report))
check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0, "no other counter moved: " .. why(report))
check(num(v.Lead) == 1 and num(v.Trail) == 2, "the brackets moved")
check(f32bits(v.Aim) == 0x3F000000, "the old writer's 0.5 did not land exactly: " .. tostring(v.Aim))
` + checks + "\n"
			luauVersionProbe(t, "VNEW_cfloat_range_widen", []string{"VOLD_cfloat_range_widen"}, 0,
				map[string][]byte{"file": luauCorpusFile(t, corpus, c.file)}, body)
		})
	}
}
