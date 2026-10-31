package luautable_test

import "testing"

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md,
// schema#1157, RULED LAWFUL AS IS): after a read that RETURNS, an arm the
// landed tag did not select is UNDEFINED. A union read DEFINES the tag and the
// SELECTED arm and nothing else, so this probe asserts exactly those, through
// both plans, and does NOT compare pick.beta or pick.gamma. That this leg
// happens to hold each unselected arm at its constructor's default is an
// observation and not a guarantee. (Named *_arm_row_test.go: a trailing _arm
// before _test.go is a GOARCH build constraint that silently skips the file.)
func TestLuauFixedVersioningUnionUnselectedArm(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	inputs := map[string][]byte{"file": luauCorpusFile(t, corpus, "old_union_append.bin")}
	body := `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and #values == 1, "the file carries one record: " .. why(report))
if values == nil then return end
check(values[1].Pick.Type == 1, "pick.type is the writer's alpha arm: " .. tostring(values[1].Pick.Type))
check(num(values[1].Pick.Alpha.M) == 7, "pick.alpha.m is the writer's 7")
check(num(values[1].Seq) == 15, "seq is the writer's 15")
check(report.widened == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.clamped == 0 and report.duplicate == 0,
	"a clean read moved a counter: " .. why(report))
check(report.reason == nil and not report.malformed, "a clean read is not a refusal: " .. why(report))
`
	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_union_append", []string{"VOLD_union_append"}, 0, inputs, body)
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VOLD_union_append", nil, 0, inputs, body)
	})
}
