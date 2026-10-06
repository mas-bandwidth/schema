package jstable

// TestFixedRoadmapT01Framing is the js leg's file-envelope, batch-capacity and
// plan-selection assertions of docs/roadmap.sexp's `fixed-tables` node, one
// subtest per task id, each against the page sentence it implements
// (docs/FIXED-FORM-ALGORITHM.md §5.3's LOAD steps 3, 5, 7, 9, 10 and 11 and the
// refusal table under it). Every file is the C++ reference's byte oracle,
// forged in one place: a probe that reads a file the reference wrote and then
// breaks exactly one thing in it.

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// t01Prelude is the probe-side vocabulary every subtest shares: a destination
// poisoned with a sentinel (a value compare, never a struct's slack, §5.9 #17),
// and `total`, which holds the joint answer of §5.3's report table — a refusal
// by name is n == -1, `refused` is that name, `malformed` is false, every
// counter is zero and not one destination byte is written.
const t01Prelude = `
import { TableFixedPlan, TableFixedLayoutView, TableFixedParseLayout } from "./@HOME@Table.js";
const SENT = 0x5A5A5A5A;
function eight() { const b = []; for (let k = 0; k < 8; k++) { b.push(new T()); InitT(b[k]); } return b; }
function poison(back, plan) {
  for (const v of back) { for (const key of Object.keys(v)) { v[key] = SENT; } }
  if (plan !== null) { plan.image.fill(0x5A); }
}
function untouched(label, back, plan) {
  for (const v of back) { for (const key of Object.keys(v)) {
    if (v[key] !== SENT) { fail(label + ": a refusal wrote the destination: " + show(v)); return; }
  } }
  if (plan !== null) { for (let i = 0; i < plan.image.length; i++) {
    if (plan.image[i] !== 0x5A) { fail(label + ": a refusal wrote plan.image[" + i + "]"); return; }
  } }
}
function counters(r) { return r.unknown + r.kindMismatch + r.clamped + r.widened + r.duplicate; }
// total: REFUSE is total (§5.3: "no counter moves, nothing is decoded, and not
// one destination byte is written"); malformed is the residue and never set
// beside a name. hash is what layout_hash must hold, or null where the page
// leaves it unsettled.
function total(label, n, r, name, hash, back, plan) {
  if (n !== -1) { fail(label + ": a refusal returns -1, got n=" + n + " " + reason(r)); }
  if (r.refused !== TableFixedRefusal[name]) {
    fail(label + ": the name is " + name + ", got " + TableFixedRefusalName(r.refused) + " " + reason(r));
  }
  if (r.malformed) { fail(label + ": refused and malformed are never both set: " + reason(r)); }
  if (counters(r) !== 0) { fail(label + ": REFUSE is total, no counter moves: " + reason(r)); }
  if (hash !== null && r.layoutHash !== hash) {
    fail(label + ": layout_hash is " + hash.toString(16) + ", got " + r.layoutHash.toString(16));
  }
  untouched(label, back, plan);
}
function copy(bytes, upto) { return Uint8Array.from(bytes.subarray(0, upto === undefined ? bytes.length : upto)); }
`

func t01Probe(t *testing.T, node, reader string, older []string, retire int, table, body string) {
	t.Helper()
	probe := strings.ReplaceAll(t01Prelude+body, "@HOME@", runtimeHome(jsUnitOf(t, reader)))
	out, err := jsRunVersionProbe(t, node, reader, older, retire, table, probe)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	older := jsReadSchema(t, "VOLD_field_append")
	table := jsFixedRootName(t, older)
	oldFile := strconv.Quote(filepath.Join(corpus, "old_field_append.bin"))

	cases := []struct {
		id    string
		body  func() (reader string, lineage []string, retire int, body string)
		after func(t *testing.T)
	}{
		// §5.3 step 3: "L := LE(4, file+16) ; if 20 + L > len(file): REFUSE
		// layout_malformed", and the table row "`20 + L` past the file |
		// `layout_malformed` | the name".
		{"js/F4", func() (string, []string, int, string) {
			return older, nil, 0, `
const data = readFileSync(` + oldFile + `);
const L = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
const layoutAt = 20;
const back = eight(); const plan = TNewPlan(4096, 4096);
{
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, data, data.length, plan, r);
  if (n < 1 || r.malformed || r.refused !== 0) { fail("the corpus file must read cleanly first: n=" + n + " " + reason(r)); }
}
// every cut from the layout's first byte to one short of its last
for (let k = layoutAt; k < layoutAt + L; k++) {
  const cut = copy(data, k);
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, cut, cut.length, plan, r);
  total("cut at " + k, n, r, "LayoutMalformed", 0n, back, plan);
}
// 20 + L == len(file) is NOT past the file: the layout and no record reads zero records
{
  const cut = copy(data, layoutAt + L);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, cut, cut.length, plan, r);
  if (n !== 0 || r.refused !== 0 || r.malformed) { fail("20 + L == len is not past the file: n=" + n + " " + reason(r)); }
}
// the declared L one past the bytes that are there, and the largest u32 (no wrap)
for (const declared of [data.length - layoutAt + 1, 0xFFFFFFFF]) {
  const f = copy(data);
  new DataView(f.buffer).setUint32(16, declared, true);
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total("declared L=" + declared, n, r, "LayoutMalformed", 0n, back, plan);
}
// the guard runs BEFORE the hash select (step 3 before step 5): a truncated file
// under a hash no lineage entry holds is layout_malformed, never layout_newer
{
  const f = copy(data, layoutAt + L - 1);
  new DataView(f.buffer).setBigUint64(8, 0xDEADBEEFCAFEF00Dn, true);
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total("truncated under an unknown hash", n, r, "LayoutMalformed", 0n, back, plan);
}
`
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			reader, lineage, retire, body := c.body()
			t01Probe(t, node, reader, lineage, retire, table, body)
			if c.after != nil {
				c.after(t)
			}
		})
	}
}
