package jstable

// §5.8 row 11, `unknown_census`, NEW-READS-OLD on the JavaScript leg. The OLD
// build wrote `old_unknown_census.bin` — one `Census`, `lead = 1`, `trail = 2`,
// four `items` with `a = 10 + i` and `drop = 900 + i` — and the NEW build removes
// `Item.drop`. The pair is deliberately UNLAWFUL (§5.1 refuses a removal), so the
// lineage entry is handed in here, never read from a lock. The asserted counter is
// `unknown`, EXACTLY 1: once per FIELD per peer, not once per element — `>= 1`
// would pass a wrong `4`, which is the divergence this row exists to catch.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningUnknownCensus(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_unknown_census")
	older := jsReadSchema(t, "VOLD_unknown_census")
	table := jsFixedRootName(t, older)
	file := filepath.Join(corpus, "old_unknown_census.bin")

	src := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(4096, 4096);
// THE POISON (§5.7, §5.9 #17): a prefill that ran and one that never did look
// alike over zeros, and a removed field's slot is the prefill's declared default.
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (n !== 1) { fail("the manifest says this file carries 1 record, not " + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a clean NEW-READS-OLD is not a refusal: " + reason(r)); }
const got = back[0];
if (got.Lead !== 1 || got.Trail !== 2) {
  fail("the brackets either side of the removed field did not stand: " + show(got));
}
const want = [10, 11, 12, 13];
for (let i = 0; i < 4; i++) {
  if (got.Items[i].A !== want[i]) {
    fail("items[" + i + "].a is " + got.Items[i].A + ", the manifest says " + want[i] + " — the removed drop moved a neighbour");
  }
}
if (r.unknown !== 1) {
  fail("unknown === 1 (once per FIELD per peer): Item.drop is one field of one peer, not " + r.unknown);
}
if (r.kindMismatch !== 0 || r.widened !== 0 || r.clamped !== 0) {
  fail("a removed field is unknown, never a moved kind: " + reason(r));
}
// THE CENSUS LANDS ONCE, AFTER THE RECORD LOOP, on a read that RETURNS (§5.9
// #6), so a SECOND read of the same peer reports unknown === 1 again.
TableFixedResetReport(report);
const m = TLoad(back, back.length, data, data.length, plan, report);
if (m !== 1 || r.refused !== 0 || r.malformed) {
  fail("the second read of the same peer must also return one clean record: " + m + " " + reason(r));
}
if (r.unknown !== 1) {
  fail("the second read owes unknown === 1 again, not " + r.unknown);
}
`, file)

	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("NEW-READS-OLD unknown_census: %v\n%s", err, out)
	}
}
