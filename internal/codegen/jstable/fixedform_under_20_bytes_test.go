package jstable

// fixed_form_under_20_bytes is F7 of schema#876: a file SHORTER than the fixed
// header is "malformed", and no leg tests it — its matrix row is GGGGGGGGG, a
// gap on all nine legs. "malformed" and "refused" are the reader's TWO answers
// and are NEVER both set: "refused" plus "reason" is one answer, "malformed" is
// the other, and the counters are a third thing again. A short file is the
// RESIDUE, not a refusal by name, so "refused" stays false and "reason" stays
// UNTOUCHED — asserting "layout_malformed" here would assert the OPPOSITE of the
// contract: that name is a KNOWN hash whose layout bytes lie, never a file with
// no header. The destination is proved untouched with a sentinel, not assumed,
// and a FRESH report is compared whole. CONTROL 2 (the emitter's short-file
// guard disabled) turns this row RED with a WRONG REPORT — refused=layout_malformed,
// malformed=false — not a thrown exception: array reads return undefined, not panic.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedFormUnder20Bytes(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	older := jsReadSchema(t, "VOLD_field_append")
	table := jsFixedRootName(t, older)
	home := runtimeHome(jsUnitOf(t, older))

	file := filepath.Join(corpus, "old_field_append.bin")
	body := fmt.Sprintf(`
import { TableFixedHeaderBytes, TableFixedLayoutHeaderBytes } from "./%[1]sTable.js";
const data = readFileSync(%[2]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const plan = TNewPlan(4096, 4096);

const first = new TableFixedReport();
const clean = TLoad(back, back.length, data, data.length, plan, first);
if (clean < 1 || first.malformed || first.refused !== 0) {
  fail("the corpus file must read cleanly first: n=" + clean + " " + reason(first));
}

const SENT = 0x5A5A5A5A;
for (let k = 0; k < 8; k++) { back[k].X = SENT; back[k].Y = SENT; back[k].Z = SENT; }

const fresh = new TableFixedReport();
fresh.malformed = true;

for (let k = 0; k < TableFixedHeaderBytes + TableFixedLayoutHeaderBytes; k++) {
  const short = data.subarray(0, k);
  const report = new TableFixedReport();
  const n = TLoad(back, back.length, short, short.length, plan, report);
  if (n !== -1) { fail("k=" + k + ": a file shorter than the header returns -1, got n=" + n); }
  if (report.malformed !== true) {
    fail("k=" + k + ": a file shorter than the header is malformed=true, got malformed=" + report.malformed);
  }
  if (report.refused !== TableFixedRefusal.None) {
    fail("k=" + k + ": a short file is the residue, not a refusal: refused=" + TableFixedRefusalName(report.refused));
  }
  if (TableFixedRefusalName(report.refused) !== "none") {
    fail("k=" + k + ": reason stays untouched, got " + TableFixedRefusalName(report.refused));
  }
  if (show(report) !== show(fresh)) {
    fail("k=" + k + ": the report is not the fresh report plus malformed=true: " + show(report));
  }
  for (let r = 0; r < 8; r++) {
    if (back[r].X !== SENT || back[r].Y !== SENT || back[r].Z !== SENT) {
      fail("k=" + k + ": a short read wrote the destination: " + show(back[r]));
    }
  }
}
`, home, file)

	out, err := jsRunVersionProbe(t, node, older, nil, 0, table, body)
	if err != nil {
		t.Fatalf("a file under the header is malformed, not refused: %v\n%s", err, out)
	}
}
