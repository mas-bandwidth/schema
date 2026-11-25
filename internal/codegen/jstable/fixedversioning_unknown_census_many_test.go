package jstable

// The three-record twin of unknown_census: many_unknown_census.bin carries
// THREE records of the same Census root, and the NEW reader removes Item.drop.
// The handed-in entry is UNLAWFUL whatever the record count (§5.4, §5.9 #30):
// the load refuses `layout_malformed` before any record lands, `unknown` stays
// zero (there is no census to count once per endpoint or once per record), and
// every row handed in is back at its declared defaults (SPEC-TABLES §6.5), on a
// first load and a second.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_unknown_census")
	older := jsReadSchema(t, "VOLD_unknown_census")
	table := jsFixedRootName(t, older)
	src := jsUnlawfulProbe(filepath.Join(corpus, "many_unknown_census.bin"), "unknown_census_many",
		`back[k].lead = 99; back[k].trail = 88; for (let i = 0; i < 4; i++) { back[k].items[i].a = 5 + i; }`,
		`back[k].lead === 1 && back[k].trail === 2 && back[k].items.every((it) => it.a === 0)`)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("NEW-READS-OLD unknown_census many: %v\n%s", err, out)
	}
}
