package fixedproof

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// LineageLoader is the one verb CheckGrownFixedBound needs of a leg: its
// EvoRunner's LoadLineage.
type LineageLoader interface {
	LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]LineageEntry, wireBytes []byte, cap int) (count int, reason string, fields map[string]string, resaved []byte, counters LoadCounters)
}

// GrownFixedBoundRows are the versioning rows whose fixed bounds grow
// (docs/FIXED-FORM-VERSIONING-TESTS.md, array_fixed_grow: a record array
// and two scalar arrays, [4] grown to [8]; F-092): each wants the flat fields the newer
// reader must land from the C++ reference's older record, the grown slots at
// their born element.
var GrownFixedBoundRows = []struct {
	Row  string
	Want map[string]string
}{
	{"array_fixed_grow", map[string]string{
		"r0.vals[0].x": "-500", "r0.vals[3].y": "-603",
		"r0.vals[4].x": "7", "r0.vals[4].y": "9", "r0.vals[7].x": "7", "r0.vals[7].y": "9",
		"r0.ints[0]": "-800", "r0.ints[3]": "-803", "r0.ints[4]": "0", "r0.ints[7]": "0",
		"r0.octet[0]": "200", "r0.octet[3]": "203", "r0.octet[4]": "0", "r0.octet[7]": "0",
	}},
}

// CheckGrownFixedBound reads each grown-bound row's older reference record
// (build/fixedform-corpus/old_<row>.bin) under the newer unit handed the older
// layout, through leg, and fails on any wanted field the read did not land.
func CheckGrownFixedBound(t *testing.T, repo, corpus string, leg LineageLoader) {
	t.Helper()
	for _, r := range GrownFixedBoundRows {
		older := LoadRepoUnit(t, repo, "test/tables/VOLD_"+r.Row+".schema")
		newer := LoadRepoUnit(t, repo, "test/tables/VNEW_"+r.Row+".schema")
		root := RootOf(t, older)
		entry, ok := LineageEntryOf(older, root)
		if !ok {
			t.Fatalf("%s: no lineage entry for %s", r.Row, root)
		}
		data, err := os.ReadFile(filepath.Join(corpus, "old_"+r.Row+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		n, reason, fields, _, _ := leg.LoadLineage(t, newer, root, map[string][]LineageEntry{root: {entry}}, data, -1)
		if n != 1 {
			t.Errorf("%s: the newer reader read %d records (%s), want 1", r.Row, n, reason)
			continue
		}
		for k, want := range r.Want {
			if got, ok := fields[k]; !ok || got != want {
				t.Errorf("%s: %s = %q (present %v), want %q", r.Row, k, got, ok, want)
			}
		}
		if t.Failed() {
			t.Logf("%s: the read landed %s", r.Row, fmt.Sprint(fields))
		}
	}
}
