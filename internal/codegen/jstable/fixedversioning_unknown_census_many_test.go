package jstable

// §5.4 row 11, `unknown_census`, NEW-READS-OLD over THREE records. The compile
// census lands ONCE PER PEER and NEVER PER RECORD, and the landed one-record row
// cannot close the second half of that clause: with one record "once per peer"
// and "once per record" are the same number, 1. `Item.drop` is ONE field of ONE
// peer, so `unknown` is asserted EXACTLY 1 across all three records — not 3 (a
// per-record census) and not `>= 1` (which a per-element census of 4 would pass).

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnknownCensusManyRecords(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_unknown_census")
	older := jsReadSchema(t, "VOLD_unknown_census")
	table := jsFixedRootName(t, older)
	file := filepath.Join(corpus, "many_unknown_census.bin")

	src := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(4096, 4096);
// THE POISON (§5.7, §5.9 #17): a prefill that ran and one that never did look
// alike over zeros, and a removed field's slot is the prefill's declared default.
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (n !== 3) { fail("the manifest says this file carries 3 records, not " + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a clean NEW-READS-OLD is not a refusal: " + reason(r)); }
const want = [10, 11, 12, 13, 20, 21, 22, 23, 30, 31, 32, 33];
for (let rec = 0; rec < 3; rec++) {
  const got = back[rec];
  if (got.Lead !== 1 || got.Trail !== 2) {
    fail("record " + rec + ": the brackets either side of the removed field did not stand: " + show(got));
  }
  for (let i = 0; i < 4; i++) {
    if (got.Items[i].A !== want[rec * 4 + i]) {
      fail("record " + rec + " items[" + i + "].a is " + got.Items[i].A + ", the manifest says " + want[rec * 4 + i] + " — the removed drop moved a neighbour");
    }
  }
}
if (r.unknown !== 1) {
  fail("unknown === 1 (once per peer, NEVER per record): Item.drop is one field of one peer, and three records owe one census, not " + r.unknown);
}
if (r.kindMismatch !== 0 || r.widened !== 0 || r.clamped !== 0 || r.duplicate !== 0) {
  fail("a removed field is unknown, never a moved kind: " + reason(r));
}
`, file)

	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("NEW-READS-OLD unknown_census many: %v\n%s", err, out)
	}
}
