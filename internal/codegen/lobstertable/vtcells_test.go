package lobstertable_test

// THE VARIABLE-TABLE CELLS on the Lobster leg (ROADMAP.md, Variable tables;
// docs/SPEC-TABLES.md §2, §3, §3.1, §4): one subtest per roadmap row, each
// over the shared tables-manifest rows that exercise that row's feature. The
// whole-manifest gates (TestLobsterTableLoadMatchesTheReports,
// TestLobsterTableSaveRoundTripsEveryInstance) answer for the manifest as a
// whole; this file answers for each row:
//
//	report     the wire loads through the emitted load.lobster to the Go
//	           engine's acceptance (read or refused) and, when read, to the
//	           Go engine's complete value dump; a refusal publishes nothing
//	instance   as a report, and the value saves back through
//	           schema_save.lobster to the Go engine's bytes
//	           (internal/tablewire.Encode of the value it decoded); a row the
//	           Go writer refuses (blob_shared) is held to the load alone
//
// TestLobsterVariableTableCellsNegativeControl plants one defect in the save
// and one in the load and requires every row's subtest to go red under each.
// Ported from mootable/vtcells_test.go.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// vtCell is one roadmap row and the manifest cases that prove it on this leg.
type vtCell struct {
	row   string   // the roadmap row (port/lobster/vt/<row>)
	cases []string // manifest report and instance case names
}

var vtCells = []vtCell{
	// fixed tables on the variable wire, form 1: value roots with no node
	// table, defaults elided, schema evolution in both directions, and the
	// form byte's verdicts
	{"fixed-class", []string{"root_full", "root_default", "profile_elide", "loadout_full", "previous_form_wide_blob", "archive",
		"v1_cfg", "v1_seams", "v2_cfg", "v2_seams", "chain_value", "chain_value_empty",
		"v1_cfg_as_v2", "v1_seams_as_v2", "v2_cfg_as_v1", "v2_seams_as_v1", "form_zero", "form_six", "form_ff", "message_as_file"}},
	// ?T: tblp3's link ?Link against tblp1's plain link both ways, tblg1's
	// optional ?Wrapped
	{"optional-fields", []string{"chain_optional", "chain_optional_empty", "p1_as_p3", "p3_as_p1", "p1_empty_as_p3", "p3_empty_as_p1",
		"p2_as_p3", "p2_empty_as_p3", "p3_as_p2", "optional_absent_no_edge", "optional_present_edge"}},
	// [E]T: tabledemo's KeyedConfig, tblr's tally [Grade]int32 across an
	// enum rename
	{"enum-keyed-arrays", []string{"keyed_config", "keyed_default", "r1_cfg", "r2_cfg", "r1_cfg_as_r2", "r2_cfg_as_r1", "k1_as_k2", "k2_as_k1"}},
	// pointers and the flat node table: sharing, empty and deep graphs,
	// guarded edges, and a list that names one node twice
	{"variable-class", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep", "guard_false_no_edge", "guard_true_both_edges",
		"list_shared", "list_before_pointer"}},
	// fixed(I, F), ufixed and 128-bit scalars, and their evolution
	{"wide-scalars", []string{"scalars_full", "scalars_default", "scalars_edges", "scalars_full_as_2", "scalars_edges_as_2", "field_scalar_widen"}},
	// union arms that are tables: tblm's Body, the message tool's arms
	{"union-table-arms", []string{"m1_open", "m1_save", "m2_close", "m2_open", "m1_open_as_m2", "m1_save_as_m2", "m2_close_as_m1", "m2_open_as_m1",
		"message_arms", "message_arm_edits"}},
	// [..N]*T and [N]*T: the stream's parts and pair, the fleet's escorts
	{"pointer-arrays", []string{"stream_parts", "stream_chain", "parts_elem_kind", "parts_count_past_bound", "parts_index_out_of_range",
		"w1_fleet", "w2_fleet"}},
	// arrays of unions: the message tool's history and edits
	{"union-arrays", []string{"message_batch", "message_arm_edits", "batch_elem_kind", "batch_unknown_arm"}},
	// ?[..N]T and ?[N]T: the message tool's trace, marks, modes,
	// checkpoints and snapshots
	{"optional-arrays", []string{"message_trace", "message_save", "message_empty", "trace_elem_kind", "trace_count_past_bound", "marks_unknown_variant"}},
}

