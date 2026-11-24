package lobstertable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
)

// lobsterUnlawfulAnswer is the Lobster leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type lobsterUnlawfulAnswer struct {
	// poison sets row `p` of the NEW type to values that are not its declared
	// defaults, so a load that leaves a row alone is seen.
	poison string
	// check is true when row `p` holds its declared defaults (read off the
	// VNEW schema: lead = 1, trail = 2, every unlisted field its type's
	// default). The probe also holds every row's fixed save to the save of a
	// fresh New<Root>(), so a field the check does not name is held too.
	check string
}

var lobsterUnlawfulAnswers = map[string]lobsterUnlawfulAnswer{
	"unknown_census": {
		poison: `p.Lead = 99; p.Trail = 88; for(4) j: p.Items[j].A = 5 + j`,
		check:  `p.Lead == 1 and p.Trail == 2 and p.Items[0].A == 0 and p.Items[3].A == 0`,
	},
	"unknown_census_many": {
		poison: `p.Lead = 99; p.Trail = 88; for(4) j: p.Items[j].A = 5 + j`,
		check:  `p.Lead == 1 and p.Trail == 2 and p.Items[0].A == 0 and p.Items[3].A == 0`,
	},
	"kind_moved": {
		poison: `p.Lead = 99; p.V = 5.5; p.Trail = 88`,
		check:  `p.Lead == 1 and p.V == 0.0 and p.Trail == 2`,
	},
	"arm_removed": {
		poison: `p.Lead = 99; p.Pick.Type = PKG.PickTypeGamma; p.Pick.Gamma.P = 77; p.Trail = 88`,
		check:  `p.Lead == 1 and p.Pick.Type == PKG.PickTypeNone and p.Pick.Gamma.P == 0 and p.Trail == 2`,
	},
	"variant_removed": {
		poison: `p.Lead = 99; p.Tier = PKG.TierGold; p.Trail = 88`,
		check:  `p.Lead == 1 and p.Tier == PKG.TierNone and p.Trail == 2`,
	},
	"key_removed": {
		poison: `p.Lead = 99; for(2) j: p.Slots[j] = 70 + j; p.Trail = 88`,
		check:  `p.Lead == 1 and p.Slots[0] == 0 and p.Slots[1] == 0 and p.Trail == 2`,
	},
	"array_shrunk": {
		poison: `p.Lead = 99; for(2) j: p.Vals[j] = 70 + j; p.Trail = 88`,
		check:  `p.Lead == 1 and p.Vals[0] == 0 and p.Vals[1] == 0 and p.Trail == 2`,
	},
	"counted_shrunk": {
		poison: `p.Lead = 99; p.ValsCount = 2; p.Vals[0] = 70; p.Vals[1] = 71; p.Trail = 88`,
		check:  `p.Lead == 1 and p.ValsCount == 0 and p.Vals[0] == 0 and p.Vals[1] == 0 and p.Trail == 2`,
	},
	"text_narrowed": {
		poison: `p.Lead = 99; p.Text = write_int8_le(write_int8_le(p.Text, 0, 122), 1, 122); p.TextLength = 2; p.Trail = 88`,
		check:  `p.Lead == 1 and p.TextLength == 0 and p.Text == schema_rt.zeros(4) and p.Trail == 2`,
	},
	"bytes_narrowed": {
		poison: `p.Lead = 99; p.Blob = write_int8_le(p.Blob, 0, 0x5A); p.BlobLength = 1; p.Trail = 88`,
		check:  `p.Lead == 1 and p.BlobLength == 0 and p.Blob == schema_rt.zeros(4) and p.Trail == 2`,
	},
}

// TestLobsterFixedUnlawfulCorpus is the Lobster leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record (-1), every counter
// zero (the report's counters are poisoned first), every poisoned row back at
// its declared defaults, on a first load and a second
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30; SPEC-TABLES
// §6.5). The refusal's NAME is pinned here as this leg's own assertion,
// layout_malformed, and never compared across legs (refusal-verdict-only).
// Lobster has no debug/release split: the refusal is a plain refusal on
// every run.
func TestLobsterFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	corpus := lobsterFixedCorpus(t)
	fixedunlawful.Run(t, "lobstertable", lobsterUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a lobsterUnlawfulAnswer) {
		t.Parallel()
		older := lobsterReadSchema(t, c.Old)
		newer := lobsterReadSchema(t, c.New)
		pkg := lobsterUnitOf(t, newer).Package
		data, err := os.ReadFile(filepath.Join(corpus, c.File))
		if err != nil {
			t.Fatal(err)
		}
		poison := strings.ReplaceAll(strings.ReplaceAll(a.poison, "PKG.", pkg+"."), "; ", "\n            ")
		check := strings.ReplaceAll(a.check, "PKG.", pkg+".")
		body := fmt.Sprintf(`
    let _f_n, fresh, _f_why = %[1]s.%[2]sFixedSaveChecked([%[1]s.New%[2]s()], schema_rt.zeros(%[1]s.%[2]sFixedMeasure(1)))
    let plan = %[1]s.make_plan(4096)
    let back:[%[1]s.%[2]s] = []
    for(8): push(back, %[1]s.New%[2]s())
    for(2) attempt:
        for(back) p:
            %[1]s.%[2]sReset(p)
            %[3]s
        let r = schema_rt.TableReport { unknown: 9, widened: 9, kind_mismatch: 9, clamped: 9, duplicate: 9, retained: 9, retain_lost: 9, reason: "stale" }
        let n = %[1]s.%[2]sFixedLoad(back, read_in("in.bin"), plan, r)
        assert_eq(n, -1, "%[4]s attempt " + string(attempt) + ": the UNLAWFUL entry loaded")
        assert_true(r.reason == "layout_malformed", "%[4]s attempt " + string(attempt) + ": refused, but not layout_malformed: " + r.reason)
        assert_true(r.unknown == 0 and r.widened == 0 and r.kind_mismatch == 0 and r.clamped == 0 and r.duplicate == 0 and r.retained == 0 and r.retain_lost == 0, "%[4]s attempt " + string(attempt) + ": a refusal moves no counter: unknown=" + string(r.unknown) + " widened=" + string(r.widened) + " kind_mismatch=" + string(r.kind_mismatch))
        for(back) p, k:
            assert_true(%[5]s, "%[4]s attempt " + string(attempt) + ": row " + string(k) + " is not at its declared defaults")
            let _s_n, saved, _s_why = %[1]s.%[2]sFixedSaveChecked([p], schema_rt.zeros(%[1]s.%[2]sFixedMeasure(1)))
            assert_true(saved == fresh, "%[4]s attempt " + string(attempt) + ": row " + string(k) + " does not save as a fresh row")
    print "PASS"
`, pkg, c.Root, poison, c.Name, check)
		if out := runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"in.bin": data}); strings.TrimSpace(out) != "PASS" {
			t.Fatalf("%s (%s): %q", c.Name, c.Change, out)
		}
	})
}
