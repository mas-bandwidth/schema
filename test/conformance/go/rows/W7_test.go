// W7_test.go — schema matrix cell go/W7: "arg and meta are two lanes"
// (docs/FIXED-FORM-ALGORITHM.md:245, fix 12).
//
// THE LAW: a fixed-plan entry carries TWO facts that are not the same fact —
// `arg` is THE GUARD'S ORDINAL and nothing else; `meta` is THE OP'S OWN
// ARGUMENT (a text flavour). They must never share one lane. A string(N)
// under a union arm needs both at once: one lane gives whichever was stamped
// last — a byte string under arm 2 read as WIDE, or an entry that runs only
// when the tag equals the flavour, UNDER THE WRONG ARM.
//
// THE GO LEG'S FIXTURE. The law names UT1/UT2; the Go conformance module does
// not generate them, but tblm1's Msg is the same shape by construction:
// `fixed table Msg { seq uint32; body Body }` with `union Body { open Open;
// save Save; quit Quit }`, and Save carries `path string(16)` — a text field
// under the SECOND arm. That is UT1's own reason for putting the string under
// arm 2: the flavour of a string(N) is 1 and the second arm's ordinal is 2,
// so a shared lane does not merely lose a fact, it turns a utf8 read WIDE or
// guards on the wrong arm. Msg's baked identity plan (tblm1.MsgFixedPlan,
// emitted by internal/codegen/gotable/fixedform.go:510-515) is:
//
//	entry 0: copy 5B  seq+tag      unguarded
//	entry 1: text 16B open.path    Guard=4 Arg=1 ArgW=1 Meta=1
//	entry 2: text 16B save.path    Guard=4 Arg=2 ArgW=1 Meta=1   <- THE PROOF
//	entry 3: bool 1B  save.force   Guard=4 Arg=2 ArgW=1 Meta=0
//	entry 4: copy 4B  quit.code    Guard=4 Arg=3 ArgW=1 Meta=0
//
// (seq u32 rides src 0..4, the body tag byte sits at src 4, arm payloads at
// src 5; entries 0's two copies coalesced into one run.) Entry 2 holds the
// ordinal 2 in Arg and the utf8 flavour 1 in Meta — two lanes, two facts,
// two DIFFERENT numbers, so a folded lane cannot hide. The op and flavour
// numbers are the generated runtime's own (tableFixedText=2,
// tableFixedTextUtf8=1, tableFixedNoGuard=0xFFFFFFFF in
// build/tables-generated-go/m1/M1Table.go); they are unexported, so the test
// states them as the wire constants they are.
//
// THE CONTROL (DONE-WHEN): the one generated-code constant the law governs is
// the save arm's ordinal stamp, `out[q].Arg = 2` at
// build/tables-generated-go/m1/M1Table.go:4844. Stamping 1 there — the
// flavour's value over the ordinal, the shared lane itself — turns this test
// red on BOTH halves: the plan loses its Arg==2 text entry, and the tag-2
// record's guard never fires, so the string drops.

package main

import (
	"testing"

	"tblm1"
)

const (
	w7OpText     = 2          // tableFixedText
	w7TextUtf8   = 1          // tableFixedTextUtf8
	w7NoGuard    = 0xFFFFFFFF // tableFixedNoGuard
	w7BodyTagAt  = 4          // src offset of Msg's body tag byte (seq is u32 at 0)
	w7SaveArmOrd = 2          // save is Body's SECOND arm
)

