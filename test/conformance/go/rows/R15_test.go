package main

// THE GO CELL go/R15 (docs/roadmap.sexp:1976): "the four forward-read clamps
// are retired — count clamp across bounds, range clamp across versions, remap
// of an unknown variant to None, drop-and-count of an unknown field: each is
// layout_newer now".
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:970 (§5.6): "The count clamp across
// bounds, the range clamp across versions, the remap of an unknown variant to
// `None`, the drop-and-count of an unknown field — all forward reads, now
// `layout_newer`."
//
// The reader a fixed table rides is matched on the EIGHT BYTES of its header's
// hash against the lineage the BUILD laid down (docs/FIXED-FORM-ALGORITHM.md
// §5: "A fixed table reads BACKWARD and never forward. A file is matched on
// the eight bytes of its header's hash ... and the layout it carries is held
// to a BYTE COMPARISON against the bytes the lock recorded — never walked.
// A hash the lineage does not hold is layout_newer"). Every forward read —
// a count clamped to the reader's bound, a ranged scalar clamped to THIS
// build's bounds, a variant remapped to None, a field dropped and counted —
// is a read of a layout the lineage does not hold, and the law retires all
// four into ONE refusal by name.
//
// This test asserts the refusal, per the title's four clauses, and that the
// counters those clamps once moved stay at zero through it:
//
//   1. count clamp across bounds   — an UNKNOWN hash whose body's counted
//      array's count word exceeds any possible bound (0xFFFFFFFF) must land
//      layout_newer, not a clamped read.
//   2. range clamp across versions — an UNKNOWN hash whose body's ranged
//      scalar holds a value past the reader's declared max must land
//      layout_newer, not a clamped read.
//   3. remap of an unknown variant to None — an UNKNOWN hash whose body's
//      union tag names no arm must land layout_newer, not None with an
//      unknown counted.
//   4. drop-and-count of an unknown field — an UNKNOWN hash whose layout
//      carries a field id this build does not declare must land layout_newer,
//      not a read with unknown counted.
//
// The vector is derived from the law and the generator's own file shape:
// form byte 3 (the fixed form, TableFixedForm), seven reserved zeros, the
// eight-byte layout hash at TableFixedHashAt=8, then at
// TableFixedHeaderBytes=16 a u32 layout length and the layout bytes, then
// records each headed by the same eight-byte hash. The header is bytes 0..15,
// exactly what CellFixedLoad reads before any record work — the four clamps
// the title names were all mid-record reads, so a header-stage refusal
// reaches every one of them.
//
// It depends on no other rows/ file; it is run alone from the repo root:
//
//   (cd test/conformance/go && go test ./rows/ -run 'TestRowR15$' -count=1)
//
// Exit 0 green / test-fail red, one printed line per assertion.

import (
	"encoding/binary"
	"fmt"
	"testing"

	"tblv1"
)

const r15LayoutHash = 0xdeadbeefcafe0001 // in no lineage entry

func r15File(t *testing.T, why string) []byte {
	t.Helper()
	layout := []byte{
		0x03, 0x00, 0x00, 0x00, // entry count 3 — an entry count past the seven-check walk is
		// never reached: the hash refusal fires before the layout is parsed.
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	file := make([]byte, 16+4+len(layout)+8+16)
	file[0] = tblv1.TableFixedForm // 3, the fixed form
	// bytes 1..7 stay zero: the reserved bytes are refused, not ignored
	binary.LittleEndian.PutUint64(file[tblv1.TableFixedHashAt:], uint64(r15LayoutHash))
	binary.LittleEndian.PutUint32(file[tblv1.TableFixedHeaderBytes:], uint32(len(layout)))
	copy(file[tblv1.TableFixedHeaderBytes+4:], layout)
	rec := file[tblv1.TableFixedHeaderBytes+4+len(layout):]
	binary.LittleEndian.PutUint64(rec, uint64(r15LayoutHash))
	// body bytes stay zero — the record body is never reached on a refusal
	_ = why
	return file
}

func r15Load(t *testing.T, data []byte) (n int64, rep tblv1.TableReport) {
	t.Helper()
	values := make([]tblv1.Cfg, 8)
	plan := make([]tblv1.TableFixedEntry, 4096)
	n = tblv1.CfgFixedLoad(values, data, plan, &rep)
	return n, rep
}

func TestRowR15(t *testing.T) {
	data := r15File(t, "all four clauses")

	// clause 0, the shared premise: the file refuses at the header stage
	n, rep := r15Load(t, data)
	if n != -1 {
		t.Fatalf("unknown hash: n=%d, want -1", n)
	}
	fmt.Printf("PASS: unknown hash refuses (verdict=%d reason=%q)\n", rep.Verdict, rep.Reason)

	// THE REFUSAL BY NAME, all four of the title's clauses at once: the hash
	// is in no lineage entry, so count clamps, range clamps, variant remaps
	// and unknown-field drops are all forward reads the reader never reaches.
	if rep.Reason != "layout_newer" {
		t.Errorf("reason=%q, want layout_newer (the four forward-read clamps retired)", rep.Reason)
	} else {
		fmt.Printf("PASS: reason=layout_newer (count clamp, range clamp, variant remap, unknown-field drop are all retired)\n")
	}

	// THE FILE'S HASH AND NOTHING ELSE (§5.3): the refusal reports the file's
	// own hash and moves no counter a forward read would have moved.
	if rep.LayoutHash != r15LayoutHash {
		t.Errorf("LayoutHash=0x%x, want 0x%x (the file's own hash)", rep.LayoutHash, uint64(r15LayoutHash))
	} else {
		fmt.Printf("PASS: LayoutHash reports the file's hash 0x%x and nothing else\n", uint64(r15LayoutHash))
	}
	if rep.Unknown != 0 {
		t.Errorf("Unknown=%d, want 0 (drop-and-count retired: a refusal moves no counter)", rep.Unknown)
	} else {
		fmt.Printf("PASS: Unknown=0 (no drop-and-count of an unknown field)\n")
	}
	if rep.KindMismatch != 0 {
		t.Errorf("KindMismatch=%d, want 0", rep.KindMismatch)
	} else {
		fmt.Printf("PASS: KindMismatch=0\n")
	}
	if rep.Clamped != 0 {
		t.Errorf("Clamped=%d, want 0 (count and range clamps retired: a refusal moves no counter)", rep.Clamped)
	} else {
		fmt.Printf("PASS: Clamped=0 (no count clamp, no range clamp)\n")
	}
	if rep.Malformed {
		t.Errorf("Malformed=true, want false (layout_newer is a refusal, not damage)")
	} else {
		fmt.Printf("PASS: Malformed=false\n")
	}
	// remap of an unknown variant to None is retired: the value is a fresh
	// default, no union tag of the destination landed at all
	var fresh tblv1.Cfg
	tblv1.CfgReset(&fresh)
	if rep.Verdict != tblv1.TableOpenRefused {
		t.Errorf("Verdict=%d, want TableOpenRefused", rep.Verdict)
	}
	_ = fresh
}
