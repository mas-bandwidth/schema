package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE HELD ELEMENT-KIND VECTORS (docs/SPEC-TABLES.md §3, §4). An array body
// (kind 14) may carry element kinds 1–13, 15, 17 and 18–30; an enum-keyed
// body (kind 16) may carry 1–11, 13 and 18–30. Any other element kind is
// damage under any count, a count of 0 included, in a field the reader cannot
// name as in one it declares. Each vector rides an unknown root field of
// graphdemo Scene and is held out of the manifest until every leg answers it,
// so no conformance gate reads it before its own leg does.
var heldElementKindVectors = []struct {
	name     string
	accepted bool
}{
	// REFUSED: element kinds off the closed list, under a count of one or more.
	{name: "array_elem_kind_32_count_2", accepted: false},
	{name: "array_elem_kind_31_count_1", accepted: false},
	{name: "array_elem_kind_16_count_1", accepted: false},
	// REFUSED: element kinds off the closed list under a count of ZERO, where
	// no element byte follows: the header's kind byte is still judged.
	{name: "array_elem_unassigned_count_0", accepted: false},
	{name: "array_elem_reserved_count_0", accepted: false},
	{name: "array_elem_zero_count_0", accepted: false},
	// READ: element kind 17 is on the array list, at count 0 and at count 1
	// where its node index names a reachable record.
	{name: "array_elem_kind_17_count_0", accepted: true},
	{name: "array_elem_kind_17_count_1", accepted: true},
}

// TestHeldElementKindVectorsAnswerTheEngine reads each pinned held vector and
// runs it through the engine exactly as TestFramingVectorsReadThroughTheEngine
// does: strictDecode on the unit and root named, comparing the verdict alone
// and never the reason (docs/rulings.json refusal-verdict-only). A different
// verdict is an engine or vector defect, never a verdict to flip here.
func TestHeldElementKindVectorsAnswerTheEngine(t *testing.T) {
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
	vectors := map[string]framingVector{}
	for _, v := range framingVectors {
		vectors[v.name] = v
	}
	// A held element-kind script without a verdict here would let the rule go
	// untested, so every one of them must be in the table above.
	listed := map[string]bool{}
	for _, tc := range heldElementKindVectors {
		listed[tc.name] = true
	}
	for _, v := range framingVectors {
		if v.held && strings.HasPrefix(v.name, "array_elem_") && !listed[v.name] {
			t.Errorf("%s: a held element-kind script with no verdict in heldElementKindVectors", v.name)
		}
	}
	for _, tc := range heldElementKindVectors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, ok := vectors[tc.name]
			if !ok {
				t.Fatalf("%s: no framing vector script", tc.name)
			}
			if !v.held {
				t.Fatalf("%s: the element-kind vectors are held out of the manifest", tc.name)
			}
			data, err := os.ReadFile(filepath.Join(root, framingDir, tc.name+".bin"))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			unit, err := u.get(v.unit)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(v.root)
			if def == nil {
				t.Fatalf("%s: the unit declares no root %q", tc.name, v.root)
			}
			inst := model.New(def)
			var report tabletext.Report
			accepted, _ := strictDecode(model, inst, data, &report)
			if accepted != tc.accepted {
				t.Errorf("%s: engine accepted=%t want=%t (report=%+v)", tc.name, accepted, tc.accepted, report)
			}
		})
	}
}
