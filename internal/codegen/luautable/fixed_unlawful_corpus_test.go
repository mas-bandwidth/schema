package luautable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// luauUnlawfulAnswer is the Luau leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): Luau that poisons the row
// `r` of the NEW type with values that are not its declared defaults, and a
// Luau condition that is true when `r` is back at its declared defaults (read
// off the VNEW schema: lead = 1, trail = 2, every unlisted field its type's
// default).
type luauUnlawfulAnswer struct {
	poison string
	check  string
}

var luauUnlawfulAnswers = map[string]luauUnlawfulAnswer{
	"unknown_census": {
		poison: `r.Lead, r.Trail = 99, 88; for i = 1, 4 do r.Items[i].A = 5 + i end`,
		check:  `num(r.Lead) == 1 and num(r.Trail) == 2 and num(r.Items[1].A) == 0 and num(r.Items[2].A) == 0 and num(r.Items[3].A) == 0 and num(r.Items[4].A) == 0`,
	},
	"unknown_census_many": {
		poison: `r.Lead, r.Trail = 99, 88; for i = 1, 4 do r.Items[i].A = 5 + i end`,
		check:  `num(r.Lead) == 1 and num(r.Trail) == 2 and num(r.Items[1].A) == 0 and num(r.Items[2].A) == 0 and num(r.Items[3].A) == 0 and num(r.Items[4].A) == 0`,
	},
	"kind_moved": {
		poison: `r.Lead, r.V, r.Trail = 99, 5.5, 88`,
		check:  `num(r.Lead) == 1 and num(r.V) == 0 and num(r.Trail) == 2`,
	},
	"arm_removed": {
		poison: `r.Lead, r.Trail = 99, 88; r.Pick.Type = 2; r.Pick.Gamma.P = 77; r.Pick.Alpha.M = 66`,
		check:  `num(r.Lead) == 1 and num(r.Pick.Type) == 0 and num(r.Pick.Alpha.M) == 0 and num(r.Pick.Gamma.P) == 0 and num(r.Trail) == 2`,
	},
	"variant_removed": {
		poison: `r.Lead, r.Tier, r.Trail = 99, 1, 88`,
		check:  `num(r.Lead) == 1 and num(r.Tier) == 0 and num(r.Trail) == 2`,
	},
	"key_removed": {
		poison: `r.Lead, r.Trail = 99, 88; r.Slots[1], r.Slots[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.Slots[1]) == 0 and num(r.Slots[2]) == 0 and num(r.Trail) == 2`,
	},
	"array_shrunk": {
		poison: `r.Lead, r.Trail = 99, 88; r.Vals[1], r.Vals[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.Vals[1]) == 0 and num(r.Vals[2]) == 0 and num(r.Trail) == 2`,
	},
	"counted_shrunk": {
		poison: `r.Lead, r.Trail = 99, 88; r.ValsCount = 2; r.Vals[1], r.Vals[2] = 70, 71`,
		check:  `num(r.Lead) == 1 and num(r.ValsCount) == 0 and num(r.Vals[1]) == 0 and num(r.Vals[2]) == 0 and num(r.Trail) == 2`,
	},
	"text_narrowed": {
		poison: `r.Lead, r.Trail = 99, 88; buffer.writestring(r.Text, 0, "zz"); r.TextLength = 2`,
		check:  `num(r.Lead) == 1 and num(r.TextLength) == 0 and buffer.len(r.Text) == 4 and buffer.readu32(r.Text, 0) == 0 and num(r.Trail) == 2`,
	},
	"bytes_narrowed": {
		poison: `r.Lead, r.Trail = 99, 88; buffer.writeu8(r.Blob, 0, 0x5A); r.BlobLength = 1`,
		check:  `num(r.Lead) == 1 and num(r.BlobLength) == 0 and buffer.len(r.Blob) == 4 and buffer.readu32(r.Blob, 0) == 0 and num(r.Trail) == 2`,
	},
}

// TestLuauFixedUnlawfulCorpus is the Luau leg's driver for the shared
// unlawful-lineage corpus. Per case the NEW build, handed the OLD layout as a
// lineage entry (GenerateLineage), reads the OLD writer's file and refuses it
// before any record lands, every counter zero, every poisoned row handed in
// back at its declared defaults, on a first load and a second, through both the
// plain strict read and readInto (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and
// EMIT, §5.4, §5.9 #30; SPEC-TABLES §6.5). The refusal's NAME is this leg's
// own and is pinned here, in luauUnlawfulBody: layout_malformed
// (refusal-verdict-only: the shared harness compares verdicts, never names).
func TestLuauFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	fixedunlawful.Run(t, "luautable", luauUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a luauUnlawfulAnswer) {
		t.Parallel()
		luauVersionProbe(t, c.New, []string{c.Old}, 0,
			map[string][]byte{"file": luauCorpusFile(t, corpus, c.File)},
			luauUnlawfulBody(c.Root, a.poison, a.check))
	})
}
