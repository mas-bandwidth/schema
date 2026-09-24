package jstable

// fixed_form_ragged_tail is F8 of schema#876: a record region that is not a
// whole number of records is "malformed", and no leg tests it — matrix row
// G~GGPPPP~, a GAP on go/cpp/cs and only PARTIAL on c/elixir. This leg's grep
// found only F7 (fixedform_under_20_bytes_test.go) asserting malformed, and it
// forges the SHORT header, never a ragged tail: nothing appends a partial record.
// It is F7's direct sibling, the next guard in the same emitted function; go
// closed it as #1291. "malformed" and "refused" are the reader's TWO answers and
// are NEVER both set. recordBytes is known.recordBytes, compiled in from the
// selected lineage entry, so the recordBytes <= 8 arm is NOT reachable from a
// file at all. rest is byteLength - layoutAt - layoutBytes, the file's bytes
// after header+layout, so this row forges only rest % recordBytes !== 0 by
// appending 1 to recordBytes-1 bytes. extra == recordBytes is one more whole
// record, not a ragged tail. CONTROL 2 (the ragged arm off) made the reader
// report SUCCESS — malformed=false, n=1, the destination written — not a crash.

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
const clean = TLoad(back, back.length, data, data.length, plan, first);
if (clean < 1 || first.malformed || first.refused !== 0) {
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
  if (report.malformed !== true) {
    fail("extra=" + extra + ": a ragged tail is malformed=true, got malformed=" + report.malformed);
  }
  if (report.refused !== TableFixedRefusal.None) {
    fail("extra=" + extra + ": a ragged tail is the residue, not a refusal: refused=" + TableFixedRefusalName(report.refused));
  }
  if (report.unknown !== 0 || report.kindMismatch !== 0 || report.clamped !== 0 || report.widened !== 0 || report.duplicate !== 0) {
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
  if (n !== -1 || report.malformed || report.refused !== TableFixedRefusal.BatchTooLarge) {
    fail("extra == recordBytes is one more whole record, so it owes batch_too_large, not malformed: n=" + n + " " + reason(report));
  }
}
`, home, file, table)

	out, err := jsRunVersionProbe(t, node, older, nil, 0, table, body)
	if err != nil {
		t.Fatalf("a record region that is not whole records is malformed, not refused: %v\n%s", err, out)
	}
}
