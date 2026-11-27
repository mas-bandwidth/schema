// R14 — `layout_record_too_large` FOR A COMPILED ENTRY REACHING PAST THE
// WRITER'S DECLARED RECORD, not only for the 65536 bound and a zero root.
//
// THE LAW:
//
//   docs/FIXED-FORM-ALGORITHM.md:395-399 (§4.2, the compile): "every compiled
//   entry is bounded by the WRITER'S OWN DECLARED RECORD SIZE — `src + size`,
//   plus 4 for a text entry's length word, and a `guard` offset too, all
//   within `root.size`. A layout reaching past it is refused WHOLE and never
//   partly compiled, under `layout_record_too_large` (fix 3)."
//
//   docs/FIXED-FORM-ALGORITHM.md:949 and :961 (§5.2 PLAN): `record :=
//   x.root.size`, then "if an entry reached past `record`: REFUSE
//   layout_record_too_large".
//
//   docs/FIXED-FORM-ALGORITHM.md:1559 (§5.8 row 13): the reference wired the
//   name only to the 65536 bound and a zero root, and returned
//   `layout_malformed` through the compile's failure path; the page owes
//   "`layout_record_too_large` for the entry too (fix 3)".
//
// THE PRODUCTION PATH ON THIS LEG. The plan compiler is the generated
// runtime's `TableFixedCompile` (emitted from
// internal/codegen/jstable/fixedruntime.go into every unit's home module,
// here build/tables-generated-js/v1/Tblv1Table.js): `TableFixedPush` takes
// every entry's reach against `plan.record = TableFixedSize(theirs, 0)` — the
// WRITER'S declared root size — and one entry past it marks the plan hostile,
// so the compile answers -2. `TableFixedLineagePlans`, which lays a plan per
// lock lineage entry down at module load, turns that -2 into a lane whose
// `why` is LayoutRecordTooLarge and never retries it with more room. Both are
// asserted here.
//
// THE VECTOR, built from the law (no corpus holds a forged lock layout; the
// same derivation test/conformance/rust/rows/R14.rs records). A layout block
// is a u32LE entry count, then 17-byte entries: id u64LE, kind u8, size
// u32LE, children u32LE. Kind 13 is a table (size = the sum of its fields),
// kind 15 a union (size = tag + widest arm, the tag 1/2/4/8), kind 8 a u32
// (§1's kind-code table; the Rust row spells the scalar as kind 4, an i32 —
// the reach is the same four bytes).
//
//   THEIRS, the writer's lock layout: a record of 5 bytes — one union whose
//   tag is 1 byte, one arm a u32.
//   MINE, the reader: the same ids with the tag grown 1 -> 8 (a lawful §5.2
//   ordinal-width evolution). The compile lands the union's None const at
//   src 0 with the READER'S tag width, so that entry reaches 0 + 8 = 8, past
//   the writer's declared record of 5 — under 65536, root non-zero, every
//   other fact lawful. The law answers layout_record_too_large.
//
//   THE LAWFUL TWIN: the tag grown only to 2 reaches 0 + 2 = 2 of 5 and
//   compiles whole — the bound is "past", not "at" — and THEIRS against
//   itself compiles whole. So the refusal is the reach and nothing else.
//
// run: node test/conformance/js/rows/R14.mjs   (from the repository root)
// exit: 0 = every assertion passed, 1 = one or more failed
// depends on: build/tables-generated-js/v1/* (make build/tables-generated-js/.stamp)

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

let pass = 0;
let fail = 0;
function check(ok, msg) {
  if (ok) { console.log("PASS " + msg); pass++; }
  else { console.log("FAIL " + msg); fail++; }
}

const rt = await load("v1/Tblv1Table.js");
const {
  TableFixedCompile,
  TableFixedLineagePlans,
  TableFixedKnownLayout,
  TableFixedLayoutView,
  TableFixedParseLayout,
  TableFixedPlan,
  TableFixedReport,
  TableFixedRefusal,
  TableFixedRefusalName,
  TableFixedLanes,
} = rt;

// ---- the vectors -------------------------------------------------------------

// One 17-byte entry: id u64LE, kind u8, size u32LE, children u32LE.
function block(entries) {
  const buf = new Uint8Array(4 + entries.length * 17);
  const v = new DataView(buf.buffer);
  v.setUint32(0, entries.length, true);
  entries.forEach(([id, kind, size, children], i) => {
    const at = 4 + i * 17;
    v.setUint32(at, id, true); // the high word stays 0
    buf[at + 8] = kind;
    v.setUint32(at + 9, size, true);
    v.setUint32(at + 13, children, true);
  });
  return buf;
}

// THEIRS — the writer's lock layout: record 5, a union of tag 1 over one u32.
const THEIRS = block([
  [1, 13, 5, 1],
  [7, 15, 5, 1],
  [9, 8, 4, 0],
]);

// MINE — the same ids, the union's tag grown to `tag` (its one arm unchanged).
function mine(tag) {
  return block([
    [1, 13, tag + 4, 1],
    [7, 15, tag + 4, 1],
    [9, 8, 4, 0],
  ]);
}

// MY SIDE, five lanes per entry [off, stride, aux, counted, arg], the way the
// generated <Root>FixedDst rows lay it: the union's tag at image 0 (its aux
// lane), its arm's payload past the tag.
function dst(tag) {
  return new Int32Array([
    0, 0, 0, 0, 0, // 0 the root
    0, 0, 0, 0, 0, // 1 the union: tag at aux 0
    tag, 0, 0, 0, 0, // 2 the u32 arm, past the tag
  ]);
}

