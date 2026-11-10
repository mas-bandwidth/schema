package mootable_test

// THE VARIABLE-TABLE CELLS on the moo leg (docs/ROADMAP.md, Variable tables;
// docs/SPEC-TABLES.md §2, §3, §3.1, §4): one subtest per roadmap row, each
// over the shared corpus rows and Go-engine constructs that exercise that
// row's feature. The whole-manifest gates (TestMooTableSaveRoundTripsEveryInstance,
// TestMooTableLoadMatchesTheReports, TestMooTableConstructsMatchTheGoEngine)
// answer for the manifest as a whole; this file answers for each row:
//
//	instance   the wire the C++ reference or the Go engine wrote loads through
//	           the emitted table_load.moo unrefused and saves back through
//	           table_save.moo byte for byte, its measure its length; a
//	           wire-refused instance is held to its refusal
//	report     the one read's verdict (read or refused) is reports.txt's, the
//	           Go engine's own verdict for that wire; no counter is compared
//	construct  a constructCases entry: the Go engine's wire of the JSON text
//	           reads through ReadStrict and saves back byte for byte
//
// TestMooVariableTableCellsNegativeControl plants defects in the variable save,
// the variable load and the fixed save, and requires every row to go red
// under at least one.

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// vtCell is one roadmap row and the corpus that proves it on this leg.
type vtCell struct {
	row        string   // the roadmap row (port/moo/vt/<row>)
	cases      []string // manifest instance and report case names
	constructs []string // constructCases names
}

