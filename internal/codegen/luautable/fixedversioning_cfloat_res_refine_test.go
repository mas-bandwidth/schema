package luautable_test

import "testing"

// cfloat_res_refine: the old writer quantized to 0.1 and the new reader's step
// is 0.01, a whole divisor, so every old value lands EXACTLY — its float32 bits
// are the manifest's — and nothing requantizes or clamps. The resolution rides
// in the definitions digest, so the two hashes differ and the read goes
// through the lineage (§5.2's 'Q' row).
func TestLuauFixedVersioningCfloatResRefine(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	reader := luauVersionUnit(t, "VNEW_cfloat_res_refine")
	root := luauVersionRoot(t, reader)
	body := `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and #values == 1, "the file carries one record: " .. why(report))
if values == nil then return end
local v = values[1]
check(report.reason == nil and not report.malformed, "not a refusal: " .. why(report))
check(report.clamped == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0 and report.duplicate == 0,
	"a refined resolution moves no counter: " .. why(report))
` + luauManifestChecks(t, corpus, "old_cfloat_res_refine.bin", root)
	luauVersionProbe(t, "VNEW_cfloat_res_refine", []string{"VOLD_cfloat_res_refine"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "old_cfloat_res_refine.bin")}, body)
}
