package jstable

// union_tag_both_plans is the fixed-form row that forges a UNION TAG past the
// arm set, in old_union_append.bin. The tag is ONE byte, not four. The locator
// is the nine-byte needle `01 07 00 00 00 0f 00 00 00` (tag 1, alpha.m=7,
// seq=15, little-endian) and it must occur EXACTLY once — a search that is not
// a locator is not a forge — so the probe writes 9 over its first byte: past
// the old writer's two arms AND the new reader's three. A forged tag is not a
// refusal and not malformed; it lands None (0), never the forged 9 and never
// the writer's 1, and the neighbour seq stays 15. Both plans read the same
// forged bytes — the COMPILED plan (reader VNEW, older VOLD) and the IDENTITY
// plan (reader VOLD, no older) — and owe the same landing, which is §5.4's
// agreement claim made concrete.
//
// THE COUNT IS ASYMMETRIC ON THIS LEG, so it was measured before it was
// written. The identity plan copies the whole body in one OP_COPY, so the
// forged 9 reaches the decode projection raw and the union-tag clamp fires
// once: `clamped == 1` EXACTLY, never `>= 1`, because a leg that counts in the
// plan's op AND again in the projection lands 2 and a looser read is the hole
// this row exists to catch. The compiled plan's arms are guarded consts, so a
// forged 9 matches no guard, leaves the prefill's None standing, and arrives at
// the projection already 0: it lands 0 and counts NOTHING. That short count is
// schema#1254, so this row asserts the compiled plan's LANDING and not its
// count — `== 1` would red, `== 0` would cement the defect. When #1254 rules,
// restore the decode union clamp in internal/codegen/jstable/fixedjs.go — the
// `if (expr.Type > len(r.Variants)) { expr.Type = 0; report.clamped++; }`
// bound — and assert `clamped == 1` on the compiled plan too. The site was
// measured 2026-09-19 to be guarded by nothing: deleted from all nine legs'
// emitters, not one landed fixed-table test went red.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_union_append")
	older := jsReadSchema(t, "VOLD_union_append")
	newTable := jsFixedRootName(t, newer)
	oldTable := jsFixedRootName(t, older)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE LANDING is the same on both plans and never names clamped: a forged
	// tag is not a refusal and not malformed; it lands None, never the forged 9
	// and never the writer's 1; the neighbour seq stays 15; no other counter
	// moves.
	landing := `
if (n !== 1) { fail("the forged file carries one record: n=" + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a forged tag is not a refusal and not malformed: " + reason(r)); }
if (back[0].Pick.Type !== 0) { fail("a forged tag lands None: " + show(back[0])); }
if (back[0].Pick.Type === 9) { fail("a forged tag never lands the forged 9: " + show(back[0])); }
if (back[0].Pick.Type === 1) { fail("a forged tag never lands the writer's 1: " + show(back[0])); }
if (back[0].Seq !== 15) { fail("the neighbour after the union is untouched: " + show(back[0])); }
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("no other counter moved: " + reason(r));
}
`

	// THE IDENTITY PLAN ALSO COUNTS the forge: the whole body is one OP_COPY, so
	// the forged 9 reaches the decode projection raw and the union-tag clamp
	// fires once. `== 1` EXACTLY, never `>= 1`, for the same reason the forged
	// ordinal row asserts it.
	identityCount := `
if (r.clamped !== 1) { fail("the forged tag is counted exactly once, never twice: " + reason(r)); }
`

	forge := `
const data = readFileSync(%q);
const bytes = new Uint8Array(data.buffer, data.byteOffset, data.length);
const NEEDLE = [0x01, 0x07, 0x00, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00];
let found = -1;
for (let i = 0; i + NEEDLE.length <= bytes.length; i++) {
  let ok = true;
  for (let j = 0; j < NEEDLE.length; j++) { if (bytes[i + j] !== NEEDLE[j]) { ok = false; break; } }
  if (ok) {
    if (found !== -1) { fail("the tag needle occurs twice; it is not a locator"); }
    found = i;
  }
}
if (found === -1) { fail("the tag needle occurs zero times; it is not a locator"); }
bytes[found] = 9;
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
%s
`

	compiled := fmt.Sprintf(forge, file, landing)
	if out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, newTable, compiled); err != nil {
		t.Fatalf("union_tag_both_plans, the COMPILED plan: %v\n%s", err, out)
	}
	identity := fmt.Sprintf(forge, file, landing+identityCount)
	if out, err := jsRunVersionProbe(t, node, older, nil, 0, oldTable, identity); err != nil {
		t.Fatalf("union_tag_both_plans, the IDENTITY plan: %v\n%s", err, out)
	}
}
