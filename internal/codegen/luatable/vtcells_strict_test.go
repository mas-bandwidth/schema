package luatable

// THE FIVE CELLS THE STRICT-100 AUDIT CUT, ON THE STRICT READ
// (docs/roadmap-evidence/lua.sexp; internal/codegen/luatable/wirecheck_test.go):
// port/lua/vt/id-table-wire, unbounded-arrays, enum-keyed-arrays, maps and
// variable-class share the framing wire-check negative control. The ten
// rows in luaWireCheckOnly are partitioned by feature below: disabling the
// check must make each read, while every other manifest verdict stays fixed.
// The malformed unknown node record remains refused independently of the
// wire-check and is asserted separately. The legal null pointer reads.

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
	{"id-table-wire", []string{"frame_repeat_last_wins", "frame_unknown_id_length", "frame_unknown_id_reference", "frame_unreferenced_entry"}},
	{"maps", []string{"map_L1_inert"}},
	{"unbounded-arrays", []string{"array_L0_inert", "array_L1_inert"}},
	{"variable-class", []string{"node_table_twice", "node_record_bytes_after_terminator", "varevo_cycle_known_via_unknown", "varevo_cycle_root_via_unknown", "varevo_cycle_two_unknowns", "varevo_cycle_unknown_self"}},
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
	// The isolated null-pointer field is lawful after Required removal; its
	// old orphan-table damage is covered by the separate orphan-cycle gate.
	if got["node_unknown_to_fixed_reader"] != "refused" {
		t.Errorf("node_unknown_to_fixed_reader: got %q, want refused", got["node_unknown_to_fixed_reader"])
	}
	if got["node_index_null"] != "read" {
		t.Errorf("node_index_null: lawful explicit null answered %q", got["node_index_null"])
	}
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
