package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	"tblv1"
)

// R7 — THE IDENTITY LANE IS AN INDEX COMPARISON, NEVER A RECOMPUTED HASH
// (docs/roadmap.sexp go/R7, audit schema#898). The law is
// docs/FIXED-FORM-ALGORITHM.md:869: "the compiler hands every reader its own
// wire hash and every known hash as CONSTANTS — `R.own_hash` and
// `R.lineage[i]`, laid down by COMPILE — and a runtime NEVER computes a hash
// from layout bytes it holds, not for the IDENTITY LANE (step 8, where the
// selected entry being the reader's own is an INDEX COMPARISON and never a
// recomputation)". The refusal table (:884) pins the answer's shape: a hash
// in no lineage entry is `layout_newer` carrying THE FILE'S HASH, and nothing
// else.
//
// The hurt the law exists for is the Elixir leg (#928): it hashed its own
// layout bytes to find itself in the lineage, matched NOTHING, and took a
// compiled plan on every read of its own files — layout_newer for its own
// files, and a compiled lane where the identity lane was owed. Both faces are
// asserted below, on the production read path.
//
// PRODUCTION PATH UNDER TEST. tblv1.CfgFixedLoad, the generated fixed-form
// reader (build/tables-generated-go/v1/V1Table.go:9028): the header's hash is
// taken AS GIVEN (step 4), tableFixedSelect(CfgFixedKnown, hash) finds the
// FIRST index with lineage[i] == h against the constants COMPILE laid down
// (step 5, V1Table.go:9063), then the floor (step 6), then the lock's byte
// comparison (step 7), then step 8 at V1Table.go:9083, `if hash !=
// CfgFixedHash`: the selected entry being the reader's own is the comparison
// against the R.own_hash constant, and the identity plan — the baked
// CfgFixedPlan, no parse, no compile — is taken; any other hash takes the
// compiled lane. Nothing on that path computes a hash from layout bytes. The
// reader is driven end to end; no helper is the witness.
//
// THE VECTOR. testdata/conformance/tables carries no form-3 data for tblv1
// (the fixed-form corpus is build/fixedform-corpus, written by the C++
// reference), so the vector is constructed here from the law through the
// PRODUCTION WRITER, CfgFixedSave, whose every byte the law fixes: the form
// byte 3 at 0; the seven reserved zeros at 1..7; the hash — the compile-time
// constant itself, since a runtime never derives one — at 8; the u32 layout
// length at 16; the layout bytes from 20; then one record per 8 + 250 = 258
// bytes, each record opening with the same eight hash bytes. The frame those
// bytes carry is asserted below, not trusted.
func TestRowR7(t *testing.T) {
	// One Cfg record, values inside every writer-side bound, defaults left
	// standing everywhere the test does not name.
	written := tblv1.Cfg{
		A:            777,
		B:            2.25,
		NameLength:   6,
		ItemsCount:   2,
		Grade:        tblv1.GradeGold,
		ExtraPresent: true,
		TierPresent:  true,
		Tier:         42,
	}
	copy(written.Name[:], "cfg r7")
	written.Items[0] = 7
	written.Items[1] = 99
	written.Extra.Factor = 0.5

	// The read-back must be the value that was written, first difference
	// named.
	drift := func(v tblv1.Cfg) string {
		switch {
		case v.A != 777:
			return "a"
		case v.B != 2.25:
			return "b"
		case v.NameLength != 6 || !bytes.Equal(v.Name[:6], []byte("cfg r7")):
			return "name"
		case v.ItemsCount != 2 || v.Items[0] != 7 || v.Items[1] != 99:
			return "items"
		case v.Grade != tblv1.GradeGold:
			return "grade"
		case !v.ExtraPresent || v.Extra.Factor != 0.5:
			return "extra"
		case !v.TierPresent || v.Tier != 42:
			return "tier"
		}
		return ""
	}

	// The report a read that answers owes: every counter zero, no damage, no
	// refusal, no layout hash carried.
	clean := func(r *tblv1.TableReport) bool {
		return r.Verdict == tblv1.TableOpenOk && !r.Malformed && r.Reason == "" &&
			r.Unknown == 0 && r.KindMismatch == 0 && r.Widened == 0 && r.Clamped == 0 &&
			r.LayoutHash == 0
	}

	// THE VECTOR, from the law through the production writer.
	buf := make([]byte, tblv1.CfgFixedMeasure(1))
	saved := tblv1.CfgFixedSave([]tblv1.Cfg{written}, buf)
	if saved != int64(len(buf)) {
		t.Fatalf("the production writer wrote %d of %d bytes (header %d + 1 record of %d)",
			saved, len(buf), tblv1.TableFixedHeaderBytes, tblv1.CfgFixedRecordBytes)
	}
	f := buf

	// THE FRAME THE LAW FIXES, and the constants it is held to. The hash in
	// the file is R.own_hash itself — HANDED to this reader by COMPILE — and
	// the lineage's last entry is the build's own, so the identity lane is
	// reachable (§5.9 #2).
	last := len(tblv1.CfgFixedKnown) - 1
	frameOk := f[0] == 3
	for _, reserved := range f[1:tblv1.TableFixedHashAt] {
		frameOk = frameOk && reserved == 0
	}
	if !frameOk {
		t.Errorf("the file's frame is not the law's: form byte 3 at 0, the seven reserved zeros at 1..7")
	}
	if got := binary.LittleEndian.Uint64(f[tblv1.TableFixedHashAt:]); got != tblv1.CfgFixedHash {
		t.Errorf("the hash at 8 is 0x%x, not the compile-time constant R.own_hash (0x%x), handed to the reader, never derived",
			got, uint64(tblv1.CfgFixedHash))
	}
	if tblv1.CfgFixedKnown[last].Hash != tblv1.CfgFixedHash {
		t.Errorf("the lineage COMPILE laid down does not end on the build's own entry: R.lineage[%d].hash is 0x%x, R.own_hash is 0x%x",
			last, tblv1.CfgFixedKnown[last].Hash, uint64(tblv1.CfgFixedHash))
	}
	layoutLen := binary.LittleEndian.Uint32(f[tblv1.TableFixedHeaderBytes:])
	if int(layoutLen) != len(tblv1.CfgFixedLayout) ||
		!bytes.Equal(f[tblv1.TableFixedHeaderBytes+4:tblv1.TableFixedHeaderBytes+4+int(layoutLen)],
			tblv1.CfgFixedKnown[last].Layout) {
		t.Errorf("the u32 at 16 is %d and the layout behind it is not byte for byte the lock's own bytes, R.lineage[%d]",
			layoutLen, last)
	}

	// THE R7 BITE, first face: the build's own file READS, on the identity
	// lane. A runtime that derived its own hash from the layout bytes it
	// holds would fold in no digest, match NOTHING in the lineage, and
	// answer layout_newer for its own files — the Elixir hurt (#928). The
	// census lands zero because the identity plan is the law's own shape,
	// not a compiled one (§5.9 #30).
	plan := make([]tblv1.TableFixedEntry, 1024)
	out := make([]tblv1.Cfg, 1)
	var report tblv1.TableReport
	n := tblv1.CfgFixedLoad(out, f, plan, &report)
	if d := drift(out[0]); n != 1 || !clean(&report) || d != "" {
		t.Errorf("the build's own file does not read on the identity lane: n=%d report=%+v drift=%s — want n=1, the census lands zero, every value round-trips",
			n, report, d)
	}

	// THE R7 BITE, second face: the identity lane is the baked plan — no
	// parse, no compile — so the same read answers at a caller plan capacity
	// of exactly the identity plan's own. A reader that took a COMPILED plan
	// on every read of its own files would hold lane.Count over the baked
	// plan's and refuse plan_too_large here.
	out1 := make([]tblv1.Cfg, 1)
	var report1 tblv1.TableReport
	idRoom := make([]tblv1.TableFixedEntry, tblv1.CfgFixedPlan.Count)
	n1 := tblv1.CfgFixedLoad(out1, f, idRoom, &report1)
	if d := drift(out1[0]); n1 != 1 || report1.Verdict == tblv1.TableOpenRefused || d != "" {
		t.Errorf("the own hash does not resolve to a plan of exactly the identity plan's size (%d entries): n=%d verdict=%v drift=%s at that caller plan capacity",
			tblv1.CfgFixedPlan.Count, n1, report1.Verdict, d)
	}

	// THE HASH WAS TAKEN AS GIVEN, NEVER RECOMPUTED. A header hash no
	// lineage entry holds is layout_newer, and the report carries THE
	// FILE'S HASH — the value the header held, not this build's constant
	// and not a hash of the layout bytes behind it, which no runtime
	// computes.
	lying := append([]byte(nil), f...)
	for i := 0; i < 8; i++ {
		lying[tblv1.TableFixedHashAt+i] ^= 0xFF
	}
	given := binary.LittleEndian.Uint64(lying[tblv1.TableFixedHashAt:])
	var lr tblv1.TableReport
	n2 := tblv1.CfgFixedLoad(out, lying, plan, &lr)
	if given != uint64(tblv1.CfgFixedHash)^0xFFFFFFFFFFFFFFFF ||
		n2 != -1 || lr.Verdict != tblv1.TableOpenRefused || lr.Reason != "layout_newer" ||
		lr.LayoutHash != given || lr.Malformed ||
		lr.Unknown != 0 || lr.KindMismatch != 0 || lr.Widened != 0 || lr.Clamped != 0 {
		t.Errorf("a header hash no lineage entry holds is not layout_newer carrying THE FILE'S HASH and nothing else: n=%d report=%+v given=0x%x — no counter moves, nothing decodes",
			n2, lr, given)
	}
}
