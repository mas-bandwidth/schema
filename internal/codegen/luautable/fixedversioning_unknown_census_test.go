package luautable_test

import (
	"strconv"
	"testing"
)

// §5.8 row 11, `unknown_census`: the NEW build (Item.drop removed) reads
// old_unknown_census.bin through a HANDED-IN lineage entry — the pair is
// deliberately unlawful, §5.1 refuses the removal, so the probe hands the
// older layout to GenerateLineage directly. `unknown` is asserted EXACTLY 1:
// Item.drop is ONE field of ONE endpoint, and a leg that counts once per
// element of items[4] lands 4. A second read owes the same census.
func TestLuauFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	luauVersionProbe(t, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "old_unknown_census.bin")}, luauCensusBody(1))
}

// luauCensusBody reads the file twice and asserts the census once per peer
// and the landed `a` of each record's four items (10 + 10k + i).
func luauCensusBody(records int) string {
	return `local want = ` + strconv.Itoa(records) + `
for pass = 1, 2 do
	local report: any = {}
	local values = LOAD("file", report)
	check(values ~= nil and #values == want, "the newer reader did not read the older writer's records: " .. why(report))
	if values == nil then return end
	check(report.reason == nil and not report.malformed, "a clean NEW-READS-OLD is not a refusal: " .. why(report))
	check(report.kindMismatch == 0 and report.widened == 0 and report.clamped == 0 and report.duplicate == 0, "counters that must not move did: " .. why(report))
	check(report.unknown == 1, "read " .. pass .. ": unknown == " .. report.unknown .. ", want 1: Item.drop is ONE field of ONE endpoint, never one per element or per record")
	for r = 1, want do
		local v = values[r]
		check(num(v.Lead) == 1 and num(v.Trail) == 2, "record " .. r .. ": the bracketing fields moved")
		for i = 1, 4 do
			check(num(v.Items[i].A) == 10 * r + i - 1, "record " .. r .. " items[" .. i .. "].a == " .. tostring(v.Items[i].A))
		end
	end
end
`
}
