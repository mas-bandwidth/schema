package rusttable

import "testing"

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root. The handed-in entry is UNLAWFUL
// whatever the record count (§5.4, §5.9 #30): the load refuses
// layout_malformed before any record lands, unknown stays zero (there is no
// census to count once per endpoint or once per record), and every row handed
// in — poisoned first — is back at its declared defaults (SPEC-TABLES §6.5),
// on two loads.

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	rustUnlawfulProbe(t, "unknown_census_many", "many_unknown_census.bin",
		"VNEW_unknown_census", "VOLD_unknown_census", censusPoison, censusCheck)
}
