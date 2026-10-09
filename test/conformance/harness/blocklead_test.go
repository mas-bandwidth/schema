package main

import "testing"

// THE BASE-ALIGNMENT ROW (docs/SPEC-TABLES.md §19.2, docs/PORTING.md I5).
//
// The cook battery has held every port's base-alignment check since the cook
// landed — `cook_lead_1..63`, one row per residue. The block battery did not:
// every one of its rows was read out of an ALIGNED base, so a port whose
// `block_open` never checked the base could not be caught by it, which is
// exactly how the Elixir port's missing check stayed invisible (schema#369).
//
// schema#387 owes the reference that row. This test is its red test: the block
// battery must carry one image at a NONZERO pointer, the reference must name the
// clause that refuses it, and that clause is the base's alignment and no other.
func TestTheBlockBatteryHoldsTheBaseAlignment(t *testing.T) {
	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	unnamed := 0
	for _, f := range m.Forgeries {
		if f.Kind != "block" || f.Pointer == 0 {
			continue
		}
		unnamed++
		if f.Verdict != "refuse" {
			t.Errorf("%s: an unaligned base has verdict %q, wanted refuse", f.Name, f.Verdict)
			continue
		}
		reason := ""
		for _, r := range m.Refusals {
			if r.Forgery == f.Name {
				reason = r.Reason
			}
		}
		if reason != "unaligned_base" {
			t.Errorf("%s: the refusal is %q, wanted unaligned_base", f.Name, reason)
		}
	}
	if unnamed == 0 {
		t.Fatal("the block battery names no nonzero pointer: an unaligned base is not held by the reference")
	}
}
