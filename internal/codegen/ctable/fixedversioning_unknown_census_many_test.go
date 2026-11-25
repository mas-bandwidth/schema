package ctable

// The many-record half of §5.8 row 11: 'many_unknown_census.bin' carries THREE
// Census records under the same UNLAWFUL lineage entry (Item.drop removed). A
// leg that still counted the removed field once per record would land 3, and
// one that counted per endpoint would land 1; the law is that the entry lays
// no plan at all, so the load refuses `layout_malformed` with `unknown` at 0,
// every one of the eight rows handed in back at its declared defaults, twice
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30; SPEC-TABLES §6.5).

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_unknown_census")
	older := cReadSchema(t, "VOLD_unknown_census")
	body := cUnlawfulBody("unknown_census_many", "census_fixed_load", cCensusPoison, cCensusCheck)
	out, err := cRunVersionProbe(t, newer, []string{older}, 0,
		filepath.Join(corpus, "many_unknown_census.bin"), body, cUnlawfulPoison(cCensusPoison))
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
