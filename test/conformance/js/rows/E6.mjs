// js/E6 — RENAMING USES THE DECLARED IDENTITY (audit schema#898, matrix
// schema#876; the cpp, dart, java and rust rows of the same cell are its
// siblings, test/conformance/<leg>/rows/E6.*).
//
// THE LAW (docs/SPEC-TABLES.md, "Renaming is not a change"): "`was =
// "old_name"` (§5) keeps a field's identity through a rename: the wire id
// stays the hash of the old name. A rename under `was` is therefore not a
// change to a fixed table at all." And the read's half (docs/FIXED-FORM-
// ALGORITHM.md §5.9 #41): "A `was =` RENAME IS PAIRED BY WIRE ID IN ANY
// HARNESS, NEVER BY NAME ... `id` is `fnv1a64(wire name)`, so `was =` matches
// by the OLD name."
//
// FX2 (test/tables/FX2.schema) spells the field `renamed_to` and declares
// `was = "renamed"`; FX1 spells it `renamed`. This row asserts both halves on
// the generated JavaScript (build/js-fixed/fx1, build/js-fixed/fx2, made by
// make build/js-fixed/.stamp, which conformance-rows-js depends on):
//
//   1. THE DECLARED IDENTITY. FX2's emitted layout carries fnv1a64("renamed")
//      as that entry's id, never fnv1a64("renamed_to"), and FX1's carries the
//      same id for `renamed`. The hash is checked against `keep`, a field
//      neither side renames, so the hash this file computes is the layout's.
//
//   2. THE READ PAIRS BY IT. An OLD writer — a lawful lineage entry built from
//      FX1's own `renamed` and `keep` entries, in the order [renamed, keep],
//      which is NOT the reader's order — is compiled against FX2's own layout
//      and dst rows by the module's own TableFixedLineagePlans, its unwritten
//      ranges prefilled from FX2's own prefill table, and the record run and
//      decoded exactly as the emitted FxRootFixedLoad does. `renamed_to` must
//      land the writer's `renamed` value, `keep` the writer's `keep`, and
//      every field the writer did not carry its declared default. (The writer
//      carries only fields the reader names: a writer field the reader does
//      not name is UNLAWFUL and refuses, §5.4, so FX1's whole layout — it has
//      `gone` — is not the vector.)
//
//   3. THE CONTROL, run every time: the same read against FX2's layout with
//      the renamed entry's id patched to fnv1a64("renamed_to") — the wire a
//      reader that ignored `was` would declare. The pairing must die with the
//      declared identity and with nothing else: the writer's `renamed` is no
//      longer named by the reader (the compile refuses it), while the same
//      vector with the true ids compiles.
//
// This row was a placeholder that printed "PASS: E6 skeleton loaded" and
// exited 0, asserting nothing.
//
// Run from the repository root:  node test/conformance/js/rows/E6.mjs

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const fixed = process.env.SCHEMA_JS_FIXED ?? "build/js-fixed";
const load = (path) => import(pathToFileURL(resolve(fixed, path)).href);

const fx1 = await load("fx1/FX1Table.js");
const fx2 = await load("fx2/FX2Table.js");
const rt = await load("fx2/Tblfx2Table.js");

let failures = 0;
function check(ok, what) {
  console.log((ok ? "ok  " : "FAIL") + " " + what);
  if (!ok) { failures++; }
}

// ---- fnv1a64 over the wire name's UTF-8 bytes (ir/tablewire.go TableWireId) ----
function fnv1a64(name) {
  let h = 0xcbf29ce484222325n;
  for (const b of new TextEncoder().encode(name)) {
    h ^= BigInt(b);
    h = (h * 0x100000001b3n) & 0xffffffffffffffffn;
  }
  return h;
}

// ---- a layout block: u32 count, then 17-byte entries (id u64, kind u8, size u32, children u32) ----
const ENTRY = 17;
function entries(layout) {
  const v = new DataView(layout.buffer, layout.byteOffset, layout.byteLength);
  const n = v.getUint32(0, true);
  const out = [];
  for (let i = 0; i < n; i++) {
    const at = 4 + i * ENTRY;
    out.push({ at, id: v.getBigUint64(at, true), kind: layout[at + 8],
      size: v.getUint32(at + 9, true), children: v.getUint32(at + 13, true) });
  }
  return out;
}

const mine = fx2.FxRootFixedLayoutCopy();
const old = fx1.FxRootFixedLayoutCopy();
const mineE = entries(mine), oldE = entries(old);
const idOf = (list, name) => list.find((e) => e.id === fnv1a64(name));

// ---- 1: the declared identity ----
check(idOf(mineE, "keep") !== undefined && idOf(oldE, "keep") !== undefined,
  "the hash: both layouts carry fnv1a64(\"keep\") for the field neither side renames");
const renamedMine = idOf(mineE, "renamed");
const renamedOld = idOf(oldE, "renamed");
check(renamedMine !== undefined,
  "FX2's renamed_to (was = \"renamed\") carries fnv1a64(\"renamed\") — the OLD name's hash");
check(idOf(mineE, "renamed_to") === undefined,
  "FX2's layout carries no fnv1a64(\"renamed_to\"): the new spelling is not the wire identity");
check(renamedOld !== undefined && renamedMine !== undefined && renamedOld.kind === renamedMine.kind && renamedOld.size === renamedMine.size,
  "FX1's renamed and FX2's renamed_to are one wire identity: same id, kind and size");
if (renamedMine === undefined || renamedOld === undefined) {
  console.log("FAILED: the declared identity is missing, the read cannot be asserted");
  process.exit(1);
}

