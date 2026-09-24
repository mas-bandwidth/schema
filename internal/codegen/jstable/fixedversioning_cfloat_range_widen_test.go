package jstable

// cfloat_range_widen (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8): OLD `aim
// float32 | min = -1, max = 1, resolution = 0.01`, NEW `min = -2, max = 2` — the
// range WIDENED. In the fixed form a compressed float rides as the float32 ITSELF
// (SPEC §3.4), so min and max are DEFINITIONS in the digest, never carried on the
// wire: the emitter lays down one pass over the READER's own min and max, and the
// OLD writer's bound has nowhere to live once a record is resolved. The clamp
// counter is asserted EXACTLY on every read, never `>= 1`: a value inside the new
// range lands WHOLE and UNCOUNTED, and only a value past the READER's own bound
// clamps and counts. `hostile_` landing 1.5 rather than the OLD writer's 1.0 is
// what proves the pass is the READER's and not the writer's; on its own that read
// stays green under either rule, and only `past_` landing 2.0 rather than the
// forged 5.0 proves there is a pass at all.

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
	hostileAim := jsManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim")
	src := jsFloatBitsPrelude + fmt.Sprintf(`
const READ = (f, wantBits, clamped, tag) => {
  const data = readFileSync(f);
  const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
  const report = new TableFixedReport(); const r = report;
  const plan = TNewPlan(4096, 4096);
  const n = TLoad(back, back.length, data, data.length, plan, report);
  if (n !== 1) { fail(tag + ": n is not 1: " + n + " " + reason(r)); }
  if (back[0].Lead !== 1 || back[0].Trail !== 2) {
    fail(tag + ": the row moved a neighbour: " + show(back[0]));
  }
  if (f32bits(back[0].Aim) !== wantBits) {
    fail(tag + ": aim landed bits " + f32bits(back[0].Aim).toString(16) + ", want " + wantBits.toString(16) + " (" + back[0].Aim + ")");
  }
  if (r.clamped !== clamped) {
    fail(tag + ": clamped landed " + r.clamped + ", want exactly " + clamped + ": " + reason(r));
  }
  if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
    fail(tag + ": counters moved on a clean read: " + reason(r));
  }
  if (r.malformed) { fail(tag + ": not malformed: " + reason(r)); }
  if (r.refused !== 0) { fail(tag + ": not a refusal: " + reason(r)); }
};
READ(%[1]q, %[4]d, 0, "old_");
READ(%[2]q, %[5]d, 0, "hostile_");
READ(%[3]q, 0x40000000, 1, "past_");
`, filepath.Join(corpus, "old_cfloat_range_widen.bin"),
		filepath.Join(corpus, "hostile_cfloat_range_widen.bin"),
		filepath.Join(corpus, "past_cfloat_range_widen.bin"), oldAim, hostileAim)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("cfloat_range_widen: %v\n%s", err, out)
	}
}
