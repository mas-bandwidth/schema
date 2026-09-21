// E6: Renaming uses the declared identity (docs/SPEC-TABLES.md §5).
//
// When a TABLE is renamed under `was` — W2's `Ship | was = "Vessel"` — the
// node type id stays as fnv1a64("Vessel").  The generated descriptors carry
// both sides of that contract:
//   - Name: the DECLARED name ("Ship" in W2, "Vessel" in W1)
//   - Id:   the WIRE identity (fnv1a64("Vessel") for both)
// and data written by one generation loads into the other without error.
//
// Law: docs/SPEC-TABLES.md:7899 "A TABLE's own name is identity too …
// the table's type id is the hash of the OLD name".

package main

import (
	"os"
	"tblw1"
	"tblw2"
	"testing"
)

func TestRowE6(t *testing.T) {
	// --- ASSERTION 1: the descriptor carries the declared name, not the old one ---
	shipType := tblw2.ShipTableType()
	if shipType.Name != "Ship" {
		t.Fatalf("Name=%q, want %q (the declared name survives)", shipType.Name, "Ship")
	}

	// --- ASSERTION 2: the wire identity survives the rename ---
	vesselType := tblw1.VesselTableType()
	if shipType.Id != vesselType.Id {
		t.Fatalf("Id mismatch: Ship=%016x Vessel=%016x", shipType.Id, vesselType.Id)
	}

	// --- ASSERTION 3: cross-generation load works both directions ---
	wdir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	wireW1, err := os.ReadFile(wdir + "/../../../../testdata/wire/tables/w1_fleet.bin")
	if err != nil {
		t.Fatalf("fixture w1_fleet: %v", err)
	}

	var repW2 tblw2.TableReport
	fleetW2 := tblw2.FleetLoad(make([]byte, 1024), wireW1, &repW2)
	if fleetW2 == nil || repW2.Malformed {
		t.Fatalf("W2 cannot load W1's wire — renaming broke backward compat: %+v", repW2)
	}

	wireW2, err := os.ReadFile(wdir + "/../../../../testdata/wire/tables/w2_fleet.bin")
	if err != nil {
		t.Fatalf("fixture w2_fleet: %v", err)
	}

	var repW1 tblw1.TableReport
	fleetW1 := tblw1.FleetLoad(make([]byte, 1024), wireW2, &repW1)
	if fleetW1 == nil || repW1.Malformed {
		t.Fatalf("W1 cannot load W2's wire — forward compat failed: %+v", repW1)
	}
}
