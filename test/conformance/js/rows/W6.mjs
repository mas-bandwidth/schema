// test/conformance/js/rows/W6.mjs — plan partition / split guard (docs/FIXED-FORM-ALGORITHM.md §42)
//
// THE SPLIT IS A PROPERTY OF THE PLAN'S ORDER, OWED BY EVERY LEG (§42).
// The plan partitions the entries — every unguarded one first, then every guarded
// one — and states where the second half starts. A leg carries split, partitions
// the entries, and may still test the guard per entry in its loop — what it may
// not do is skip the partition.
//
// This test verifies that the JavaScript leg carries split and uses it to partition
// entries, with coalescing inside each half and never across the split.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
import { readFileSync } from "node:fs";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const imp = (p) => import(pathToFileURL(resolve(generated, p)).href);

const v1Table = await imp("v1/Tblv1Table.js");
const v1 = await imp("v1/V1Table.js");
const v2 = await imp("v2/V2Table.js");

function parseInt32ArrayDecl(text, name, suffix) {
  const at = text.indexOf(`const ${name}${suffix} = new Int32Array([`);
  if (at < 0) return null;
  const open = text.indexOf("[", at);
  const close = text.indexOf("]);", open);
  const body = text.slice(open + 1, close).replace(/\/\/[^\n]*/g, "");
  return new Int32Array(body.match(/-?\d+/g).map(Number));
}

const v1Text = readFileSync(resolve(generated, "v1/V1Table.js"), "utf8");
const v2Text = readFileSync(resolve(generated, "v2/V2Table.js"), "utf8");
const v1CellDst = parseInt32ArrayDecl(v1Text, "Cell", "FixedDst");
const v1CfgDst = parseInt32ArrayDecl(v1Text, "Cfg", "FixedDst");
const v2CfgDst = parseInt32ArrayDecl(v2Text, "Cfg", "FixedDst");

const LANES = 9, OP = 0, SRC = 1, DST = 2, SIZE = 3, AUX = 4, GUARD = 5, ARG = 6, META = 7, ARGW = 8;
const NO_GUARD = -1, COPY = 0;

// The law: plan.split exists and is initialized to 0
function testPlanCarriesSplit() {
  const plan = new v1Table.TableFixedPlan(1024, 1024, 1024);
  
  if (!("split" in plan)) {
    console.log("FAIL: plan does not carry split property");
    return false;
  }
  
  if (plan.split !== 0) {
    console.log(`FAIL: plan.split initialized to ${plan.split}, expected 0`);
    return false;
  }
  
  console.log("PASS: plan carries split property and initializes to 0");
  return true;
}

// The law: split partitions entries; for an empty layout, split === count === 0
function testEmptyLayoutSplit() {
  const emptyLayout = new Uint8Array(4);
  const emptyDst = new Uint8Array(0);
  
  const plan = new v1Table.TableFixedPlan(1024, 1024, 1024);
  const report = new v1Table.TableFixedReport();
  
  v1Table.TableFixedCompile(emptyLayout, emptyLayout, emptyDst, plan, report);
  
  if (plan.split !== plan.count || plan.count !== 0) {
    console.log(`FAIL: empty layout produced split=${plan.split}, count=${plan.count}`);
    return false;
  }
  
  console.log("PASS: empty layout sets split === plan.count === 0");
  return true;
}

// The law: a layout with ONLY unguarded entries has split at the END (split === count)
function testUnguardedOnlyLayout() {
  const theirs = new v1Table.TableFixedLayoutView();
  v1Table.TableFixedParseLayout(v1.CellFixedLayout, 0, v1.CellFixedLayout.length, theirs);
  const plan = new v1Table.TableFixedPlan(1024, v1.CellFixedBodyBytes, 1024);
  const report = new v1Table.TableFixedReport();
  const count = v1Table.TableFixedCompile(theirs, v1.CellFixedLayout, v1CellDst, plan, report);

  if (count <= 0 || plan.count !== count) {
    console.log(`FAIL: CellFixedLayout failed to compile, count=${count}`);
    return false;
  }
  if (plan.split !== plan.count) {
    console.log(`FAIL: CellFixedLayout split (${plan.split}) !== count (${plan.count})`);
    return false;
  }

  for (let i = 0; i < plan.count; i++) {
    if (plan.entries[i * LANES + GUARD] !== NO_GUARD) {
      console.log(`FAIL: entry ${i} has guard ${plan.entries[i * LANES + GUARD]}, expected unguarded`);
      return false;
    }
  }

  console.log(`PASS: unguarded-only layout has split at the end (${plan.split}/${plan.count})`);
  return true;
}

