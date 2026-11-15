package main

import (
	"fmt"
	"os"
	"path/filepath"
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
// judge.
func TestFramingVectorIdTablesAreCanonical(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	vectors := append(append([]framingVector{}, framingVectors...), noWriterVectors...)
	for _, v := range vectors {
		if _, exempt := framingNonCanonicalTables[v.name]; exempt {
			continue
		}
		if v.count != nil {
			continue // the entry count is the defect: the table does not frame
		}
		data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		if fault := framingTableFault(v, data); fault != "" {
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
