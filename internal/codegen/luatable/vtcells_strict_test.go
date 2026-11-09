package luatable

// THE FIVE CELLS THE STRICT-100 AUDIT CUT, ON THE STRICT READ
// (docs/roadmap-evidence/lua.sexp; internal/codegen/luatable/wirecheck_test.go):
// port/lua/vt/id-table-wire, unbounded-arrays, enum-keyed-arrays, maps and
// variable-class left the overlay because the plain Load read ten conformance
// framing vectors the one read refuses (frame_repeat_last_wins,
// frame_unknown_id_length, frame_unknown_id_reference, array_L0_inert,
// array_L1_inert, keyed_L1_inert, map_L1_inert, node_index_null,
// node_table_twice, node_unknown_to_fixed_reader), and no test named that
// refusal for the row it belongs to. The wire-alone check (wire_check in
// load.lua) refuses them now; this file names, per cell, the vectors that
// cell's feature owns and holds the strict read to their refusal:
//
//	id-table-wire     a repeated field id, and unknown field bodies whose
//	                  own bytes are damaged or point past the node table
//	unbounded-arrays  array bodies with L 0 or 1, which carry no count
//	enum-keyed-arrays an enum-keyed body with L 1, which carries no count
//	maps              a map body with L 1, which carries no count
//	variable-class    a node record no pointer reaches, a node table
//	                  twice, and a node table an unknown field carries
//	                  whose record's type ref is 0
//
// The ten vectors are exactly luaWireCheckOnly: the walk reads them and
// only the wire-alone check refuses them, so the negative control below
// switches that check off in the generated load.lua (the real artifact)
// and requires every vector of every cell to read again, and no other
// framing row to move.

import (
	"slices"
	"sort"
	"testing"
)

// luaVTFramingCells maps each cut cell to the framing vectors its feature
// owns. Together they partition luaWireCheckOnly; the tests hold that.
var luaVTFramingCells = []struct {
	cell    string
	vectors []string
}{
	{"enum-keyed-arrays", []string{"keyed_L1_inert"}},
	{"id-table-wire", []string{"frame_repeat_last_wins", "frame_unknown_id_length", "frame_unknown_id_reference"}},
	{"maps", []string{"map_L1_inert"}},
	{"unbounded-arrays", []string{"array_L0_inert", "array_L1_inert"}},
	{"variable-class", []string{"node_index_null", "node_table_twice", "node_unknown_to_fixed_reader"}},
}

// luaVTFramingPins answers the manifest verdict of every vector the five
// cells claim, and holds the claim to be a partition of luaWireCheckOnly:
// every claimed vector is pinned refused, claimed once, and no vector
// only the wire check refuses is left unclaimed.
func luaVTFramingPins(t *testing.T, rows []luaFramingRow) map[string]bool {
	t.Helper()
	pins := map[string]string{}
	for _, r := range rows {
		pins[r.name] = r.verdict
	}
	claimed := map[string]bool{}
	for _, c := range luaVTFramingCells {
		for _, v := range c.vectors {
			if claimed[v] {
				t.Fatalf("%s is claimed by two cells", v)
			}
			claimed[v] = true
			if pins[v] != "refused" {
				t.Fatalf("%s (%s): the manifest pins %q, want refused", v, c.cell, pins[v])
			}
		}
	}
	want := slices.Clone(luaWireCheckOnly)
	sort.Strings(want)
	got := make([]string, 0, len(claimed))
	for v := range claimed {
		got = append(got, v)
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Fatalf("the five cells claim %v, the wire-check-only vectors are %v", got, want)
	}
	return claimed
}

// TestLuaVariableCellsRefuseTheFramingVectors: for each of the five cells,
// every framing vector that cell's feature owns is refused by the strict
// read (load.Load) of the vector's own unit and root.
func TestLuaVariableCellsRefuseTheFramingVectors(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	luaVTFramingPins(t, rows)
	got := luaFramingVerdicts(t, rows, units, nil)
	for _, c := range luaVTFramingCells {
		t.Run(c.cell, func(t *testing.T) {
			t.Parallel()
			for _, v := range c.vectors {
				if got[v] != "refused" {
					t.Errorf("%s (%s): the strict load answers %q, want refused", v, c.cell, got[v])
				}
			}
			t.Logf("%s: %d framing vectors refused", c.cell, len(c.vectors))
		})
	}
}

// TestLuaVariableCellsRefuseTheFramingVectorsNegativeControl plants its
// fault in the real artifact: the generated load.lua with its wire-alone
// check switched off. Every vector of every cell must then read (the walk
// alone accepts it), and no other framing row may move, so each cell's
// refusal above is the check's doing and nothing else's.
func TestLuaVariableCellsRefuseTheFramingVectorsNegativeControl(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	claimed := luaVTFramingPins(t, rows)
	got := luaFramingVerdicts(t, rows, units, func(files map[string][]byte) { withoutWireCheck(t, files) })
	for _, c := range luaVTFramingCells {
		t.Run(c.cell, func(t *testing.T) {
			t.Parallel()
			for _, v := range c.vectors {
				if got[v] != "read" {
					t.Errorf("%s (%s): with the wire check off the strict load answers %q, want read", v, c.cell, got[v])
				}
			}
		})
	}
	for _, r := range rows {
		if claimed[r.name] {
			continue
		}
		if got[r.name] != r.verdict {
			t.Errorf("%s: with the wire check off the row moved to %q, the manifest pins %q", r.name, got[r.name], r.verdict)
		}
	}
}
