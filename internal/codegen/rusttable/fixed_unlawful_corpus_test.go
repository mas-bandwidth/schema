package rusttable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// rustUnlawfulAnswer is the Rust leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): a poison that sets one row
// `v` of the NEW type to values that are not its declared defaults, and a Rust
// boolean over `v` that holds only when the row is back at its declared
// defaults (lead = 1, trail = 2, every unlisted field its type's zero).
type rustUnlawfulAnswer struct {
	poison string
	check  string
}

var rustUnlawfulAnswers = map[string]rustUnlawfulAnswer{
	"unknown_census":      {poison: censusPoison, check: censusCheck},
	"unknown_census_many": {poison: censusPoison, check: censusCheck},
	"kind_moved": {
		poison: `v.lead = 99; v.v = 5.5; v.trail = 88;`,
		check:  `v.lead == 1 && v.v == 0.0 && v.trail == 2`,
	},
	"arm_removed": {
		poison: `v.lead = 99; v.pick.set_gamma(GammaRow { p: 77 }); v.trail = 88;`,
		check:  `v.lead == 1 && v.pick.tag() == 0 && unsafe { v.pick.arms.gamma.p } == 0 && v.trail == 2`,
	},
	"variant_removed": {
		poison: `v.lead = 99; v.tier = Tier::GOLD.0; v.trail = 88;`,
		check:  `v.lead == 1 && v.tier == 0 && v.trail == 2`,
	},
	"key_removed": {
		poison: `v.lead = 99; for j in 0..v.slots.len() { v.slots[j] = 70 + j as i32; } v.trail = 88;`,
		check:  `v.lead == 1 && v.slots.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"array_shrunk": {
		poison: `v.lead = 99; for j in 0..v.vals.len() { v.vals[j] = 70 + j as i32; } v.trail = 88;`,
		check:  `v.lead == 1 && v.vals.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"counted_shrunk": {
		poison: `v.lead = 99; v.vals_count = 2; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		check:  `v.lead == 1 && v.vals_count == 0 && v.vals.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"text_narrowed": {
		poison: `v.lead = 99; v.text[0] = b'z'; v.text[1] = b'z'; v.text_length = 2; v.trail = 88;`,
		check:  `v.lead == 1 && v.text_length == 0 && v.text.iter().all(|&b| b == 0) && v.trail == 2`,
	},
	"bytes_narrowed": {
		poison: `v.lead = 99; v.blob[0] = 0x5A; v.blob_length = 1; v.trail = 88;`,
		check:  `v.lead == 1 && v.blob_length == 0 && v.blob.iter().all(|&b| b == 0) && v.trail == 2`,
	},
}

// TestRustFixedUnlawfulCorpus is the Rust leg's driver for the shared
// unlawful-lineage corpus. Per case, the NEW build handed the OLD layout as a
// lineage entry refuses the OLD file before any record, moves no counter, and
// leaves every poisoned row at its declared defaults, on a first load and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30;
// SPEC-TABLES §6.5). The shared harness compares verdicts only; the Rust
// refusal's NAME, layout_malformed, is pinned here by rustUnlawfulProbe and is
// never compared across legs (refusal-verdict-only).
//
// The cases run one after another: every probe builds into the one shared
// cargo target dir (build/rust-versioning-target), whose lock would serialize
// them anyway.
func TestRustFixedUnlawfulCorpus(t *testing.T) {
	fixedunlawful.Run(t, "rusttable", rustUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a rustUnlawfulAnswer) {
		rustUnlawfulProbe(t, c.Name, c.File, c.New, c.Old, a.poison, a.check)
	})
}
