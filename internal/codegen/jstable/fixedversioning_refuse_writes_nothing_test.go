package jstable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// §5.8 row 9, `refuse_writes_nothing`: the NEW reader reads `old_nested_append.bin`
// through its lineage, so a COMPILED plan with a NONEMPTY fill list is in hand
// (the appended nested `Vec.w = 88` is the one nonzero prefill) when record 0's
// per-record hash word — its first eight bytes, bitwise INVERTED — fails the hash
// check; the header's hash at file+8 stays untouched. The verdict is `n == -1`,
// `no_layout`, `malformed` false, every counter EXACTLY zero, every byte still 0x5A.
// A `>= 1` passes the read this row catches: a prefill run before the hash check moves no counter.
func TestJSFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_nested_append")
	older := jsReadSchema(t, "VOLD_nested_append")
	table := jsFixedRootName(t, older)

	src := fmt.Sprintf(`
const data = readFileSync(%q);
const view = new DataView(data.buffer, data.byteOffset, data.length);
if (data.length < 20) { fail("the corpus file is too short for a header"); }
const rec0 = 20 + view.getUint32(16, true);
if (rec0 + 8 > data.length) { fail("record 0 does not fit the corpus file"); }
for (let i = 0; i < 8; i++) { data[rec0 + i] ^= 0xff; } // the forge: record 0's per-record hash, inverted
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
// THE POISON, through the byte destination this leg writes (§5.7, §5.9 #17).
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
const r = report;
if (n !== -1) { fail("a record whose hash names no held layout must refuse: n=" + n + " " + reason(r)); }
if (r.refused !== TableFixedRefusal.NoLayout) {
  fail("the refusal is no_layout, not " + TableFixedRefusalName(r.refused) + ": " + reason(r));
}
if (r.malformed) { fail("a refusal by name never sets malformed too (§5.3): " + reason(r)); }
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.clamped !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("REFUSE is total: no counter moves: " + reason(r));
}
for (let i = 0; i < plan.image.length; i++) {
  if (plan.image[i] !== 0x5A) {
    fail("the prefill ran before the hash check: image[" + i + "] = " + plan.image[i] + ", not 0x5A");
  }
}
`, filepath.Join(corpus, "old_nested_append.bin"))

	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
	}
}
