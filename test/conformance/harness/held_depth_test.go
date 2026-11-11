package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE HELD DEPTH VECTORS (docs/SPEC-TABLES.md §4, ONE NESTING BOUND; §6.6
// retain_walk_depth). The root body is at depth 0 and each structural payload
// inside it is one level deeper: a table body, an array, a keyed or map body,
// and a union whose arm holds one of those or another union. Depth 128 passes
// and 129 refuses in every walk of the binary forms, unknown fields included.
// These vectors are held out of the manifest until every leg answers them, so
// no conformance gate reads them before its own leg does.
type heldDepthVector struct {
	name string
	read bool
}

var heldDepthVectors = []heldDepthVector{
	{name: "depth_tables_128", read: true},
	{name: "depth_arrays_128", read: true},
	{name: "depth_union_tables_64", read: true},
	{name: "depth_keyed_128", read: true},
	{name: "depth_tables_129", read: false},
	{name: "depth_arrays_129", read: false},
	{name: "depth_union_tables_65", read: false},
	{name: "depth_keyed_129", read: false},
	{name: "depth_tables_5000", read: false},
}

// TestHeldDepthVectorsAnswerTheEngine reads each pinned held vector through the
// engine exactly as the framing oracle does: strictDecode on the unit and root
// named, comparing the verdict and never the reason. It also holds each READ
// vector's retaining read and save to its own bytes (§6.6): the retained
// unknown field must ride back out unchanged.
func TestHeldDepthVectorsAnswerTheEngine(t *testing.T) {
	t.Parallel()
	// Resolve the manifest's paths against the repository root rather than
	// changing process cwd, which a parallel test may not do.
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
	unit, err := u.get("graphdemo")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup("Scene")
	if def == nil {
		t.Fatal("graphdemo declares no root Scene")
	}
	for _, v := range heldDepthVectors {
		t.Run(v.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
			if err != nil {
				t.Fatalf("%s: %v — run FRAMING_WRITE=1", v.name, err)
			}
			inst := model.New(def)
			var report tabletext.Report
			accepted, _ := strictDecode(model, inst, data, &report)
			if accepted != v.read {
				t.Fatalf("%s: accepted=%t, want %t", v.name, accepted, v.read)
			}
			if !v.read {
				return
			}
			retained := model.New(def)
			store := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 12}
			var loadReport tabletext.Report
			ok, err := strictDecodeRetain(model, retained, data, store, &loadReport)
			if err != nil || !ok || loadReport.Refused {
				t.Fatalf("%s: the retaining read refused a sound wire: ok=%v err=%v %+v", v.name, ok, err, loadReport)
			}
			var saveReport tabletext.Report
			saved, err := tablewire.EncodeRetain(model, retained, store, &saveReport)
			if err != nil {
				t.Fatalf("%s: the retaining save refused: %v", v.name, err)
			}
			if saveReport.Refused || saveReport.RetainLost != 0 {
				t.Fatalf("%s: the retaining save refused or lost a record: %+v", v.name, saveReport)
			}
			if !bytes.Equal(saved, data) {
				t.Fatalf("%s: the retaining save is %d bytes and the file is %d, and they differ at byte %d",
					v.name, len(saved), len(data), firstDifference(saved, data))
			}
		})
	}
}
