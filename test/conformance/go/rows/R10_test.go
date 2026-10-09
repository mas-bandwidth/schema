package main

import (
	"encoding/binary"
	"testing"

	"tblp3"
)

// TestRowR10 asserts go/R10, "the run-time walk of a stranger's layout and
// the recompute of the header's hash are retired"
// (docs/FIXED-FORM-ALGORITHM.md §5.6, line 967; docs/roadmap.sexp go/R10).
//
// The fixed-form reader refuses a layout the lock has never seen BY NAME —
// "layout_newer" — before any byte-walk of the layout. The header's hash is
// the file's, taken at the offset the prologue names; the reader does not
// re-derive a digest from the layout behind it, because the wire carries no
// digest to recompute, and the byte comparison of the layout to the lock's
// bytes is what binds the two together.
func TestRowR10(t *testing.T) {
	// Build a valid Chain and save it to the fixed form. The writer lays the
	// header's hash down from the COMPILED ChainFixedHash constant — that is
	// the one number the reader will compare against, and any number other
	// than it is a stranger's layout.
	values := []tblp3.Chain{
		{
			Name:       [16]byte{'h', 'e', 'l', 'l', 'o'},
			NameLength: 5,
			Link: tblp3.Link{
				Value:     42,
				Tag:       [8]byte{'t', 'a', 'g'},
				TagLength: 3,
			},
			LinkPresent: true,
		},
	}
	measure := tblp3.ChainFixedMeasure(1)
	wantMeasure := int64(tblp3.TableFixedHeaderBytes) + 4 + int64(len(tblp3.ChainFixedLayout)) + tblp3.ChainFixedRecordBytes
	if measure != wantMeasure {
		t.Fatalf("ChainFixedMeasure: got %d, want %d", measure, wantMeasure)
	}
	buf := make([]byte, measure)
	n := tblp3.ChainFixedSave(values, buf)
	if n < 0 {
		t.Fatal("ChainFixedSave failed")
	}
	if int64(n) != measure {
		t.Fatalf("ChainFixedSave: wrote %d bytes, want %d", n, measure)
	}
	data := buf[:n]

	// The file's header hash, at the offset the prologue names, IS the
	// compiled ChainFixedHash. The writer places it there from the constant;
	// the reader compares against it. A recompute would derive a different
	// value (or none at all) and break this property — so its absence is the
	// first thing the row asserts.
	wantHash := uint64(tblp3.ChainFixedHash)
	if got := binary.LittleEndian.Uint64(data[tblp3.TableFixedHashAt : tblp3.TableFixedHashAt+8]); got != wantHash {
		t.Fatalf("file header hash: got 0x%016x, want ChainFixedHash 0x%016x",
			got, wantHash)
	}

	plan := make([]tblp3.TableFixedEntry, 64)
	var scratch [1]tblp3.Chain

	// Sanity: the valid file loads cleanly and returns exactly one record.
	// This is the case where the file's hash MATCHES the compiled constant
	// and the layout bytes MATCH the lock's bytes, so the reader has every
	// reason to walk it and lands values.
	scratch[0] = tblp3.Chain{}
	var sanityReport tblp3.TableReport
	sanityN := tblp3.ChainFixedLoad(scratch[:], data, plan, &sanityReport)
	if sanityN != 1 {
		t.Fatalf("valid file: n=%d, want 1", sanityN)
	}
	if !scratch[0].LinkPresent || scratch[0].Link.Value != 42 {
		t.Fatalf("valid file: wrong values: LinkPresent=%v Value=%d", scratch[0].LinkPresent, scratch[0].Link.Value)
	}

	// ---- ASSERTION 1: stranger's layout is REFUSED WITHOUT BEING WALKED ----
	//
	// A file whose header hash is NOT in the lineage. The reader's order
	// (§5.3) is form byte → reserved bytes → layout length → layout bytes
	// → HASH → lineage match; the layout length is checked BEFORE the hash
	// is consulted, so a hostile length past the file end would trigger
	// "layout_malformed" before the stranger gate ever fires. The way to
	// assert that a stranger's layout is refused by name is to lay down a
	// VALID length (matching the file's actual layout size) and a hostily
	// shaped layout behind it — a layout a reader that walked the stranger
	// would crash on. The reader must refuse "layout_newer" and report THE
	// FILE'S hash, because there is no third thing to check and the hash
	// is taken from the prologue as given.
	stranger := make([]byte, len(data))
	copy(stranger, data)
	const strangerHash uint64 = 0xcafebabedeadbeef
	binary.LittleEndian.PutUint64(stranger[tblp3.TableFixedHashAt:tblp3.TableFixedHashAt+8], strangerHash)
	// A VALID layout length — equal to the file's actual layout size — so
	// the reader passes the length check and reaches the hash lookup.
	layoutStart := tblp3.TableFixedHeaderBytes + 4
	layoutLen := uint32(len(stranger) - layoutStart)
	binary.LittleEndian.PutUint32(stranger[tblp3.TableFixedHeaderBytes:tblp3.TableFixedHeaderBytes+4], layoutLen)
	// Hostile layout: a long run of 0xFF, the shape a stranger would have
	// if a writer outside the corpus laid it down. A walk would refuse it
	// (or worse); a refusal-by-hash never sees it.
	for i := layoutStart; int(i) < len(stranger); i++ {
		stranger[i] = 0xFF
	}
	// Sanity: the value we wrote is NOT in ChainFixedKnown, and not equal to
	// the compiled ChainFixedHash. A value that matched would let the reader
	// past the stranger gate and the assertion would prove nothing.
	if strangerHash == tblp3.ChainFixedHash {
		t.Fatal("strangerHash collides with ChainFixedHash; pick a stranger")
	}

	var sReport tblp3.TableReport
	sN := tblp3.ChainFixedLoad(nil, stranger, plan, &sReport)
	if sN != -1 {
		t.Fatalf("stranger's layout: n=%d, want -1 (refused)", sN)
	}
	if sReport.Verdict != tblp3.TableOpenRefused {
		t.Fatalf("stranger's layout: verdict=%v, want TableOpenRefused", sReport.Verdict)
	}
	if sReport.Reason != "layout_newer" {
		t.Fatalf("stranger's layout: reason=%q, want \"layout_newer\"", sReport.Reason)
	}
	if sReport.LayoutHash != strangerHash {
		t.Fatalf("stranger's layout: layout_hash=0x%016x, want 0x%016x (the file's)",
			sReport.LayoutHash, strangerHash)
	}
	if sReport.Malformed {
		t.Fatalf("stranger's layout: malformed=true; the reader walked the stranger's layout")
	}
	// A refusal by name moves no counter (§5.3's joint table).
	if sReport.Widened != 0 || sReport.Unknown != 0 || sReport.KindMismatch != 0 ||
		sReport.Clamped != 0 || sReport.Duplicate != 0 {
		t.Fatalf("stranger's layout: counters moved (w=%d u=%d k=%d c=%d d=%d)",
			sReport.Widened, sReport.Unknown, sReport.KindMismatch, sReport.Clamped, sReport.Duplicate)
	}

	// ---- ASSERTION 2: the header's hash is taken from the FILE, not recomputed ----
	//
	// A file whose header hash MATCHES the compiled ChainFixedHash, but whose
	// layout byte at offset TableFixedHeaderBytes+4 (= the first layout byte)
	// differs from the lock's bytes. A reader that recomputed the hash from
	// the corrupted layout would see a digest that is NOT in ChainFixedKnown
	// and refuse "layout_newer". A reader that takes the file's header hash
	// AS GIVEN and compares the layout bytes to the lock's bytes refuses
	// "layout_malformed" — because the hash on the file is exactly the
	// compiled constant and the layout behind it does not match.
	known := make([]byte, len(data))
	copy(known, data)
	// The header hash at TableFixedHashAt..+8 is unchanged — this file
	// CLAIMS to be a known version.
	if got := binary.LittleEndian.Uint64(known[tblp3.TableFixedHashAt : tblp3.TableFixedHashAt+8]); got != tblp3.ChainFixedHash {
		t.Fatal("known-hash bad-layout file: header hash drifted; test setup wrong")
	}
	// Flip ONE byte of the layout (the first byte, at offset
	// TableFixedHeaderBytes+4). This is what the law governs: the reader
	// compares the file's layout bytes to the lock's, byte for byte, and
	// does not re-derive a hash from those bytes to consult instead.
	known[tblp3.TableFixedHeaderBytes+4] ^= 0x01

	var kReport tblp3.TableReport
	kN := tblp3.ChainFixedLoad(nil, known, plan, &kReport)
	if kN != -1 {
		t.Fatalf("known-hash bad-layout: n=%d, want -1", kN)
	}
	if kReport.Verdict != tblp3.TableOpenRefused {
		t.Fatalf("known-hash bad-layout: verdict=%v", kReport.Verdict)
	}
	if kReport.Reason != "layout_malformed" {
		t.Fatalf("known-hash bad-layout: reason=%q, want \"layout_malformed\" "+
			"(a recompute of the hash from the layout would answer \"layout_newer\")",
			kReport.Reason)
	}
	// Only the two named-hash refusals (layout_newer, layout_unsupported)
	// report the file's hash; layout_malformed does not, because the bytes
	// failed the comparison the lock holds, and no third thing is checked.
	if kReport.LayoutHash != 0 {
		t.Fatalf("known-hash bad-layout: layout_hash=0x%016x, want 0 "+
			"(layout_malformed does not report the file's hash)",
			kReport.LayoutHash)
	}
	if kReport.Malformed {
		t.Fatalf("known-hash bad-layout: malformed=true; the refusal is by name, not damage")
	}
}