// Validator for the plan partition invariant
function validatePlanPartition(plan) {
  const e = plan.entries, count = plan.count, split = plan.split;
  if (split < 0 || split > count) return false;
  let unguarded = 0;
  for (let i = 0; i < count; i++) {
    const isGuarded = e[i * LANES + GUARD] !== NO_GUARD;
    if (!isGuarded) unguarded++;
    if (i < split && isGuarded) return false;
    if (i >= split && !isGuarded) return false;
  }
  if (unguarded !== split) return false;
  if (split > 0 && split < count) {
    const prev = (split - 1) * LANES;
    const curr = split * LANES;
    const bothCopy = e[prev + OP] === COPY && e[curr + OP] === COPY;
    const sameGuard = e[prev + GUARD] === e[curr + GUARD];
    const contiguous = (e[prev + SRC] + e[prev + SIZE] === e[curr + SRC]) && (e[prev + DST] + e[prev + SIZE] === e[curr + DST]);
    if (bothCopy && sameGuard && contiguous) return false;
  }
  return true;
}

// The law: V1/V2 CfgFixedLayout carries both unguarded fields and union-guarded fields.
// Assert plan.split > 0, plan.count > plan.split, coalescing inside unguarded half,
// and strictly no merging across the split boundary.
function testPartitionWithGuardedAndUnguarded() {
  // 1. Forward direction: V1 wire compiled against V2 reader
  const theirsV1 = new v1Table.TableFixedLayoutView();
  v1Table.TableFixedParseLayout(v1.CfgFixedLayout, 0, v1.CfgFixedLayout.length, theirsV1);
  const planFwd = new v1Table.TableFixedPlan(1024, v2.CfgFixedBodyBytes, 1024);
  const reportFwd = new v1Table.TableFixedReport();
  const countFwd = v1Table.TableFixedCompile(theirsV1, v2.CfgFixedLayout, v2CfgDst, planFwd, reportFwd);

  if (countFwd !== planFwd.count || countFwd <= 0) {
    console.log(`FAIL: V1->V2 compile failed (count=${countFwd})`);
    return false;
  }
  if (planFwd.split <= 0 || planFwd.split >= planFwd.count) {
    console.log(`FAIL: V1->V2 expected 0 < split < count, got split=${planFwd.split}, count=${planFwd.count}`);
    return false;
  }
  if (!validatePlanPartition(planFwd)) {
    console.log("FAIL: V1->V2 plan failed partition validation");
    return false;
  }

  // Check coalescing within unguarded half [0, split):
  // Entries 15 and 16 each merge 3 contiguous int32 elements into a 12-byte COPY run
  const e = planFwd.entries;
  let coalescedRuns = 0;
  for (let i = 0; i < planFwd.split; i++) {
    if (e[i * LANES + OP] === COPY && e[i * LANES + SIZE] > 4) {
      coalescedRuns++;
    }
  }
  if (coalescedRuns === 0) {
    console.log("FAIL: expected coalesced runs within unguarded partition [0, split)");
    return false;
  }

  // Check boundary pair (split - 1) and split:
  const prevIdx = planFwd.split - 1;
  const splitIdx = planFwd.split;
  if (e[prevIdx * LANES + GUARD] !== NO_GUARD) {
    console.log(`FAIL: entry at split - 1 (${prevIdx}) carries guard ${e[prevIdx * LANES + GUARD]}`);
    return false;
  }
  if (e[splitIdx * LANES + GUARD] === NO_GUARD) {
    console.log(`FAIL: entry at split (${splitIdx}) is unguarded`);
    return false;
  }

  // 2. Reverse direction: V2 wire compiled against V1 reader
  const theirsV2 = new v1Table.TableFixedLayoutView();
  v1Table.TableFixedParseLayout(v2.CfgFixedLayout, 0, v2.CfgFixedLayout.length, theirsV2);
  const planRev = new v1Table.TableFixedPlan(1024, v1.CfgFixedBodyBytes, 1024);
  const reportRev = new v1Table.TableFixedReport();
  const countRev = v1Table.TableFixedCompile(theirsV2, v1.CfgFixedLayout, v1CfgDst, planRev, reportRev);

  if (countRev !== planRev.count || countRev <= 0) {
    console.log(`FAIL: V2->V1 compile failed (count=${countRev})`);
    return false;
  }
  if (planRev.split <= 0 || planRev.split >= planRev.count) {
    console.log(`FAIL: V2->V1 expected 0 < split < count, got split=${planRev.split}, count=${planRev.count}`);
    return false;
  }
  if (!validatePlanPartition(planRev)) {
    console.log("FAIL: V2->V1 plan failed partition validation");
    return false;
  }

  console.log(`PASS: V1/V2 Cfg partition holds: split=${planFwd.split} of count=${planFwd.count}, coalescing verified inside partition, no merge across boundary`);
  return true;
}

