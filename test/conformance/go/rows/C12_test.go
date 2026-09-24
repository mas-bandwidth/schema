package main

import (
	"testing"

	"tblm1"
)

// go/C12: "bool byte != 0" (docs/FIXED-FORM-ALGORITHM.md:337 — "a `bool` ... lands as
// `byte != 0`, normalised to the language's own true. `0x02` is not a bool a reader
// stores verbatim"). tblm1 fixed table Save carries a plain `force bool`
// (test/tables/M1.schema). The vector is built from the law: the same value saved
// with force false and true differs in exactly one byte, the force byte; forge 0x02
// there and the reader must land Go's own true.
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

	data := onFile
	data[off] = 0x02 // a byte that is not 0/1: the law says it lands as true

	var scratch [1]tblm1.Save
	plan := make([]tblm1.TableFixedEntry, 64)
	var report tblm1.TableReport
	n := tblm1.SaveFixedLoad(scratch[:], data, plan, &report)
	if n != 1 || report.Malformed || report.Verdict != tblm1.TableOpenOk {
		t.Fatalf("load: n=%d malformed=%v verdict=%v", n, report.Malformed, report.Verdict)
	}
	if scratch[0].Force != true {
		t.Errorf("C12: force byte 0x02 at file offset %d did not normalise to true", off)
	} else {
		t.Logf("C12: forged bool byte 0x02 at file offset %d normalised to true (byte != 0)", off)
	}
}
