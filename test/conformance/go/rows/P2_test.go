package main

// go/P2 — "write-read-write byte-identical" (docs/roadmap.sexp, cell
// interoperability/go, "Shared byte oracle and round-trip conformance").
//
// THE LAW. The reference's own statement of the write round trip,
// test/tables/fixedform_dump.cpp: "THE WRITE: read the file, save the values
// back, and the bytes must be identical. That reaches every field: a byte a
// port encodes differently is a byte that does not come back." The Go leg's
// byte gate holds the same clause twice (make/go.mk:536-537, the audit's P2
// evidence, bench/tables/go/table_main.go:414/421): "writes them back and
// compares the WHOLE FILE, byte for byte, twice more into reused storage",
// and the reused half's reason: "The load owns the prefill, so nothing is
// reset between the two passes." SPEC-TABLES.md's round-trip row names the
// standard the same way (§3.3's battery: "Red if one byte differs in either
// direction").
//
// THE VECTOR. The reference dump's own p3 row (test/tables/
// fixedform_dump.cpp p3_file), plus one all-default record so the write also
// crosses the empty end: r0 carries a present optional with the range's own
// top value (1000) and a full-capacity tag, r1 carries an ABSENT optional
// whose storage still holds stores — §3.4: "THE PAYLOAD RIDES WHOLE WHETHER
// OR NOT IT IS PRESENT, and when the flag is 0 what rides is ZERO" — and r2
// carries nothing but defaults. A write-read-write over these three is
// byte-identical only if every field's encoding and the absent payload's
// zeroing both come back unchanged.
//
// The reused-storage passes poison the caller's storage with 0x5A first
// (§5.8 row 9's form, docs/FIXED-FORM-ALGORITHM.md porting step 8's poison):
// the load prefills every declared byte it does not land, so a value that
// fails to land cannot hide — it rides 0x5A into the second write and the
// byte comparison names it.

import (
	"bytes"
	"testing"
	"unsafe"

	"tblp3"
)

func TestRowP2(t *testing.T) {
	values := []tblp3.Chain{
		// r0: present, the range's top value, a full-capacity tag.
		{Name: [16]byte{'r', 'o', 'u', 'n', 'd', 't', 'r', 'i', 'p'}, NameLength: 9, LinkPresent: true,
			Link: tblp3.Link{Value: 1000, Tag: [8]byte{'0', '1', '2', '3', '4', '5', '6', '7'}, TagLength: 8}},
		// r1: absent, but the storage still carries stores — the write must
		// zero the payload, or the round trip is not the same bytes.
		{Name: [16]byte{'a', 'b', 's', 'e', 'n', 't'}, NameLength: 6, LinkPresent: false,
			Link: tblp3.Link{Value: 99, Tag: [8]byte{'s', 't', 'i', 'l', 'l'}, TagLength: 5}},
		// r2: nothing but defaults.
		{Name: [16]byte{}, NameLength: 0, LinkPresent: false},
	}

	// W1: the first write.
	need := tblp3.ChainFixedMeasure(int64(len(values)))
	w1 := make([]byte, need)
	if n := tblp3.ChainFixedSave(values, w1); n != need {
		t.Fatalf("P2: the first write refused or short-wrote its own measure: n=%d need=%d", n, need)
	}
	t.Logf("P2: the first write filled its own measure exactly: %d bytes", need)

	// R: read it back into fresh storage, and the read must be silent.
	loaded := make([]tblp3.Chain, len(values))
	plan := make([]tblp3.TableFixedEntry, 64)
	var report tblp3.TableReport
	n := tblp3.ChainFixedLoad(loaded, w1, plan, &report)
	if n != int64(len(values)) || report.Malformed || report.Verdict != tblp3.TableOpenOk {
		t.Fatalf("P2: the read of the first write is not clean: n=%d report=%+v", n, report)
	}
	if report.Unknown != 0 || report.KindMismatch != 0 || report.Clamped != 0 || report.Duplicate != 0 {
		t.Fatalf("P2: a clean round trip moved a counter: %+v", report)
	}
	t.Logf("P2: the read of the first write is clean: n=%d, no counter moved", n)

	// W2: write what was read. Byte-identical, or a field's encoding moved.
	w2 := make([]byte, need)
	if n := tblp3.ChainFixedSave(loaded, w2); n != need {
		t.Fatalf("P2: the second write refused or short-wrote: n=%d need=%d", n, need)
	}
	if !bytes.Equal(w1, w2) {
		t.Errorf("P2: write-read-write is not byte-identical: the two writes differ (%d bytes each)", need)
	} else {
		t.Logf("P2: write-read-write byte-identical: %d bytes, w1 == w2", need)
	}

	// W3: reused storage, POISONED before the load — the load owns the
	// prefill, so a byte it fails to land stays 0x5A and the write carries
	// it to the comparison.
	out := make([]tblp3.Chain, len(values))
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&out[0])), len(out)*int(unsafe.Sizeof(out[0])))
	for i := range raw {
		raw[i] = 0x5A
	}
	var poisonReport tblp3.TableReport
	if n := tblp3.ChainFixedLoad(out, w1, plan, &poisonReport); n != int64(len(values)) || poisonReport.Malformed || poisonReport.Verdict != tblp3.TableOpenOk {
		t.Fatalf("P2: the poisoned-storage read is not clean: n=%d report=%+v", n, poisonReport)
	}
	w3 := make([]byte, need)
	if n := tblp3.ChainFixedSave(out, w3); n != need {
		t.Fatalf("P2: the third write refused or short-wrote: n=%d need=%d", n, need)
	}
	if !bytes.Equal(w1, w3) {
		t.Errorf("P2: the poisoned-storage round trip is not byte-identical: a byte the read failed to land rode 0x5A into the write")
	} else {
		t.Logf("P2: reused poisoned storage round-trips byte-identical: w1 == w3")
	}

	// W4: the same storage again, nothing reset between the passes (the
	// gate's own second reuse).
	var reuseReport tblp3.TableReport
	if n := tblp3.ChainFixedLoad(out, w1, plan, &reuseReport); n != int64(len(values)) || reuseReport.Malformed || reuseReport.Verdict != tblp3.TableOpenOk {
		t.Fatalf("P2: the reused-storage read is not clean: n=%d report=%+v", n, reuseReport)
	}
	w4 := make([]byte, need)
	if n := tblp3.ChainFixedSave(out, w4); n != need {
		t.Fatalf("P2: the fourth write refused or short-wrote: n=%d need=%d", n, need)
	}
	if !bytes.Equal(w1, w4) {
		t.Errorf("P2: the second reused-storage round trip is not byte-identical")
	} else {
		t.Logf("P2: the second reused-storage round trip is byte-identical: w1 == w4")
	}
}
