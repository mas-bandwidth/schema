package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"tblv2"
)

func TestRowC14(t *testing.T) {
	t.Parallel()
	// The bounds pass walks LIVE elements only: a counted array's live count,
	// not its slack; an optional's payload only when present
	// (docs/FIXED-FORM-ALGORITHM.md §4.6).
	//
	// Build a Cfg with ItemsCount=1, then forge Items[4] out of range BEYOND
	// the live count: the non-zero dead byte refuses before bounds are walked.
	// The clean control still checks the live value.
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

	// Dead slots must be zero; live elements and enum bytes keep their own
	// value checks (docs/SPEC-TABLES.md §3.4, §4). Each mutation is independent.
	bodyAt := tblv2.TableFixedHeaderBytes + 4 + len(tblv2.CfgFixedLayout) + 8
	plan := make([]tblv2.TableFixedEntry, 64)
	var sentinel tblv2.Cfg
	tblv2.CfgReset(&sentinel)
	sentinel.ItemsCount = 2
	sentinel.Items[0] = 77
	read := func(forge func(body []byte)) (tblv2.Cfg, int64, tblv2.TableReport) {
		wire := append([]byte(nil), data...)
		forge(wire[bodyAt : bodyAt+tblv2.CfgFixedBodyBytes])
		scratch := [1]tblv2.Cfg{sentinel}
		var report tblv2.TableReport
		got := tblv2.CfgFixedLoad(scratch[:], wire, plan, &report)
		return scratch[0], got, report
	}
	clean, got, report := read(func([]byte) {})
	if got != 1 || report.Err() != nil || clean.ItemsCount != 1 || clean.Items[0] != 7 || clean.Items[4] != 0 {
		t.Fatalf("clean live-extent control: n=%d report=%+v value=%+v", got, report, clean)
	}
	again := make([]byte, len(data))
	if tblv2.CfgFixedSave([]tblv2.Cfg{clean}, again) != int64(len(again)) || !bytes.Equal(again, data) {
		t.Fatal("clean control did not save byte identically")
	}
	slack := func(body []byte) { binary.LittleEndian.PutUint32(body[70:], 500) }
	if value, got, report := read(slack); got >= 0 || report.Err() == nil || !reflect.DeepEqual(value, sentinel) {
		t.Fatalf("non-zero dead slot read: n=%d report=%+v", got, report)
	}
	live := func(body []byte) { binary.LittleEndian.PutUint32(body[54:], 300) }
	if _, got, report := read(live); got != -1 || report.Err() == nil {
		t.Fatalf("live element out of range read: got=%d %+v", got, report)
	}
	mode := func(body []byte) { body[5] = 99 }
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
