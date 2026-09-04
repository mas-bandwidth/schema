package main

import (
	"os"
	"sort"
	"testing"
)

// FIRST-USE ORDER, ELISION AND DISTINCTNESS, over every pinned instance
// (docs/SPEC-TABLES.md §3). The id table holds every id the body used, ONCE
// EACH, in first-use order over the whole wire — root body first, then the node
// table's records in index order — and an id no body references is never
// written at all.
//
// The byte-for-byte compare of a saved instance against its pinned wire is what
// holds a writer to this; this is the instrument that says WHICH of the three
// rules a drifted wire broke.
func TestTheIdTableIsFirstUseOrderAndNothingElse(t *testing.T) {
	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, inst := range m.Instances {
		data, err := os.ReadFile(root + inst.Wire)
		if err != nil {
			t.Errorf("%s: %v", inst.Name, err)
			continue
		}
		f := frameWire(data)
		if len(f.entries) == 0 {
			continue // a wire that names no id at all: nothing to order
		}
		// THE ENTRIES ARE DISTINCT: an id already in the table is referenced
		// again and never appended twice
		seen := map[uint64]int{}
		for i, id := range f.entries {
			if prev, dup := seen[id]; dup {
				t.Errorf("%s: entries %d and %d carry one id %016x", inst.Name, prev+1, i+1, id)
			}
			seen[id] = i
		}
		// and they are in FIRST-USE order, with nothing the body never named
		order := map[uint64]bool{}
		next := uint64(1)
		for _, sp := range f.spots {
			switch sp.kind {
			case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
			default:
				continue
			}
			if sp.value == 0 || order[sp.value] {
				continue // the zero reference names no id; a repeat is not a first use
			}
			order[sp.value] = true
			if sp.value != next {
				t.Errorf("%s: the reference at byte %d first uses entry %d where first-use order expects %d",
					inst.Name, sp.off, sp.value, next)
				break
			}
			next++
		}
		if int(next)-1 != len(f.entries) {
			var unused []int
			for i := range f.entries {
				if !order[uint64(i)+1] {
					unused = append(unused, i+1)
				}
			}
			sort.Ints(unused)
			t.Errorf("%s: the table carries %d entries and the body names %d — an ELIDED field costs no entry (entries %v are named by nothing)",
				inst.Name, len(f.entries), int(next)-1, unused)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no instance was checked at all — the instrument is watching nothing")
	}
	t.Logf("first-use order, distinctness and elision hold over %d pinned instances", checked)
}
