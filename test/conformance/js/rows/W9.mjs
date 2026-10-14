// W9 — PREFILL THE UNWRITTEN RANGES, AND ONLY THEM (docs/FIXED-FORM-ALGORITHM.md
// §4.3, the cell's law pointer at :308, quoted: "Prefill the bytes the plan does
// not write. The plan compiler computes the UNWRITTEN RANGES — the destination
// bytes no entry covers — and only those take the declared defaults before the
// loop; for the identity plan that list is EMPTY, so the identity read pays no
// prefill (fix 15). That answers 'absent field' — no plan entry, so the field
// keeps what the prefill put there." Companion: docs/SPEC-TABLES.md:6910, "THE
// PREFILL IS WHAT ANSWERS 'ABSENT FIELD'".)
//
// WHY THIS ROW EXISTS: the leg's own half of this law is retired BY NAME and
// owed — test/js-tables/fixedform.mjs:128 retires `holePrefillCompiled`, "the
// prefill through a plan compiled from a file", leaving only
// `holePrefillIdentity` (the empty list on the identity plan). This file pays
// that debt through the SAME machinery the generated reader runs, the way
// test/conformance/js/rows/W14.mjs lays a lineage plan: the module's own
// TableFixedLineagePlans compiles the plan, TableFixedHoles states the
// unwritten ranges, the declared-defaults table is the module's own
// <Name>FixedPrefill, and TableFixedRun + <Name>FixedDecode land the values.
//
// THE VECTOR, derived from Keyed.schema (package tabledemo):
//   fixed table TeamConfig { spawn_count int32 = 4 | min = 0, max = 64;
//                            banner string(16) }
// MY body is 24 bytes, positional: spawn_count at 0..4, banner's length word
// at 4..8, its 16-unit buffer at 8..24 — the dst rows the artifact emits are
// {0,...}, {4, aux 8, flavour 1}. THE WRITER (theirs) PREDATES SPAWN_COUNT —
// §4.3's "absent field": its layout is my own layout bytes with the
// spawn_count entry removed and the root's size and children patched to
// 20 and 1 (an id is matched, never arithmetic; the removed entry is 17 bytes
// at 21..38, so theirs is 38 bytes: count 2, the root with size 20, the
// banner entry verbatim). A THEIRS record is the 8-byte hash and then 20
// bytes: length 3 and "hey" (0x68 0x65 0x79), slack zeroed. The four bytes no
// entry of that plan covers are exactly MY spawn range, [0,4); the declared
// default is int32 4, so the prefill table's first lane is 04 00 00 00.
//
// THE NEGATIVE CONTROL is NOT run here: it breaks one constant of this law in
// the generated tree (build/tables-generated-js/examples/TabledemoTable.js,
// TableFixedHoles' `cover.fill(0)` -> `cover.fill(1)`, so the unwritten-range
// census sees no holes and no default is ever laid) and this file goes RED.
// It is restored after; the red/green pair is recorded in RESULT.md.
//
// Run from the repository root:  node test/conformance/js/rows/W9.mjs

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs), so a control that points the
// same path at a sabotaged copy aims this row too.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const root = resolve(generated);
const load = (path) => import(pathToFileURL(resolve(root, path)).href);

const keyed = await load("examples/KeyedTable.js");
const home = await load("examples/TabledemoTable.js");
const {
  TableFixedKnownLayout, TableFixedLineagePlans, TableFixedHoles, TableFixedRun,
  TableFixedReport, TeamConfig, TeamConfigFixedDecode,
} = home;

let failures = 0;
function check(ok, what) {
  console.log((ok ? "ok  " : "FAIL") + " " + what);
  if (!ok) { failures++; }
}

// ---- the artifact's own module-private plan constants, as W14 reads them ----

function numericArray(text, name, suffix, ctor) {
  const at = text.indexOf(`const ${name}${suffix} = new ${ctor}([`);
  if (at < 0) { return null; }
  const open = text.indexOf("[", at);
  const close = text.indexOf("]);", open);
  if (close < 0) { return null; }
  const lanes = text.slice(open + 1, close).replace(/\/\/[^\n]*/g, "")
    .match(/-?(?:0x[0-9a-fA-F]+|\d+)/g);
  return lanes === null ? [] : lanes.map(Number);
}

const keyedText = readFileSync(resolve(root, "examples", "KeyedTable.js"), "utf8");
const identity = numericArray(keyedText, "TeamConfig", "FixedIdentity", "Int32Array");
const dstRows = numericArray(keyedText, "TeamConfig", "FixedDst", "Int32Array");
const prefill = numericArray(keyedText, "TeamConfig", "FixedPrefill", "Uint8Array");
check(identity !== null && dstRows !== null && prefill !== null &&
  identity.length === 9 && dstRows.length === 15 && prefill.length === keyed.TeamConfigFixedBodyBytes,
  `vector: the artifact's identity plan, dst rows and prefill table are all present (${identity && identity.length}/${dstRows && dstRows.length}/${prefill && prefill.length} lanes)`);
if (identity === null || dstRows === null || prefill === null) {
  console.log("FAILED: the artifact states none of its plan constants");
  process.exit(1);
}
const body = keyed.TeamConfigFixedBodyBytes; // 24: spawn 0..4, banner len 4..8, buffer 8..24

// ---- 1: the identity plan's unwritten list is EMPTY, and it pays no prefill ----

