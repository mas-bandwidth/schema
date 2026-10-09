package gotable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNDEFINED — one word, for every target (Glenn, 2026-09-19: "as
// designed it is 'undefined'"). A union read DEFINES the tag and the SELECTED arm
// and nothing else. The row's whole content is the assertion it REFUSES to make:
// this probe does NOT compare pick.beta or pick.gamma, and a reader who
// "completes" it by asserting pick.gamma.p == 0 has reversed a ruling and should
// read the issue first. That this leg happens to land the declared default in
// every unselected arm is an OBSERVATION AND NOT A GUARANTEE, and nothing may be
// relied on it. The 0x5A poison is carried for uniformity with the other legs
// but cannot be observed here, because the prefill resets every unselected arm to
// its default. (Named *_arm_row_test.go, not *_arm_test.go: a trailing _arm before
// _test.go is a GOARCH build constraint that silently skips the file.)

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_union_append")
	newer := readSchema(t, "VNEW_union_append")
	table := fixedRootName(t, older)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ PROMISES.
	// Nothing here names beta or gamma.
	checks := `
	if n != 1 {
		t.Fatalf("the file carries one record, not %d (%+v)", n, r)
	}
	if back[0].Pick.Type != PickTypeAlpha {
		t.Fatalf("pick.type is the writer's alpha arm, not %v", back[0].Pick.Type)
	}
	if back[0].Pick.Alpha.M != 7 {
		t.Fatalf("pick.alpha.m is the writer's 7, not %v", back[0].Pick.Alpha.M)
	}
	if back[0].Seq != 15 {
		t.Fatalf("seq is the writer's 15, not %v", back[0].Seq)
	}
	if r.Retained != 0 || r.RetainLost != 0 || r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("a clean read moved a counter: %+v", r)
	}
	if r.Malformed {
		t.Fatalf("a clean backward read is not malformed: %+v", r)
	}
	if r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("a clean backward read is not a refusal: %+v", r)
	}
	`

	// probe 1, the compiled column: reader VNEW, older VOLD. The poison is laid
	// field by field across the value surface, exactly as the sibling legs do;
	// on this leg the prefill resets every unselected arm to its default, so the
	// poison is invisible and carried only for uniformity.
	compiled := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestUnionUnselectedArm(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	back[0].Pick.Type = PickType(0x5A)
	back[0].Pick.Alpha.M = 0x5A5A5A5A
	back[0].Pick.Beta.N = 0x5A5A5A5A
	back[0].Pick.Gamma.P = 0x5A5A5A5A
	back[0].Seq = 0x5A5A5A5A
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	%[3]s
}
`, file, table, checks)
	if out, err := runVersionProbe(t, newer, []string{older}, compiled); err != nil {
		t.Fatalf("union_unselected_arm, compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older. Its own hash, its own
	// plan, the same file, the same poison — without the arm the old build does
	// not declare.
	identity := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestUnionUnselectedArm(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	back[0].Pick.Type = PickType(0x5A)
	back[0].Pick.Alpha.M = 0x5A5A5A5A
	back[0].Pick.Beta.N = 0x5A5A5A5A
	back[0].Seq = 0x5A5A5A5A
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	%[3]s
}
`, file, table, checks)
	if out, err := runVersionProbe(t, older, nil, identity); err != nil {
		t.Fatalf("union_unselected_arm, identity column: %v\n%s", err, out)
	}
}
