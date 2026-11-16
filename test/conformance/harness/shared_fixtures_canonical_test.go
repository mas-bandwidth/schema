package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSharedFixturesCanonical inventories shared file, message and witness
// wires. Framing damage is reported separately: the walk checks its trustworthy
// prefix and independently framed siblings, but cannot certify a damaged suffix.
func TestSharedFixturesCanonical(t *testing.T) {
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
	type fileRow struct {
		name, unit, table, path string
		read, variable          bool
	}
	var files []fileRow
	for _, x := range m.Instances {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, !x.WireRefused, false})
	}
	for _, x := range m.Reports {
		verdict, ok := reports[x.Name]
		if !ok || (verdict != "read" && verdict != "refused") {
			t.Errorf("%s: missing or invalid report verdict %q", x.Name, verdict)
			continue
		}
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, verdict == "read", false})
	}
	for _, x := range m.Framings {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, !x.Refused, true})
	}
	for _, x := range m.Rewrites {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, true})
	}
	for _, x := range m.Retains {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, false})
	}
	for _, x := range m.JSONWriteRefusals {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, false})
	}
	for _, x := range m.WriteRefusals {
		unit := x.Unit
		if x.Form == "message" {
			c, e := m.LookupConnection(x.Unit)
			if e != nil {
				t.Fatal(e)
			}
			unit = c.Unit
		}
		files = append(files, fileRow{x.Name, unit, x.Root, x.Wire, true, false})
	}
	// These are assertions of named purposes, not exemptions.
	shapesWant := map[string]string{"testdata/wire/tables/framing/array_L2_empty.bin": "stored-default", "testdata/wire/tables/framing/union_arm_zero_in_field.bin": "union-arm-zero", "testdata/wire/tables/framing/node_index_null.bin": "explicit-null-pointer"}
	idsWant := map[string]string{"testdata/wire/tables/framing/frame_unreferenced_entry.bin": "unused ID-table entries", "testdata/wire/tables/rewrite_ids_reversed.bin": "first use of ID slot", "testdata/wire/tables/framing/frame_duplicate_entry.bin": "duplicate ID-table entry"}
	seen := map[string]bool{}
	foundPurposes := map[string]bool{}
	counts := map[string]int{}
	checkFile := func(x fileRow) {
		key := fmt.Sprintf("%s/%s/%s/variable=%t", x.unit, x.table, x.path, x.variable)
		if seen[key] {
			return
		}
		seen[key] = true
		path := x.path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		if len(data) == 0 {
			if x.read {
				t.Errorf("%s: empty readable file", x.name)
			} else {
				counts["empty-refused"]++
			}
			return
		}
		u, e := units.get(x.unit)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		st := u.Tables[x.table]
		if st == nil {
			t.Errorf("%s: missing root %s", x.name, x.table)
			return
		}
		if st.Integrity != "" && data[0] == 1 {
			if len(data) < 5 {
				t.Errorf("%s: integrity trailer missing", x.name)
				return
			}
			data = data[:len(data)-4]
		}
		if data[0] != 1 {
			if data[0] == 3 {
				counts["fixed"]++
				return
			}
			if x.read {
				t.Errorf("%s: unsupported form %d on readable input", x.name, data[0])
			} else {
				counts["refused-form"]++
			}
			return
		}
		counts["variable"]++
		fault, walkErr := damageAwareIDTableFault(data)
		var damage *fixtureDamage
		var unsupported *fixtureUnsupported
		if walkErr != nil {
			if errors.As(walkErr, &unsupported) {
				t.Errorf("%s: %v", x.name, walkErr)
				return
			}
			if !errors.As(walkErr, &damage) {
				t.Errorf("%s: %v", x.name, walkErr)
				return
			}
			counts["damaged-prefix-only"]++
			t.Logf("damage %s (%s): prefix ends byte %d; %v", x.name, x.path, damage.PrefixEnd, damage.Issues)
			if x.read {
				t.Errorf("%s: readable input has framing damage: %v", x.name, walkErr)
			}
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		purpose := filepath.ToSlash(rel)
		wantID := idsWant[purpose]
		if wantID != "" || shapesWant[purpose] != "" {
			foundPurposes[purpose] = true
		}
		if wantID != "" {
			if !strings.Contains(fault, wantID) {
				t.Errorf("%s: ID fault=%q, want %q", x.name, fault, wantID)
			}
		} else if fault != "" {
			t.Errorf("%s (%s): %s", x.name, x.path, fault)
		}
		// Framing rows deliberately exercise the variable surface even when
		// their declared type also has a fixed file representation.
		if st.FixedDeclared && x.variable {
			u, e = framingVariableUnit(m, x.unit, x.table)
			if e != nil {
				t.Errorf("%s: %v", x.name, e)
				return
			}
			st = u.Tables[x.table]
			counts["framing-variable"]++
		} else if st.FixedDeclared {
			counts["previous-form"]++
			if x.read {
				t.Errorf("%s: fixed reader receives variable form", x.name)
			}
			return
		}
		shapes, e := damageAwareFileSpellings(u, st, data)
		if e != nil {
			if !errors.As(e, &damage) {
				t.Errorf("%s: %v", x.name, e)
				return
			}
		}
		want := shapesWant[purpose]
		if want != "" {
			if len(shapes) != 1 || !shapes[want] {
				t.Errorf("%s: shapes=%v, want %s", x.name, shapes, want)
			}
		} else if len(shapes) > 0 {
			t.Errorf("%s (%s): shapes=%v", x.name, x.path, shapes)
		}
	}
	// Canonical output files are fixtures too: read every declared save and
	// rewrite, and every file side of a message row.
	for _, x := range m.Reports {
		if reports[x.Name] == "read" {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, false})
		}
	}
	for _, x := range m.Framings {
		if !x.Refused {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, true})
		}
	}
	for _, x := range m.Rewrites {
		files = append(files, fileRow{x.Name + "/canonical", x.Unit, x.Root, x.Canonical, true, true})
	}
	for _, x := range m.Retains {
		if x.Saves != "" {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Saves, true, false})
		}
	}
	for _, x := range m.Hostiles {
		if !x.Refused {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, false})
		}
	}
	for _, x := range m.Messages {
		files = append(files, fileRow{x.Name + "/file", x.Unit, x.Root, x.FileWire, true, false})
	}
	for _, c := range m.Connections {
		announcement, e := os.ReadFile(filepath.Join(root, c.Wire))
		if e != nil {
			t.Errorf("%s announcement: %v", c.Key, e)
			continue
		}
		if _, _, e := sharedMessageAnnouncement(announcement); e != nil {
			t.Errorf("%s announcement: %v", c.Key, e)
		} else {
			counts["announcements"]++
		}
	}
	// The ID-order refusal remains pinned even when its old rewrite row is
	// removed by the strict engine integration.
	files = append(files, fileRow{"rewrite_ids_reversed/purpose", "tblw1", "Fleet", "testdata/wire/tables/rewrite_ids_reversed.bin", false, true})
	// Aggregate repeated surface rows before walking so a refused occurrence
	// cannot hide another row that requires the same bytes to read.
	rows := map[string]fileRow{}
	for _, x := range files {
		key := fmt.Sprintf("%s/%s/%s/variable=%t", x.unit, x.table, x.path, x.variable)
		if old, ok := rows[key]; ok {
			old.read = old.read || x.read
			rows[key] = old
		} else {
			rows[key] = x
		}
	}
	for _, x := range rows {
		checkFile(x)
	}
	index, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors/INDEX.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(index), "\n") {
		p := strings.Fields(line)
		if len(p) == 0 || strings.HasPrefix(p[0], "#") {
			continue
		}
		if len(p) == 4 {
			checkFile(fileRow{p[0], p[1], p[2], p[3], false, false})
			continue
		}
		if len(p) != 5 || p[4] != "message" {
			t.Errorf("bad fuzz index row: %s", line)
			continue
		}
		data, e := os.ReadFile(filepath.Join(root, p[3]))
		if e != nil {
			t.Error(e)
			continue
		}
		u, e := units.get(p[1])
		if e != nil {
			t.Error(e)
			continue
		}
		st := u.Tables[p[2]]
		if st == nil {
			t.Errorf("%s missing root", p[0])
			continue
		}
		var conn string
		for _, c := range m.Connections {
			if c.Unit == p[1] {
				if conn != "" {
					t.Errorf("%s has multiple announcements", p[0])
				}
				conn = c.Wire
			}
		}
		if conn == "" {
			t.Errorf("%s has no announcement", p[0])
			continue
		}
		announce, e := os.ReadFile(filepath.Join(root, conn))
		if e != nil {
			t.Error(e)
			continue
		}
		shapes, e := messageStructuralSpellings(u, st, announce, data)
		var damage *sharedMessageDamage
		var unsupported *sharedMessageUnsupported
		if e != nil {
			if errors.As(e, &unsupported) {
				t.Errorf("%s unsupported message: %v", p[0], e)
			} else if errors.As(e, &damage) {
				counts["damaged-message-unclassified"]++
				t.Logf("message damage %s: %v", p[0], e)
			} else {
				t.Errorf("%s: %v", p[0], e)
			}
			continue
		}
		if len(shapes) > 0 {
			t.Errorf("%s: message shapes=%v", p[0], shapes)
		}
		counts["message-index"]++
	}
	for _, sub := range []struct {
		name string
		fn   func(*testing.T)
	}{{"messages", TestSharedMessageManifestSpellings}, {"message-purposes", TestSharedMessagePurposeSpellings}, {"blob-witnesses", TestSharedBlobWitnessSpellings}, {"variable-purposes", TestNoncanonicalVariablePurposeShapes}, {"framing-purposes", TestFramingPurposeSpellingsRemain}} {
		t.Run(sub.name, sub.fn)
	}
	for path := range idsWant {
		if !foundPurposes[path] {
			t.Errorf("missing ID purpose %s", path)
		}
	}
	for path := range shapesWant {
		if !foundPurposes[path] {
			t.Errorf("missing spelling purpose %s", path)
		}
	}
	if len(seen) == 0 {
		t.Fatal("empty shared fixture inventory")
	}
	t.Logf("inventoried %d unit/root/files: %s", len(seen), fmt.Sprint(counts))
}
