package cstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"testing"
)

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root. The handed-in entry is UNLAWFUL
// whatever the record count (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #30): the
// load refuses `layout_malformed` before any record lands, `unknown` stays zero
// (there is no census to count once per endpoint or once per record), and every
// row handed in, poisoned first, is back at its declared defaults
// (SPEC-TABLES §6.5) on both of two loads.
func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")
	corpus := csFixedCorpusFile(t, "many_unknown_census.bin")
	csRunOneVersionProbe(t, corpus, csVersionProbe{
		name:   "unknown_census_many/new_reads_old",
		reader: "VNEW_unknown_census",
		older:  []string{"VOLD_unknown_census"},
		file:   "many_unknown_census.bin",
		body: csUnlawfulBody(
			`back[k].Lead = 99u; back[k].Trail = 88u; for (int i = 0; i < 4; ++i) { back[k].Items[i].A = 5 + i; }`,
			`back[k].Lead == 1u && back[k].Trail == 2u && back[k].Items[0].A == 0 && back[k].Items[1].A == 0 && back[k].Items[2].A == 0 && back[k].Items[3].A == 0`),
	})
}
