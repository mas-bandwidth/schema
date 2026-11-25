package jstable

// fixed_form_ragged_tail is F8 of schema#876: a record region that is not a
// whole number of records REFUSES, recommended name `truncated`
// (docs/FIXED-FORM-ALGORITHM.md §5.3's refusal table, "a ragged tail:
// rest mod record_bytes != 0 | truncated | the name"; "There is no `malformed`
// flag on this form's report"). The verdict is the cross-language answer and
// the name is this leg's diagnostic, so the row asserts both: refused, by the
// name Truncated, every counter at zero and not one destination byte written.
// It is F7's direct sibling (fixedform_under_20_bytes_test.go), the next guard
// in the same emitted function. recordBytes is known.recordBytes, compiled in
// from the selected lineage entry, so the recordBytes <= 8 arm is NOT
// reachable from a file at all. rest is byteLength - layoutAt - layoutBytes,
// the file's bytes after header+layout, so this row forges only
// rest % recordBytes !== 0 by appending 1 to recordBytes-1 bytes.
// extra == recordBytes is one more whole record, not a ragged tail.
//
// THE REPORT CARRIES NO SECOND ANSWER CHANNEL: a fresh TableFixedReport has no
// `damaged` property at all, and a ragged read does not grow one.
//
// PLANT: the emitter's ragged guard `report.refused = TableFixedRefusal.Truncated`
// put back to `report.damaged = true` turns this row RED (refused=none, and a
// `damaged` property appears on the report).

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedFormRaggedTail(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	older := jsReadSchema(t, "VOLD_field_append")
	table := jsFixedRootName(t, older)
	home := runtimeHome(jsUnitOf(t, older))

	file := filepath.Join(corpus, "old_field_append.bin")
	body := fmt.Sprintf(`
import { TableFixedHeaderBytes, TableFixedLayoutHeaderBytes } from "./%[1]sTable.js";
import { %[3]sFixedRecordBytes } from "./ProbeTable.js";
const data = readFileSync(%[2]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const plan = TNewPlan(4096, 4096);

const recordBytes = %[3]sFixedRecordBytes;
if (recordBytes <= 8) { fail("this row needs a record over eight bytes: recordBytes=" + recordBytes); }

const first = new TableFixedReport();
if ("damaged" in first) { fail("the fixed form's report has no malformed flag (§5.3), but a fresh report carries damaged"); }
const clean = TLoad(back, back.length, data, data.length, plan, first);
if (clean < 1 || first.refused !== 0) {
  fail("the corpus file must read cleanly first: n=" + clean + " " + reason(first));
}

const SENT = 0x5A5A5A5A;

for (let extra = 1; extra < recordBytes; extra++) {
  for (let k = 0; k < 8; k++) { back[k].X = SENT; back[k].Y = SENT; back[k].Z = SENT; }
  const ragged = new Uint8Array(data.length + extra);
  ragged.set(data);
  const report = new TableFixedReport();
  const n = TLoad(back, back.length, ragged, ragged.length, plan, report);
  if (n !== -1) { fail("extra=" + extra + ": a ragged tail returns -1, got n=" + n); }
  if (report.refused !== TableFixedRefusal.Truncated) {
    fail("extra=" + extra + ": a ragged tail refuses truncated, got refused=" + TableFixedRefusalName(report.refused));
  }
  if (TableFixedRefusalName(report.refused) !== "truncated") {
    fail("extra=" + extra + ": the name is spelled truncated, got " + TableFixedRefusalName(report.refused));
  }
  if ("damaged" in report) { fail("extra=" + extra + ": a refusal grows no second answer channel: damaged=" + report.damaged); }
  if (report.unknown !== 0 || report.mismatch !== 0 || report.oor !== 0 || report.widened !== 0 || report.repeated !== 0) {
    fail("extra=" + extra + ": a ragged tail moves no counter: " + reason(report));
  }
  for (let k = 0; k < 8; k++) {
    if (back[k].X !== SENT || back[k].Y !== SENT || back[k].Z !== SENT) {
      fail("extra=" + extra + ": a ragged read wrote the destination: " + show(back[k]));
    }
  }
}

{
  const back1 = [new T()]; InitT(back1[0]);
  const whole = new Uint8Array(data.length + recordBytes);
  whole.set(data);
  const report = new TableFixedReport();
  const n = TLoad(back1, back1.length, whole, whole.length, plan, report);
  if (n !== -1 || report.refused !== TableFixedRefusal.BatchTooLarge) {
    fail("extra == recordBytes is one more whole record, so it owes batch_too_large, not truncated: n=" + n + " " + reason(report));
  }
}
`, home, file, table)

	out, err := jsRunVersionProbe(t, node, older, nil, 0, table, body)
	if err != nil {
		t.Fatalf("a record region that is not whole records refuses truncated: %v\n%s", err, out)
	}
}
