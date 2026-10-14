package gotable

// union_tag_both_plans (§5.8): a union TAG past the arm set was guarded by nothing
// on this leg. The probe forges the ONE-BYTE tag of old_union_append.bin — the
// nine-byte needle 01 07 00 00 00 0f 00 00 00 (type=alpha, m=7, seq=15), located
// with bytes.Index and asserted to occur EXACTLY once, never zero and never twice,
// because a search that is not a locator is not a forge — to 9, past the OLD
// writer's two arms AND the NEW reader's three, so no arm lands. The SAME forged
// bytes are read through BOTH plans — the compiled (VNEW over VOLD) and the
// identity (VOLD alone) — and both land the tag None and never the forged 9, and
// leave seq at the writer's 15. Clamped is == 1 EXACTLY and never >= 1: a looser
// read would let a leg that also counts the tag in the plan's op land 2 and pass.
// Measured 2026-09-19: the emitter's tag bound deleted from all nine legs left
// zero red of 72/36/70/69/72/68/41/70/70.
//
// schema#1254: on this leg the COMPILED plan counts ZERO over these forged bytes —
// a tag naming no shared arm lands None through the lineage plan's guarded const,
// with no counter — while the IDENTITY plan counts 1 through the decode bound's
// clamp. The compiled column therefore asserts the landing and NOT the count (a
// == 1 would land red, a == 0 would cement the defect). When #1254 is ruled on,
// delete the compiled column's landing-only body and hand back the same
// `clamped == 1` the identity column asserts.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_union_append")
	older := readSchema(t, "VOLD_union_append")
	table := fixedRootName(t, older)
	file := filepath.Join(corpus, "old_union_append.bin")

	// The compiled column: reader VNEW, older [VOLD]. The tag that names no
	// shared arm lands None through the lineage plan's guarded const with no
	// counter (schema#1254), so the landing is asserted and the count is not.
	compiled := fmt.Sprintf(`package probe

import ("bytes"; "os"; "testing")

func TestUnionTag(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("union_tag_both_plans: the record body (pick.type=alpha, m=7, seq=15) is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("union_tag_both_plans: the forged-tag needle occurs more than once, so the search is not a locator")
	}
	data[at] = 9 // the forge: past the OLD writer's two arms and the NEW reader's three

	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("union_tag_both_plans: the forged file reads one record, n=%%d %%+v", n, r)
	}
	if r.Verdict == TableOpenRefused || r.Reason != "" || r.Malformed {
		t.Fatalf("union_tag_both_plans: a forged tag is a clamp, not a refusal and not malformed: %%+v", r)
	}
	if back[0].Pick.Type != PickTypeNone {
		t.Fatalf("union_tag_both_plans: the forged tag lands None, not %%v (record %%+v)", back[0].Pick.Type, back[0])
	}
	if back[0].Seq != 15 {
		t.Fatalf("union_tag_both_plans: the neighbour after the union must stand at 15, not %%v (record %%+v)", back[0].Seq, back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("union_tag_both_plans: the clamp counts once, Clamped=%%d, want 1 (report %%+v)", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("union_tag_both_plans: a counter other than Clamped moved: %%+v", r)
	}
}
`, file, table)

	// The identity column: reader VOLD, no older at all, reading on its own hash.
	// The decode bound's clamp lands None and counts once, so the count is
	// asserted here and asserted == 1, never >= 1.
	identity := fmt.Sprintf(`package probe

import ("bytes"; "os"; "testing")

func TestUnionTag(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("union_tag_both_plans: the record body (pick.type=alpha, m=7, seq=15) is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("union_tag_both_plans: the forged-tag needle occurs more than once, so the search is not a locator")
	}
	data[at] = 9 // the forge: past the OLD writer's two arms and the NEW reader's three

	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("union_tag_both_plans: the forged file reads one record, n=%%d %%+v", n, r)
	}
	if r.Verdict == TableOpenRefused || r.Reason != "" || r.Malformed {
		t.Fatalf("union_tag_both_plans: a forged tag is a clamp, not a refusal and not malformed: %%+v", r)
	}
	if back[0].Pick.Type != PickTypeNone {
		t.Fatalf("union_tag_both_plans: the forged tag lands None, not %%v (record %%+v)", back[0].Pick.Type, back[0])
	}
	if back[0].Seq != 15 {
		t.Fatalf("union_tag_both_plans: the neighbour after the union must stand at 15, not %%v (record %%+v)", back[0].Seq, back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("union_tag_both_plans: the clamp counts once, Clamped=%%d, want 1 (report %%+v)", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("union_tag_both_plans: a counter other than Clamped moved: %%+v", r)
	}
}
`, file, table)

	if out, err := runVersionProbe(t, newer, []string{older}, compiled); err != nil {
		t.Fatalf("union_tag_both_plans, the compiled plan: %v\n%s", err, out)
	}
	if out, err := runVersionProbe(t, older, nil, identity); err != nil {
		t.Fatalf("union_tag_both_plans, the identity plan: %v\n%s", err, out)
	}
}
