package mootable_test

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// framingVerdicts reads every `framing` row of the conformance manifest
// through the emitted table_load.moo's ReadStrict, the variable form's one
// read (a fixed root reads the form-1 vector the way the harness reads it,
// test/conformance/harness/generate.go framingVariableUnit), with edit
// applied to each unit's files first when it is not nil. It answers each
// case's verdict, `read` or `refused`, and the verdict the row pins.
func framingVerdicts(t *testing.T, edit func(t *testing.T, g *generatedUnit)) (got, want map[string]string) {
	t.Helper()
	units, _ := readManifest(t)
	byUnit := map[string][][]string{}
	want = map[string]string{}
	for _, f := range manifestLines(t, "framing") {
		byUnit[f[2]] = append(byUnit[f[2]], f)
		want[f[1]] = f[5]
	}
	keys := make([]string, 0, len(byUnit))
	for k := range byUnit {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	got = map[string]string{}
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			t.Fatalf("unit %s: %v", key, g.err)
		}
		if edit != nil {
			edit(t, g)
		}
		var b strings.Builder
		b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\n" + g.requireAll())
		inputs := map[string][]byte{}
		for i, f := range byUnit[key] {
			name := fmt.Sprintf("w%d", i)
			inputs[name] = readRoot(t, f[4])
			fmt.Fprintf(&b, "do\n\tlocal rep = L.newReport()\n\tlocal v = L.ReadStrict(%q, inputs.%s, rep)\n\tprint(%q .. \" \" .. (if v ~= nil and not rep.damaged and rep.reason == nil then \"read\" else \"refused\"))\nend\n", f[3], name, f[1])
		}
		for line := range strings.SplitSeq(g.run(t, b.String(), inputs), "\n") {
			name, verdict, ok := strings.Cut(line, " ")
			if !ok {
				t.Fatalf("unit %s: probe line %q", key, line)
			}
			got[name] = verdict
		}
	}
	return got, want
}

// TestMooReadStrictAnswersEveryFramingRow: THE WIRE-ALONE WALK (§6.5;
// internal/tablewire/wirecheck.go). Every framing vector of the manifest,
// read through moo's one read, answers the verdict its row pins: damage
// inside a field this reader cannot name (ill-formed nested framing, a
// pointer past the node table), a node record no pointer reaches and a node
// table that cannot be read whole all refuse the load, as the Go engine's
// phase one refuses them.
func TestMooReadStrictAnswersEveryFramingRow(t *testing.T) {
	t.Parallel()
	got, want := framingVerdicts(t, nil)
	if len(want) == 0 {
		t.Fatal("the manifest names no framing row")
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: moo answers %q, the row pins %q", name, got[name], w)
		}
	}
	t.Logf("%d framing rows answer their verdicts", len(want))
}

// The control removes phase one, the wire-alone walk, from the emitted read:
// the rows only it refuses then read, and every other row keeps its verdict.
// An id-table entry no reference names is the walk's alone to refuse (§3); an
// explicit null pointer reads and rewrites (docs/rulings.json
// reads-refuse-non-canonical), so node_index_null is no row the walk owns.
// A union nested in unknown union arms past the framed depth bound is the
// walk's too: the walk bounds framed depth (internal/tablewire/wirecheck.go,
// ir.TableBinaryMaxDepth), and without it moo reads those rows.
//
// The framing rows the 2026-11-23 wave added are the walk's too, each a
// condition §6.5 decides FROM THE WIRE ALONE (the Lua leg's control holds the
// same rows, internal/codegen/luatable/wirecheck_test.go luaWireCheckOnly):
// an element kind no reader can skip, a binary nesting depth past 128, an id
// table out of first-use order, a node record typed with a reserved id
// (§3.1, test/conformance/harness/held_reserved_type_test.go), and the graph
// damage reached only through pointers this reader does not declare: a
// cycle, an unreachable record, a shared blob or ill-formed text
// (test/conformance/harness/held_graph_test.go).
func TestMooReadStrictWireCheckNegativeControl(t *testing.T) {
	t.Parallel()
	got, want := framingVerdicts(t, func(t *testing.T, g *generatedUnit) {
		removeMooDiscard(t, g, "table_load.moo", wirePhaseOne, wirePhaseOneOff)
	})
	walkOnly := []string{
		"frame_unknown_id_length", "frame_unknown_id_reference", "frame_unreferenced_entry",
		"skipped_ids_out_of_order",
		"node_record_bytes_after_terminator", "node_unknown_to_fixed_reader",
		"node_type_node_table_id", "node_type_build_version_id", "node_type_vocabulary_id",
		"varevo_cycle_unknown_self", "varevo_cycle_two_unknowns", "varevo_cycle_root_via_unknown", "varevo_cycle_known_via_unknown",
		"union_chain_unknown_past_bound", "union_chain_unknown_deep",
		"array_elem_kind_16_count_1", "array_elem_kind_31_count_1", "array_elem_kind_32_count_2",
		"array_elem_reserved_count_0", "array_elem_unassigned_count_0", "array_elem_zero_count_0",
		"depth_arrays_129", "depth_keyed_129", "depth_tables_129", "depth_tables_5000", "depth_union_tables_65",
		"graph_unknown_type_self", "graph_unknown_type_pair", "graph_listnode_unknown_self", "graph_unknown_to_root",
		"graph_blob_two_unknown_fields", "graph_blob_two_unknown_elements",
		"graph_string_blob_ill_formed", "graph_string_blob_zero_byte",
		"graph_unknown_type_unreached", "graph_string_blob_unreached",
	}
	for _, name := range walkOnly {
		if want[name] != "refused" || got[name] != "read" {
			t.Errorf("%s: without the walk moo answers %q (the row pins %q); the control wants it read", name, got[name], want[name])
		}
	}
	if len(want) == 0 {
		t.Fatal("want is empty: the loop below would check nothing")
	}
	for name, w := range want {
		if !slices.Contains(walkOnly, name) && got[name] != w {
			t.Errorf("%s: removing the walk moved a row it does not own: %q, the row pins %q", name, got[name], w)
		}
	}
}
