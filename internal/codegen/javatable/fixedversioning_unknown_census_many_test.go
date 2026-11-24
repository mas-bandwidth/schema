package javatable

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root, the removed Item.drop in every element
// of every record. The handed-in entry is UNLAWFUL whatever the record count
// (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30): the load refuses
// `layoutMalformed` before any record lands, `unknown` stays zero (there is no
// census to count once per endpoint or once per record), and every row handed
// in, poisoned first, is back at its declared defaults (SPEC-TABLES §6.5), on
// every one of the three loads.
import "testing"

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	file := javaUnlawfulRequired(t, "many_unknown_census.bin")
	javaUnlawfulRun(t, "unknown_census", file, javaUnlawfulCensusPoison, javaUnlawfulCensusDefaults)
}
