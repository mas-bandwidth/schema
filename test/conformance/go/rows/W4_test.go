// W4 — only live elements are read as values; every dead byte is zero
// (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3). A non-zero unused array slot
// refuses the whole load before any destination value changes.
package main

import (
	"bytes"
	"reflect"
	"testing"

	tblv1 "tblv1"
)

func TestRowW4(t *testing.T) {
	t.Parallel()
	// ONE LIVE ELEMENT: ItemsCount=1, Items[0]=7 (inside [0,255]).
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

	// The clean writer image reads its live value and saves byte identically.
	var back [1]tblv1.Cfg
	var rep tblv1.TableReport
	plan := make([]tblv1.TableFixedEntry, tblv1.CfgFixedPlan.Count)
	if got := tblv1.CfgFixedLoad(back[:], save, plan, &rep); got != 1 || rep.Err() != nil || back[0].ItemsCount != 1 || back[0].Items[0] != 7 {
		t.Fatalf("clean control: n=%d report=%+v value=%+v", got, rep, back[0])
	}
	again := make([]byte, len(save))
	if tblv1.CfgFixedSave(back[:], again) != int64(len(again)) || !bytes.Equal(again, save) {
		t.Fatal("clean control did not save byte identically")
	}

	// STAIN THE SLACK: every slack int32 gets 0xFF — a value past the
	// declared [0,255] range that a bounds pass walking the full array would
	// refuse if it were live.
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

	// The dead-slot check refuses before the batch lands.
	back[0].ItemsCount = 3
	back[0].Items[0] = 23
	before := back
	if got := tblv1.CfgFixedLoad(back[:], save, plan, &rep); got >= 0 || rep.Err() == nil {
		t.Fatalf("stained dead slots read: n=%d report=%+v", got, rep)
	}
	if !reflect.DeepEqual(back, before) {
		t.Fatal("dead-slot refusal changed the destination")
	}
}
