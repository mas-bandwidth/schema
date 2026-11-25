package main

import (
	"slices"
	"testing"
)

// TestCaseFilesNameWhatACaseOwesByName holds caseFiles to the files a case
// owes that no field of its line names: an announce or read-into golden, a
// retain-refusal's two saves and a message batch's wire, each found by the
// case's name, and the announcement wire and schema of a connection a line
// names by its key. A row that leaves one out does not index it, and the
// extract counts the file as unmoved.
func TestCaseFilesNameWhatACaseOwesByName(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := caseFiles(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, file string }{
		{"ann_4096_write", "testdata/conformance/tables/announce/ann_4096_write.bin"},
		{"ri_fixed", "testdata/conformance/tables/read-into/ri_fixed.txt"},
		{"retain_refusal_body", "testdata/conformance/tables/retain-refusal/retain_refusal_body.before.bin"},
		{"retain_refusal_body", "testdata/conformance/tables/retain-refusal/retain_refusal_body.after.bin"},
		{"batch_cut_in_second", "testdata/conformance/tables/message-batch/batch_cut_in_second.bin"},
		{"smref_fixed_long", "testdata/conformance/tables/wire/surfaces_conn.bin"},
		{"smref_fixed_long", "test/tables/surfaces/Surfaces.schema"},
		{"widenbound_msg", "testdata/conformance/tables/wire/widenbound_conn.bin"},
	} {
		if !slices.Contains(files[c.name], c.file) {
			t.Errorf("case %s: its files %v leave out %s", c.name, files[c.name], c.file)
		}
	}
}