// vtCellFailures runs one row and answers what differs and how many cases
// it held. mutate, when set, edits each generated unit's directory before
// the run.
func vtCellFailures(t *testing.T, c vtCell, mutate func(*testing.T, string)) (fails []string, held int) {
	t.Helper()
	units, _ := readManifest(t)
	byCase := map[string]loadRow{}
	for _, r := range loadRows(t) {
		if _, dup := byCase[r.name]; !dup {
			byCase[r.name] = r
		}
	}
	byUnit := map[string][]loadRow{}
	var order []string
	for _, name := range c.cases {
		row, ok := byCase[name]
		if !ok {
			t.Fatalf("%s names case %s, which the manifest does not carry", c.row, name)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	for _, key := range order {
		rows := byUnit[key]
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			fails = append(fails, fmt.Sprintf("unit %s is refused: %v", key, err))
			continue
		}
		if mutate != nil {
			mutate(t, g.dir)
		}
		res := runUnit(t, g, rows)
		var saveRows []loadRow
		var want [][]byte
		for i, row := range rows {
			held++
			verdict, dump := res.lob[2*i], res.lob[2*i+1]
			if verdict != res.goLine[i] {
				fails = append(fails, fmt.Sprintf("%s %s (%s/%s): acceptance %s, the Go engine %s", row.kind, row.name, row.unit, row.root, verdict, res.goLine[i]))
				continue
			}
			if dump != res.goDump[i] {
				fails = append(fails, fmt.Sprintf("%s %s (%s/%s): value differs at %s", row.kind, row.name, row.unit, row.root, divergence(dump, res.goDump[i])))
				continue
			}
			if verdict != "read" && dump != "nil" {
				fails = append(fails, fmt.Sprintf("%s %s: a refusal publishes a partial value", row.kind, row.name))
				continue
			}
			if row.kind != "instance" || verdict != "read" {
				continue
			}
			wire, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
			out, goErr := goSave(g, row.root, wire)
			if goErr != nil {
				// blob_shared (§2.5): the Go writer refuses the value; the
				// load alone is held (save_blob_shared_test holds Lobster's
				// own refusal)
				continue
			}
			saveRows = append(saveRows, row)
			want = append(want, out)
		}
		if len(saveRows) == 0 {
			continue
		}
		saved := runSaveRows(t, g, saveRows)
		for i, row := range saveRows {
			if w := hex.EncodeToString(want[i]); saved.saved[i] != w {
				fails = append(fails, fmt.Sprintf("instance %s (%s/%s): the save is not the Go engine's:\nlobster: %s\ngo:      %s", row.name, row.unit, row.root, saved.saved[i], w))
			}
		}
	}
	return fails, held
}

func TestLobsterVariableTableCells(t *testing.T) {
	slowtest.Gate(t, "the pinned Lobster toolchain (it runs the emitted table runtime)")
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
			t.Logf("%s: %d manifest cases held", c.row, held)
		})
	}
}

// TestLobsterVariableTableCellsNegativeControl plants, one at a time, a
// defect in the emitted save (the variable form's id-table count one too
// many, and the fixed form's layout length one too many, so no saved wire is
// the reference's) and in the emitted load (the id table resolved in
// reverse, and the fixed form's copy runs landing their first byte flipped,
// so no loaded value is the reference's), and requires every row's subtest
// to go red under each: no row's gate is green by holding nothing.
func TestLobsterVariableTableCellsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Lobster toolchain (it runs the emitted table runtime)")
	t.Parallel()
	plant := func(file, from, to string) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			t.Helper()
			p := filepath.Join(dir, file)
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(src), from); n != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, n)
			}
			if err := os.WriteFile(p, []byte(strings.Replace(string(src), from, to, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	both := func(fs ...func(*testing.T, string)) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			for _, f := range fs {
				f(t, dir)
			}
		}
	}
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"save-framing", both(
			plant("schema_save.lobster", "    so_u64(out, length(e.ids.ids))\n", "    so_u64(out, length(e.ids.ids) + 1)\n"),
			plant("fixed.lobster", "    out = write_int32_le(out, 16, length(layout))\n", "    out = write_int32_le(out, 16, length(layout) + 1)\n"))},
		{"load-resolution", both(
			plant("load.lobster", "    return ids[ref - 1], true\n", "    return ids[length(ids) - ref], true\n"),
			plant("fixedevolve.lobster", "    var b = dst\n    var k = 0\n    while k + 8 <= n:\n", "    var b = dst\n    var k = 0\n    if n > 0:\n        b = write_int8_le(b, dst_off, read_uint8_le(src, src_off) ^ 1)\n        k = 1\n    while k + 8 <= n:\n"))},
	} {
		for _, c := range vtCells {
			t.Run(ctl.name+"/"+c.row, func(t *testing.T) {
				t.Parallel()
				fails, _ := vtCellFailures(t, c, ctl.mutate)
				if len(fails) == 0 {
					t.Fatalf("%s: the %s row stayed green", ctl.name, c.row)
				}
				t.Logf("%s: %d findings, first %s", c.row, len(fails), fails[0])
			})
		}
	}
}
