package javatable

// union_tag_both_plans: a forged union TAG is a different emitter site from a
// forged ENUM ORDINAL on every leg but elixir, and the identity plan's half of
// it was guarded by NOTHING. The forge sets the one-byte `pick.type` tag of
// `old_union_append.bin`'s single record to 9 — past the OLD writer's two arms
// and the NEW reader's three — located by the nine-byte needle
// 01 07 00 00 00 0F 00 00 00, asserted to occur EXACTLY ONCE, and the tag is
// ONE byte, not four. Both plans read the same forged bytes: the compiled plan
// (VNEW reader, VOLD in its lineage) and the identity plan (VOLD reader on its
// own hash). `clamped` is asserted == 1 and never >= 1: a leg that counts the
// clamp in the plan's op AND again in the decode bounds lands 2, and the looser
// read is precisely what this row exists to catch.
//
// MEASURED 2026-09-19 ON THE GATE RIG AT 380f1cad, one leg at a time, each edit
// counted to exactly 1 and each file restored and proved restored: the union tag
// bound was deleted from the emitter of ALL NINE LEGS and not one of 36 to 72
// landed fixed-table tests went red on any of them. `forged_ordinal_both_plans`
// (§5.8 row 12) forges an ENUM ordinal, which is a different site; darttable's
// `TestFixedCompiledPlanTagPastArmSet` reads the COMPILED plan only. Nothing
// anywhere read the identity half.
//
// THE COMPILED HALF DOES NOT ASSERT `clamped`, AND THAT OMISSION IS THIS ROW'S
// CONTENT, NOT ITS WEAKNESS — schema#1254. Over these same forged bytes the
// identity plan counts ONE and the compiled plan counts ZERO: the compiled plan
// lands a tag naming no shared arm through the plan's own remap with no counter,
// while the identity path's decode bound (fixedform.go's
// `if (v.type > len(un.Variants))`) is the one that counts. Asserting == 1 on
// the compiled half would land a red row; asserting == 0 would cement the
// defect. So that half asserts the LANDING — None, never the forged 9, the
// neighbour untouched, no refusal and no other counter — and names the count as
// owed. WHEN #1254 IS RULED ON, delete this paragraph and hand `clamped == 1` to
// both halves.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_union_append.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE LOCATOR, and it must match EXACTLY ONCE: a search that is not a
	// locator is not a forge. The record body is the file's last nine bytes —
	// the eight before it are that record's layout hash — and the tag is ONE
	// byte: `01` (pick.type = alpha), then `07 00 00 00` (pick.alpha.m = 7),
	// then `0f 00 00 00` (seq = 15).
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	forged := append([]byte(nil), raw...)
	forged[at] = 0x09 // the forge: a tag past the old writer's two arms and the new reader's three
	hostile := filepath.Join(t.TempDir(), "hostile_union_append.bin")
	if err := os.WriteFile(hostile, forged, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "union_append", []sideSpec{
		{key: "reads", schema: "VNEW_union_append.schema", older: []string{"VOLD_union_append.schema"}},
		{key: "refuses", schema: "VOLD_union_append.schema"},
	})

	compiled := runProbe(t, classes, "Probe_reads", hostile)
	identity := runProbe(t, classes, "Probe_refuses", hostile)

	landing := func(which string, r probeRun) {
		if r.n != 1 {
			t.Errorf("%s: n=%d, want 1", which, r.n)
		}
		if r.refused {
			t.Errorf("%s: refused=%v reason=%s, want no refusal", which, r.refused, r.reason)
		}
		if r.malformed {
			t.Errorf("%s: malformed=%v, want false", which, r.malformed)
		}
		if got := r.value["pick.type"]; got != "0" {
			t.Errorf("%s: pick.type=%s, want None (0) and never the forged 9", which, got)
		}
		if got := r.value["seq"]; got != "15" {
			t.Errorf("%s: seq=%s, want the writer's 15", which, got)
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
			t.Errorf("%s: counters moved: unknown=%d kindMismatch=%d widened=%d, want all 0",
				which, r.unknown, r.kindMismatch, r.widened)
		}
	}
	landing("the NEW build (compiled plan)", compiled)
	landing("the OLD build (identity plan)", identity)

	if identity.clamped != 1 {
		t.Errorf("the OLD build (identity plan): clamped=%d, want 1 exactly", identity.clamped)
	}
	// The compiled plan's clamped is deliberately NOT asserted: schema#1254, and
	// the header says why.
}
