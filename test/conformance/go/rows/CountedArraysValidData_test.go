package main

import (
	"testing"

	"tblv1"
)

func TestRowCountedArraysValidData(t *testing.T) {
	// Bounded arrays with a live count: valid-data write/read acceptance
	// (docs/SPEC-TABLES.md §3.4, the [Min..Max]T row).
	//
	// The law: a counted array writes a 4-byte i32 LE count then Max elements;
	// on read the count is clamped to [0, Max] and only the live elements are
	// decoded. The live count survives the round-trip.
	//
	// tblv1.Cfg carries two counted arrays:
	//   items  [..8]int32   — a bounded array of scalars
	//   grades [..4]Grade   — a bounded array of enum values

	items := []int32{10, 20, 30}
	grades := []tblv1.Grade{tblv1.GradeGold, tblv1.GradeBronze}

	cfg := tblv1.Cfg{
		A:           42,
		Mode:        tblv1.ModeAlpha,
		Grade:       tblv1.GradeGold,
		ItemsCount:  int32(len(items)),
		GradesCount: int32(len(grades)),
	}
	for i, v := range items {
		cfg.Items[i] = v
	}
	for i, v := range grades {
		cfg.Grades[i] = v
	}

	buf := make([]byte, tblv1.CfgFixedMeasure(1))
	n := tblv1.CfgFixedSave([]tblv1.Cfg{cfg}, buf)
	if n < 0 {
		t.Fatalf("CfgFixedSave failed: %d", n)
	}
	data := buf[:n]

	var plan [64]tblv1.TableFixedEntry
	var loaded [1]tblv1.Cfg
	var report tblv1.TableReport
	loaded[0] = tblv1.Cfg{}
	loadedN := tblv1.CfgFixedLoad(loaded[:], data, plan[:], &report)
	if loadedN != 1 {
		t.Fatalf("CfgFixedLoad returned n=%d, want 1", loadedN)
	}
	if report.Malformed {
		t.Fatal("CfgFixedLoad reported malformed")
	}
	if report.Verdict != tblv1.TableOpenOk {
		t.Fatalf("CfgFixedLoad verdict=%v, want TableOpenOk", report.Verdict)
	}

	// The live count survives the round-trip.
	if loaded[0].ItemsCount != int32(len(items)) {
		t.Errorf("ItemsCount=%d, want %d", loaded[0].ItemsCount, len(items))
	}
	if loaded[0].GradesCount != int32(len(grades)) {
		t.Errorf("GradesCount=%d, want %d", loaded[0].GradesCount, len(grades))
	}

	// The element values survive the round-trip.
	for i, want := range items {
		if int32(i) >= loaded[0].ItemsCount {
			break
		}
		if loaded[0].Items[i] != want {
			t.Errorf("Items[%d]=%d, want %d", i, loaded[0].Items[i], want)
		}
	}
	for i, want := range grades {
		if int32(i) >= loaded[0].GradesCount {
			break
		}
		if loaded[0].Grades[i] != want {
			t.Errorf("Grades[%d]=%v, want %v", i, loaded[0].Grades[i], want)
		}
	}
}