// ---- the OLD writer: [root, renamed, keep], a different order from mine ----
const keepOld = idOf(oldE, "keep");
const theirs = new Uint8Array(4 + 3 * ENTRY);
const tv = new DataView(theirs.buffer);
tv.setUint32(0, 3, true);
theirs.set(old.subarray(4, 4 + ENTRY), 4);                          // the root entry, FX1's own id and kind
tv.setUint32(4 + 9, renamedOld.size + keepOld.size, true);          // its body: the two fields
tv.setUint32(4 + 13, 2, true);                                      // two children
theirs.set(old.subarray(renamedOld.at, renamedOld.at + ENTRY), 4 + ENTRY);     // renamed first
theirs.set(old.subarray(keepOld.at, keepOld.at + ENTRY), 4 + 2 * ENTRY);       // then keep
const theirBody = renamedOld.size + keepOld.size;                   // 4 + 4

const THEIRS_LO = 0x5eed0e60, THEIRS_HI = 0x0ff1ce06;
function compile(myLayout) {
  const known = [new rt.TableFixedKnownLayout(THEIRS_LO, THEIRS_HI, theirs, theirs.length, 8 + theirBody)];
  return rt.TableFixedLineagePlans(known, fx2.FxRootFixedHashLo, fx2.FxRootFixedHashHi,
    myLayout, fx2.FxRootFixedDstCopy(), fx2.FxRootFixedBodyBytes)[0];
}

// The record: the 8-byte hash, then renamed = 321 at 0 and keep = 4242 at 4.
const rec = new Uint8Array(8 + theirBody);
const rv = new DataView(rec.buffer);
rv.setUint32(0, THEIRS_LO, true);
rv.setUint32(4, THEIRS_HI, true);
rv.setInt32(8, 321, true);
rv.setUint32(12, 4242, true);

// ---- 2: the read pairs by the declared identity ----
const lane = compile(mine);
check(lane !== null && lane.ready === true && lane.why === 0,
  `the old writer's lineage entry compiles against FX2 (why=${lane && rt.TableFixedRefusalName(lane.why)})`);
if (lane !== null && lane.ready === true) {
  const plan = fx2.FxRootFixedNewPlan();
  const prefill = fx2.FxRootFixedPrefillCopy();
  const holeN = rt.TableFixedHoles(lane.entries, lane.count, plan.cover, plan.holes);
  plan.image.fill(0x5a); // stain: only the plan and the prefill may decide a byte
  for (let h = 0; h < holeN; h++) {
    const o = plan.holes[h * 2], z = plan.holes[h * 2 + 1];
    for (let i = 0; i < z; i++) { plan.image[o + i] = prefill[o + i]; }
  }
  const report = new rt.TableFixedReport();
  rt.TableFixedRun(lane.entries, lane.count, rec, rv, 8, plan.image, plan.view, lane.remap, report);
  rt.FxRootFixedCheckRead(plan.view, 0, report);
  check(report.refused === 0 && !("damaged" in report),
    `the record runs and checks clean (refused=${rt.TableFixedRefusalName(report.refused)})`);
  const v = new rt.FxRoot();
  v.renamedTo = -1; v.keep = 0; v.added = -1;
  rt.FxRootFixedDecode(v, plan.view, 0, report);
  check(v.renamedTo === 321,
    `renamed_to lands the old writer's renamed value 321, paired by id across a different position (got ${v.renamedTo})`);
  check(v.keep === 4242, `keep lands the writer's 4242 (got ${v.keep})`);
  const d = new rt.FxRoot();
  check(v.added === d.added && v.narrow === d.narrow && v.labelLength === d.labelLength &&
    v.nested.a === d.nested.a && v.extra.x === d.extra.x,
    `every field the writer did not carry holds its declared default (added ${v.added}, narrow ${v.narrow})`);
}

// ---- 3: the control — patch my renamed entry to the NEW name's hash ----
{
  const ignoringWas = new Uint8Array(mine);
  new DataView(ignoringWas.buffer).setBigUint64(renamedMine.at, fnv1a64("renamed_to"), true);
  const bad = compile(ignoringWas);
  // The old writer's `renamed` is now a writer field no reader field names.
  // What the read then does is a separate law (§5.4 calls the pair UNLAWFUL,
  // refused layout_malformed; this leg, like the C++ reference, still lays a
  // plan and counts it unknown), so this control asserts only what E6 owns:
  // with the declared identity gone, the old value is NOT paired into
  // renamed_to, whichever way the entry is answered.
  let paired = false;
  if (bad !== null && bad.ready === true && bad.why === 0) {
    const plan = fx2.FxRootFixedNewPlan();
    const prefill = fx2.FxRootFixedPrefillCopy();
    const holeN = rt.TableFixedHoles(bad.entries, bad.count, plan.cover, plan.holes);
    for (let h = 0; h < holeN; h++) {
      const o = plan.holes[h * 2], z = plan.holes[h * 2 + 1];
      for (let i = 0; i < z; i++) { plan.image[o + i] = prefill[o + i]; }
    }
    const report = new rt.TableFixedReport();
    rt.TableFixedRun(bad.entries, bad.count, rec, rv, 8, plan.image, plan.view, bad.remap, report);
    const v = new rt.FxRoot();
    rt.FxRootFixedDecode(v, plan.view, 0, report);
    paired = v.renamedTo === 321;
    check(v.keep === 4242, "the control's bite is the declared identity alone: keep still pairs (got " + v.keep + ")");
  }
  check(!paired,
    `a reader that ignored \`was\` does not pair the old writer's renamed into renamed_to (entry ${bad && bad.ready ? "compiled" : "refused " + (bad && rt.TableFixedRefusalName(bad.why))})`);
}

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("E6: a `was =` rename keeps the declared identity, and the read pairs by it");
