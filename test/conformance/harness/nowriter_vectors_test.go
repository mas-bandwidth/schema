package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// The no-writer framing vectors (docs/SPEC-TABLES.md §4, the maintainer's
// 2026-10-31 list; PR #2557 carried onto main): each script below is input
// no writer produces, so the engine refuses the whole load. Several are
// renames of vectors main already carried under tolerant names
// (frame_repeat_last_wins, the *_inert and *_kind_mismatch crossings);
// the old names keep their manifest rows until that file, outside this
// card's paths, is re-pinned, and these names pin the same bytes under
// the refusal they now owe. Every case was checked at the card's base
// first: main's strict wire-alone walk already refused each one, and
// this test holds that verdict.
var noWriterVectors = []framingVector{
	{name: "frame_repeat_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(gradeSilver, b(1, 30, 9), b(0))},
	{name: "frame_repeat_unknown_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(16, 4), i32(1), b(16, 4), i32(2), gradeSilver, b(0))},
	{name: "nested_repeat_id", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 13, 7, 10), f32(1.5), b(7, 10), f32(2.5), b(0), gradeSilver, b(0))},
	{name: "array_L0_short", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 0), gradeSilver, b(0))},
	{name: "array_L1_short", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 1, 30), gradeSilver, b(0))},
	{name: "array_header_crosses_L_foreign_kind", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(8, 14, 2, 4, 0x82), gradeSilver, b(0))},
	{name: "map_header_crosses_L_foreign_kind", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 2, 14, 0x83), name50, b(0))},
	{name: "map_repeat_key", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 32, 13, 2), squadEntry(2, 20), squadEntry(2, 30), name50, b(0))},
	{name: "map_entry_key_twice", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 20, 13, 1, 17, 2, 6, 2, 2, 6, 2, 3, 13, 7, 4, 4), i32(20), b(0, 0), name50, b(0))},
	{name: "map_L1_short", unit: "listdemo", root: "Squad", ids: squadIds, body: cat(b(1, 14, 1, 13), name50, b(0))},
	{name: "keyed_header_crosses_L_foreign_kind", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 2, 30, 0x82), gradeSilver, b(0))},
	{name: "keyed_repeat_slot", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 2, 4), i32(7), b(2, 4), i32(3), b(0))},
	{name: "keyed_repeat_unknown_slot", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 14, 4, 2, 16, 4), i32(7), b(16, 4), i32(3), b(0))},
	{name: "keyed_L1_short", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(10, 16, 1, 4), gradeSilver, b(0))},
	{name: "node_table_twice", unit: "graphdemo", root: "ListNode", ids: listNodeIds, body: cat(rootValue1, nextIs2, nodeTable(1, listNodeRecord(2)), nodeTable(1, listNodeRecord(3)), b(0))},
}

func TestNoWriterFramingVectorsRefused(t *testing.T) {
	m, _, u := corpus(t)
	for _, v := range noWriterVectors {
		want, err := v.bytes(u)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		path := filepath.Join(framingDir, v.name+".bin")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the script's bytes", v.name)
			continue
		}
		unit, err := u.get(v.unit)
		if err != nil {
			t.Fatal(err)
		}
		if st := unit.Tables[v.root]; st != nil && st.FixedDeclared {
			if unit, err = framingVariableUnit(m, v.unit, v.root); err != nil {
				t.Fatal(err)
			}
		}
		model := tabletext.NewModel(unit)
		inst := model.New(model.Lookup(v.root))
		var report tabletext.Report
		ok, derr := strictDecode(model, inst, got, &report)
		if ok || !report.Refused {
			t.Errorf("%s: engine accepted input no writer produces: ok=%v err=%v report=%+v", v.name, ok, derr, report)
		}
	}
}
