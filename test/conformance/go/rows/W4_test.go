// W4 — read slack unspecified (docs/FIXED-FORM-ALGORITHM.md:347).
//
// "It walks only what a read can have written — a counted array's LIVE
// elements and never its slack."  The bounds pass over a fixed table's
// STORAGE iterates a counted array's live count, not its declared capacity,
// so bytes behind the live count on the wire — even bytes past the ranged
// element's clamped range — must move no counter and must not be malformed.
// This is the Go leg's assertion of go/W4.
package main

import (
	"testing"

	tblv1 "tblv1"
)

func TestRowW4(t *testing.T) {
	// ONE LIVE ELEMENT: ItemsCount=1, Items[0]=7 (in [0,255], so no clamp).
	var v1 tblv1.Cfg
	tblv1.CfgReset(&v1)
	v1.ItemsCount = 1
	v1.Items[0] = 7

	// Save it — we get back a fixed-form buffer whose slack is zeroed by
	// CfgFixedSave's clear().
	save := make([]byte, tblv1.CfgFixedMeasure(1))
	n := tblv1.CfgFixedSave([]tblv1.Cfg{v1}, save)
	if n < 0 {
		t.Fatal("save failed")
	}
	save = save[:n]

	// The body starts after the fixed header (16), the layout length (4), and
	// the layout itself, then the record hash (8).  Inside the body,
	// CfgFixedWriteBody puts ItemsCount at offset 49, then Items[0..7] at
	// offset 53 (8×4=32 bytes, ending at 85).  Items[1..7] (slack when
	// ItemsCount=1) is bytes 57..84 inside the body.
	headerAndLayout := 16 + 4 + len(tblv1.CfgFixedLayout)
	bodyStart := headerAndLayout + 8
	itemsCountAt := bodyStart + 49
	slackStart := bodyStart + 57 // items[1] begins
	slackEnd := bodyStart + 85   // items[8] ends

	// Confirm the count really is 1 on the wire (little-endian 4 bytes).
	if save[itemsCountAt] != 1 || save[itemsCountAt+1] != 0 || save[itemsCountAt+2] != 0 || save[itemsCountAt+3] != 0 {
		t.Fatal("wire count is not 1 — can't find ItemsCount offset")
	}

	// STAIN THE SLACK: every slack int32 gets 0xFF — a value past the
	// declared [0,255] range that a bounds pass walking the full array would
	// clamp.
	for i := slackStart; i < slackEnd; i++ {
		save[i] = 0xFF
	}

	// Verify the stain actually took.
	var stained int
	for i := slackStart; i < slackEnd; i++ {
		if save[i] == 0xFF {
			stained++
		}
	}
	if stained != slackEnd-slackStart {
		t.Fatal("stain verification failed: not all slack bytes are 0xFF")
	}

	// READ BACK with the identity plan.
	var back [1]tblv1.Cfg
	var rep tblv1.TableReport
	plan := make([]tblv1.TableFixedEntry, tblv1.CfgFixedPlan.Count)
	copy(plan, tblv1.CfgFixedPlan.Entries)
	read := tblv1.CfgFixedLoad(back[:], save, plan, &rep)
	if read != 1 {
		t.Fatalf("CfgFixedLoad returned %d, want 1", read)
	}

	// SLACK IS UNSPECIFIED: stained slack is neither malformed nor a refusal.
	if rep.Malformed {
		t.Error("malformed=true — stained slack must not be malformed")
	}
	if rep.Verdict != tblv1.TableOpenOk {
		t.Errorf("verdict=%v — stained slack must not be a refusal", rep.Verdict)
	}

	// The USED UNITS landed: ItemsCount=1, Items[0]=7.
	if back[0].ItemsCount != 1 {
		t.Errorf("ItemsCount=%d, want 1", back[0].ItemsCount)
	}
	if back[0].Items[0] != 7 {
		t.Errorf("Items[0]=%d, want 7", back[0].Items[0])
	}

	// The bounds pass saw only the LIVE count and clamped NOTHING.
	if rep.Clamped != 0 {
		t.Errorf("Clamped=%d — bounds pass clamped stained slack, but only live elements should be walked", rep.Clamped)
	}
}
