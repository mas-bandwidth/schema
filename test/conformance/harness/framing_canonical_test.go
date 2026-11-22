package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// framingNonCanonicalTables are the vectors whose PURPOSE is a non-canonical id
// table, so the one defect they carry is the one this check looks for.
var framingNonCanonicalTables = map[string]string{
	// F7: the vector IS an entry no body references.
	"frame_unreferenced_entry": "F7: an entry no body references is the vector",
}

// TestFramingVectorIdTablesAreCanonical holds every pinned framing vector's id
// table, the no-writer vectors' included, to FIRST-USE ORDER
// (docs/SPEC-TABLES.md §3): the table carries the ids its body references, each
// in the slot of its first use, root body first and then the node table's
// records in index order, and nothing the body never names. A reader refuses
// any other table (§4, docs/rulings.json reads-refuse-non-canonical), so a
// vector built to exercise some other rule and carrying a stray or misplaced
// entry would be refused for the wrong reason. The references are located by
// the wire fuzzer's framing locator; a reference above the entry count names no
// entry, and a vector whose entry count is itself the defect has no table to
// judge. A vector in framingNonCanonicalTables is held to its one defect: an
// unused entry, its table otherwise in first-use order.
//
// Then every vector is held to ALL SIX spellings (judgeFramingSpellings).
func TestFramingVectorIdTablesAreCanonical(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	vectors := append(append([]framingVector{}, framingVectors...), noWriterVectors...)
	for _, v := range vectors {
		if v.count != nil {
			continue // the entry count is the defect: the table does not frame
		}
		data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		fault := framingTableFault(v, data)
		if _, purpose := framingNonCanonicalTables[v.name]; purpose {
			if !strings.HasPrefix(fault, "[unused slots") {
				t.Errorf("%s: id table %q, want only unused slots", v.name, fault)
			}
			continue
		}
		if fault != "" {
			t.Errorf("%s: %s", v.name, fault)
		}
	}
	for name := range framingNonCanonicalTables {
		found := false
		for _, v := range vectors {
			found = found || v.name == name
		}
		if !found {
			t.Errorf("%s: framingNonCanonicalTables names no vector", name)
		}
	}
	judgeFramingSpellings(t, root, vectors)
}

// framingTableFault is "" when the file's id table is the body's references in
// first-use order, and otherwise says which slots break it.
func framingTableFault(v framingVector, data []byte) string {
	f := frameWire(data)
	n := uint64(len(f.entries))
	if n == 0 {
		return ""
	}
	slot := func(ref uint64) string {
		if v.entries == nil && int(ref) <= len(v.ids) {
			return fmt.Sprintf("%d (%s)", ref, v.ids[ref-1])
		}
		return fmt.Sprintf("%d", ref)
	}
	used := map[uint64]bool{}
	next := uint64(1)
	var faults []string
	for _, sp := range f.spots {
		switch sp.kind {
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
		default:
			continue
		}
		if sp.value == 0 || sp.value > n || used[sp.value] {
			continue // names no entry, or is not a first use
		}
		used[sp.value] = true
		if sp.value != next && len(faults) == 0 {
			faults = append(faults, fmt.Sprintf("byte %d first uses slot %s where first-use order expects slot %s", sp.off, slot(sp.value), slot(next)))
		}
		next++
	}
	var unused []string
	for ref := uint64(1); ref <= n; ref++ {
		if !used[ref] {
			unused = append(unused, slot(ref))
		}
	}
	if len(unused) > 0 {
		faults = append(faults, fmt.Sprintf("unused slots %v", unused))
	}
	if len(faults) == 0 {
		return ""
	}
	return fmt.Sprint(faults)
}

// judgeFramingSpellings FAILS every framing vector that holds one of §4's
// non-canonical spellings (docs/SPEC-TABLES.md §4, docs/rulings.json
// reads-refuse-non-canonical) other than its named purpose
// (sharedFixturePurposes): fields out of declaration order, a field stored at
// its default, an explicit null pointer, an id-table entry unused, repeated or
// out of first-use order, an empty node table or one out of its place, and a
// union field written with arm 0. A strict reader refuses each of them, so a
// vector built to exercise some other rule and carrying one would be refused
// for the wrong reason.
//
// The walk is the byte-level one TestSharedFixturesCanonical uses
// (sharedFixtureSpellings over damageAwareFileSpellings, and idTableSpellings):
// it never calls the engine's read or write. A vector whose framing is damaged
// is judged on the prefix and siblings the walk can frame, which is where a
// strict reader meets a spelling before the damage.
func judgeFramingSpellings(t *testing.T, root string, vectors []framingVector) {
	t.Helper()
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
	judged := 0
	for _, v := range vectors {
		if v.unit == "" || v.root == "" {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(framingDir, v.name+".bin"))
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // the id-table pass above already says so
		}
		if len(data) == 0 || data[0] != 1 {
			continue // not a variable file: the form byte is the vector
		}
		unit, err := u.get(v.unit)
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		st := unit.Tables[v.root]
		if st == nil {
			t.Errorf("%s: no root %s", v.name, v.root)
			continue
		}
		if st.FixedDeclared {
			if unit, err = framingVariableUnit(m, v.unit, v.root); err != nil {
				t.Errorf("%s: %v", v.name, err)
				continue
			}
			st = unit.Tables[v.root]
		}
		if st.Integrity != "" && len(data) >= 5 {
			data = data[:len(data)-4]
		}
		shapes, err := sharedFixtureSpellings(unit, st, data)
		if _, damaged := errors.AsType[*fixtureDamage](err); err != nil && !damaged {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		ids, err := idTableSpellings(data)
		if _, damaged := errors.AsType[*fixtureDamage](err); err != nil && !damaged {
			t.Errorf("%s: id table: %v", v.name, err)
			continue
		}
		for shape := range ids {
			shapes[shape] = true
		}
		judgeSharedFixture(t, v.name, rel, sharedFixturePurposes[rel], shapes)
		judged++
	}
	if judged == 0 {
		t.Fatal("no framing vector judged for spellings")
	}
	t.Logf("SPELLING: %d framing vectors judged on all six spellings", judged)
}
