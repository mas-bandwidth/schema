// test/conformance/js/rows/W5.mjs — js/W5 "write checks DEBUG only"
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:208-210): "**Write-side bound checks
// are DEBUG ONLY, by rule.** `count <= Max` and `length <= N` are a caller
// contract; a release build removes them exactly as it removes `assert`,
// pays nothing, and **clamps nothing**. The READ side keeps the wire safe: it
// checks the same number in every build and counts the clamp."
//
// THE JS FACT: this backend emits ONE artifact, the release one — there is
// no debug build to carry the checks — so the law's demand on the emitted
// writer is exactly its release clause: NO check (the write refuses nothing
// and throws nothing on a contract-violating length) and NO clamp (the
// caller's length lands on the wire exactly as given, not the declared
// bound). The contrast that makes the rule visible is the read side, which
// checks the same number in every build: it clamps the length back to the
// declared bound and counts the clamp.
//
// DERIVATION OF THE WIRE LAYOUT (fixed form 3, little-endian; the generated
// PaddedRowFixedDst rows state the same numbers). PaddedRow body = 50 bytes:
//     0 tag uint8, 1 value float64, 9 flag bool, 10 id uint32,
//    14 label length int32 — string(15), so N = 15 —,
//    18 label buffer (15 bytes, 18..33), 33 slots [4]uint16 (33..41),
//    41 teams [Team]uint8 (41..45), 45 counter present flag, 46 counter
//    int32 (46..50).
// The vector is written by the generated Save itself: one PaddedRow whose
// LabelLength is 20 — five past the string(15) bound — with the fifteen
// storage bytes the bound names filled with distinct values. A DEBUG build
// would refuse or clamp that; the release artifact must do neither.
//
// Run from the repository root:  node test/conformance/js/rows/W5.mjs
// Green prints one line per assertion and exits 0; any red prints FAIL and
// exits 1.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs), so the harness's negative
// controls can aim this test at a sabotaged copy too.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const runtime = await load("block/BlockdemoTable.js");
const tables = await load("block/PaddedTable.js");

const { PaddedRow, TableFixedReport, TableFixedResetReport, TableFixedHeaderBytes,
  TableFixedLayoutHeaderBytes } = runtime;
const { PaddedRowFixedSave, PaddedRowFixedMeasure, PaddedRowFixedLoad,
  PaddedRowFixedNewPlan, PaddedRowFixedLayoutBytes } = tables;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log(`FAIL: ${msg}`);
    failures++;
  } else {
    console.log(`PASS: ${msg}`);
  }
}

// ---- the value: a caller contract violation, five units past N = 15
const v = new PaddedRow();
v.Tag = 0x33;
v.Value = 4.5;
v.Flag = true;
v.Id = 0xCAFEBABE;
for (let i = 0; i < 15; i++) { v.Label[i] = 0x41 + i; } // "ABCDEFGHIJKLMNO"
v.LabelLength = 20; // past the string(15) bound: the caller's contract, not the writer's
v.Slots = [1, 2, 3, 4];
v.Teams.set([9, 8, 7, 6], 0);
v.CounterPresent = true;
v.Counter = -12345;

const file = new Uint8Array(PaddedRowFixedMeasure(1));

// ---- THE LAW, write side: DEBUG-only checks are absent from the release
// artifact — the writer refuses nothing and clamps nothing.
assert(PaddedRowFixedSave([v], 1, file) === file.length,
  "no write-side check: the writer answers its measure on a contract-violating length, refusing nothing and throwing nothing");

const dv = new DataView(file.buffer, file.byteOffset, file.length);
const recordAt = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes + PaddedRowFixedLayoutBytes;
const body = recordAt + 8; // past the record's 8-byte hash

assert(dv.getInt32(body + 14, true) === 20,
  "no write-side clamp: the caller's length 20 lands on the wire exactly as given, not the declared bound 15");

let landed = true;
for (let i = 0; i < 15; i++) { if (file[body + 18 + i] !== 0x41 + i) { landed = false; } }
assert(landed,
  "the live units the length names landed, and the write kept going past the bound (no check aborted anything)");
assert(dv.getUint16(body + 33, true) === 1 && dv.getUint16(body + 35, true) === 2 &&
  dv.getUint16(body + 37, true) === 3 && dv.getUint16(body + 39, true) === 4,
  "the fields after the violating length landed too — the release write is a straight line of stores, not a gated one");

// ---- THE LAW, read side: the same number, checked in every build, clamped
// and counted — the wire stays safe where the writer paid nothing.
const plan = PaddedRowFixedNewPlan(64, 64);
const back = [new PaddedRow()];
const report = new TableFixedReport();
TableFixedResetReport(report);
const n = PaddedRowFixedLoad(back, 1, file, file.length, plan, report);
assert(n === 1 && !report.malformed && report.refused === 0,
  "the record still reads: a caller contract violation is not a refusal");
assert(back[0].LabelLength === 15 && report.clamped === 1,
  "the read side checks the same number in every build: it clamps 20 to the declared bound 15 and counts exactly one clamp");
let live = true;
for (let i = 0; i < 15; i++) { if (back[0].Label[i] !== 0x41 + i) { live = false; } }
assert(live,
  "the clamped read keeps the live units the wire carries");

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