var vtCells = []vtCell{
	// fixed tables on the variable wire, form 1: value roots with no node
	// table, defaults elided, schema evolution in both directions, and the
	// form byte's verdicts
	{"fixed-class", []string{"root_full", "root_default", "profile_elide", "loadout_full", "archive",
		"v1_cfg", "v1_seams", "v2_cfg", "v2_seams", "chain_value", "chain_value_empty",
		"v1_cfg_as_v2", "v1_seams_as_v2", "v2_cfg_as_v1", "v2_seams_as_v1", "form_zero", "form_six", "form_ff", "message_as_file"}, nil},
	// ?T: tblp3's link ?Link against tblp1's plain link both ways, tblg1's
	// optional ?Wrapped, an optional variable closure and an optional map
	{"optional-fields", []string{"chain_optional", "chain_optional_empty", "p1_as_p3", "p3_as_p1", "p1_empty_as_p3", "p3_empty_as_p1",
		"p2_as_p3", "p2_empty_as_p3", "p3_as_p2", "optional_absent_no_edge", "optional_present_edge"},
		[]string{"optional_variable", "optional_map"}},
	// [E]T: tabledemo's KeyedConfig, tblr's tally [Grade]int32 across an
	// enum rename, and the Go engine's gear [Slot]Item
	{"enum-keyed-arrays", []string{"keyed_config", "keyed_default", "r1_cfg", "r2_cfg", "r1_cfg_as_r2", "r2_cfg_as_r1", "k1_as_k2", "k2_as_k1"},
		[]string{"unions_maps_keyed"}},
	// pointers and the flat node table: sharing, empty and deep graphs,
	// guarded edges, and a list that names one node twice
	{"variable-class", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep", "guard_false_no_edge", "guard_true_both_edges",
		"list_shared", "list_before_pointer"}, []string{"blobs_pointers"}},
	// fixed(I, F), ufixed and 128-bit scalars, and their evolution
	{"wide-scalars", []string{"scalars_full", "scalars_default", "scalars_edges", "scalars_full_as_2", "scalars_edges_as_2", "field_scalar_widen"},
		[]string{"wide_ints"}},
	// union arms that are tables: tblm's Body, the message tool's arms
	{"union-table-arms", []string{"m1_open", "m1_save", "m2_close", "m2_open", "m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
		"message_arms", "message_arm_edits"}, []string{"unions_maps_keyed"}},
	// [..N]*T and [N]*T: the stream's parts and pair, the fleet's escorts
	{"pointer-arrays", []string{"stream_parts", "stream_chain", "parts_elem_kind", "parts_count_past_bound", "parts_index_out_of_range",
		"w1_fleet", "w2_fleet"}, []string{"blobs_pointers"}},
	// arrays of unions: the message tool's history and edits
	{"union-arrays", []string{"message_batch", "message_arm_edits", "batch_elem_kind", "batch_unknown_arm"}, []string{"unions_maps_keyed"}},
	// ?[..N]T and ?[N]T: the message tool's trace, marks, modes,
	// checkpoints and snapshots
	{"optional-arrays", []string{"message_trace", "message_save", "message_empty", "trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant"}, nil},
	// *bytes and *string, at every size class, and one blob from two slots
	{"blobs", []string{"blob_small", "blob_empty", "blob_large", "blob_str8", "blob_str16", "blob_equal_bytes", "blob_shared", "blob_shared_string"},
		[]string{"blobs_pointers"}},
	// maps of every key and value class, a widened key and a key of the
	// wrong kind at the root
	{"maps", []string{"map_cells", "map_chunks", "map_crews", "map_depth", "map_docs", "map_empty", "map_full", "map_pairs", "map_slots",
		"map_spans", "map_text", "map_trails", "map_runs", "map_key_widen", "map_depth_root_kind", "list_of_maps"},
		[]string{"unions_maps_keyed", "optional_map"}},
	// an arm of every field kind: tbla's int, enum, pointer, bytes, string
	// and body arms read across a schema change, the stream's arms, an arm
	// past its frame, and escape kinds at an arm
	{"union-arms-any", []string{"stream_arm_first", "stream_arm_pointer", "stream_arm_tag", "a2_arm_enum_as_int", "a2_arm_pointer_as_int",
		"a2_arm_bytes_as_string", "a2_arm_length_off_width", "a1_arm_moved_width_as_a2", "a1_arm_one_width_as_a2", "a1_arm_as_string_as_a2",
		"a1_arm_as_body_as_a2", "a1_arm_body_as_a2", "a1_arm_unknown_as_a2", "nested_arm_trailing", "escape_arm"}, []string{"unions_maps_keyed"}},
	// string, bytes and flags defaults (tblw's Badge, Ship), a table renamed
	// with was (Ship was Vessel), and §4.2's value defaults at and off
	{"defaults-and-table-rename", []string{"w1_fleet", "w1_fleet_default", "w2_fleet", "w1_fleet_as_w2", "w2_fleet_as_w1", "w1_fleet_default_as_w2"},
		[]string{"defaults_at", "defaults_off"}},
	// a renamed variant, field and two arms (tblr2), read both ways
	{"renames", []string{"r1_cfg", "r1_ping", "r2_cfg", "r1_cfg_as_r2", "r1_ping_as_r2", "r2_cfg_as_r1"}, nil},
	// the id table: 10 and 20 vocabularies, two peers' tables, the escape
	// kinds at a field, an element and a payload-free field
	{"id-table-wire", []string{"vocab_wide", "vocab_low", "peer_a", "peer_b", "vocab9_wide", "vocab9_low", "login_full", "match_full",
		"escape_unnamed_field", "escape_named_field", "escape_element", "nopayload_named_field"}, nil},
	// []T: tables, scalars, empty, erased, mixed, nested, maps, a migration
	// from a bounded array, and a widened element
	{"unbounded-arrays", []string{"list_tables", "list_scalars", "list_empty", "list_erased", "list_mixed", "list_shared", "list_nested",
		"list_of_maps", "list_migrates", "list_scalar_widen"}, []string{"blobs_pointers"}},
}

