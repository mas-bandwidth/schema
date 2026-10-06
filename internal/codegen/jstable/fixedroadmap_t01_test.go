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
	newer := jsReadSchema(t, "VNEW_field_append")
	table := jsFixedRootName(t, older)
	oldFile := strconv.Quote(filepath.Join(corpus, "old_field_append.bin"))
	newFile := strconv.Quote(filepath.Join(corpus, "new_field_append.bin"))

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
		// §5.3 step 10: "n := rest / record_bytes ; if n > capacity: REFUSE
		// batch_too_large", and the table row "more records than the caller's
		// capacity | `batch_too_large` | the name".
		{"js/F9", func() (string, []string, int, string) {
			return older, nil, 0, `
const data = readFileSync(` + oldFile + `);
const L = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
const recAt = 20 + L, recLen = data.length - recAt;
const back = eight(); const plan = TNewPlan(4096, 4096);
{
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, data, data.length, plan, r);
  if (n !== 1 || r.malformed || r.refused !== 0) { fail("the corpus file is one clean record: n=" + n + " " + reason(r)); }
}
// n == capacity is the boundary that reads
{
  const r = new TableFixedReport();
  const n = TLoad(back, 1, data, data.length, plan, r);
  if (n !== 1 || r.refused !== 0 || r.malformed) { fail("n == capacity reads: n=" + n + " " + reason(r)); }
}
// n == capacity + 1 refuses by name
{
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, 0, data, data.length, plan, r);
  total("one record, capacity 0", n, r, "BatchTooLarge", 0n, back, plan);
}
// two whole records into a capacity of one, and into a capacity of two (reads)
{
  const two = new Uint8Array(data.length + recLen);
  two.set(data); two.set(data.subarray(recAt), data.length);
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, 1, two, two.length, plan, r);
  total("two records, capacity 1", n, r, "BatchTooLarge", 0n, back, plan);
  const ok = new TableFixedReport();
  const m = TLoad(back, 2, two, two.length, plan, ok);
  if (m !== 2 || ok.refused !== 0 || ok.malformed) { fail("two records read into a capacity of two: n=" + m + " " + reason(ok)); }
  // step 10 runs before step 11: a second record whose hash is no layout's
  // still overflows by name, before a record is looked at
  two[data.length] ^= 0xFF;
  poison(back, plan);
  const q = new TableFixedReport();
  const p = TLoad(back, 1, two, two.length, plan, q);
  total("two records, the second's hash bad, capacity 1", p, q, "BatchTooLarge", 0n, back, plan);
}
`
		}, nil},
		// §5.3 step 11: "per record, at at: if LE(8, at) != h: REFUSE no_layout
		// -- BEFORE any byte is landed", and the table row "a record hash that
		// is not the file's | `no_layout` | the name".
		{"js/F10", func() (string, []string, int, string) {
			return older, nil, 0, `
const data = readFileSync(` + oldFile + `);
const L = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
const recAt = 20 + L, recLen = data.length - recAt;
const back = eight(); const plan = TNewPlan(4096, 4096);
// one byte of the eight at a time: a compare that reads only a lane passes the
// all-eight inversion and fails here
for (let b = 0; b < 8; b++) {
  const f = copy(data);
  f[recAt + b] ^= 0x01;
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  // layout_hash after no_layout is left open: SPEC-TABLES §3.4's table says the
  // reader states the hash, FIXED-FORM-VERSIONING-TESTS' refuse_writes_nothing row
  // says it is untouched, so it is not asserted
  total("record hash byte " + b, n, r, "NoLayout", null, back, plan);
}
// the bad hash is in the LAST record, the first being whole: the same name, no counter moves
{
  const two = new Uint8Array(data.length + recLen);
  two.set(data); two.set(data.subarray(recAt), data.length);
  two[data.length + 3] ^= 0x01;
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, two, two.length, plan, r);
  if (n !== -1 || r.refused !== TableFixedRefusal.NoLayout || r.malformed || counters(r) !== 0) {
    fail("a later record's bad hash is no_layout too: n=" + n + " " + reason(r));
  }
}
`
		}, nil},
		// §5.3: "the compiler hands every reader its own wire hash ... and a
		// runtime NEVER computes a hash from layout bytes it holds, not for the
		// IDENTITY LANE (step 8, where the selected entry being the reader's own
		// is an INDEX COMPARISON and never a recomputation)".
		{"js/R7", func() (string, []string, int, string) {
			return newer, []string{older}, 0, `
const own = readFileSync(` + newFile + `);
const old = readFileSync(` + oldFile + `);
const big = TNewPlan(4096, 4096);
// the identity lane is the one that ignores the caller's entry capacity: its plan
// is the baked one-entry plan, so a capacity of ZERO still reads the reader's own
// file, and a compiled lane (the older entry) refuses plan_too_large against it
const none = new TableFixedPlan(0, big.image.length, 1);
const back = eight();
{
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, own, own.length, none, r);
  if (n !== 1 || r.refused !== 0 || r.malformed) {
    fail("the reader's own file takes the identity lane, which needs no plan entries: n=" + n + " " + reason(r));
  }
  if (back[0].W !== 777) { fail("the identity read lost a value: " + show(back[0])); }
}
{
  poison(back, none);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, old, old.length, none, r);
  total("an older entry is a compiled lane", n, r, "PlanTooLarge", 0n, back, none);
}
// and no hash is ever computed at run time: the only mention of the layout hash
// function in the emitted modules is its definition
for (const name of ["./ProbeTable.js", "./@HOME@Table.js"]) {
  const text = readFileSync(name, "utf8");
  let at = 0, calls = 0;
  for (;;) {
    at = text.indexOf("TableFixedHashOf(", at);
    if (at < 0) { break; }
    if (text.slice(Math.max(0, at - 9), at) !== "function ") { calls++; }
    at += 1;
  }
  if (calls !== 0) { fail(name + " computes a hash from layout bytes " + calls + " time(s)"); }
}
`
		}, nil},
		// §5.3 step 5: "if none: REFUSE layout_newer, reporting h AND NOTHING
		// ELSE" — and step 5 comes BEFORE step 7's layout compare, so a stranger's
		// layout is never looked at: garbage behind an unknown hash is still this name.
		{"js/R8", func() (string, []string, int, string) {
			return older, nil, 0, `
const data = readFileSync(` + oldFile + `);
const L = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
const want = 0xDEADBEEFCAFEF00Dn;
const back = eight(); const plan = TNewPlan(4096, 4096);
for (const garbage of [false, true]) {
  const f = copy(data);
  new DataView(f.buffer).setBigUint64(8, want, true);
  if (garbage) { for (let i = 20; i < f.length; i++) { f[i] ^= 0xA5; } } // layout AND records
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total("unknown hash, garbage=" + garbage, n, r, "LayoutNewer", want, back, plan);
  // AND NOTHING ELSE: the whole report is a fresh one with refused and layoutHash set
  const fresh = new TableFixedReport();
  fresh.refused = TableFixedRefusal.LayoutNewer; fresh.layoutHash = want;
  if (show(r) !== show(fresh)) { fail("layout_newer reports the hash and nothing else: " + show(r)); }
}
`
		}, nil},
		// §5.3 step 7 and the table row "a known hash, different layout length or
		// bytes | `layout_malformed` | the name": "The seven §1.1 malformations
		// under a KNOWN hash all come back as one name, `layout_malformed`".
		{"js/R9", func() (string, []string, int, string) {
			return newer, []string{older}, 0, `
const files = [readFileSync(` + oldFile + `), readFileSync(` + newFile + `)];
const hashOf = (d) => new DataView(d.buffer, d.byteOffset, d.length).getBigUint64(8, true);
const back = eight(); const plan = TNewPlan(4096, 4096);
function load(label, f) {
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, f, f.length, plan, r);
  total(label, n, r, "LayoutMalformed", 0n, back, plan);
}
for (const [which, data] of files.entries()) {
  const L = new DataView(data.buffer, data.byteOffset, data.length).getUint32(16, true);
  // the clean file reads first, so a refusal below is the forge's and not the file's
  { const r = new TableFixedReport();
    const n = TLoad(back, back.length, data, data.length, plan, r);
    if (n < 1 || r.refused !== 0 || r.malformed) { fail("file " + which + " must read cleanly first: n=" + n + " " + reason(r)); } }
  // a different length, either way, and none at all
  for (const declared of [L - 1, L + 1, 0]) {
    const f = copy(data);
    new DataView(f.buffer).setUint32(16, declared, true);
    load("file " + which + " declared L=" + declared, f);
  }
  // a different byte, at EVERY position of the layout
  for (let p = 0; p < L; p++) {
    const f = copy(data);
    f[20 + p] ^= 0xFF;
    load("file " + which + " layout byte " + p, f);
  }
  // THE SEVEN §1.1 RULES, each forged into the layout under the file's own known
  // hash. Each forge is first held to the rule it claims by the parser (so it
  // is that malformation and not another), then to the one name LOAD gives it.
  const at = (i) => 20 + 4 + 17 * i; // entry i
  const forges = [
    ["LayoutCountMismatch", (f, v) => v.setUint32(20, 5, true)],
    ["LayoutKindUnknown", (f, v) => { f[at(1) + 8] = 31; }],
    ["LayoutSizeMismatch", (f, v) => v.setUint32(at(1) + 9, 8, true)],
    ["LayoutKindInvalid", (f, v) => v.setUint32(at(1) + 13, 1, true)],
    ["LayoutTreeUnclosed", (f, v) => v.setUint32(at(0) + 13, 4, true)],
    ["LayoutRecordTooLarge", (f, v) => v.setUint32(at(0) + 9, 65537, true)],
  ];
  if (which === 0) {
    for (const [name, forge] of forges) {
      const f = copy(data);
      forge(f, new DataView(f.buffer));
      const view = new TableFixedLayoutView();
      const ok = TableFixedParseLayout(f, 20, L, view);
      if (ok || view.refusal !== TableFixedRefusal[name]) {
        fail(name + ": the forge is not that malformation: " + TableFixedRefusalName(view.refusal));
      }
      load("§1.1 " + name, f);
    }
    // layout_too_deep needs a chain past the walk's bound, so the layout is longer
    // than the lock's and the file is rebuilt around it
    const depth = 67, lay = new Uint8Array(4 + depth * 17), lv = new DataView(lay.buffer);
    lv.setUint32(0, depth, true);
    for (let i = 0; i < depth; i++) {
      const e = 4 + i * 17, leaf = i === depth - 1;
      lay[e + 8] = leaf ? 4 : 13;
      lv.setUint32(e + 9, 4, true);
      lv.setUint32(e + 13, leaf ? 0 : 1, true);
    }
    const view = new TableFixedLayoutView();
    if (TableFixedParseLayout(lay, 0, lay.length, view) || view.refusal !== TableFixedRefusal.LayoutTooDeep) {
      fail("LayoutTooDeep: the forge is not that malformation: " + TableFixedRefusalName(view.refusal));
    }
    const recs = data.subarray(20 + L);
    const f = new Uint8Array(20 + lay.length + recs.length);
    f.set(data.subarray(0, 20)); f.set(lay, 20); f.set(recs, 20 + lay.length);
    new DataView(f.buffer).setUint32(16, lay.length, true);
    if (hashOf(f) !== hashOf(data)) { fail("the deep forge keeps the known hash"); }
    load("§1.1 LayoutTooDeep", f);
  }
}
`
		}, nil},
		// §5.3's closing rule: "REFUSE is total: no counter moves, nothing is
		// decoded, and not one destination byte is written — the prefill
		// included. malformed is the residue and not a bucket a named rule falls
		// into", and the two-answers table ("refused plus reason is one answer;
		// malformed is the other; they are NEVER both set").
		{"js/R13", func() (string, []string, int, string) {
			return newer, []string{older}, 0, `
const old = readFileSync(` + oldFile + `);
const L = new DataView(old.buffer, old.byteOffset, old.length).getUint32(16, true);
const recAt = 20 + L;
const back = eight(); const big = TNewPlan(4096, 4096);
const none = new TableFixedPlan(0, big.image.length, 1);
const hashOf = (d) => new DataView(d.buffer, d.byteOffset, d.length).getBigUint64(8, true);
function refuse(label, f, name, hash, capacity, plan) {
  poison(back, plan);
  const r = new TableFixedReport();
  const n = TLoad(back, capacity, f, f.length, plan, r);
  total(label, n, r, name, hash, back, plan);
}
// the three form bytes, whatever the length
for (const [b, name] of [[1, "PreviousForm"], [2, "MessageFormAsFile"], [6, "NewerForm"]]) {
  const f = copy(old); f[0] = b; refuse("form byte " + b, f, name, 0n, back.length, big);
  const s = new Uint8Array([b]); refuse("form byte " + b + " alone", s, name, 0n, back.length, big);
}
{ const f = copy(old, 40); refuse("cut inside the layout", f, "LayoutMalformed", 0n, back.length, big); }
{ const f = copy(old); f[20] ^= 0xFF; refuse("a lie about a known layout", f, "LayoutMalformed", 0n, back.length, big); }
{ const f = copy(old); const v = new DataView(f.buffer); v.setBigUint64(8, 0xDEADBEEFCAFEF00Dn, true);
  refuse("a hash in no lineage entry", f, "LayoutNewer", 0xDEADBEEFCAFEF00Dn, back.length, big); }
refuse("more records than the caller's room", old, "BatchTooLarge", 0n, 0, big);
refuse("a plan that does not fit", old, "PlanTooLarge", 0n, back.length, none);
refuse("no plan at all", old, "PlanTooLarge", 0n, back.length, null);
{ const f = copy(old); f[recAt + 7] ^= 0xFF; refuse("a record hash naming no layout", f, "NoLayout", null, back.length, big); }
`
		}, func(t *testing.T) {
			// layout_unsupported is the floor's name, and a floor needs a retired
			// entry: the file above is below it, so it is its own probe.
			t01Probe(t, node, newer, []string{older}, 1, table, `
const old = readFileSync(`+oldFile+`);
const back = eight(); const plan = TNewPlan(4096, 4096);
const hash = new DataView(old.buffer, old.byteOffset, old.length).getBigUint64(8, true);
poison(back, plan);
const r = new TableFixedReport();
const n = TLoad(back, back.length, old, old.length, plan, r);
total("below the floor", n, r, "LayoutUnsupported", hash, back, plan);
`)
		}},
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
