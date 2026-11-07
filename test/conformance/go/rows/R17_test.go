package main

import (
	"testing"

	"tblm1"
	"tblp3"
)

// go/R17: a bool or present byte that is not 0/1 refuses the fixed read
// (docs/FIXED-FORM-ALGORITHM.md §4.6, docs/SPEC-TABLES.md §4). Both sides of the pair are
// in scope here: a bool field (tblm1 Save.Force) and a presence byte (tblp3
// Chain.LinkPresent). The vectors use the same single-byte isolation as C12/C13 and forge
// 0x02.
func TestRowR17(t *testing.T) {
	// bool side — tblm1 Save.Force
	saveBool := func(force bool) []byte {
		values := []tblm1.Save{{
			Path:       [16]byte{'o', 'p', 'e', 'n'},
			PathLength: 4,
			Force:      force,
		}}
		buf := make([]byte, tblm1.SaveFixedMeasure(1))
		n := tblm1.SaveFixedSave(values, buf)
		if n < 0 {
			t.Fatal("SaveFixedSave failed")
		}
		return buf[:n]
	}
	boolOff, boolOn := saveBool(false), saveBool(true)
	bOff, bDiffs := -1, 0
	for i := range boolOn {
		if boolOff[i] != boolOn[i] {
			bOff, bDiffs = i, bDiffs+1
		}
	}
	if bDiffs != 1 || bOff < 0 || boolOn[bOff] != 1 {
		t.Fatalf("bool byte not isolated: %d differing bytes", bDiffs)
	}
	boolOn[bOff] = 0x02

	var bScratch [1]tblm1.Save
	bPlan := make([]tblm1.TableFixedEntry, 64)
	var bReport tblm1.TableReport
	if bN := tblm1.SaveFixedLoad(bScratch[:], boolOn, bPlan, &bReport); bN != -1 || bReport.Err() == nil {
		t.Fatalf("R17/bool: byte 0x02 read: n=%d %+v", bN, bReport)
	}

	// present-flag side — tblp3 Chain.LinkPresent
	// zero link body keeps the record payload identical in both saves, so the
	// only differing byte is the presence byte itself
	link := tblp3.Link{}
	savePresent := func(present bool) []byte {
		values := []tblp3.Chain{{
			Name:        [16]byte{'h', 'e', 'l', 'l', 'o'},
			NameLength:  5,
			Link:        link,
			LinkPresent: present,
		}}
		buf := make([]byte, tblp3.ChainFixedMeasure(1))
		n := tblp3.ChainFixedSave(values, buf)
		if n < 0 {
			t.Fatal("ChainFixedSave failed")
		}
		return buf[:n]
	}
	pOff, pOn := savePresent(false), savePresent(true)
	pByte, pDiffs := -1, 0
	for i := range pOn {
		if pOff[i] != pOn[i] {
			pByte, pDiffs = i, pDiffs+1
		}
	}
	if pDiffs != 1 || pByte < 0 || pOn[pByte] != 1 {
		t.Fatalf("present byte not isolated: %d differing bytes", pDiffs)
	}
	pOn[pByte] = 0x02

	var pScratch [1]tblp3.Chain
	pPlan := make([]tblp3.TableFixedEntry, 64)
	var pReport tblp3.TableReport
	if pN := tblp3.ChainFixedLoad(pScratch[:], pOn, pPlan, &pReport); pN != -1 || pReport.Err() == nil {
		t.Fatalf("R17/present: byte 0x02 read: n=%d %+v", pN, pReport)
	}
}
