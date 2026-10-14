package jstable

// The writer_bound_count divergence row with TWO lineage peers
// (docs/FIXED-FORM-VERSIONING-TESTS.md §5.8 row 4). The reader is
// VNEW_array_bounded_grow ([..8]int32). It is handed TWO peers with DIFFERENT
// bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE the file, and
// VMID_array_bounded_grow ([..6]int32), the distractor, which did not. The
// count word is forged to 7, exactly as the landed single-peer test forges it.
// The number that makes this test worth having is 6: a reader that clamps to
// its own bound lands 8, a reader that clamps to whichever peer it saw last
// lands 6, and only a plan that carries the WRITING peer's bound lands 4.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_array_bounded_grow")
	writer := jsReadSchema(t, "VOLD_array_bounded_grow")
	distractor := jsReadSchema(t, "VMID_array_bounded_grow")
	table := jsFixedRootName(t, writer)
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
if (n !== 1) { fail("writer_bound_count: n is not 1: " + n + " " + reason(r)); }
if (back[0].ValsCount !== 4) {
  fail("vals_count is the WRITING peer's bound carried by the plan, never the distractor's 6, never the reader's 8 nor the forged 7: " + show(back[0].ValsCount));
}
for (let i = 0; i < 4; i++) {
  if (back[0].Vals[i] !== 1000 + i) { fail("vals[" + i + "] did not land: " + show(back[0])); }
}
for (let i = 4; i < 8; i++) {
  if (back[0].Vals[i] !== 0) { fail("vals[" + i + "] is not the reader's declared default: " + show(back[0])); }
}
if (back[0].Lead !== 0xAAAAAAAA || back[0].Trail !== 0xBBBBBBBB) {
  fail("the row moved a neighbour: " + show(back[0]));
}
if (r.clamped !== 1) {
  fail("the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: " + reason(r));
}
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0) {
  fail("counters moved on a clean clamped read: " + reason(r));
}
if (r.malformed) { fail("a count inside the writer's bound is not malformed: " + reason(r)); }
if (r.refused !== 0) { fail("a count inside the writer's bound is not a refusal: " + reason(r)); }
`, filepath.Join(corpus, "old_array_bounded_grow.bin"))
	out, err := jsRunVersionProbe(t, node, newer, []string{writer, distractor}, 0, table, src)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
