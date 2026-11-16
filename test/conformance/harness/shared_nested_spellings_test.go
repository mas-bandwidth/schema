package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type nestedScope struct {
	owner   *ir.Struct
	element *ir.Struct
	union   *ir.Union
}

func scopeField(st *ir.Struct, id uint64) *ir.Field {
	if st == nil {
		return nil
	}
	for _, fd := range st.Fields {
		if ir.TableFieldWireId(fd) == id {
			return fd
		}
	}
	return nil
}

func scopeTarget(fd *ir.Field) *ir.Struct {
	if fd == nil {
		return nil
	}
	if fd.MapEntry != nil {
		return fd.MapEntry
	}
	st, _ := fd.Type.Ref.(*ir.Struct)
	return st
}

// nestedScopes maps each enclosing length to the declaration of its body or
// its collection element. Unknown fields intentionally carry no declaration.
func nestedScopes(u *ir.Unit, root *ir.Struct, f *wireFrame) map[int]nestedScope {
	scopes := map[int]nestedScope{-1: {owner: root}}
	nodeTypes := map[uint64]*ir.Struct{}
	for _, st := range u.Tables {
		nodeTypes[ir.TableTypeId(st.WireName())] = st
	}
	for i, sp := range f.spots {
		if sp.kind != spotLength {
			continue
		}
		parentKey := -1
		if len(sp.enclosing) > 0 {
			parentKey = sp.enclosing[len(sp.enclosing)-1]
		}
		parent := scopes[parentKey]
		scope := nestedScope{}
		if i > 0 && f.spots[i-1].kind == spotRecordType {
			if id, ok := f.entryOf(f.spots[i-1].value); ok {
				scope.owner = nodeTypes[id]
			}
		} else if i >= 2 && f.spots[i-1].kind == spotKind && f.spots[i-1].role == roleField && f.spots[i-2].kind == spotRef {
			id, _ := f.entryOf(f.spots[i-2].value)
			fd := scopeField(parent.owner, id)
			if fd != nil && !fd.Deprecated && int(f.spots[i-1].value) == ir.TableWireFieldKind(fd) {
				switch f.spots[i-1].value {
				case ir.TableKindTable:
					scope.owner = scopeTarget(fd)
				case ir.TableKindArray, ir.TableKindKeyed:
					scope.element = scopeTarget(fd)
					scope.union, _ = fd.Type.Ref.(*ir.Union)
				}
			}
		} else if i > 0 && f.spots[i-1].kind == spotKey {
			scope.owner = parent.element
		} else if sp.body && parent.element != nil {
			scope.owner = parent.element
		}
		// A table-valued union arm carries the arm's declaration, not the union's.
		if sp.arm && i >= 2 && f.spots[i-1].kind == spotKind && f.spots[i-2].kind == spotArm {
			un := parent.union
			if un == nil && parent.owner != nil {
				// The arm belongs to the closest enclosing declared union field.
				for j := i - 3; j >= 0; j-- {
					q := f.spots[j]
					if len(q.enclosing) != len(sp.enclosing) {
						continue
					}
					if q.kind == spotRef {
						id, _ := f.entryOf(q.value)
						fd := scopeField(parent.owner, id)
						if fd != nil {
							un, _ = fd.Type.Ref.(*ir.Union)
						}
						break
					}
				}
			}
			if un != nil {
				if id, ok := f.entryOf(f.spots[i-2].value); ok {
					for _, arm := range un.Variants {
						if arm.F != nil && ir.TableFieldWireId(arm.F) == id {
							scope.owner = scopeTarget(arm.F)
							scope.union, _ = arm.F.Type.Ref.(*ir.Union)
							break
						}
					}
				}
			}
		}
		scopes[i] = scope
	}
	return scopes
}

