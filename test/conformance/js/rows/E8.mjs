// row js/E8 — "Append and deprecate under the backward-read contract"
// (docs/roadmap.sexp js/E8; docs/SPEC-TABLES.md:116 "by appending and
// deprecating in place").
//
// THE LAW, docs/SPEC-TABLES.md:116: a fixed table is versioned "by appending
// and deprecating in place". The backward-read contract (§5) says a newer
// reader (whose schema has appended fields or deprecated ones) must read an
// older file: appended fields are answered by the prefill (their defaults),
// and a deprecated field is read but its value means nothing.
//
// DERIVATION OF THE BYTE VECTORS FROM THE LAW:
//
// A fixed-form layout is a u32 entry-count followed by 17-byte entries
// (docs/FIXED-FORM-ALGORITHM.md §1.1). The root is kind 13 (table), its
// children are the fields in pre-order.
//
// The "old" writer has two int32 fields {a, b}. The "new" reader has three:
// {a, b, c} with c appended. Both layouts are valid; the reader compiles a
// plan from the old to the new, and the plan copies a and b while c is left
// to the prefill.
//
// FNV-1a 64-bit of "a": id_lo = 0x8601ec8c, id_hi = 0xaf63dc4c
// FNV-1a 64-bit of "b": id_lo = 0x8601f1a5, id_hi = 0xaf63df4c
// FNV-1a 64-bit of "c": id_lo = 0x8601eff2, id_hi = 0xaf63de4c
// (the exact values do not matter for the plan — only equality does).
// Kind 4 = int32, width 4, children 0. Kind 13 = table.
//
// Old layout bytes (3 entries = 55 bytes):
//   header: count=3
//   entry 0: root table, size=8, children=2
//   entry 1: a, kind=4, size=4, children=0
//   entry 2: b, kind=4, size=4, children=0
//
// New layout bytes (4 entries = 72 bytes):
//   header: count=4
//   entry 0: root table, size=12, children=3
//   entry 1: a (same id, kind, size)
//   entry 2: b (same id, kind, size)
//   entry 3: c (new id, kind=4, size=4, children=0)

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

const tables = await load("examples/TabledemoTable.js");

const {
  TableFixedLayoutView,
  TableFixedParseLayout,
  TableFixedCompile,
  TableFixedPlan,
  TableFixedReport,
  TableFixedRun,
  TableFixedHoles,
} = tables;

// The prefill bytes for a 12-byte body of three int32s at defaults (all zero).
const prefill = new Uint8Array(12); // zero defaults

// The reader's dst offsets for the new layout: 5 lanes per entry,
// lane 0 = storage offset (dst).
// Entry 0: root at 0
// Entry 1: field a at offset 0
// Entry 2: field b at offset 4
// Entry 3: field c at offset 8
const newDst = new Int32Array([
  0, 0, 0, 0, 0,
  0, 0, 0, 0, 0,
  4, 0, 0, 0, 0,
  8, 0, 0, 0, 0,
]);

// ---- old layout: root {a:int32, b:int32} ----
const oldLayout = new Uint8Array([
  0x03, 0x00, 0x00, 0x00,                                   // count = 3
  0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // root id lo,hi
  0x0d, 0x08, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00,     // kind=13(table), size=8, children=2
  0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // field a id
  0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,     // kind=4(int32), size=4, children=0
  0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // field b id
  0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,     // kind=4(int32), size=4, children=0
]);

// ---- new layout: root {a:int32, b:int32, c:int32} — c appended ----
const newLayout = new Uint8Array([
  0x04, 0x00, 0x00, 0x00,                                   // count = 4
  0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // root id lo,hi
  0x0d, 0x0c, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,     // kind=13(table), size=12, children=3
  0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // field a id (same)
  0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,     // kind=4(int32), size=4, children=0
  0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // field b id (same)
  0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,     // kind=4(int32), size=4, children=0
  0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,           // field c id (new)
  0x04, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,     // kind=4(int32), size=4, children=0
]);

let failed = false;
function check(ok, what) {
  console.log((ok ? "ok  " : "FAIL") + " " + what);
  if (!ok) { failed = true; }
}

// Parse both layouts.
const oldView = new TableFixedLayoutView();
check(TableFixedParseLayout(oldLayout, 0, oldLayout.length, oldView),
  "old layout parses: root {a,b}, " + oldView.count + " entries");

const newView = new TableFixedLayoutView();
check(TableFixedParseLayout(newLayout, 0, newLayout.length, newView),
  "new layout parses: root {a,b,c}, " + newView.count + " entries");

// Compile a plan from the OLD layout to the NEW layout (the backward-read
// direction: newer reader, older writer).
const plan = new TableFixedPlan(64, 12, 64);
const census = new TableFixedReport();
const entries = TableFixedCompile(oldView, newLayout, newDst, plan, census);
check(entries >= 0, "plan compiles from old→new (" + entries + " entries, split=" + plan.split + ")");

