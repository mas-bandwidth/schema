package cpptable

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root. The handed-in entry is UNLAWFUL
// whatever the record count (§5.4, §5.9 #30): the load refuses
// `layout_malformed` before any record lands, `unknown` stays zero (there is no
// census to count once per endpoint or once per record), and every row handed
// in is back at its declared defaults (SPEC-TABLES §6.5).

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := cppFixedCorpus(t)
	body := cppUnlawfulBody("unknown_census_many", "Census",
		`back[k].lead = 99u; back[k].trail = 88u; for ( int i = 0; i < 4; ++i ) { back[k].items[i].a = 5 + i; }`,
		`back[k].lead == 1u && back[k].trail == 2u && back[k].items[0].a == 0 && back[k].items[3].a == 0`)
	out, err := cppRunVersionProbe(t, "unknown_census",
		filepath.Join(corpus, "many_unknown_census.bin"), body, "")
	if err != nil {
		t.Fatalf("unknown_census_many: %v\n%s", err, out)
	}
}
