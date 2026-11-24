package luautable_test

import (
	"fmt"
	"testing"
)

// §5.8 row 11, `unknown_census`: the NEW build (Item.drop removed) is handed
// the OLD writer's layout as a lineage entry (GenerateLineage directly). §5.1
// refuses a removal, so a lawful older writer never carries a field the reader
// lacks: the entry is UNLAWFUL, no plan is laid for it and the load refuses
// `layout_malformed` with every counter at zero — `unknown` included, because
// there is no compile census (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4,
// §5.9 #30). After the refusal every row handed in is back at its declared
// defaults (SPEC-TABLES §6.5), and a second load refuses the same way.
func TestLuauFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	luauVersionProbe(t, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "old_unknown_census.bin")}, luauCensusBody())
}

// luauCensusBody is the unlawful-entry body for the Census root: poisoned
// rows, lead=1 trail=2 and every items[i].a=0 after each refusal.
func luauCensusBody() string {
	return luauUnlawfulBody("Census",
		`r.Lead, r.Trail = 99, 88; for i = 1, 4 do r.Items[i].A = 5 + i end`,
		`num(r.Lead) == 1 and num(r.Trail) == 2 and num(r.Items[1].A) == 0 and num(r.Items[4].A) == 0`)
}

// luauUnlawfulBody is the probe body every unlawful-entry row shares: two
// loads, each through the plain strict read and through readInto over rows
// POISONED first, and each refuses `layout_malformed` by name, hands back
// nothing, moves no counter (the report's own counters are poisoned too) and
// leaves every row handed in at its declared defaults. `poison` sets non-default
// values on the row `r`; `check` is true when `r` is at its declared defaults.
func luauUnlawfulBody(root, poison, check string) string {
	return fmt.Sprintf(`local into = {}
for k = 1, 8 do into[k] = T.New%[1]s() end
for pass = 1, 2 do
	local report: any = { unknown = 9, kindMismatch = 9, widened = 9 }
	check(LOAD("file", report) == nil, "pass " .. pass .. ": the unlawful entry LOADED: " .. why(report))
	check(report.reason == "layout_malformed", "pass " .. pass .. ": not refused layout_malformed: " .. why(report))
	check(report.unknown == 0 and report.kindMismatch == 0 and report.widened == 0, "pass " .. pass .. ": a refusal moved a counter: " .. why(report))
	for k = 1, 8 do
		local r = into[k]
		%[2]s
	end
	local rep: any = { unknown = 9, kindMismatch = 9, widened = 9 }
	check(F.readInto(T.%[1]sDescriptor, fixed, inputs.file, rep, into) == nil, "pass " .. pass .. ": readInto handed back rows: " .. why(rep))
	check(rep.reason == "layout_malformed", "pass " .. pass .. ": readInto not refused layout_malformed: " .. why(rep))
	check(rep.unknown == 0 and rep.kindMismatch == 0 and rep.widened == 0, "pass " .. pass .. ": readInto moved a counter: " .. why(rep))
	for k = 1, 8 do
		local r = into[k]
		check(%[3]s, "pass " .. pass .. ": row " .. k .. " is not at its declared defaults")
	end
end
`, root, poison, check)
}
