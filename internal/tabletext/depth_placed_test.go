package tabletext_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE READ'S DEPTH CAP COUNTS EVERY CONTAINER IT WALKS, PLACED OR SKIPPED
// (docs/SPEC-TABLES.md §16.2): the root is depth 0, a container at depth 128
// is the deepest read, and the 130th nested container refuses. A placed map's
// object, a union's object, a keyed object, an array and a flags array each
// count, as a table's object does. The engine once walked a map's entries and
// a union's arm at the depth of the object holding them, so a text nested one
// past the cap through a map (two through a union and a map) was read.
// Regression rows: rr-depth-placed-union-map-128/-129 and
// rr-depth-placed-keyed-map-128/-129 (docs/fix-ledger-1122.md).
func TestReadDepthCapCountsPlacedContainers(t *testing.T) {
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/maps"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Depth")
	if st == nil {
		t.Fatal("no table Depth")
	}
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	for _, tc := range []struct {
		name, form string
		// arrays is the count of nested arrays under "x" that puts the deepest
		// container at exactly depth 128
		arrays int
	}{
		// root 0, union 1, Squad 2, map 3, entry 4, x 5..128
		{"union then map", `{ "arm": { "squad": { "roster": { "7": { "count": 1, "x": %s } } } } }`, 124},
		// root 0, keyed 1, Squad 2, map 3, entry 4, x 5..128
		{"keyed then map", `{ "keyed": { "Alpha": { "roster": { "5": { "count": 1, "x": %s } } } } }`, 124},
		// root 0, Squad 1, map 2, entry 3, x 4..128
		{"map", `{ "one": { "roster": { "5": { "count": 1, "x": %s } } } }`, 125},
		// root 0, array 1, Squad 2, map 3, entry 4, x 5..128
		{"array then map", `{ "many": [ { "roster": { "5": { "count": 1, "x": %s } } } ] }`, 124},
		// root 0 and four skipped objects, x 5..128
		{"skipped", `{ "zz": { "a": { "b": { "c": { "d": %s } } } } }`, 124},
	} {
		for _, extra := range []int{0, 1} {
			inst := m.New(st)
			var r tabletext.Report
			ok := m.Read(inst, []byte(fmt.Sprintf(tc.form, nest(tc.arrays+extra))), &r)
			if want := extra == 0; ok != want || r.Refused == want {
				t.Errorf("%s, deepest container at depth %d: read %v (refused %v), want read %v", tc.name, 128+extra, ok, r.Refused, want)
			}
		}
	}
}
