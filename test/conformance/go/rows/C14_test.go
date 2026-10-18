package main

import (
	"encoding/binary"
	"testing"

	"tblv2"
)

func TestRowC14(t *testing.T) {
	// The bounds pass walks LIVE elements only: a counted array's live count,
	// not its slack; an optional's payload only when present
	// (docs/FIXED-FORM-ALGORITHM.md §4.6).
	//
	// Build a Cfg with Mode out of range, ItemsCount=1 and Items[0] out of
	// range, and Items[4] out of range but BEYOND the live count.  After
	// save+load the slack element Items[4] must retain its original value,
	// because the bounds pass only walks the live count.
	//
	// Wire layout (CfgFixedWriteBody):
	//   [0..4)   a      float32
	//   [4..5)   c      bool
	//   [5..6)   Mode   uint8
	//   [6..10)  title(string)  length int32
	//   [10..42) title bytes
	//   [42..50) inner 8 bytes
	//   [50..54) ItemsCount int32           (count)
	//   [54..86) Items      [8]int32        (all 8 slots, live and slack)
	//   etc.

	var v tblv2.Cfg
	tblv2.CfgReset(&v)

	// Save in-range values: the writer now refuses what its reader would
	// clamp, so the hostile storage values are forged into the saved bytes
	// below (C12 forges a bool byte the same way).
	v.Mode = tblv2.ModeBeta // in range; forged out of range below
	v.ItemsCount = 1
	v.Items[0] = 7   // in range; forged out of range below
	v.Items[4] = 500 // ranges 0..255; in SLACK — must NOT clamp, forged below

	buf := make([]byte, tblv2.CfgFixedMeasure(1))
	n := tblv2.CfgFixedSave([]tblv2.Cfg{v}, buf)
	if n < 0 {
		t.Fatal("CfgFixedSave failed")
	}
	data := buf[:n]

	// Three storage values out of range. Items[4] is in the slack
	// (ItemsCount=1), so the pass must NOT clamp it.
	bodyAt := tblv2.TableFixedHeaderBytes + 4 + len(tblv2.CfgFixedLayout) + 8
	body := data[bodyAt : bodyAt+tblv2.CfgFixedBodyBytes]
	body[5] = 99                                  // enum past {Alpha=1, Beta=2}; will clamp
	binary.LittleEndian.PutUint32(body[54:], 300) // ranges 0..255; will clamp (live)
	binary.LittleEndian.PutUint32(body[70:], 500) // ranges 0..255; SLACK — must NOT clamp

	plan := make([]tblv2.TableFixedEntry, 64)
	var scratch [1]tblv2.Cfg
	var report tblv2.TableReport
	scratch[0] = tblv2.Cfg{}
	got := tblv2.CfgFixedLoad(scratch[:], data, plan, &report)
	if got != 1 {
		t.Fatalf("CfgFixedLoad: got %d records, want 1", got)
	}
	if report.Malformed {
		t.Fatal("CfgFixedLoad: malformed")
	}
	if report.Verdict != tblv2.TableOpenOk {
		t.Fatal("CfgFixedLoad: refused")
	}

	// The LIVE count ItemsCount=1 means ONE item was walked.  Items[0]=300
	// should be clamped to 255.  Items[4]=500 is in the SLACK (index >=
	// ItemsCount) so it must NOT be clamped and must retain its value.
	// Mode=99 is clamped to ModeNone (=0).
	if report.Clamped != 2 {
		t.Fatalf("clamped=%d, want 2 (Mode=99->ModeNone=%d, Items[0]=%d->255; Items[4]=%d must NOT clamp)",
			report.Clamped, tblv2.ModeNone, v.Items[0], v.Items[4])
	}
	if scratch[0].Mode != tblv2.ModeNone {
		t.Fatalf("Mode=%d, want %d (ModeNone, clamped)", scratch[0].Mode, tblv2.ModeNone)
	}
	if scratch[0].Items[0] != 255 {
		t.Fatalf("Items[0]=%d, want 255 (clamped)", scratch[0].Items[0])
	}
	if scratch[0].Items[4] != 500 {
		t.Fatalf("Items[4]=%d, want 500 (slack, must NOT be clamped)", scratch[0].Items[4])
	}
	if scratch[0].ItemsCount != 1 {
		t.Fatalf("ItemsCount=%d, want 1 (live count preserved)", scratch[0].ItemsCount)
	}
}

func TestRowC14WriterRefusesOutOfRange(t *testing.T) {
	t.Parallel()
	v := tblv2.Cfg{}
	tblv2.CfgReset(&v)
	v.Mode = 99 // enum past {Alpha=1, Beta=2}
	v.ItemsCount = 1
	v.Items[0] = 300 // ranges 0..255 (live)
	v.Items[4] = 500 // ranges 0..255 (slack)

	buf := make([]byte, tblv2.CfgFixedMeasure(1))
	n, reason := tblv2.CfgFixedSaveReason([]tblv2.Cfg{v}, buf)
	if n >= 0 {
		t.Fatalf("CfgFixedSave accepted Mode=99 and Items[0]=300; want refusal")
	}
	if reason != tblv2.TableRefuseInvalidValue {
		t.Fatalf("CfgFixedSave refused with %v, want %v", reason, tblv2.TableRefuseInvalidValue)
	}
	t.Logf("C14: the writer refuses the value the row used to save: %v", reason)
}
