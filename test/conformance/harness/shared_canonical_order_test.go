package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFramingRootFieldsInDeclarationOrder checks the known root-order cases
// directly against the declaration, without decoding or re-encoding the file.
func TestFramingRootFieldsInDeclarationOrder(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	names := map[string]bool{
		"nested_unknown_id_inside": true, "union_unknown_arm": true, "union_arm_zero_in_field": true,
		"nested_terminator_early": true, "nested_ref_above_count_inside": true, "nested_terminator_missing": true,
		"nested_damage_midway": true, "array_L0_inert": true, "array_L1_inert": true, "keyed_L1_inert": true,
		"union_arm_L_not_width": true, "union_body_arm_terminator_early": true, "union_void_arm_L_nonzero": true,
		"node_index_above_count": true, "nested_repeat_id": true, "array_L0_short": true, "array_L1_short": true, "keyed_L1_short": true,
	}
	for _, v := range append(append([]framingVector{}, framingVectors...), noWriterVectors...) {
		if !names[v.name] {
			continue
		}
		delete(names, v.name)
		unit, err := units.get(v.unit)
		if err != nil {
			t.Fatal(err)
		}
		st := unit.Tables[v.root]
		if st == nil {
			t.Fatalf("%s: missing root %s", v.name, v.root)
		}
		place := make(map[uint64]int, len(st.Fields))
		for i, fd := range st.Fields {
			place[ir.TableFieldWireId(fd)] = i
		}
		data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		f := frameWire(data)
		last := -1
		for _, sp := range f.spots {
			if sp.kind != spotRef || len(sp.enclosing) != 0 {
				continue
			}
			id, ok := f.entryOf(sp.value)
			if !ok {
				continue
			}
			idx, ok := place[id]
			if !ok {
				continue
			}
			if idx < last {
				t.Errorf("%s: root fields out of declaration order at byte %d", v.name, sp.off)
				break
			}
			last = idx
		}
	}
	for name := range names {
		t.Errorf("%s: expected framing vector is missing", name)
	}

}