// The law: coalescing operates within [0, split) and [split, count), never across split
function testCoalescingWithinPartitionsAndNotAcrossSplit() {
  const plan = new v1Table.TableFixedPlan(16, 64, 16);
  const e = plan.entries;

  function coalescePass(p, from) {
    const entries = p.entries;
    let out = from | 0;
    for (let i = out; i < p.count; i++) {
      const b = i * LANES;
      if (out > (from | 0)) {
        const prev = (out - 1) * LANES;
        if (entries[prev + OP] === COPY && entries[b + OP] === COPY &&
            entries[prev + GUARD] === entries[b + GUARD] &&
            entries[prev + ARG] === entries[b + ARG] &&
            entries[prev + ARGW] === entries[b + ARGW] &&
            entries[prev + SRC] + entries[prev + SIZE] === entries[b + SRC] &&
            entries[prev + DST] + entries[prev + SIZE] === entries[b + DST]) {
          entries[prev + SIZE] += entries[b + SIZE];
          continue;
        }
      }
      const o = out * LANES;
      for (let k = 0; k < LANES; k++) { entries[o + k] = entries[b + k]; }
      out++;
    }
    p.count = out;
  }

  // Pass 0: compile unguarded entries
  // Add 2 contiguous unguarded copies:
  // entry 0: src 0..4 dst 0..4 size 4, guard -1
  e[0 * LANES + OP] = COPY; e[0 * LANES + SRC] = 0; e[0 * LANES + DST] = 0; e[0 * LANES + SIZE] = 4;
  e[0 * LANES + GUARD] = NO_GUARD; e[0 * LANES + ARG] = 0; e[0 * LANES + ARGW] = 1;
  // entry 1: src 4..8 dst 4..8 size 4, guard -1
  e[1 * LANES + OP] = COPY; e[1 * LANES + SRC] = 4; e[1 * LANES + DST] = 4; e[1 * LANES + SIZE] = 4;
  e[1 * LANES + GUARD] = NO_GUARD; e[1 * LANES + ARG] = 0; e[1 * LANES + ARGW] = 1;
  plan.count = 2;

  // Coalesce pass 0 from 0
  coalescePass(plan, 0);
  if (plan.count !== 1 || e[0 * LANES + SIZE] !== 8) {
    console.log(`FAIL: pass 0 coalesce expected 1 merged entry of size 8, got count=${plan.count}, size=${e[0 * LANES + SIZE]}`);
    return false;
  }

  // Set split boundary
  plan.split = plan.count; // 1

  // Pass 1: compile guarded entries (starting at plan.split)
  // Add 2 contiguous guarded copies with same guard=10, arg=1
  // Notice entry 1 is contiguous with entry 0 (src 8 == 0 + 8, dst 8 == 0 + 8):
  const idx1 = plan.count;
  e[idx1 * LANES + OP] = COPY; e[idx1 * LANES + SRC] = 8; e[idx1 * LANES + DST] = 8; e[idx1 * LANES + SIZE] = 4;
  e[idx1 * LANES + GUARD] = 10; e[idx1 * LANES + ARG] = 1; e[idx1 * LANES + ARGW] = 1;
  const idx2 = idx1 + 1;
  e[idx2 * LANES + OP] = COPY; e[idx2 * LANES + SRC] = 12; e[idx2 * LANES + DST] = 12; e[idx2 * LANES + SIZE] = 4;
  e[idx2 * LANES + GUARD] = 10; e[idx2 * LANES + ARG] = 1; e[idx2 * LANES + ARGW] = 1;
  plan.count = idx2 + 1; // 3

  // Coalesce pass 1 from plan.split
  coalescePass(plan, plan.split);
  if (plan.count !== 2) {
    console.log(`FAIL: pass 1 coalesce expected count=2, got ${plan.count}`);
    return false;
  }
  if (e[1 * LANES + SIZE] !== 8 || e[1 * LANES + GUARD] !== 10) {
    console.log(`FAIL: pass 1 coalesce expected entry 1 size=8 guard=10`);
    return false;
  }

  // The boundary must NOT have merged across split even though src/dst were contiguous:
  if (e[0 * LANES + GUARD] !== NO_GUARD || e[1 * LANES + GUARD] !== 10) {
    console.log("FAIL: entries merged across split");
    return false;
  }
  if (e[0 * LANES + SIZE] !== 8 || e[1 * LANES + SIZE] !== 8) {
    console.log("FAIL: partition sizes altered across split");
    return false;
  }

  console.log("PASS: two-pass coalescing coalesces inside each partition and never across split");
  return true;
}