func TestRowW7(t *testing.T) {
	plan := tblm1.MsgFixedPlan.Entries[:tblm1.MsgFixedPlan.Count]

	// ---- the plan half: the two facts ride two lanes ---------------------

	// The proving entry: save.path — a text op under the second arm.
	found := false
	for i, e := range plan {
		if e.Op == w7OpText && e.Guard == w7BodyTagAt && e.Arg == w7SaveArmOrd {
			found = true
			if e.Meta != w7TextUtf8 {
				t.Errorf("entry %d: save.path's Meta is %d, want the utf8 flavour %d — the op's own argument shares the guard's lane", i, e.Meta, w7TextUtf8)
			}
			if e.ArgW != 1 {
				t.Errorf("entry %d: save.path's ArgW is %d, want 1 — the guard compares at the tag's own width", i, e.ArgW)
			}
			if e.Size != 16 {
				t.Errorf("entry %d: save.path's Size is %d, want the string(16) byte span 16", i, e.Size)
			}
		}
	}
	if !found {
		t.Fatal("no text entry guarded on the body tag with Arg=2: save.path's ordinal and flavour share one lane, or the arm's stamp is gone")
	}

	// The lane audit over the whole plan: Meta names a flavour only on a text
	// op, and a guarded entry's Arg is its arm's ordinal — neither lane ever
	// carries the other's fact. open.path (Arg=1, Meta=1) is the SAME number
	// for two facts, which is exactly why it cannot be the proof and save's
	// path (2 against 1) is.
	for i, e := range plan {
		if e.Meta != 0 && e.Op != w7OpText {
			t.Errorf("entry %d: Meta=%d on op %d — the flavour lane carries a non-text fact", i, e.Meta, e.Op)
		}
		if e.Guard != w7NoGuard && (e.Arg < 1 || e.Arg > 3) {
			t.Errorf("entry %d: guarded entry's Arg=%d is no arm ordinal of Body — the ordinal lane carries something else", i, e.Arg)
		}
		if e.Guard == w7NoGuard && e.Arg != 0 {
			t.Errorf("entry %d: unguarded entry's Arg=%d — the guard's ordinal leaked onto an entry with no guard", i, e.Arg)
		}
	}

	// ---- the record half: the runner spends the lanes independently ------
	//
	// One record whose tag is 2 and whose arm carries the five bytes
	// "two-lanes!" — the guard fires because the tag equals Arg (2) at ArgW,
	// and the text lands as utf8 because Meta (1) says unit 1 with a one-byte
	// terminator. A shared lane either never fires (the ordinal stamped over
	// the flavour: the guard compares 2 against 1) or reads the span WIDE —
	// both lose the string under this tag.
	values := []tblm1.Msg{{Seq: 7}}
	values[0].Body.Type = tblm1.BodyTypeSave
	copy(values[0].Body.Save.Path[:], "two-lanes!")
	values[0].Body.Save.PathLength = 10
	values[0].Body.Save.Force = true

	buf := make([]byte, tblm1.MsgFixedMeasure(1))
	n := tblm1.MsgFixedSave(values, buf)
	if n < 0 {
		t.Fatal("MsgFixedSave failed")
	}
	data := buf[:n]
	scratch := make([]tblm1.TableFixedEntry, 16)

	var report tblm1.TableReport
	back := make([]tblm1.Msg, 1)
	got := tblm1.MsgFixedLoad(back, data, scratch, &report)
	if got != 1 || report.Malformed || report.Verdict != tblm1.TableOpenOk {
		t.Fatalf("valid file: n=%d malformed=%v verdict=%v reason=%q", got, report.Malformed, report.Verdict, report.Reason)
	}
	if report.Clamped != 0 {
		t.Fatalf("valid file: clamped=%d — a clean read moves no counter", report.Clamped)
	}
	if back[0].Body.Type != tblm1.BodyTypeSave {
		t.Fatalf("the arm: tag landed %v, want Save — the guard's ordinal is not reaching the tag", back[0].Body.Type)
	}
	if back[0].Body.Save.PathLength != 10 || string(back[0].Body.Save.Path[:10]) != "two-lanes!" {
		t.Fatalf("the arm's string(16): length=%d path=%q — arg and meta are not two lanes",
			back[0].Body.Save.PathLength, string(back[0].Body.Save.Path[:]))
	}
	if !back[0].Body.Save.Force || back[0].Seq != 7 {
		t.Fatalf("the rest of the record: force=%v seq=%d", back[0].Body.Save.Force, back[0].Seq)
	}
}
