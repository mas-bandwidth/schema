package jstable

// fixedroadmap_t13 — the js leg's js/F12 row of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "File framing and layout announcements"): the
// second-layout-for-a-held-hash refusal. Law: docs/FIXED-FORM-ALGORITHM.md §2
// row 5 (the LOAD table at lines 150-158): "a known hash over different bytes is
// `layout_malformed`", and the step 3 row "`L := LE(4, b+16)`; if `20 + L >
// bytes`, `REFUSE layout_malformed`"; docs/SPEC-TABLES.md §3.4: "A second layout
// for a hash already held is refused by name."
//
// The header names the layout by the hash at bytes 8..15, its byte length at
// 16..19 and its bytes from 20 (docs/FIXED-FORM-ALGORITHM.md §2's file map). The
// hash is taken as given — the definitions digest is not on the wire — so a
// second layout under a hash this reader already holds is a lie about a known
// version, and the ONE name for it is layout_malformed, never layout_newer:
// the js reader already compares the held length and the held bytes and refuses
// the mismatch (fixedmodule.go, TableFixedLoad's step 7 at lines 374-376). This
// test is the js port of C's second-layout case
// (internal/codegen/ctable/fixedroadmap_t01_test.go, `cT01SecondLayout`) and
// pins the refusal on this leg.

import "testing"

// TestFixedRoadmapJsT13LayoutHashCollision proves js/F12 through the generated
// reader under the leg's own node: a valid file for a known layout reads, a copy
// whose header carries the held hash over different layout bytes is refused
// layout_malformed before any record lands, and a copy whose held layout length
// differs is refused the same way. Both refusals write no destination byte and
// move no counter (§5.3's joint answer).
func TestFixedRoadmapJsT13LayoutHashCollision(t *testing.T) {
	t.Parallel()
	const src = `package probe

fixed table T
{
    keep uint32 = 0
    label string(8) = "fx"
}
`
	node := jsNode(t)
	table := jsFixedRootName(t, src)
	const body = `
// A VALID FILE FOR A KNOWN LAYOUT, BUILT IN MEMORY: the writer lays down the
// header's hash (bytes 8..15), the layout length (16..19) and the layout bytes
// from 20, which is exactly the header the reader holds (fixedmodule.go's write
// template). The file must read first, so a refusal below is the edit's and
// never the fixture's.
const v = new T(); InitT(v);
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("t13: the writer could not lay down a file for the held layout"); }
const L = new DataView(buf.buffer, buf.byteOffset, buf.length).getUint32(16, true);
{
  const back = eight(); const plan = TNewPlan(4096, 4096);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, buf, buf.length, plan, r);
  if (!(n === 1 && !r.malformed && r.refused === 0)) {
    fail("t13: the valid file must read: n=" + n + " " + reason(r));
  }
}

// A SECOND LAYOUT UNDER THE HELD HASH, BYTES DIFFER: the header hash is left
// alone and the layout's own first byte (the entry count at 20) is bent, so the
// length still matches and only the byte comparison can catch it.
{
  const f = copy(buf);
  f[20] ^= 0xFF;
  const back = eight(); const plan = TNewPlan(4096, 4096); poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total("a second layout under a held hash, bytes differ", n, r, "LayoutMalformed", 0n, back, plan);
}

// A SECOND LAYOUT UNDER THE HELD HASH, LENGTH DIFFERS: the declared layout
// length at 16 is one short of the held length — still inside the file, so step
// 3's "20 + L > bytes" does not fire — and the length comparison is what refuses.
{
  const f = copy(buf);
  new DataView(f.buffer).setUint32(16, L - 1, true);
  const back = eight(); const plan = TNewPlan(4096, 4096); poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total("a second layout under a held hash, length differs", n, r, "LayoutMalformed", 0n, back, plan);
}
`
	t01Probe(t, node, src, nil, 0, table, body)
}
