package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFramingPurposeSpellingsRemain checks the deliberate non-canonical
// framing and rewrite cases directly on the wire, without asking a reader.
func TestFramingPurposeSpellingsRemain(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	f := frameWire(read(filepath.Join(framingDir, "frame_unreferenced_entry.bin")))
	if len(f.entries) != 1 || len(f.spots) == 0 {
		t.Fatal("unused-entry vector lost its entry")
	}
	if fault := framingTableFault(framingVector{}, read(filepath.Join(framingDir, "frame_unreferenced_entry.bin"))); !strings.Contains(fault, "unused slots") {
		t.Errorf("unused-entry purpose missing: %s", fault)
	}
	f = frameWire(read(filepath.Join(framingDir, "array_L2_empty.bin")))
	found := false
	for i, sp := range f.spots {
		if sp.kind == spotKind && sp.role == roleField && sp.value == ir.TableKindArray && i+3 < len(f.spots) && f.spots[i+3].kind == spotCount && f.spots[i+3].value == 0 {
			found = true
		}
	}
	if !found {
		t.Error("empty array purpose missing")
	}
	f = frameWire(read(filepath.Join(framingDir, "union_arm_zero_in_field.bin")))
	found = false
	for _, sp := range f.spots {
		if sp.kind == spotArm && sp.value == 0 {
			found = true
		}
	}
	if !found {
		t.Error("zero union arm purpose missing")
	}
	reversed := read("testdata/wire/tables/rewrite_ids_reversed.bin")
	if fault := framingTableFault(framingVector{}, reversed); !strings.Contains(fault, "first uses slot") {
		t.Errorf("id order purpose missing: %s", fault)
	}
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
	u, err := newUnits(m).get("tblw1")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Fleet"]
	if st == nil {
		t.Fatal("Fleet declaration missing")
	}
	place := map[uint64]int{}
	for i, fd := range st.Fields {
		place[ir.TableFieldWireId(fd)] = i
	}
	f = frameWire(read("testdata/wire/tables/rewrite_fields_reversed.bin"))
	last := -1
	found = false
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
			found = true
		}
		last = idx
	}
	if !found {
		t.Error("field order purpose missing")
	}
}

// TestNoncanonicalGraphPurposesAreIsolated requires the null-pointer and
// empty-node-table vectors not to carry an orphan or a dangling node index.
func TestNoncanonicalGraphPurposesAreIsolated(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	for _, tc := range []struct {
		name          string
		nulls, tables int
	}{
		{"variable-explicit-null-pointer.bin", 1, 0},
		{"variable-empty-node-table.bin", 0, 1},
	} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/non-canonical", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		f := frameWire(data)
		nulls, tables, nonNulls := 0, 0, 0
		for _, sp := range f.spots {
			if sp.kind == spotIndex {
				if sp.value == 0 {
					nulls++
				} else {
					nonNulls++
				}
			}
			if sp.kind == spotNodeCount {
				tables++
				if sp.value != 0 {
					t.Errorf("%s: node table is not empty", tc.name)
				}
			}
		}
		if nulls != tc.nulls || tables != tc.tables || nonNulls != 0 {
			t.Errorf("%s: nulls=%d node tables=%d nonnull indexes=%d", tc.name, nulls, tables, nonNulls)
		}
		if fault := framingTableFault(framingVector{}, data); fault != "" {
			t.Errorf("%s: id table %s", tc.name, fault)
		}
	}
}
