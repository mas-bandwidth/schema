package jstable

// A COMPRESSED FLOAT'S NEGATIVE ZERO IS +0 (docs/SPEC-TABLES.md §3.3, §3.4;
// docs/rulings.json fixed-compressed-negzero). The fixed form carries the
// float's value, so a record holding the sign bit alone is inside every range
// that holds +0; the read lands the value 0 itself, refuses nothing and counts
// nothing, and a read-then-save writes the +0 image. A PLAIN float's -0 is
// carried bit-exact and is not this rule.
//
// The file is the reference's own compressed-float row (old_cfloat_range_widen)
// with its one field changed to -0's bits; the canonical file is the same bytes
// with the field at +0.

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated reader over the compressed-float row)")
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	src := jsReadSchema(t, "VOLD_cfloat_range_widen")
	table := jsFixedRootName(t, src)
	file := filepath.Join(corpus, "old_cfloat_range_widen.bin")
	probe := jsFloatBitsPrelude + fmt.Sprintf(`
const data = readFileSync(%[1]q);
const layoutAt = 20;
const layoutBytes = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
const aimAt = layoutAt + layoutBytes + 8 + 4;
function variant(bits) {
  const out = Uint8Array.from(data);
  new DataView(out.buffer).setUint32(aimAt, bits, true);
  return out;
}
function read(bytes, tag) {
  const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, bytes, bytes.length, TNewPlan(4096, 4096), r);
  if (n !== 1) { fail(tag + ": the file must read one record, n=" + n + " " + reason(r)); }
  if (back[0].lead !== 1 || back[0].trail !== 2) { fail(tag + ": a neighbour moved: " + show(back[0])); }
  if (!Object.is(back[0].aim, 0) || f32bits(back[0].aim) !== 0) {
    fail(tag + ": aim is not +0: value " + back[0].aim + " bits " + f32bits(back[0].aim).toString(16));
  }
  if (r.oor !== 0 || r.damaged || r.refused !== 0) { fail(tag + ": -0 is inside the range and uncounted: " + reason(r)); }
  if (r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.repeated !== 0) {
    fail(tag + ": counters moved on a clean read: " + reason(r));
  }
  return back;
}
const back = read(variant(0x80000000), "negative_zero");
const out = new Uint8Array(TMeasure(1));
if (TSave(back, 1, out) !== out.length) { fail("negative_zero: the save refused"); }
const canonical = variant(0);
if (out.length !== canonical.length) { fail("negative_zero: the save is " + out.length + " bytes, the canonical file " + canonical.length); }
for (let i = 0; i < canonical.length; i++) {
  if (out[i] !== canonical[i]) { fail("negative_zero: the save differs from the +0 image at byte " + i + ": " + out[i] + " vs " + canonical[i]); }
}
`, file)
	out, err := jsRunVersionProbe(t, node, src, nil, 0, table, probe)
	if err != nil {
		t.Fatalf("compressed -0: %v\n%s", err, out)
	}
}