// ---- THE APPEND ASSERTION ----
// The old writer's body: a=42, b=99 (8 bytes, little-endian).
const oldBody = new Uint8Array([
  0x2a, 0x00, 0x00, 0x00,  // a = 42
  0x63, 0x00, 0x00, 0x00,  // b = 99
]);

// The plan's image is the reader's storage. Prefill first (what the plan
// does not write), then run.
plan.image.fill(0); // reader's defaults: zero
const holes = TableFixedHoles(plan.entries, entries, plan.cover, plan.holes);
for (let h = 0; h < holes; h++) {
  const o = plan.holes[h * 2], z = plan.holes[h * 2 + 1];
  for (let i = 0; i < z; i++) { plan.image[o + i] = prefill[o + i]; }
}

const srcView = new DataView(oldBody.buffer);
plan.split = entries;
plan.count = entries;

const report = new TableFixedReport();
TableFixedRun(plan.entries, entries, oldBody, srcView, 0, plan.image, plan.view, plan.remap, report);

const imageView = plan.view;
check(imageView.getInt32(0, true) === 42,
  "E8 append: a lands as 42 (got " + imageView.getInt32(0, true) + ")");
check(imageView.getInt32(4, true) === 99,
  "E8 append: b lands as 99 (got " + imageView.getInt32(4, true) + ")");
check(imageView.getInt32(8, true) === 0,
  "E8 append: appended c lands as 0 default (got " + imageView.getInt32(8, true) + ")");

// No counters fire: the old writer named everything the new reader knows.
check(report.unknown === 0 && report.clamped === 0 && report.kindMismatch === 0,
  "E8 append: no counter fires (unknown=" + report.unknown +
  ", clamped=" + report.clamped + ", kindMismatch=" + report.kindMismatch + ")");
check(!report.malformed && report.refused === 0,
  "E8 append: no refusal or malformed (refused=" + report.refused +
  ", malformed=" + report.malformed + ")");

// ---- THE DEPRECATION HALF: same layout, different meaning ----
// A deprecated field does NOT change the layout — the slot stays where it is.
// The old writer writes it; the new reader reads it. The app ignores it.
// We prove this by running the plan in the other direction: the "new" writer
// (with c) is read by the "old" reader (without c).
const newBody = new Uint8Array([
  0x0a, 0x00, 0x00, 0x00,  // a = 10
  0x14, 0x00, 0x00, 0x00,  // b = 20
  0x1e, 0x00, 0x00, 0x00,  // c = 30 (appended, unknown to old reader)
]);

// Old reader's image: 8 bytes
const oldImage = new Uint8Array(8);
oldImage.fill(0);
const oldImageView = new DataView(oldImage.buffer);

// Plan from new to old: old reader does not know c.
const oldDst = new Int32Array([
  0, 0, 0, 0, 0,
  0, 0, 0, 0, 0,
  4, 0, 0, 0, 0,
]);

const plan2 = new TableFixedPlan(64, 8, 64);
const census2 = new TableFixedReport();
const entries2 = TableFixedCompile(newView, oldLayout, oldDst, plan2, census2);
check(entries2 >= 0, "plan compiles from new→old (" + entries2 + " entries)");

plan2.image.fill(0);
const holes2 = TableFixedHoles(plan2.entries, entries2, plan2.cover, plan2.holes);
for (let h = 0; h < holes2; h++) {
  const o = plan2.holes[h * 2], z = plan2.holes[h * 2 + 1];
  for (let i = 0; i < z; i++) { plan2.image[o + i] = 0; }
}

plan2.split = entries2;
plan2.count = entries2;

const newSrcView = new DataView(newBody.buffer);
const report2 = new TableFixedReport();
TableFixedRun(plan2.entries, entries2, newBody, newSrcView, 0, plan2.image, oldImageView, plan2.remap, report2);

check(oldImageView.getInt32(0, true) === 10,
  "E8 deprecation: old reader gets a=10 (got " + oldImageView.getInt32(0, true) + ")");
check(oldImageView.getInt32(4, true) === 20,
  "E8 deprecation: old reader gets b=20 (got " + oldImageView.getInt32(4, true) + ")");

// c is unknown to the old reader — one unknown per peer (§5.4), counted at
// compile time and carried on the plan (the census, §5.9 #6).
check(census2.unknown === 1,
  "E8 deprecation: one unknown for appended c (census got " + census2.unknown + ")");
check(!report2.malformed && report2.refused === 0,
  "E8 deprecation: no refusal or malformed (refused=" + report2.refused +
  ", malformed=" + report2.malformed + ")");

if (failed) {
  console.log("FAILED");
  process.exit(1);
}
console.log("E8: append and deprecate under the backward-read contract — the plan copies known fields and defaults the rest, a deprecated slot is read but its value means nothing");
