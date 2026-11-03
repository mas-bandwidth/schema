package jstable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated NEW reader over the forged file)")
	corpus := jsFixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_range_widen")
	older := jsReadSchema(t, "VOLD_range_widen")
	table := jsFixedRootName(t, older)
	src := fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const r = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, r);
console.log("range_widen_hostile: landed v=" + back[0].v + " clamped=" + r.clamped);
if (n !== 1 || r.malformed || r.refused !== 0) { fail("the forged file reads one record: n=" + n + " " + reason(r)); }
if (back[0].v !== %[2]d || r.clamped !== %[3]d) {
  fail("v=" + back[0].v + " clamped=" + r.clamped + ", want v=%[2]d clamped=%[3]d: the forged 150 clamps to the WRITER's max and counts once");
}
if (back[0].lead !== %[4]d || back[0].trail !== %[5]d) { fail("the row moved a neighbour: " + show(back[0])); }
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) { fail("a counter that must not move did: " + reason(r)); }
`, filepath.Join(corpus, fixedvector.RangeWidenHostile), v, want.Clamped, lead, trail)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
