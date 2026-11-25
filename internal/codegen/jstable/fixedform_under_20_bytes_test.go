package jstable

// fixed_form_under_20_bytes is F7 of schema#876: a file SHORTER than the fixed
// header REFUSES, recommended name `truncated`, with nothing landed
// (docs/FIXED-FORM-ALGORITHM.md §5.3's refusal table: "no bytes at all — there
// is no first byte to read | truncated" and "shorter than 20 bytes, the form
// byte having already answered | truncated"; "There is no `malformed` flag on
// this form's report"). The verdict is the cross-language answer and the name
// is this leg's diagnostic. Asserting `layout_malformed` here would be wrong:
// that name is a KNOWN hash whose layout bytes lie, never a file with no
// header. The destination is proved untouched with a sentinel, not assumed,
// and the report is compared WHOLE against a fresh report whose only change is
// refused = Truncated: no counter moves, no layout hash, no second channel.
//
// PLANT: the emitter's short-file guard `report.refused = TableFixedRefusal.Truncated`
// put back to `report.damaged = true` turns this row RED (refused=none and the
// whole-report compare differs).

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
if (clean < 1 || first.refused !== 0) {
  fail("the corpus file must read cleanly first: n=" + clean + " " + reason(first));
}

const SENT = 0x5A5A5A5A;
for (let k = 0; k < 8; k++) { back[k].X = SENT; back[k].Y = SENT; back[k].Z = SENT; }

const fresh = new TableFixedReport();
fresh.refused = TableFixedRefusal.Truncated;

for (let k = 0; k < TableFixedHeaderBytes + TableFixedLayoutHeaderBytes; k++) {
  const short = data.subarray(0, k);
  const report = new TableFixedReport();
  const n = TLoad(back, back.length, short, short.length, plan, report);
  if (n !== -1) { fail("k=" + k + ": a file shorter than the header returns -1, got n=" + n); }
  if (report.refused !== TableFixedRefusal.Truncated) {
    fail("k=" + k + ": a file shorter than the header refuses truncated, got refused=" + TableFixedRefusalName(report.refused));
  }
  if (TableFixedRefusalName(report.refused) !== "truncated") {
    fail("k=" + k + ": the name is spelled truncated, got " + TableFixedRefusalName(report.refused));
  }
  if ("damaged" in report) { fail("k=" + k + ": the fixed form's report has no malformed flag (§5.3): damaged=" + report.damaged); }
  if (show(report) !== show(fresh)) {
    fail("k=" + k + ": the report is not the fresh report plus refused=truncated: " + show(report));
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
		t.Fatalf("a file under the header refuses truncated: %v\n%s", err, out)
	}
}
