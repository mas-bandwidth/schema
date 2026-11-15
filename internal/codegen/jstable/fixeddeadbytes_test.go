package jstable

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO ON READ (docs/SPEC-TABLES.md §3.4,
// §13.10; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/rulings.json
// fixed-dead-bytes-are-zero). The shared corpus (fixedvector.DeadBytes) is one
// control per dead region — every dead byte zero — and one file per region made
// non-zero. The generated JavaScript load must REFUSE the whole load for the
// changed files before any value lands, and read and save the controls back
// byte for byte. The verdict is the contract, never the refusal's name.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated reader over the dead-byte corpus)")
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	src := jsReadSchema(t, "DeadBytes")
	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			table := vec.Root
			verdict := fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const fresh0 = show(back[0]);
const r = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, r);
if (n !== -1) { fail(%[2]q + ": the load must refuse, n=" + n + " " + reason(r)); }
if (show(back[0]) !== fresh0) { fail(%[2]q + ": a refused load must leave the destination untouched: " + show(back[0])); }
`, filepath.Join(corpus, vec.File), vec.File)
			if vec.Reads {
				verdict = fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const r = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, r);
if (n !== 1 || r.damaged || r.refused !== 0) { fail(%[2]q + ": the control must read one record, n=" + n + " " + reason(r)); }
const out = new Uint8Array(TMeasure(1));
if (TSave(back, 1, out) !== out.length) { fail(%[2]q + ": the control's save refused"); }
if (out.length !== data.length) { fail(%[2]q + ": the control's save is " + out.length + " bytes, the file " + data.length); }
for (let i = 0; i < data.length; i++) {
  if (out[i] !== data[i]) { fail(%[2]q + ": the control's save differs at byte " + i + ": " + out[i] + " vs " + data[i]); }
}
`, filepath.Join(corpus, vec.File), vec.File)
			}
			out, err := jsRunVersionProbe(t, node, src, nil, 0, table, verdict)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			if strings.Contains(string(out), "FAIL:") {
				t.Fatalf("%s: %s", vec.File, out)
			}
			t.Logf("%s", out)
		})
	}
}
