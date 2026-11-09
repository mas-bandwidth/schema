package jstable

// The writer_bound_count divergence row (docs/FIXED-FORM-VERSIONING-TESTS.md
// §5.8 row 4): the reader's bound is WIDER than the writer's, and the forged
// file carries a count BETWEEN the two. The NEW build reads the forged OLD
// bytes through its lineage, so the plan is a COMPILED one and the count's
// bound is the WRITER's, carried by the plan — never the reader's 8, never the
// forged 7. The forge is a line of this probe's own JavaScript over the bytes
// it just read, because this leg has no `pre` hook.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_array_bounded_grow")
	older := jsReadSchema(t, "VOLD_array_bounded_grow")
	table := jsFixedRootName(t, older)
	src := fmt.Sprintf(`
const data = readFileSync(%q);
const bytes = new Uint8Array(data.buffer, data.byteOffset, data.length);
// THE FORGE'S LOCATOR: lead 0xAAAAAAAA immediately followed by the count 4,
// both little-endian uint32 — the eight-byte needle that must occur once.
const NEEDLE = [0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00];
let found = -1;
for (let i = 0; i + NEEDLE.length <= bytes.length; i++) {
  let ok = true;
  for (let j = 0; j < NEEDLE.length; j++) { if (bytes[i + j] !== NEEDLE[j]) { ok = false; break; } }
  if (ok) {
    if (found !== -1) { fail("the lead+count needle occurs twice; it is not a locator"); }
    found = i;
  }
}
if (found === -1) { fail("the lead+count needle occurs zero times; it is not a locator"); }
const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);
view.setUint32(found + 4, 7, true); /* the forge */
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== -1 || r.refused !== TableFixedRefusal.CountOverBound || r.damaged || r.oor !== 0) {
  fail("writer_bound_count: a count past the WRITER's 4 owes count_over_bound, never the reader's 8: n=" + n + " " + reason(r));
}
if (back[0].valsCount !== 0 || back[0].lead !== 1 || back[0].trail !== 2) {
  fail("a refused read must leave the destination untouched: " + show(back[0]));
}
if (r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.repeated !== 0) {
  fail("REFUSE is total: no counter moves: " + reason(r));
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"))
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
	}
}
