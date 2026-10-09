package jstable

// The count_clamp_ends row (docs/roadmap.sexp, the array-bounds work set): the
// two ENDS of the count clamp, whose task titles are quoted here verbatim —
// js/C1 "count clamp v<0" and js/C2 "count clamp v>Max". Row 4
// (writer_bound_count) forges the count word from 4 to 7, a value over the
// PLAN's bound of 4 but neither negative nor past the READER's own 8, so it
// exercises neither end. The negative arm was MEASURED on 2026-09-19 to be
// guarded by nothing: deleted from the emitted runtime of five legs, not one
// landed fixed-table test went red. `clamped` is asserted EXACTLY `== 1`,
// never `>= 1`, because a leg that counts in the count op AND again in the
// bounds pass lands 2 and a looser read is what this row exists to catch. C2's
// landed count of 4 is the WRITER's bound carried by the plan, never the
// reader's own 8 and never the forged 9.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningCountClampEnds(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_array_bounded_grow")
	older := jsReadSchema(t, "VOLD_array_bounded_grow")
	table := jsFixedRootName(t, older)
	file := filepath.Join(corpus, "old_array_bounded_grow.bin")

	// PROBE C1: the count forged to a NEGATIVE value — 0xFFFFFFFF, which is -1
	// read as the little-endian int32 the wire carries. The negative count must
	// clamp to ZERO, never to -1, never to the writer's 4, never to the
	// reader's 8.
	c1 := fmt.Sprintf(`
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
view.setUint32(found + 4, 0xFFFFFFFF, true); /* the forge: -1 as the little-endian int32 the wire carries */
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== 1) { fail("count_clamp_ends C1: n is not 1: " + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a forged count is not a refusal and not malformed: " + reason(r)); }
if (back[0].Lead !== 0xAAAAAAAA || back[0].Trail !== 0xBBBBBBBB) {
  fail("a forged count moved a neighbour: " + show(back[0]));
}
if (back[0].ValsCount !== 0) {
  fail("the negative count does not clamp to zero: " + show(back[0].ValsCount));
}
if (r.clamped !== 1) {
  fail("the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: " + reason(r));
}
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("counters moved on a clean clamped read: " + reason(r));
}
`, file)
	if out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, c1); err != nil {
		t.Fatalf("count_clamp_ends C1: %v\n%s", err, out)
	}

	// PROBE C2: the count forged PAST THE READER'S OWN BOUND — 9, past the
	// writer's 4 AND past the reader's 8, which is what makes it C2 and not
	// row 4. It must land the WRITER's bound 4, never the reader's own 8 and
	// never the forged 9.
	c2 := fmt.Sprintf(`
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
view.setUint32(found + 4, 9, true); /* the forge */
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== 1) { fail("count_clamp_ends C2: n is not 1: " + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a forged count is not a refusal and not malformed: " + reason(r)); }
if (back[0].Lead !== 0xAAAAAAAA || back[0].Trail !== 0xBBBBBBBB) {
  fail("a forged count moved a neighbour: " + show(back[0]));
}
if (back[0].ValsCount !== 4) {
  fail("the count past the reader's bound lands the WRITER's bound 4, never the reader's 8 nor the forged 9: " + show(back[0].ValsCount));
}
for (let i = 0; i < 4; i++) {
  if (back[0].Vals[i] !== 1000 + i) { fail("vals[" + i + "] did not land: " + show(back[0])); }
}
for (let i = 4; i < 8; i++) {
  if (back[0].Vals[i] !== 0) { fail("vals[" + i + "] is not the reader's declared default: " + show(back[0])); }
}
if (r.clamped !== 1) {
  fail("the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: " + reason(r));
}
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("counters moved on a clean clamped read: " + reason(r));
}
`, file)
	if out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, c2); err != nil {
		t.Fatalf("count_clamp_ends C2: %v\n%s", err, out)
	}
}
