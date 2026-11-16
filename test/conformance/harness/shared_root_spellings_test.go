package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rootStructuralSpellings examines the root body's field order, known
// non-required null pointers and zero union arms, and node-table placement.
// Unknown and required fields can lawfully carry null or an empty union.
// It does not judge stored defaults or nested bodies.
func rootStructuralSpellings(st *ir.Struct, data []byte) map[string]bool {
	f := frameWire(data)
	out := map[string]bool{}
	places := map[uint64]int{}
	declared := map[uint64]*ir.Field{}
	for i, field := range st.Fields {
		id := ir.TableFieldWireId(field)
		places[id] = i
		declared[id] = field
	}
	last, nodePos, fields := -1, -1, 0
	for i, sp := range f.spots {
		if sp.kind == spotNodeCount && sp.value == 0 {
			out["empty-node-table"] = true
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
		} else if at, ok := places[id]; ok {
			if at < last {
				out["fields-out-of-order"] = true
			}
			last = at
		}
		fields++
		if i+2 >= len(f.spots) {
			continue
		}
		k, next := f.spots[i+1], f.spots[i+2]
		fd := declared[id]
		if k.kind != spotKind || k.role != roleField || fd == nil || fd.Required || fd.Deprecated || fd.Type.Optional || ir.TableWireFieldKind(fd) != int(k.value) {
			continue
		}
		if k.value == ir.TableKindPointer && next.kind == spotIndex && next.value == 0 {
			out["explicit-null-pointer"] = true
		}
		if k.value == ir.TableKindUnion && next.kind == spotArm && next.value == 0 {
			out["union-arm-zero"] = true
		}
	}
	if nodePos >= 0 && nodePos != fields-1 {
		out["node-table-out-of-place"] = true
	}
	return out
}

// TestSharedReadableRootStructuralSpellings checks these root structural
// spellings on every readable file-form manifest input. It is not the full
// nested/default/message canonicality check.
func TestSharedReadableRootStructuralSpellings(t *testing.T) {
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
	reports, err := readReports(filepath.Join(root, defaultReports))
	if err != nil {
		t.Fatal(err)
	}
	units := newUnits(m)
	type item struct{ name, unit, table, path string }
	var cases []item
	for _, x := range m.Instances {
		if !x.WireRefused {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Reports {
		if reports[x.Name] == "read" {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Framings {
		if !x.Refused {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Rewrites {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.Retains {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.JSONWriteRefusals {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	seen := map[string]bool{}
	for _, x := range cases {
		key := x.unit + "/" + x.table + "/" + x.path
		if seen[key] {
			continue
		}
		seen[key] = true
		data, err := os.ReadFile(filepath.Join(root, x.path))
		if err != nil {
			t.Errorf("%s: %v", x.name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s: empty readable wire", x.name)
			continue
		}
		if data[0] == 3 {
			continue
		}
		if data[0] != 1 {
			t.Errorf("%s: unexpected form %d", x.name, data[0])
			continue
		}
		u, err := units.get(x.unit)
		if err != nil {
			t.Fatalf("%s: %v", x.name, err)
		}
		st := u.Tables[x.table]
		if st == nil {
			t.Fatalf("%s: no %s root", x.name, x.table)
		}
		if st.Integrity != "" {
			if len(data) < 5 {
				t.Errorf("%s: missing integrity trailer", x.name)
				continue
			}
			data = data[:len(data)-4]
		}
		shapes := rootStructuralSpellings(st, data)
		want := ""
		switch filepath.Base(x.path) {
		case "rewrite_fields_reversed.bin":
			want = "fields-out-of-order"
		case "union_arm_zero_in_field.bin":
			want = "union-arm-zero"
		}
		if want == "" {
			if len(shapes) != 0 {
				t.Errorf("%s (%s): root structural spellings %v", x.name, x.path, shapes)
			}
		} else if len(shapes) != 1 || !shapes[want] {
			t.Errorf("%s: got %v, want only %s", x.name, shapes, want)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no readable file cases")
	}
}

// TestUnknownEmptyUnionPurpose preserves the unknown union arm-zero positive:
// the newer writer may have declared that unknown field required.
func TestUnknownEmptyUnionPurpose(t *testing.T) {
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
	u, err := newUnits(m).get("tblr1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, framingDir, "frame_unknown_id_union_empty.bin"))
	if err != nil {
		t.Fatal(err)
	}
	shapes := rootStructuralSpellings(u.Tables["Cfg"], data)
	if len(shapes) != 0 {
		t.Errorf("unknown empty union was classified noncanonical: %v", shapes)
	}
	found := false
	for _, spot := range frameWire(data).spots {
		if spot.kind == spotArm && spot.value == 0 {
			found = true
		}
	}
	if !found {
		t.Error("unknown empty union no longer carries the arm-zero control")
	}
	if fault := framingTableFault(framingVector{}, data); fault != "" {
		t.Errorf("unknown empty union id table: %s", fault)
	}
}

// TestRootStructuralScannerKnownCases exercises every structural root shape
// against the dedicated corpus, plus a canonical positive.
func TestRootStructuralScannerKnownCases(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	u, err := compiler.New().Load([]string{filepath.Join(root, "test/non-canonical/NonCanonical.schema")})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, shape string }{
		{"variable-canonical.bin", ""}, {"variable-fields-out-of-order.bin", "fields-out-of-order"},
		{"variable-explicit-null-pointer.bin", "explicit-null-pointer"}, {"variable-empty-node-table.bin", "empty-node-table"},
		{"variable-node-table-out-of-place.bin", "node-table-out-of-place"}, {"variable-union-arm-zero.bin", "union-arm-zero"},
	} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/non-canonical", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		got := rootStructuralSpellings(u.Tables["Root"], data)
		if tc.shape == "" {
			if len(got) != 0 {
				t.Errorf("%s: got %v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !got[tc.shape] {
			t.Errorf("%s: got %v, want only %s", tc.name, got, tc.shape)
		}
	}
}

// TestRootStructuralRequiredAndUnknownControls preserves the required-field
// and unknown-field evolution cases beside the known non-required refusals.
func TestRootStructuralRequiredAndUnknownControls(t *testing.T) {
	t.Parallel()
	schema := []byte("package requiredcontrol\ntable Node { value uint8 }\nunion Pick { plain int32 }\ntable Required { p *Node | required\n u Pick | required }\ntable Ordinary { p *Node\n u Pick }\ntable Old { x uint8 }\ntable Deprecated { p *Node | deprecated\n u Pick | deprecated }\n")
	path := filepath.Join(t.TempDir(), "Required.schema")
	if err := os.WriteFile(path, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	v := framingVector{ids: []string{"p", "u"}, body: b(1, 17, 0, 2, 15, 0, 0)}
	wire, err := v.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Required", "Old", "Deprecated"} {
		if got := rootStructuralSpellings(u.Tables[name], wire); len(got) != 0 {
			t.Errorf("%s: lawful shapes classified %v", name, got)
		}
	}
	got := rootStructuralSpellings(u.Tables["Ordinary"], wire)
	if len(got) != 2 || !got["explicit-null-pointer"] || !got["union-arm-zero"] {
		t.Errorf("known nonrequired controls: got %v", got)
	}
}
