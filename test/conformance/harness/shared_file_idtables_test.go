package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSharedReadableFileIdTables checks the ID-table part of canonicality for
// readable file-form manifest inputs. The two deliberate ID-table spellings
// are required by name, not ignored. Other spelling classes have separate
// checks; this test does not claim to check them.
func TestSharedReadableFileIdTables(t *testing.T) {
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
	type fileCase struct{ name, unit, table, path string }
	var cases []fileCase
	for _, x := range m.Instances {
		if !x.WireRefused {
			cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Reports {
		if reports[x.Name] == "read" {
			cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Framings {
		if !x.Refused {
			cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Rewrites {
		cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.Retains {
		cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.JSONWriteRefusals {
		cases = append(cases, fileCase{x.Name, x.Unit, x.Root, x.Wire})
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
		} // fixed form has no ID table
		if data[0] != 1 {
			t.Errorf("%s: readable file has unexpected form %d", x.name, data[0])
			continue
		}
		u, err := units.get(x.unit)
		if err != nil {
			t.Fatalf("%s: %v", x.name, err)
		}
		if st := u.Tables[x.table]; st != nil && st.Integrity != "" {
			if len(data) < 5 {
				t.Errorf("%s: integrity trailer missing", x.name)
				continue
			}
			data = data[:len(data)-4]
		}
		f := frameWire(data)
		if f.countAt == 0 {
			t.Errorf("%s: variable-form trailer did not frame", x.name)
			continue
		}
		fault := framingTableFault(framingVector{}, data)
		switch filepath.Base(x.path) {
		case "frame_unreferenced_entry.bin":
			if !strings.Contains(fault, "unused slots") || strings.Contains(fault, "first uses slot") {
				t.Errorf("%s: missing intentional unused entry: %s", x.name, fault)
			}
		case "rewrite_ids_reversed.bin":
			if !strings.Contains(fault, "first uses slot") || strings.Contains(fault, "unused slots") {
				t.Errorf("%s: missing intentional ID order: %s", x.name, fault)
			}
		default:
			if fault != "" {
				t.Errorf("%s (%s): %s", x.name, x.path, fault)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no readable file cases")
	}
}

// TestRewriteFieldOrderFixturePinned reverses the declared root fields of the
// canonical Fleet file and then numbers all IDs by their actual first use.
func TestRewriteFieldOrderFixturePinned(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	base, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/w1_fleet.bin"))
	if err != nil {
		t.Fatal(err)
	}
	f := frameWire(base)
	var fields []wireField
	var node *wireField
	for _, field := range f.fields {
		if len(field.enclosing) != 0 {
			continue
		}
		ref, _ := readLeb(base, field.start, f.bodyEnd)
		id, ok := f.entryOf(ref)
		if !ok {
			t.Fatal("unresolved root field")
		}
		if id == ir.TableNodeWireId {
			copy := field
			node = &copy
		} else {
			fields = append(fields, field)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].start < fields[j].start })
	if len(fields) != 3 || node == nil || f.bodyEnd < 1 || base[f.bodyEnd-1] != 0 {
		t.Fatal("canonical Fleet fixture shape changed")
	}
	body := []byte{base[0]}
	for i := len(fields) - 1; i >= 0; i-- {
		body = append(body, base[fields[i].start:fields[i].end]...)
	}
	body = append(body, base[node.start:node.end]...)
	body = append(body, 0)
	body = append(body, base[f.entriesAt:]...)
	want, err := firstUseOrder(body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/rewrite_fields_reversed.bin")
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
		t.Error("field-order fixture differs from its first-use-ordered generator")
	}
}
