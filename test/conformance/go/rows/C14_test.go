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
	// Build a Cfg with ItemsCount=1, then forge Items[4] out of range BEYOND
	// the live count: the read keeps the slack value, because the bounds pass
	// only walks the live count.
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

	// Save in-range values: the writer refuses what its reader would refuse,
	// so the hostile storage values are forged into the saved bytes below.
	v.Mode = tblv2.ModeBeta // in range; forged out of range below
	v.ItemsCount = 1
	v.Items[0] = 7 // in range; forged out of range below
	v.Items[4] = 9 // ranges 0..255; in SLACK, forged out of range below

	buf := make([]byte, tblv2.CfgFixedMeasure(1))
	n := tblv2.CfgFixedSave([]tblv2.Cfg{v}, buf)
	if n < 0 {
		t.Fatal("CfgFixedSave failed")
	}
	data := buf[:n]

	// Items[4] out of range in the slack (ItemsCount=1) is not walked, and the
	// file reads with the slack value kept; the same value in a live element,
	// or an enum byte past its variants, refuses (docs/SPEC-TABLES.md §4).
	bodyAt := tblv2.TableFixedHeaderBytes + 4 + len(tblv2.CfgFixedLayout) + 8
	plan := make([]tblv2.TableFixedEntry, 64)
	read := func(forge func(body []byte)) (tblv2.Cfg, int64, tblv2.TableReport) {
		wire := append([]byte(nil), data...)
		forge(wire[bodyAt : bodyAt+tblv2.CfgFixedBodyBytes])
		var scratch [1]tblv2.Cfg
		var report tblv2.TableReport
		got := tblv2.CfgFixedLoad(scratch[:], wire, plan, &report)
		return scratch[0], got, report
	}
	slack := func(body []byte) { binary.LittleEndian.PutUint32(body[70:], 500) }
	if v, got, report := read(slack); got != 1 || report.Err() != nil || v.Items[4] != 500 || v.ItemsCount != 1 || v.Items[0] != 7 {
		t.Fatalf("slack out of range: got=%d %+v items=%v count=%d", got, report, v.Items, v.ItemsCount)
	}
	live := func(body []byte) { slack(body); binary.LittleEndian.PutUint32(body[54:], 300) }
	if _, got, report := read(live); got != -1 || report.Err() == nil {
		t.Fatalf("live element out of range read: got=%d %+v", got, report)
	}
	mode := func(body []byte) { slack(body); body[5] = 99 }
	if _, got, report := read(mode); got != -1 || report.Err() == nil {
		t.Fatalf("enum byte past its variants read: got=%d %+v", got, report)
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
