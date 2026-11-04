package main

import (
	"testing"

	"tblp3"
)

// go/C13: a present flag byte other than 0 or 1 refuses the fixed read
// (docs/FIXED-FORM-ALGORITHM.md §4.6, docs/SPEC-TABLES.md §4). tblp3 fixed table Chain
// carries `link ?Link`, so a presence byte beside the inline record (test/tables/P3.schema).
// The vector is built from the law: the same value with the flag false and true differs in
// exactly one byte, the presence byte; 0x01 there reads present, and 0x02 forged there
// refuses.
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

	var scratch [1]tblp3.Chain
	plan := make([]tblp3.TableFixedEntry, 64)
	var report tblp3.TableReport
	if n := tblp3.ChainFixedLoad(scratch[:], onFile, plan, &report); n != 1 || report.Err() != nil || !scratch[0].LinkPresent {
		t.Fatalf("C13: byte 0x01: n=%d present=%v %+v", n, scratch[0].LinkPresent, report)
	}
	onFile[off] = 0x02
	report = tblp3.TableReport{}
	if n := tblp3.ChainFixedLoad(scratch[:], onFile, plan, &report); n != -1 || report.Err() == nil {
		t.Fatalf("C13: presence byte 0x02 at file offset %d read: n=%d %+v", off, n, report)
	}
}