// nestedStructuralSpellings reports null-pointer and arm-zero
// spellings inside known nested bodies. Required, deprecated, optional and
// unknown fields are not condemned. Map entry key/value slots always ride.
func nestedStructuralSpellings(u *ir.Unit, root *ir.Struct, data []byte) map[string]bool {
	f := frameWire(data)
	scopes := nestedScopes(u, root, f)
	out := map[string]bool{}
	for i, sp := range f.spots {
		if sp.kind != spotRef || len(sp.enclosing) == 0 || sp.value == 0 || i+2 >= len(f.spots) {
			continue
		}
		key := sp.enclosing[len(sp.enclosing)-1]
		st := scopes[key].owner
		if st == nil {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		fd := scopeField(st, id)
		if fd == nil {
			continue
		}
		if fd.Required || fd.Deprecated || fd.Type.Optional || (st.MapEntryOf != "" && (fd.Name == "key" || fd.Name == "value")) {
			continue
		}
		kind, next := f.spots[i+1], f.spots[i+2]
		if kind.kind != spotKind || kind.role != roleField || int(kind.value) != ir.TableWireFieldKind(fd) {
			continue
		}
		if kind.value == ir.TableKindPointer && next.kind == spotIndex && next.value == 0 {
			out["explicit-null-pointer"] = true
		}
		if kind.value == ir.TableKindUnion && next.kind == spotArm && next.value == 0 {
			out["union-arm-zero"] = true
		}
	}
	return out
}

// TestNestedStructuralSpellingsControls checks nested table and table-array
// bodies, including required and unknown fields, without invoking a reader.
func TestNestedStructuralSpellingsControls(t *testing.T) {
	t.Parallel()
	schema := []byte("package nestedcheck\ntable Node { value uint8\n p *Node }\ntable Inner { a uint8\n b uint8\n p *Node }\ntable RequiredInner { p *Node | required }\nenum Tier { A }\nunion Pick { item Inner }\ntable Root { inner Inner\n list [..3]Inner\n keyed [Tier]Inner\n entries map[int32]Inner\n pick Pick\n required RequiredInner\n head *Node }\n")
	path := filepath.Join(t.TempDir(), "Nested.schema")
	if err := os.WriteFile(path, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ids  []string
		body []byte
		want string
	}{
		{"nested null", []string{"inner", "p"}, b(1, 13, 4, 2, 17, 0, 0, 0), "explicit-null-pointer"},
		{"array element null", []string{"list", "p"}, b(1, 14, 7, 13, 1, 4, 2, 17, 0, 0, 0), "explicit-null-pointer"},
		{"nested unknown", []string{"inner", "future"}, b(1, 13, 4, 2, 17, 0, 0, 0), ""},
		{"nested valid", []string{"inner", "a"}, b(1, 13, 4, 2, 6, 1, 0, 0), ""},
		{"nested reordered fields", []string{"inner", "b", "a"}, b(1, 13, 7, 2, 6, 1, 3, 6, 1, 0, 0), ""},
		{"keyed element null", []string{"keyed", "A", "p"}, b(1, 16, 8, 13, 1, 2, 4, 3, 17, 0, 0, 0), "explicit-null-pointer"},
		{"map value nested null", []string{"entries", "key", "value", "p"}, b(1, 14, 17, 13, 1, 14, 2, 4, 0, 0, 0, 0, 3, 13, 4, 4, 17, 0, 0, 0, 0), "explicit-null-pointer"},
		{"union arm nested null", []string{"pick", "item", "p"}, b(1, 15, 2, 13, 4, 3, 17, 0, 0, 0), "explicit-null-pointer"},
		{"nested required null", []string{"required", "p"}, b(1, 13, 4, 2, 17, 0, 0, 0), ""},
	} {
		v := framingVector{ids: tc.ids, body: tc.body}
		wire, err := v.bytes(nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := nestedStructuralSpellings(u, u.Tables["Root"], wire)
		if tc.want == "" {
			if len(got) != 0 {
				t.Errorf("%s: got %v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !got[tc.want] {
			t.Errorf("%s: got %v, want %s", tc.name, got, tc.want)
		}
	}
	node := framingVector{entries: []uint64{ir.TableWireId("head"), ir.TableNodeWireId, ir.TableTypeId("Node"), ir.TableWireId("p")}, body: b(1, 17, 2, 2, 12, 7, 1, 3, 4, 4, 17, 0, 0, 0)}
	wire, err := node.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := nestedStructuralSpellings(u, u.Tables["Root"], wire)
	if len(got) != 1 || !got["explicit-null-pointer"] {
		t.Errorf("node record null: got %v", got)
	}
}

// TestSharedReadableNestedStructuralSpellings checks known nested bodies on readable file rows.
func TestSharedReadableNestedStructuralSpellings(t *testing.T) {
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
		shapes := nestedStructuralSpellings(u, st, data)
		if len(shapes) != 0 {
			t.Errorf("%s (%s): nested structural spellings %v", x.name, x.path, shapes)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no readable file cases")
	}
}

// nestedStoredDefaults checks supported elisions inside known nested bodies.
// Collection elements and map entry key/value fields themselves always ride;
// the ordinary rule still applies to fields within their table bodies.
func nestedStoredDefaults(u *ir.Unit, root *ir.Struct, data []byte) map[string]bool {
	f := frameWire(data)
	out := map[string]bool{}
	for scope, body := range nestedScopes(u, root, f) {
		if scope < 0 || body.owner == nil {
			continue
		}
		for field := range bodyStoredDefaults(body.owner, data, f, scope) {
			out[field] = true
		}
	}
	return out
}

func TestNestedStoredDefaultsControls(t *testing.T) {
	t.Parallel()
	schema := []byte("package nesteddefault\ntable Inner { a uint8 }\nenum Tier { A }\ntable Root { inner Inner\n list [..3]Inner\n keyed [Tier]Inner\n entries map[int32]Inner\n scalars map[int32]int32 }\n")
	path := filepath.Join(t.TempDir(), "Nested.schema")
	if err := os.WriteFile(path, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ids  []string
		body []byte
		want string
	}{
		{"nested field default", []string{"inner", "a"}, b(1, 13, 4, 2, 6, 0, 0, 0), "a"},
		{"array element field default", []string{"list", "a"}, b(1, 14, 7, 13, 1, 4, 2, 6, 0, 0, 0), "a"},
		{"keyed element field default", []string{"keyed", "A", "a"}, b(1, 16, 8, 13, 1, 2, 4, 3, 6, 0, 0, 0), "a"},
		{"map value field default", []string{"entries", "key", "value", "a"}, b(1, 14, 17, 13, 1, 14, 2, 4, 0, 0, 0, 0, 3, 13, 4, 4, 6, 0, 0, 0, 0), "a"},
		{"map key and value ride", []string{"scalars", "key", "value"}, b(1, 14, 16, 13, 1, 13, 2, 4, 0, 0, 0, 0, 3, 4, 0, 0, 0, 0, 0, 0), ""},
		{"array element rides", []string{"list"}, b(1, 14, 4, 13, 1, 1, 0, 0), ""},
	} {
		v := framingVector{ids: tc.ids, body: tc.body}
		wire, err := v.bytes(nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := nestedStoredDefaults(u, u.Tables["Root"], wire)
		if tc.want == "" {
			if len(got) != 0 {
				t.Errorf("%s: got defaults %v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !got[tc.want] {
			t.Errorf("%s: got %v, want %s", tc.name, got, tc.want)
		}
	}
}

// TestSharedReadableNestedElision checks supported nested defaults in readable file rows.
func TestSharedReadableNestedElision(t *testing.T) {
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
		shapes := nestedStoredDefaults(u, st, data)
		if len(shapes) != 0 {
			t.Errorf("%s (%s): nested fields stored at default %v", x.name, x.path, shapes)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no readable file cases")
	}
}
