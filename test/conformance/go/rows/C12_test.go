package main

import (
	"testing"

	"tblm1"
)

// go/C12: a bool byte other than 0 or 1 refuses the fixed read (docs/FIXED-FORM-ALGORITHM.md
// §4.6, docs/SPEC-TABLES.md §4). tblm1 fixed table Save carries a plain `force bool`
// (test/tables/M1.schema). The vector is built from the law: the same value saved with
// force false and true differs in exactly one byte, the force byte; 0x01 there reads true,
// and 0x02 forged there refuses.
func TestRowC12(t *testing.T) {
	saveFile := func(force bool) []byte {
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
	offFile := saveFile(false)
	onFile := saveFile(true)

	off, diffs := -1, 0
	for i := range onFile {
		if offFile[i] != onFile[i] {
			off, diffs = i, diffs+1
		}
	}
	if diffs != 1 || off < 0 {
		t.Fatalf("force byte not isolated: %d differing bytes", diffs)
	}
	if onFile[off] != 1 {
		t.Fatalf("force byte was not the flag bit (got 0x%x)", onFile[off])
	}

	var scratch [1]tblm1.Save
	plan := make([]tblm1.TableFixedEntry, 64)
	var report tblm1.TableReport
	if n := tblm1.SaveFixedLoad(scratch[:], onFile, plan, &report); n != 1 || report.Err() != nil || !scratch[0].Force {
		t.Fatalf("C12: byte 0x01: n=%d force=%v %+v", n, scratch[0].Force, report)
	}
	onFile[off] = 0x02
	report = tblm1.TableReport{}
	if n := tblm1.SaveFixedLoad(scratch[:], onFile, plan, &report); n != -1 || report.Err() == nil {
		t.Fatalf("C12: force byte 0x02 at file offset %d read: n=%d %+v", off, n, report)
	}
}
