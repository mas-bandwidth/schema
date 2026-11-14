// W4 — every dead byte is zero on read (docs/SPEC-TABLES.md §3.4, THE SLACK
// RULE; docs/FIXED-FORM-ALGORITHM.md §3.1; docs/rulings.json
// fixed-dead-bytes-zero).
//
// The owner, 2026-11-10: "Every dead byte", "Be strict". The bytes behind a
// counted array's live count are slack, a conforming writer leaves them zero,
// and a reader refuses the whole record (wire_damaged) when one is not, with
// nothing landed and no counter moved. The row pinned the retired rule
// (slack unspecified on read, stained slack read) until the ruling; it now
// pins the rule that replaced it. This is the Go leg's assertion of go/W4.
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

	load := func(wire []byte) (tblv1.Cfg, int64, tblv1.TableReport) {
		var back [1]tblv1.Cfg
		var rep tblv1.TableReport
		plan := make([]tblv1.TableFixedEntry, tblv1.CfgFixedPlan.Count)
		copy(plan, tblv1.CfgFixedPlan.Entries)
		read := tblv1.CfgFixedLoad(back[:], wire, plan, &rep)
		return back[0], read, rep
	}

	// THE CONTROL: the writer's zeros in the slack read, and the USED UNITS
	// land: ItemsCount=1, Items[0]=7.
	back, read, rep := load(save)
	if read != 1 || rep.Refused() {
		t.Fatalf("the clean record: CfgFixedLoad returned %d %+v, want 1", read, rep)
	}
	if back.ItemsCount != 1 || back.Items[0] != 7 {
		t.Fatalf("the used units did not land: count=%d items[0]=%d", back.ItemsCount, back.Items[0])
	}

	// STAIN THE SLACK, one byte at a time at both ends and once whole: a byte
	// of 1 is an in-range element if it were live, so the zero rule is the only
	// rule that can refuse it.
	for _, at := range []int{slackStart, slackEnd - 1} {
		stained := append([]byte(nil), save...)
		stained[at] = 1
		if _, read, rep := load(stained); read != -1 || rep.Reason != "wire_damaged" {
			t.Errorf("slack byte %d stained: CfgFixedLoad returned %d %+v, want the record refused wire_damaged", at-bodyStart, read, rep)
		} else if rep.Unknown != 0 || rep.Widened != 0 {
			t.Errorf("slack byte %d stained: the refusal moved a counter: %+v", at-bodyStart, rep)
		}
	}
	stained := append([]byte(nil), save...)
	for i := slackStart; i < slackEnd; i++ {
		stained[i] = 0xFF
	}
	if _, read, rep := load(stained); read != -1 || rep.Reason != "wire_damaged" {
		t.Errorf("the whole slack stained: CfgFixedLoad returned %d %+v, want the record refused wire_damaged", read, rep)
	}
}