// vtCellFailures runs one row and answers what differs, and how many rows,
// instances and constructs it held. mutate, when set, edits each generated
// unit's emitted files first.
func vtCellFailures(t *testing.T, c vtCell, mutate func(*testing.T, map[string][]byte)) (fails []string, held int) {
	t.Helper()
	units, _ := readManifest(t)
	expect := reportsExpect(t)
	byCase := map[string]reportRow{}
	for _, r := range manifestRows(t) {
		byCase[r.caseName] = r
	}
	cache := map[string]*generatedUnit{}
	unit := func(key string) *generatedUnit {
		if g, ok := cache[key]; ok {
			return g
		}
		g := generateUnit(t, units, key)
		if g.err == nil && mutate != nil {
			files := maps.Clone(g.files)
			mutate(t, files)
			g = writeUnit(t, g.unit, files)
		}
		cache[key] = g
		return g
	}
	for _, name := range c.cases {
		row, ok := byCase[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		g := unit(row.unit)
		if g.err != nil {
			fails = append(fails, fmt.Sprintf("%s: unit %s is refused: %v", name, row.unit, g.err))
			continue
		}
		wire := rowWire(t, row)
		held++
		if row.kind == "report" {
			want, ok := expect[name]
			if !ok {
				t.Fatalf("report %s: reports.txt names no such case", name)
			}
			if got := runWire(t, g, row.root, wire); got != want {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): moo %s, the Go engine %s", name, row.unit, row.root, got, want))
			}
			continue
		}
		saved, measure, err := saveRoot(t, g, wire, row.root)
		switch {
		case row.blobShared:
			if err == nil || !strings.Contains(err.Error(), "blob_shared") {
				fails = append(fails, fmt.Sprintf("%s (%s/%s): a shared blob must be refused as blob_shared, got %v", name, row.unit, row.root, err))
			}
		case err != nil:
			fails = append(fails, fmt.Sprintf("%s (%s/%s): %v", name, row.unit, row.root, err))
		case string(saved) != string(wire):
			fails = append(fails, fmt.Sprintf("%s (%s/%s): the save is not the wire: %s", name, row.unit, row.root, firstDiff(saved, wire)))
		case measure != len(saved):
			fails = append(fails, fmt.Sprintf("%s (%s/%s): measure %d, saved %d bytes", name, row.unit, row.root, measure, len(saved)))
		}
	}
	for _, name := range c.constructs {
		i := slices.IndexFunc(constructCases, func(cc constructCase) bool { return cc.name == name })
		if i < 0 {
			t.Fatalf("%s names construct %s, which constructCases does not carry", c.row, name)
		}
		cc := constructCases[i]
		u := unitFromSource(t, cc.schema)
		wire := goOracleWire(t, u, cc.root, cc.json)
		files, err := generate(u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("construct %s: moo refuses it: %v", name, err))
			continue
		}
		if mutate != nil {
			mutate(t, files)
		}
		g := writeUnit(t, u, files)
		held++
		if out, want := g.run(t, strictRoundTrip(g, cc.root), map[string][]byte{"wire": wire}), fmt.Sprintf("ok %d %x", len(wire), wire); out != want {
			fails = append(fails, fmt.Sprintf("construct %s: moo %s, the Go engine %s", name, out, want))
		}
	}
	return fails, held
}

func TestMooVariableTableCells(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	for _, c := range vtCells {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			fails, held := vtCellFailures(t, c, nil)
			for _, f := range fails {
				t.Error(f)
			}
			if held == 0 {
				t.Fatal("the row holds nothing")
			}
			t.Logf("%s: %d corpus rows and %d constructs held", c.row, len(c.cases), len(c.constructs))
		})
	}
}

// TestMooVariableTableCellsNegativeControl plants, one at a time, a defect in
// the emitted save (the id table's count one too many, so no saved variable
// wire is the reference's), in the emitted load (the id table read in
// reverse, so every reference names another field) and in the fixed form's
// save (the form byte off by one, so no saved fixed root is the reference's).
// Every row must go red under at least one plant (a row of fixed roots never
// meets the id table), and every plant must turn some row red: no row's gate
// is green by holding nothing.
func TestMooVariableTableCellsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	plant := func(file, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src := string(files[file])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(src, from))
			}
			files[file] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	plants := []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
	}{
		{"save-id-count", plant("table_save.moo", "\tw_u32(w, #c.ids.list)\n\tw_u32(w, 0)\n", "\tw_u32(w, #c.ids.list + 1)\n\tw_u32(w, 0)\n")},
		{"load-id-order", plant("table_load.moo", "\t\tids[i + 1] = id\n", "\t\tids[count - i] = id\n")},
		{"fixed-save-form", plant("table_fixed.moo", "\t\tbuffer.writeu8(out, 0, FORM)\n", "\t\tbuffer.writeu8(out, 0, FORM + 1)\n")},
	}
	var mu sync.Mutex
	reddened := map[string]int{}
	t.Run("rows", func(t *testing.T) {
		for _, c := range vtCells {
			t.Run(c.row, func(t *testing.T) {
				t.Parallel()
				red := 0
				for _, p := range plants {
					fails, _ := vtCellFailures(t, c, p.mutate)
					if len(fails) > 0 {
						red++
						mu.Lock()
						reddened[p.name]++
						mu.Unlock()
						t.Logf("%s: %d findings, first %s", p.name, len(fails), fails[0])
					}
				}
				if red == 0 {
					t.Fatalf("the %s row stayed green under every plant", c.row)
				}
			})
		}
	})
	for _, p := range plants {
		if reddened[p.name] == 0 {
			t.Errorf("the %s plant turned no row red", p.name)
		}
	}
}
