package javatable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNSPECIFIED on separate-storage targets. THE ROW'S CONTENT IS THE
// ASSERTION IT REFUSES TO MAKE: this test asserts the tag, the SELECTED arm and
// seq, and does NOT compare pick.beta or pick.gamma. Java leaves the caller's
// bytes in an unselected arm, and that is lawful. A poison on this leg must be
// the TOTAL one: a nested value and a union arm are both emitted final, and the
// shallow poison skips final fields, so it would poison nothing that matters —
// that exact gap is what surfaced schema#1157. A reader who "completes" this by
// asserting pick.gamma.p == 0 has reversed a ruling and should read the issue
// first. (Named *_arm_row_test.go, not *_arm_test.go: a trailing _arm before
// _test.go is a GOARCH build constraint.)

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_union_append.bin")
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatalf("the corpus has no old_union_append.bin: run make tables-fixedform-corpus")
	}

	_, classes := buildRow(t, t.TempDir(), "union_unselected_arm", []sideSpec{
		{key: "compiled", schema: "VNEW_union_append.schema", older: []string{"VOLD_union_append.schema"}},
		{key: "identity", schema: "VOLD_union_append.schema"},
	})

	_, javaBin := javaTools(t)

	for _, col := range []struct{ name, class string }{
		{"compiled column", "Probe_compiled"},
		{"identity column", "Probe_identity"},
	} {
		// The TOTAL poison ("deep") is the row's own: a union arm is emitted
		// final, and the shallow poison skips final fields, so it would leave the
		// arm's own zero rather than the 0x5A that proves the poison reached it.
		out, err := exec.Command(javaBin, "-ea", "-cp", classes, col.class, oldFile, "deep").CombinedOutput()
		if err != nil {
			t.Fatalf("java %s %s deep: %v\n%s", col.class, filepath.Base(oldFile), err, out)
		}
		r := parseProbe(t, string(out))

		// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ
		// PROMISES. Nothing here names beta or gamma.
		if r.n != 1 {
			t.Errorf("union_unselected_arm, %s: n=%d, want 1", col.name, r.n)
		}
		if r.refused || r.malformed {
			t.Errorf("union_unselected_arm, %s: a clean read refused: refused=%v reason=%s malformed=%v", col.name, r.refused, r.reason, r.malformed)
		}
		if got := r.value["pick.type"]; got != "1" {
			t.Errorf("union_unselected_arm, %s: pick.type=%s, want the writer's alpha arm (1)", col.name, got)
		}
		if got := r.value["pick.alpha.m"]; got != "7" {
			t.Errorf("union_unselected_arm, %s: pick.alpha.m=%s, want the writer's 7", col.name, got)
		}
		if got := r.value["seq"]; got != "15" {
			t.Errorf("union_unselected_arm, %s: seq=%s, want the writer's 15", col.name, got)
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("union_unselected_arm, %s: a counter moved on a clean read: unknown=%d kindMismatch=%d widened=%d clamped=%d, want all 0",
				col.name, r.unknown, r.kindMismatch, r.widened, r.clamped)
		}
	}
}
