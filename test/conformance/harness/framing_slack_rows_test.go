package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// framingSlackRows are the framing rows that hold bytes inside a body's L after
// the point its own framing says it ends: a count short of its L for an array,
// a keyed array and a map (L6), and a node record whose body reaches its
// terminator before the record's L ends (L3). Each is damage and owes the
// refusal alone (D1), which the engine's strict read must give.
var framingSlackRows = []string{
	"array_elements_past_count",
	"keyed_slots_past_count",
	"map_entries_past_count",
	"node_record_bytes_after_terminator",
}

// TestFramingRowsHoldBytesInsideL holds the four rows to the manifest and the
// engine: each is a `framing` row, each pins `refused`, and the engine's strict
// read refuses each (docs/LAYERS/1-framing.md, L3 and L6).
func TestFramingRowsHoldBytesInsideL(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	rows := map[string]FramingCase{}
	for _, fc := range m.Framings {
		rows[fc.Name] = fc
	}
	for _, name := range framingSlackRows {
		fc, ok := rows[name]
		if !ok {
			t.Errorf("%s: the manifest has no framing row for it", name)
			continue
		}
		if !fc.Refused {
			t.Errorf("%s: the row pins %s, want refused", name, fc.Verdict())
		}
		if _, retired := framingStrictRefusals[name]; retired {
			t.Errorf("%s: is in framingStrictRefusals, which is for rows whose manifest line pins a retired answer", name)
		}
		wantWire := filepath.ToSlash(filepath.Join(framingDir, name+".bin"))
		if fc.Wire != wantWire {
			t.Errorf("%s: the row reads %s, the vector is %s", name, fc.Wire, wantWire)
		}
		unit, err := u.get(fc.Unit)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, fc.Wire))
		if err != nil {
			t.Fatalf("%s: %v — run with FRAMING_WRITE=1 to write it", name, err)
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(fc.Root)
		if def == nil {
			t.Fatalf("%s: the unit declares no root %q", name, fc.Root)
		}
		inst := model.New(def)
		var report tabletext.Report
		accepted, _ := strictDecode(model, inst, data, &report)
		if accepted || !report.Refused {
			t.Errorf("%s: the engine's strict read accepted=%t report=%+v, want refused", name, accepted, report)
		}
	}
}
