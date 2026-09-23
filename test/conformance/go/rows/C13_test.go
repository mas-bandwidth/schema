package main

import (
	"testing"

	"tblp3"
)

// go/C13: "present flag byte != 0" (docs/FIXED-FORM-ALGORITHM.md:337 — "a `bool` or a
// PRESENT FLAG lands as `byte != 0`, normalised to the language's own true. `0x02` is
// not a bool a reader stores verbatim"). tblp3 fixed table Chain carries `link ?Link`,
// so a presence byte beside the inline record (test/tables/P3.schema). The vector is
// built from the law: the same value with the flag false and true differs in exactly
// one byte, the presence byte; forge 0x02 there and the reader must land Go's true.
func TestRowC13(t *testing.T) {
// zero link body keeps the record payload identical in both saves, so the
	// only differing byte is the presence byte itself
	link := tblp3.Link{}
	saveFile := func(present bool) []byte {
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
	offFile := saveFile(false)
	onFile := saveFile(true)

	off, diffs := -1, 0
	for i := range onFile {
		if offFile[i] != onFile[i] {
			off, diffs = i, diffs+1
		}
	}
	if diffs != 1 || off < 0 {
		t.Fatalf("presence byte not isolated: %d differing bytes", diffs)
	}
	if onFile[off] != 1 {
		t.Fatalf("presence byte was not the flag bit (got 0x%x)", onFile[off])
	}

	data := onFile
	data[off] = 0x02 // a byte that is not 0/1: the law says it lands as true

	var scratch [1]tblp3.Chain
	plan := make([]tblp3.TableFixedEntry, 64)
	var report tblp3.TableReport
	n := tblp3.ChainFixedLoad(scratch[:], data, plan, &report)
	if n != 1 || report.Malformed || report.Verdict != tblp3.TableOpenOk {
		t.Fatalf("load: n=%d malformed=%v verdict=%v", n, report.Malformed, report.Verdict)
	}
	if scratch[0].LinkPresent != true {
		t.Errorf("C13: presence byte 0x02 at file offset %d did not normalise to true", off)
	} else {
		t.Logf("C13: forged presence byte 0x02 at file offset %d normalised to true (byte != 0)", off)
	}
}
