package javatable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// javaUnlawfulAnswer is the java leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): Java statements on `v`, a
// row of the NEW root, that set it to values that are not its declared
// defaults, and a Java boolean on `v` that holds exactly when the row is at
// its declared defaults (lead 1, trail 2, every unlisted field its type's
// default; read off the VNEW_ schema).
type javaUnlawfulAnswer struct {
	poison   string
	defaults string
}

var javaUnlawfulAnswers = map[string]javaUnlawfulAnswer{
	"unknown_census":      {javaUnlawfulCensusPoison, javaUnlawfulCensusDefaults},
	"unknown_census_many": {javaUnlawfulCensusPoison, javaUnlawfulCensusDefaults},
	"kind_moved": {
		poison:   `v.lead = 99; v.v = 5.5f; v.trail = 88;`,
		defaults: `v.lead == 1 && v.v == 0.0f && v.trail == 2`,
	},
	"arm_removed": {
		// gamma is the arm the file takes and the reader keeps.
		poison:   `v.lead = 99; v.pick.type = 2; v.pick.gamma.p = 77; v.trail = 88;`,
		defaults: `v.lead == 1 && v.pick.type == 0 && v.pick.alpha.m == 0 && v.pick.gamma.p == 0 && v.trail == 2`,
	},
	"variant_removed": {
		// Gold, the variant the file holds and the reader keeps, at a new id.
		poison:   `v.lead = 99; v.tier = (byte) 2; v.trail = 88;`,
		defaults: `v.lead == 1 && v.tier == 0 && v.trail == 2`,
	},
	"key_removed": {
		poison:   `v.lead = 99; v.slots[0] = 70; v.slots[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.slots[0] == 0 && v.slots[1] == 0 && v.trail == 2`,
	},
	"array_shrunk": {
		poison:   `v.lead = 99; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.vals[0] == 0 && v.vals[1] == 0 && v.trail == 2`,
	},
	"counted_shrunk": {
		poison:   `v.lead = 99; v.valsCount = 2; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		defaults: `v.lead == 1 && v.valsCount == 0 && v.vals[0] == 0 && v.vals[1] == 0 && v.trail == 2`,
	},
	"text_narrowed": {
		poison:   `v.lead = 99; v.text[0] = (byte) 'z'; v.text[1] = (byte) 'z'; v.textLength = 2; v.trail = 88;`,
		defaults: `v.lead == 1 && v.textLength == 0 && java.util.Arrays.equals(v.text, new byte[4]) && v.trail == 2`,
	},
	"bytes_narrowed": {
		poison:   `v.lead = 99; v.blob[0] = (byte) 0x5A; v.blobLength = 1; v.trail = 88;`,
		defaults: `v.lead == 1 && v.blobLength == 0 && java.util.Arrays.equals(v.blob, new byte[4]) && v.trail == 2`,
	},
}

// TestJavaFixedUnlawfulCorpus is the java leg's driver for the shared
// unlawful-lineage corpus. Per case the NEW build, handed the OLD layout as a
// lineage entry, loads the OLD file three times (a fresh report, a second
// fresh one, the first handed back): each refuses before any record (-1),
// every counter zero, every poisoned row back at its declared defaults
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4; SPEC-TABLES §6.5). The
// java leg's own refusal name, layoutMalformed (not damaged), is pinned by
// javaUnlawfulRunPair beside the verdict and is never compared across legs
// (refusal-verdict-only).
func TestJavaFixedUnlawfulCorpus(t *testing.T) {
	fixedunlawful.Run(t, "javatable", javaUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a javaUnlawfulAnswer) {
		t.Parallel()
		file := javaUnlawfulRequired(t, c.File)
		javaUnlawfulRunPair(t, c.Name, c.Old, c.New, file, a.poison, a.defaults)
	})
}
