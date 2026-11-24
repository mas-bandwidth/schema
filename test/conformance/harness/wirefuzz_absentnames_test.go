package main

import (
	"strings"
	"testing"
)

// The absent roots are named in the split, each with its seed count, in name
// order; a leg that answers every root leaves the list empty. Planting the
// old count-only split (names always "") turns every named case red.
func TestWireFuzzAbsentRootsAreNamed(t *testing.T) {
	roots := []*wireRoot{
		{unit: "tblvaluedefaults", root: "FixedDefaults"},
		{unit: "shapes", root: "Bounds"},
		{unit: "packetunions", root: "ShapeMsg"},
	}
	var seeds []*wireSeed
	seedRoot := map[*wireSeed]int{}
	for ri, n := range []int{2, 3, 2} {
		for i := 0; i < n; i++ {
			s := &wireSeed{}
			seeds = append(seeds, s)
			seedRoot[s] = ri
		}
	}
	for _, tc := range []struct {
		name   string
		known  []bool
		live   int
		absent int
		names  string
	}{
		{"every root answered", []bool{true, true, true}, 7, 0, ""},
		{"one root absent", []bool{true, false, true}, 4, 3, "shapes.Bounds (3 seeds)"},
		{"two roots absent, in name order", []bool{false, true, false}, 3, 4,
			"packetunions.ShapeMsg (2 seeds), tblvaluedefaults.FixedDefaults (2 seeds)"},
		{"every root absent", []bool{false, false, false}, 0, 7,
			"packetunions.ShapeMsg (2 seeds), shapes.Bounds (3 seeds), tblvaluedefaults.FixedDefaults (2 seeds)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live, absent, names := splitLiveSeeds(seeds, seedRoot, roots, tc.known)
			if len(live) != tc.live || absent != tc.absent {
				t.Fatalf("split %d live, %d absent; want %d live, %d absent", len(live), absent, tc.live, tc.absent)
			}
			if names != tc.names {
				t.Fatalf("absent roots named %q; want %q", names, tc.names)
			}
			for _, s := range live {
				if !tc.known[seedRoot[s]] {
					t.Fatalf("a seed of an absent root was kept live")
				}
			}
			if absent > 0 && !strings.Contains(absentClause(absent, names), names) {
				t.Fatalf("the summary clause %q drops the names", absentClause(absent, names))
			}
		})
	}
}
