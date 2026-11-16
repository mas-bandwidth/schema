package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFramingPurposeSpellingsRemain checks the non-canonical framing and
// ID-order cases and the reordered-field positive directly on the wire.
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
		t.Error("reordered-field positive lost its reordered fields")
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

// TestExplicitNullPurposeAndPositive checks the intentional null pointer in a
// field against a control where that field is absent. Neither carries nodes.
func TestExplicitNullPurposeAndPositive(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	for _, tc := range []struct {
		name  string
		nulls int
	}{{"node_index_null", 1}, {"node_index_null_canonical", 0}} {
		data, err := os.ReadFile(filepath.Join(root, framingDir, tc.name+".bin"))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		f := frameWire(data)
		nulls, nodes := 0, 0
		for _, sp := range f.spots {
			if sp.kind == spotIndex && sp.value == 0 {
				nulls++
			}
			if sp.kind == spotNodeCount {
				nodes++
			}
		}
		if nulls != tc.nulls || nodes != 0 {
			t.Errorf("%s: null pointers=%d node tables=%d", tc.name, nulls, nodes)
		}
		if fault := framingTableFault(framingVector{}, data); fault != "" {
			t.Errorf("%s: id table %s", tc.name, fault)
		}
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
	u, err := newUnits(m).get("graphdemo")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("ListNode"))
	positive, err := os.ReadFile(filepath.Join(root, framingDir, "node_index_null_canonical.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	ok, err := strictDecode(model, inst, positive, &report)
	if err != nil || !ok || report.Refused {
		t.Fatalf("canonical absent-pointer control: ok=%v err=%v report=%+v", ok, err, report)
	}

}

// noncanonicalPurposeShapes classifies this small corpus directly from the
// wire. It does not decode the value; variable field order can evolve.
func noncanonicalPurposeShapes(data []byte) map[string]bool {
	f := frameWire(data)
	shapes := map[string]bool{}
	fault := framingTableFault(framingVector{}, data)
	if strings.Contains(fault, "unused slots") {
		shapes["unused-id-entry"] = true
	}
	if strings.Contains(fault, "first uses slot") {
		shapes["id-table-out-of-order"] = true
	}
	nodePos, fields := -1, 0
	for i, sp := range f.spots {
		if sp.kind == spotNodeCount && sp.value == 0 {
			shapes["empty-node-table"] = true
		}
		if sp.kind != spotRef || len(sp.enclosing) != 0 || sp.value == 0 {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		if id == ir.TableNodeWireId {
			nodePos = fields
		}
		fields++
		if i+1 >= len(f.spots) || f.spots[i+1].kind != spotKind {
			continue
		}
		k := f.spots[i+1]
		if id == ir.TableWireId("mode") && k.value == ir.TableKindU8 && k.off+1 < len(data) && data[k.off+1] == 7 {
			shapes["field-at-default"] = true
		}
		if i+2 < len(f.spots) {
			next := f.spots[i+2]
			if k.value == ir.TableKindPointer && next.kind == spotIndex && next.value == 0 {
				shapes["explicit-null-pointer"] = true
			}
			if k.value == ir.TableKindUnion && next.kind == spotArm && next.value == 0 {
				shapes["union-arm-zero"] = true
			}
		}
	}
	if nodePos >= 0 && nodePos != fields-1 {
		shapes["node-table-out-of-place"] = true
	}
	return shapes
}

// TestNoncanonicalVariablePurposeShapes requires every variable-form corpus
// row to contain exactly its named spelling, and the canonical control none.
func TestNoncanonicalVariablePurposeShapes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(conformanceRoot(t), "testdata/wire/non-canonical")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, line := range strings.Split(string(manifest), "\n") {
		row := strings.Split(line, "\t")
		if len(row) != 4 || row[1] != "variable" {
			continue
		}
		seen++
		data, err := os.ReadFile(filepath.Join(dir, row[0]))
		if err != nil {
			t.Fatal(err)
		}
		shapes := noncanonicalPurposeShapes(data)
		if row[2] == "canonical" {
			if len(shapes) != 0 {
				t.Errorf("%s: canonical vector has %v", row[0], shapes)
			}
			continue
		}
		if len(shapes) != 1 || !shapes[row[2]] {
			t.Errorf("%s: got spellings %v, want only %s", row[0], shapes, row[2])
		}
	}
	if seen != 9 {
		t.Errorf("variable corpus has %d rows, want 9", seen)
	}
}

// messageOneShapes walks the known three-bit vocabulary of One.schema; each
// integer payload is eight bits and an empty union has a zero arm reference.
func messageOneShapes(data []byte) (map[string]bool, error) {
	if len(data) < 2 || data[0] != ir.TableWireMessageForm {
		return nil, fmt.Errorf("not a message batch")
	}
	r := newStrictBitReader(data[1:])
	n, ok := r.get(8)
	if !ok || n != 0 {
		return nil, fmt.Errorf("not a one-body batch")
	}
	shapes := map[string]bool{}
	last := uint64(0)
	for {
		ref, ok := r.get(3)
		if !ok {
			return nil, fmt.Errorf("truncated field reference")
		}
		if ref == 0 {
			break
		}
		if ref < last {
			shapes["fields-out-of-order"] = true
		}
		last = ref
		switch ref {
		case 1, 2:
			value, ok := r.get(8)
			if !ok {
				return nil, fmt.Errorf("truncated integer")
			}
			if ref == 1 && value == 7 {
				shapes["field-at-default"] = true
			}
		case 3:
			arm, ok := r.get(3)
			if !ok {
				return nil, fmt.Errorf("truncated union")
			}
			if arm == 0 {
				shapes["union-arm-zero"] = true
			} else {
				return nil, fmt.Errorf("unsupported arm")
			}
		default:
			return nil, fmt.Errorf("unexpected field reference %d", ref)
		}
	}
	if !r.align() || r.off != r.n {
		return nil, fmt.Errorf("bad padding")
	}
	return shapes, nil
}

func TestMessageNoncanonicalPurposeShapes(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(conformanceRoot(t), "testdata/wire/message-one-encoding")
	for _, tc := range []struct{ name, shape string }{{"canonical.bin", ""}, {"explicit-default.bin", "field-at-default"}, {"fields-reordered.bin", "fields-out-of-order"}, {"union-none.bin", "union-arm-zero"}} {
		data, err := os.ReadFile(filepath.Join(dir, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		shapes, err := messageOneShapes(data)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if tc.shape == "" {
			if len(shapes) != 0 {
				t.Errorf("%s: got %v", tc.name, shapes)
			}
			continue
		}
		if len(shapes) != 1 || !shapes[tc.shape] {
			t.Errorf("%s: got %v, want only %s", tc.name, shapes, tc.shape)
		}
	}
}

func TestMessageFieldOrderVectorPinned(t *testing.T) {
	t.Parallel()
	body := &bitw{}
	body.put(2, 3)
	body.put(5, 8)
	body.put(1, 3)
	body.put(8, 8)
	body.put(0, 3)
	want := batchOf(body)
	path := filepath.Join(conformanceRoot(t), "testdata/wire/message-one-encoding/fields-reordered.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("field-order vector differs from its bit generator")
	}
}
