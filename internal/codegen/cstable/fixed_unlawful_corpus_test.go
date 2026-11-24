package cstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// csUnlawfulAnswer is the C# leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): a poison that sets row
// back[k] of the NEW type to values that are not its declared defaults, and a
// check that is true when back[k] is at its declared defaults (lead = 1,
// trail = 2, every unlisted field its type's default).
type csUnlawfulAnswer struct {
	poison string
	check  string
}

var csUnlawfulAnswers = map[string]csUnlawfulAnswer{
	"unknown_census": {
		poison: `back[k].Lead = 99u; back[k].Trail = 88u; for (int i = 0; i < 4; ++i) { back[k].Items[i].A = 5 + i; }`,
		check:  `back[k].Lead == 1u && back[k].Trail == 2u && back[k].Items[0].A == 0 && back[k].Items[1].A == 0 && back[k].Items[2].A == 0 && back[k].Items[3].A == 0`,
	},
	"unknown_census_many": {
		poison: `back[k].Lead = 99u; back[k].Trail = 88u; for (int i = 0; i < 4; ++i) { back[k].Items[i].A = 5 + i; }`,
		check:  `back[k].Lead == 1u && back[k].Trail == 2u && back[k].Items[0].A == 0 && back[k].Items[1].A == 0 && back[k].Items[2].A == 0 && back[k].Items[3].A == 0`,
	},
	"kind_moved": {
		poison: `back[k].Lead = 99u; back[k].V = 5.5f; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].V == 0.0f && back[k].Trail == 2u`,
	},
	"arm_removed": {
		poison: `back[k].Lead = 99u; back[k].Pick.Type = PickType.Gamma; back[k].Pick.Gamma.P = 77; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].Pick.Type == PickType.None && back[k].Pick.Alpha.M == 0 && back[k].Pick.Gamma.P == 0 && back[k].Trail == 2u`,
	},
	"variant_removed": {
		poison: `back[k].Lead = 99u; back[k].Tier = Tier.Gold; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].Tier == Tier.None && back[k].Trail == 2u`,
	},
	"key_removed": {
		poison: `back[k].Lead = 99u; for (int i = 0; i < back[k].Slots.Slots.Length; ++i) { back[k].Slots.Slots[i] = 70 + i; } back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].Slots.Slots.Length == 2 && back[k].Slots.Slots[0] == 0 && back[k].Slots.Slots[1] == 0 && back[k].Trail == 2u`,
	},
	"array_shrunk": {
		poison: `back[k].Lead = 99u; for (int i = 0; i < back[k].Vals.Length; ++i) { back[k].Vals[i] = 70 + i; } back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].Vals.Length == 2 && back[k].Vals[0] == 0 && back[k].Vals[1] == 0 && back[k].Trail == 2u`,
	},
	"counted_shrunk": {
		poison: `back[k].Lead = 99u; back[k].ValsCount = 2; back[k].Vals[0] = 70; back[k].Vals[1] = 71; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].ValsCount == 0 && back[k].Vals.Length == 2 && back[k].Vals[0] == 0 && back[k].Vals[1] == 0 && back[k].Trail == 2u`,
	},
	"text_narrowed": {
		poison: `back[k].Lead = 99u; back[k].Text[0] = (byte)'z'; back[k].Text[1] = (byte)'z'; back[k].TextLength = 2; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].TextLength == 0 && back[k].Text.Length == 4 && back[k].Text[0] == 0 && back[k].Text[1] == 0 && back[k].Text[2] == 0 && back[k].Text[3] == 0 && back[k].Trail == 2u`,
	},
	"bytes_narrowed": {
		poison: `back[k].Lead = 99u; back[k].Blob[0] = 0x5A; back[k].BlobLength = 1; back[k].Trail = 88u;`,
		check:  `back[k].Lead == 1u && back[k].BlobLength == 0 && back[k].Blob.Length == 4 && back[k].Blob[0] == 0 && back[k].Blob[1] == 0 && back[k].Blob[2] == 0 && back[k].Blob[3] == 0 && back[k].Trail == 2u`,
	},
}

// TestCSharpFixedUnlawfulCorpus is the C# leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record, every counter zero,
// every poisoned row back at its declared defaults, on a first load and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30;
// SPEC-TABLES §6.5). The refusal's NAME, layout_malformed, is this leg's own
// and is pinned here by csUnlawfulBody; it is never compared across legs
// (ruling refusal-verdict-only).
func TestCSharpFixedUnlawfulCorpus(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")
	fixedunlawful.Run(t, "cstable", csUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a csUnlawfulAnswer) {
		t.Parallel()
		corpus := csFixedCorpusFile(t, c.File)
		body := strings.ReplaceAll(csUnlawfulBody(a.poison, a.check), "@TABLE@", c.Root)
		csRunOneVersionProbe(t, corpus, csVersionProbe{
			name:   c.Name + "/new_reads_old",
			reader: c.New,
			older:  []string{c.Old},
			file:   c.File,
			body:   body,
		})
	})
}
