package jstable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNSPECIFIED on separate-storage targets. THE ROW'S CONTENT IS THE
// ASSERTION IT REFUSES TO MAKE: it does NOT compare pick.beta or pick.gamma; a
// reader who "completes" it by asserting pick.gamma.p == 0 reversed a ruling and
// should read the issue first. JS leaves the caller's bytes, and that is lawful.
// (Named *_arm_row_test.go, not *_arm_test.go: Go reads a trailing _arm before
// _test.go as a GOARCH build constraint and silently skips the file.)

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	older := jsReadSchema(t, "VOLD_union_append")
	newer := jsReadSchema(t, "VNEW_union_append")
	table := jsFixedRootName(t, older)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE SELECTED ARM AND THE TAG ARE THE WHOLE OF WHAT A UNION READ PROMISES.
	// Nothing here names beta or gamma.
	checks := `
if (n !== 1) { fail("the file carries one record, not " + n + " " + reason(r)); }
if (back[0].Pick.Type !== 1) { fail("pick.type is the writer's alpha arm (1), not " + back[0].Pick.Type); }
if (back[0].Pick.Alpha.M !== 7) { fail("pick.alpha.m is the writer's 7, not " + back[0].Pick.Alpha.M); }
if (back[0].Seq !== 15) { fail("seq is the writer's 15, not " + back[0].Seq); }
if (r.clamped !== 0 || r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("a clean read moved a counter: " + reason(r));
}
if (r.malformed) { fail("a clean backward read is not malformed: " + reason(r)); }
if (r.refused !== 0) { fail("a clean backward read is not a refusal: " + reason(r)); }
`

	// probe 1, the compiled column: reader VNEW, older VOLD. The poison is laid
	// field by field across the VALUE SURFACE, because the question is what the
	// image-to-value scatter does to an unselected arm.
	compiled := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
back[0].Pick.Type = 0x5A5A5A5A;
back[0].Pick.Alpha.M = 0x5A5A5A5A;
back[0].Pick.Beta.N = 0x5A5A5A5A;
back[0].Pick.Gamma.P = 0x5A5A5A5A;
back[0].Seq = 0x5A5A5A5A;
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
%s
`, file, checks)
	if out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, compiled); err != nil {
		t.Fatalf("union_unselected_arm, compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older. Its own hash, its own
	// plan, the same file, the same poison — without the arm the old build does
	// not declare.
	identity := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
back[0].Pick.Type = 0x5A5A5A5A;
back[0].Pick.Alpha.M = 0x5A5A5A5A;
back[0].Pick.Beta.N = 0x5A5A5A5A;
back[0].Seq = 0x5A5A5A5A;
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
%s
`, file, checks)
	if out, err := jsRunVersionProbe(t, node, older, nil, 0, table, identity); err != nil {
		t.Fatalf("union_unselected_arm, identity column: %v\n%s", err, out)
	}
}
