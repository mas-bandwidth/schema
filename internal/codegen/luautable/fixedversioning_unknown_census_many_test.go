package luautable_test

import "testing"

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root. The handed-in entry is UNLAWFUL
// whatever the record count (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30): the
// load refuses `layout_malformed` before any record lands, `unknown` stays zero
// (there is no census to count once per endpoint or once per record), and every
// row handed in is back at its declared defaults (SPEC-TABLES §6.5), twice.
func TestLuauFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	luauVersionProbe(t, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "many_unknown_census.bin")}, luauCensusBody())
}