const idPlan = keyed.TeamConfigFixedNewPlan(64, 64);
const idHoles = TableFixedHoles(Int32Array.from(identity), 1, idPlan.cover, idPlan.holes);
check(idHoles === 0, `identity list: the unwritten-range census of the identity plan is empty (got ${idHoles})`);

{
  // THE RECORD OF THIS BUILD: spawn 9 — NOT the declared default 4 — and the
  // banner "hey". Stain the caller's storage first: if the read paid any
  // prefill at all, the stained bytes under a written range would come back
  // as defaults before the copy, and the whole body is a copy, so what must
  // survive is the WRITER's 9 and the dirt's erasure by the plan alone.
  const rec = new Uint8Array(8 + body);
  const view = new DataView(rec.buffer);
  view.setUint32(8, 9, true);
  view.setInt32(12, 3, true);
  rec.set([0x68, 0x65, 0x79], 16);
  idPlan.image.fill(0x5a);
  const report = new TableFixedReport();
  TableFixedRun(Int32Array.from(identity), 1, rec, view, 8, idPlan.image, idPlan.view, null, report);
  const value = new TeamConfig();
  TeamConfigFixedDecode(value, idPlan.view, 0, report);
  check(value.SpawnCount === 9 && value.BannerLength === 3 && value.Banner[0] === 0x68 &&
    report.clamped === 0 && idPlan.image[0] === 9,
    "identity pays no prefill: spawn 9 — not the default 4 — lands through the empty hole list (image[0]=" + idPlan.image[0] + ")");
}

// ---- 2: a writer that predates a field — the absent field's plan ----

const own = keyed.TeamConfigFixedLayout; // count u32, then 17-byte entries
const theirs = new Uint8Array(4 + 17 * 2);
const t = new DataView(theirs.buffer);
t.setUint32(0, 2, true);                 // the root and the banner
theirs.set(own.subarray(4, 21), 4);      // the root entry, ids compared never parsed
t.setUint32(4 + 9, 20, true);            // their body: the banner's 4 + 16, nothing before it
t.setUint32(4 + 13, 1, true);            // one child
theirs.set(own.subarray(38, 55), 21);    // the banner entry verbatim

const THEIRS_LO = 0xfeedface, THEIRS_HI = 0x0badc0de;
const known = [new TableFixedKnownLayout(THEIRS_LO, THEIRS_HI, theirs, theirs.length, 8 + 20)];
const lane = TableFixedLineagePlans(known, keyed.TeamConfigFixedHashLo, keyed.TeamConfigFixedHashHi,
  own, Int32Array.from(dstRows), body)[0];
check(lane !== null && lane.ready === true && lane.why === 0 && lane.unknown === 0 && lane.kindMismatch === 0,
  `absent field: the pre-spawn writer's layout compiles to a plan of ${lane && lane.count} entries, nothing unknown`);

// ---- 3: the unwritten ranges are EXACTLY the absent field's bytes ----

const plan = keyed.TeamConfigFixedNewPlan(64, 64);
const holeN = TableFixedHoles(lane.entries, lane.count, plan.cover, plan.holes);
check(holeN === 1 && plan.holes[0] === 0 && plan.holes[1] === 4,
  `unwritten ranges: exactly spawn's four bytes, [0,4), no entry covers them (got ${holeN} @${plan.holes[0]},+${plan.holes[1]})`);
check(prefill[0] === 0x04 && prefill[1] === 0 && prefill[2] === 0 && prefill[3] === 0,
  `declared defaults: the prefill table's first lane is int32 4 — Keyed.schema's \`spawn_count int32 = 4\` (got ${prefill.slice(0, 4).join(",")})`);

// ---- 4: ONLY the unwritten range takes the default; the written bytes are the writer's ----

{
  const rec = new Uint8Array(8 + 20);
  const view = new DataView(rec.buffer);
  view.setUint32(0, THEIRS_LO, true);
  view.setUint32(4, THEIRS_HI, true);
  view.setInt32(8, 3, true);             // the banner's length at THEIR position 0
  rec.set([0x68, 0x65, 0x79], 12);       // "hey"
  plan.image.fill(0x5a);                 // stain the caller's storage
  const report = new TableFixedReport();
  for (let h = 0; h < holeN; h++) {      // the emitted FixedLoad's prefill, verbatim
    const o = plan.holes[h * 2], z = plan.holes[h * 2 + 1];
    for (let i = 0; i < z; i++) { plan.image[o + i] = prefill[o + i]; }
  }
  TableFixedRun(lane.entries, lane.count, rec, view, 8, plan.image, plan.view, lane.remap, report);
  const value = new TeamConfig();
  TeamConfigFixedDecode(value, plan.view, 0, report);
  check(value.SpawnCount === 4 && plan.image[0] === 0x04 && plan.image[1] === 0,
    `prefill lands the default: the absent field keeps ${value.SpawnCount}, the declared 4, not the stain (image[0]=${plan.image[0]})`);
  check(value.BannerLength === 3 && value.Banner[0] === 0x68 && value.Banner[1] === 0x65 &&
    value.Banner[2] === 0x79 && plan.image[4] === 3,
    "only those: the ranges an entry covers keep the WRITER'S bytes — \"hey\", not the default 4 and not the stain");
  check(!report.malformed && report.refused === 0 && report.clamped === 0 &&
    report.unknown === 0 && report.kindMismatch === 0,
    `the prefill moves nothing else: verdict clean, counters 0 (clamped=${report.clamped})`);
}

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("W9: the prefill takes the unwritten ranges only, and the identity list is empty");
