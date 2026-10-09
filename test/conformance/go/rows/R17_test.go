package main

import (
	"testing"

	"tblm1"
	"tblp3"
)

// go/R17: "a bool or present byte that is not 0/1 normalises and counts nothing"
// (docs/FIXED-FORM-ALGORITHM.md:949-950 — "A `bool` or a present byte that is not `0`
// or `1` is normalised to the language's own true and **counts nothing** (bill
// §12.12)."). Both sides of the pair are in scope here: a bool field (tblm1
// Save.Force) and a presence byte (tblp3 Chain.LinkPresent). The vectors use the same
// single-byte isolation as C12/C13 and forge 0x02; the report beside a true value must
// keep every counter at zero.
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
	bN := tblm1.SaveFixedLoad(bScratch[:], boolOn, bPlan, &bReport)
	if bN != 1 {
		t.Fatalf("bool load: n=%d", bN)
	}
	if bScratch[0].Force != true {
		t.Errorf("R17/bool: byte 0x02 did not normalise to true")
	}
	if bReport.Malformed || bReport.Clamped != 0 || bReport.Widened != 0 ||
		bReport.Unknown != 0 || bReport.KindMismatch != 0 || bReport.Duplicate != 0 {
		t.Errorf("R17/bool: a not-0/1 byte counted: malformed=%v clamped=%d widened=%d unknown=%d kindmismatch=%d duplicate=%d",
			bReport.Malformed, bReport.Clamped, bReport.Widened, bReport.Unknown, bReport.KindMismatch, bReport.Duplicate)
	} else {
		t.Logf("R17: bool byte 0x02 normalised to true and every counter stayed at zero (counts nothing)")
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
	pN := tblp3.ChainFixedLoad(pScratch[:], pOn, pPlan, &pReport)
	if pN != 1 {
		t.Fatalf("present load: n=%d", pN)
	}
	if pScratch[0].LinkPresent != true {
		t.Errorf("R17/present: byte 0x02 did not normalise to true")
	}
	if pReport.Malformed || pReport.Clamped != 0 || pReport.Widened != 0 ||
		pReport.Unknown != 0 || pReport.KindMismatch != 0 || pReport.Duplicate != 0 {
		t.Errorf("R17/present: a not-0/1 byte counted: malformed=%v clamped=%d widened=%d unknown=%d kindmismatch=%d duplicate=%d",
			pReport.Malformed, pReport.Clamped, pReport.Widened, pReport.Unknown, pReport.KindMismatch, pReport.Duplicate)
	} else {
		t.Logf("R17: presence byte 0x02 normalised to true and every counter stayed at zero (counts nothing)")
	}
}
