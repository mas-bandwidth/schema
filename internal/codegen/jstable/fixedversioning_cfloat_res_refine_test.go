package jstable

// The cfloat_res_refine row (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8):
// OLD `aim float32 | min = -1, max = 1, resolution = 0.1`, NEW `resolution =
// 0.01`. The step REFINED, which is this form's widening: in the FIXED form a
// compressed float rides as the float32 ITSELF (SPEC §3.4), so the resolution
// is a DEFINITION in the digest ('Q', bill §13) and the wire hash moves when
// the step does. There is no forge and no hostile file: a value off the old
// grid is still a float32 the reader lands. Reading `old_cfloat_res_refine.bin`
// on the NEW build lands every old value EXACTLY — the old writer quantized to
// 0.1, a whole multiple of this reader's 0.01, so nothing requantizes and
// `clamped == 0` EXACTLY (a `>= 1` would pass the read this row exists to
// catch, where any requantization is the bug).

import (
	"fmt"
	"path/filepath"
	"testing"
)

// `aim` IS COMPARED BY BITS, AGAINST THE MANIFEST (schema#1164, Glenn
// 2026-09-19: "do whatever is needed to make sure that a new reader can read an
// old writer"). It was `back[0].Aim !== Math.fround(0.3)` — a value comparison
// against a HARDCODED literal. JS has ONE numeric type, so the only way to
// compare a float32 exactly is to store it as one, which `f32bits` does.
func TestJSFixedVersioningCfloatResRefine(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_cfloat_res_refine")
	older := jsReadSchema(t, "VOLD_cfloat_res_refine")
	table := jsFixedRootName(t, older)
	aim := jsManifestFloatBits(t, corpus, "old_cfloat_res_refine.bin", "values", "r0.aim")
	src := jsFloatBitsPrelude + fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== 1) { fail("cfloat_res_refine: n is not 1: " + n + " " + reason(r)); }
if (back[0].Lead !== 1 || back[0].Trail !== 2) {
  fail("the row moved a neighbour: " + show(back[0]));
}
if (f32bits(back[0].Aim) !== %[2]d) {
  fail("the refined reader lands the old writer's value BIT-EXACT: aim bits " + f32bits(back[0].Aim).toString(16) + ", manifest " + (%[2]d).toString(16) + ": " + show(back[0]));
}
if (r.clamped !== 0) {
  fail("0.1 is a whole multiple of 0.01, so nothing requantizes and clamped is EXACTLY 0: " + reason(r));
}
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("counters moved on a clean backward read: " + reason(r));
}
if (r.malformed) { fail("a clean NEW-READS-OLD is not malformed: " + reason(r)); }
if (r.refused !== 0) { fail("a clean NEW-READS-OLD is not a refusal: " + reason(r)); }
`, filepath.Join(corpus, "old_cfloat_res_refine.bin"), aim)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("cfloat_res_refine: %v\n%s", err, out)
	}
}
