package jstable

// A COMPRESSED FLOAT'S NEGATIVE ZERO IS +0 (docs/SPEC-TABLES.md §3.3, §3.4;
// docs/rulings.json fixed-compressed-negzero). The shared vector
// (fixedvector.NegativeZero) is the reference writer's file holding the sign
// bit alone in its compressed float field. The generated JavaScript load lands
// the value 0 itself with the sign bit clear, refuses nothing and counts
// nothing, and a read-then-save writes the canonical +0 bytes. JavaScript's
// -0 compares equal to 0, so the sign is looked at with Object.is and the
// float32 bits. A PLAIN float's -0 is carried bit-exact and is not this rule.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated reader over the negative-zero vector)")
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	src := jsReadSchema(t, "FNZ")
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			probe := jsFloatBitsPrelude + fmt.Sprintf(`
const data = readFileSync(%[1]q);
const view = new DataView(data.buffer, data.byteOffset, data.length);
const layoutBytes = view.getUint32(16, true);
const valueAt = 16 + 4 + layoutBytes + 8 + 4;
if (view.getUint32(valueAt, true) !== 0x80000000) {
  fail(%[2]q + ": the vector does not hold a compressed float's -0: " + view.getUint32(valueAt, true).toString(16));
}
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const r = new TableFixedReport();
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), r);
if (n !== 1) { fail(%[2]q + ": the reader refused a compressed -0, n=" + n + " " + reason(r)); }
if (back[0].lead !== 1 || back[0].trail !== 2) { fail(%[2]q + ": a neighbour moved: " + show(back[0])); }
if (!Object.is(back[0].value, 0) || f32bits(back[0].value) !== 0) {
  fail(%[2]q + ": a compressed float's -0 kept its sign: value " + back[0].value + " bits " + f32bits(back[0].value).toString(16));
}
if (r.oor !== 0 || r.damaged || r.refused !== 0 || r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.repeated !== 0) {
  fail(%[2]q + ": the read of a compressed -0 counted something: " + reason(r));
}
const out = new Uint8Array(TMeasure(1));
if (TSave(back, 1, out) !== out.length) { fail(%[2]q + ": the save refused"); }
const want = Uint8Array.from(data);
want.fill(0, valueAt, valueAt + 4);
if (out.length !== want.length) { fail(%[2]q + ": the save is " + out.length + " bytes, the file " + want.length); }
for (let i = 0; i < want.length; i++) {
  if (out[i] !== want[i]) { fail(%[2]q + ": the save differs from the +0 image at byte " + i + ": " + out[i] + " vs " + want[i]); break; }
}
`, filepath.Join(corpus, vec.File), vec.File)
			out, err := jsRunVersionProbe(t, node, src, nil, 0, vec.Root, probe)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			if strings.Contains(string(out), "FAIL:") {
				t.Fatalf("%s: %s", vec.File, out)
			}
		})
	}
}