function parse(bytes) {
  const v = new TableFixedLayoutView();
  const ok = TableFixedParseLayout(bytes, 0, bytes.length, v);
  return { ok, v };
}

function compile(theirsBytes, tag) {
  const t = parse(theirsBytes);
  const plan = new TableFixedPlan(256, tag + 4, 256);
  const report = new TableFixedReport();
  const made = t.ok ? TableFixedCompile(t.v, mine(tag), dst(tag), plan, report) : NaN;
  return { made, plan, report };
}

// THE ENTRIES A PLAN HOLDS, as [op, src, size] triples, for the reach checks.
function entries(plan) {
  const out = [];
  for (let i = 0; i < plan.count; i++) {
    const b = i * TableFixedLanes;
    out.push([plan.entries[b], plan.entries[b + 1], plan.entries[b + 3]]);
  }
  return out;
}

// ---- 0. every vector is a lawful layout: the compile is what is reached ------
{
  const t = parse(THEIRS);
  check(t.ok && t.v.count === 3, "THEIRS parses under §1.1 (3 entries)");
  check(t.ok && rt.TableFixedSize(t.v, 0) === 5,
    "the writer's declared record is 5 bytes: no 65536 bound, no zero root");
  for (const tag of [1, 2, 8]) {
    const m = parse(mine(tag));
    check(m.ok, `MINE with tag ${tag} parses under §1.1`);
  }
}

// ---- 1. THE POSITIVE CONTROL: THEIRS against itself compiles whole -----------
{
  const { made, plan, report } = compile(THEIRS, 1);
  check(made > 0 && made === plan.count, `THEIRS against THEIRS compiles whole (${made} entries)`);
  check(!plan.hostile, "no entry of the identity pair reaches past the record");
  check(report.unknown === 0 && report.mismatch === 0, "no census moves on the identity pair");
  const es = entries(plan);
  check(es.some(([op, src, size]) => op === 0 && src === 1 && size === 4),
    "the u32 arm compiles to a copy of 4 bytes at src 1 (past the 1-byte tag)");
  check(es.every(([, src, size]) => src + size <= 5),
    "every compiled entry lies inside the writer's record of 5");
}

// ---- 2. THE BOUNDARY: a reach of exactly the record still compiles ------------
{
  const { made, plan } = compile(THEIRS, 2);
  check(made > 0 && !plan.hostile, `tag grown 1 -> 2 reaches 2 of 5 and compiles whole (${made} entries)`);
}

// ---- 3. THE CELL: the None const reaches 0 + 8 against a record of 5 ----------
{
  const { made, plan, report } = compile(THEIRS, 8);
  check(made === -2, `the compile refuses the plan whole: TableFixedCompile answers -2, got ${made}`);
  check(plan.hostile === true, "the refusal is the reach past the writer's record (plan.hostile)");
  check(made !== -1 && !plan.overflow, "not plan_too_large: more room cannot mend a stranger's sizes");
  check(report.unknown === 0 && report.mismatch === 0, "refuse is total: no census moves");
}

// ---- 4. THE NAME, through the lineage path the generated module runs ---------
//
// A lock lineage entry whose hash is not this build's own is compiled at module
// load by TableFixedLineagePlans; its lane carries the refusal a load answers.
{
  const own = [0x11111111, 0x22222222];
  const lane = (tag) => {
    const known = [new TableFixedKnownLayout(0x33333333, 0x44444444, THEIRS, THEIRS.length, 5)];
    return TableFixedLineagePlans(known, own[0], own[1], mine(tag), dst(tag), tag + 4)[0];
  };

  const bad = lane(8);
  check(bad !== null && bad.why === TableFixedRefusal.LayoutRecordTooLarge,
    "the lineage lane for the over-reaching entry carries LayoutRecordTooLarge");
  check(bad !== null && TableFixedRefusalName(bad.why) === "layout_record_too_large",
    `the name spells layout_record_too_large, got ${bad && TableFixedRefusalName(bad.why)}`);
  check(bad !== null && bad.why !== TableFixedRefusal.LayoutMalformed,
    "§5.8 row 13: the compile's failure path is NOT layout_malformed");
  check(bad !== null && bad.why !== TableFixedRefusal.PlanTooLarge && bad.ready !== true,
    "not plan_too_large, and no partly compiled plan is handed out");

  const good = lane(2);
  check(good !== null && good.why === 0 && good.ready === true && good.recordBytes === 5,
    "the lawful twin (tag 2) lays a ready plan with the lock's record size");
}

// ---- 5. ONE NAME across both wirings: the parse bound lands the same constant -
{
  const over = THEIRS.slice();
  new DataView(over.buffer).setUint32(4 + 9, 65537, true);
  const zero = THEIRS.slice();
  new DataView(zero.buffer).setUint32(4 + 9, 0, true);
  for (const [what, bytes] of [["a root of 65537", over], ["a zero root", zero]]) {
    const v = new TableFixedLayoutView();
    const ok = TableFixedParseLayout(bytes, 0, bytes.length, v);
    check(!ok && v.refusal === TableFixedRefusal.LayoutRecordTooLarge,
      `the parse refuses ${what} under the same layout_record_too_large`);
  }
}

if (fail > 0) {
  console.log(`\nFAILED: ${fail} of ${pass + fail} assertions`);
  process.exit(1);
}
console.log(`\nPASSED: ${pass} assertions`);
process.exit(0);
