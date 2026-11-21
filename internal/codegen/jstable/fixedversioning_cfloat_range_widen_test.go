package jstable

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8, "Hostile rows"):
// OLD `aim float32 | min = -1, max = 1, resolution = 0.01`, NEW `min = -2, max =
// 2` — the range WIDENED. In the fixed form a compressed float rides as the
// float32 ITSELF (SPEC §3.4), so min and max are DEFINITIONS in the digest; the
// lineage plan carries the OLD writer's range, and the NEW reader holds a value
// read under that entry to the WRITER's range (docs/rulings.json
// strict-only-read, FIXED-FORM-ALGORITHM §4.6): a value outside it refuses the
// whole load out_of_range, never a clamp. `old_` (0.5, inside the writer's
// range) is the control and lands exactly; `hostile_` (1.5: inside the reader's
// range, outside the writer's) refuses whole, which only the writer's pass can
// make it do; `past_` (5.0, outside both) refuses whole. A refusal leaves the
// destination untouched and moves no counter.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_cfloat_range_widen")
	older := jsReadSchema(t, "VOLD_cfloat_range_widen")
	table := jsFixedRootName(t, older)
	oldAim := jsManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim")
	src := jsFloatBitsPrelude + fmt.Sprintf(`
const LOAD = (f) => {
  const data = readFileSync(f);
  const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
  const fresh0 = show(back[0]);
  const r = new TableFixedReport();
  const plan = TNewPlan(4096, 4096);
  const n = TLoad(back, back.length, data, data.length, plan, r);
  if (r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.repeated !== 0) {
    fail(f + ": a counter that must not move did: " + reason(r));
  }
  return { back, fresh0, r, n };
};
{
  const { back, r, n } = LOAD(%[1]q);
  if (n !== 1 || r.refused !== 0 || r.damaged || r.oor !== 0) {
    fail("old_: the control lands one record: n=" + n + " " + reason(r));
  }
  if (back[0].lead !== 1 || back[0].trail !== 2) {
    fail("old_: the row moved a neighbor: " + show(back[0]));
  }
  if (f32bits(back[0].aim) !== %[4]d) {
    fail("old_: aim landed bits " + f32bits(back[0].aim).toString(16) + ", want " + (%[4]d).toString(16) + " (" + back[0].aim + ")");
  }
}
const REFUSE = (f, tag) => {
  const { back, fresh0, r, n } = LOAD(f);
  if (n !== -1 || r.refused !== TableFixedRefusal.OutOfRange || r.damaged || r.oor !== 0) {
    fail(tag + ": a value past the WRITER's range owes out_of_range: n=" + n + " " + reason(r));
  }
  if (show(back[0]) !== fresh0) {
    fail(tag + ": a refused read must leave the destination untouched: " + show(back[0]));
  }
};
REFUSE(%[2]q, "hostile_");
REFUSE(%[3]q, "past_");
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), oldAim)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("cfloat_range_widen: %v\n%s", err, out)
	}
}