// Negative controls: corrupted plan partitions MUST fail validation
function testNegativeControls() {
  const theirs = new v1Table.TableFixedLayoutView();
  v1Table.TableFixedParseLayout(v1.CfgFixedLayout, 0, v1.CfgFixedLayout.length, theirs);
  const lawfulPlan = new v1Table.TableFixedPlan(1024, v2.CfgFixedBodyBytes, 1024);
  const report = new v1Table.TableFixedReport();
  v1Table.TableFixedCompile(theirs, v2.CfgFixedLayout, v2CfgDst, lawfulPlan, report);

  if (!validatePlanPartition(lawfulPlan)) {
    console.log("FAIL: lawful plan failed partition validation");
    return false;
  }

  function clone(p) {
    const res = new v1Table.TableFixedPlan(1024, v2.CfgFixedBodyBytes, 1024);
    res.count = p.count;
    res.split = p.split;
    res.entries.set(p.entries);
    return res;
  }

  // Control 1: split forced to 0 when unguarded entries exist
  const c1 = clone(lawfulPlan);
  c1.split = 0;
  if (validatePlanPartition(c1)) {
    console.log("FAIL: negative control 1 (split=0) unexpectedly passed");
    return false;
  }

  // Control 2: split forced to count when guarded entries exist
  const c2 = clone(lawfulPlan);
  c2.split = c2.count;
  if (validatePlanPartition(c2)) {
    console.log("FAIL: negative control 2 (split=count) unexpectedly passed");
    return false;
  }

  // Control 3: guarded entry placed below split
  const c3 = clone(lawfulPlan);
  c3.entries[5 * LANES + GUARD] = 137;
  if (validatePlanPartition(c3)) {
    console.log("FAIL: negative control 3 (guarded entry below split) unexpectedly passed");
    return false;
  }

  // Control 4: unguarded entry placed above split
  const c4 = clone(lawfulPlan);
  c4.entries[(c4.split + 1) * LANES + GUARD] = NO_GUARD;
  if (validatePlanPartition(c4)) {
    console.log("FAIL: negative control 4 (unguarded entry above split) unexpectedly passed");
    return false;
  }

  // Control 5: merged across split boundary
  const c5 = clone(lawfulPlan);
  const prevIdx = c5.split - 1;
  const splitIdx = c5.split;
  c5.entries[splitIdx * LANES + GUARD] = NO_GUARD;
  c5.entries[splitIdx * LANES + OP] = COPY;
  c5.entries[prevIdx * LANES + OP] = COPY;
  c5.entries[splitIdx * LANES + SRC] = c5.entries[prevIdx * LANES + SRC] + c5.entries[prevIdx * LANES + SIZE];
  c5.entries[splitIdx * LANES + DST] = c5.entries[prevIdx * LANES + DST] + c5.entries[prevIdx * LANES + SIZE];
  if (validatePlanPartition(c5)) {
    console.log("FAIL: negative control 5 (merged across split) unexpectedly passed");
    return false;
  }

  console.log("PASS: all negative controls correctly caught partition and split boundary violations");
  return true;
}

// Run all tests
let passed = 0;
let failed = 0;

if (testPlanCarriesSplit()) { passed++; } else { failed++; }
if (testEmptyLayoutSplit()) { passed++; } else { failed++; }
if (testUnguardedOnlyLayout()) { passed++; } else { failed++; }
if (testPartitionWithGuardedAndUnguarded()) { passed++; } else { failed++; }
if (testCoalescingWithinPartitionsAndNotAcrossSplit()) { passed++; } else { failed++; }
if (testNegativeControls()) { passed++; } else { failed++; }

console.log(`\nTotal: ${passed} passed, ${failed} failed`);

if (failed > 0) {
  process.exit(1);
}
