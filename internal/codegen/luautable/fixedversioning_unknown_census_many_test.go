package luautable_test

import "testing"

// docs/FIXED-FORM-ALGORITHM.md §5.4: the compile census lands once per
// endpoint AND never per record. With one record the two are the same number,
// so this row reads many_unknown_census.bin, THREE records of the same Census
// root, and asserts `unknown == 1` exactly: a per-record count lands 3.
func TestLuauFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	luauVersionProbe(t, "VNEW_unknown_census", []string{"VOLD_unknown_census"}, 0,
		map[string][]byte{"file": luauCorpusFile(t, corpus, "many_unknown_census.bin")}, luauCensusBody(3))
}
