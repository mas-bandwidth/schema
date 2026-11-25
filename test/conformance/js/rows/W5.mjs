// test/conformance/js/rows/W5.mjs — js/W5 "the write-side bound check, per leg"
//
// THE LAW AS IT STANDS (docs/FIXED-FORM-ALGORITHM.md, line ~249, and SPEC.md
// §5.1's per-leg table; docs/rulings.json `write-refusal`): "Write-side bound
// checks use each language's own idiom, and that decides the build: a
// debug-only assert in C, C++, C#, Rust, Java and Dart, and a REFUSAL IN EVERY
// BUILD in Go, JavaScript, Elixir, Lua, Luau, Squirrel, MOO and Goose." The
// check is over the LIVE extent and runs over the whole batch before the first
// byte is stored, so a refused batch leaves the buffer untouched; it clamps
// nothing. The READ side checks the same number in every build: a length past
// its bound refuses `length_over_bound` (§4.5).
//
// This row was written when the law said write checks were DEBUG ONLY in every
// leg, and asserted that the JS writer (one release artifact) stored a
// contract-violating length as given and that the reader clamped it. Both
// halves are retired; the row keeps its vector and asserts the law as it
// stands for this leg: the writer REFUSES, and the reader REFUSES.
//
// THE VECTOR (fixed form 3, little-endian; PaddedRowFixedDst states the same
// numbers). PaddedRow body = 50 bytes: 0 tag uint8, 1 value float64, 9 flag
// bool, 10 id uint32, 14 label length int32 — string(15), so N = 15 —,
// 18 label buffer (15 bytes), 33 slots [4]uint16, 41 teams [Team]uint8,
// 45 counter present flag, 46 counter int32.
//
// Run from the repository root:  node test/conformance/js/rows/W5.mjs

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const runtime = await load("block/BlockdemoTable.js");
const tables = await load("block/PaddedTable.js");

const { PaddedRow, InitPaddedRow, TableFixedReport, TableFixedHeaderBytes,
  TableFixedLayoutHeaderBytes, TableFixedRefusal, TableFixedRefusalName } = runtime;
const { PaddedRowFixedSave, PaddedRowFixedMeasure, PaddedRowFixedLoad,
  PaddedRowFixedNewPlan, PaddedRowFixedLayoutBytes } = tables;

let failures = 0;
function assert(condition, msg) {
  if (!condition) { console.log(`FAIL: ${msg}`); failures++; }
  else { console.log(`PASS: ${msg}`); }
}

function row(labelLength) {
  const v = new PaddedRow();
  v.tag = 0x33;
  v.value = 4.5;
  v.flag = true;
  v.id = 0xCAFEBABE;
  for (let i = 0; i < 15; i++) { v.label[i] = 0x41 + i; } // "ABCDEFGHIJKLMNO"
  v.labelLength = labelLength;
  v.slots = [1, 2, 3, 4];
  v.teams.set([1, 2, 3, 0], 0);
  v.counterPresent = true;
  v.counter = -12345;
  return v;
}

const POISON = 0x5a;
const body = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes + PaddedRowFixedLayoutBytes + 8;

// ---- THE WRITE SIDE: JavaScript refuses in every build ----
for (const bad of [16, 20, -1]) {
  const file = new Uint8Array(PaddedRowFixedMeasure(1)).fill(POISON);
  let got, threw = null;
  try { got = PaddedRowFixedSave([row(bad)], 1, file); } catch (e) { threw = e; }
  assert(threw === null && got === -1,
    `the writer REFUSES a label length of ${bad} against string(15) in every build: -1, never a throw (got ${threw === null ? got : threw})`);
  assert(file.every((b) => b === POISON),
    `a refused write (length ${bad}) leaves the buffer untouched — the check runs before the first byte is stored`);
}

{
  // A REFUSED BATCH IS REFUSED WHOLE: a lawful record ahead of the bad one is
  // not stored either.
  const file = new Uint8Array(PaddedRowFixedMeasure(2)).fill(POISON);
  const got = PaddedRowFixedSave([row(5), row(20)], 2, file);
  assert(got === -1 && file.every((b) => b === POISON),
    "the check covers the whole batch before the first store: a lawful record ahead of the bad one is not written");
}

// ---- THE BOUND ITSELF IS LAWFUL: 15 saves, lands as given, and reads ----
const lawful = new Uint8Array(PaddedRowFixedMeasure(1));
assert(PaddedRowFixedSave([row(15)], 1, lawful) === lawful.length,
  "a label length AT the bound 15 saves whole");
const dv = new DataView(lawful.buffer, lawful.byteOffset, lawful.length);
assert(dv.getInt32(body + 14, true) === 15, "the length 15 lands on the wire as given — the writer clamps nothing");
{
  const back = [new PaddedRow()];
  const report = new TableFixedReport();
  const n = PaddedRowFixedLoad(back, 1, lawful, lawful.length, PaddedRowFixedNewPlan(), report);
  let live = true;
  for (let i = 0; i < 15; i++) { if (back[0].label[i] !== 0x41 + i) { live = false; } }
  assert(n === 1 && report.refused === 0 && !("damaged" in report) && back[0].labelLength === 15 && live && back[0].slots[3] === 4,
    `the lawful record reads back whole (n=${n}, length ${back[0].labelLength})`);
}

// ---- THE READ SIDE: the same number, checked in every build, REFUSED ----
// The forged length cannot come from this writer any more, so it is planted
// into the lawful record the writer produced.
{
  const forged = new Uint8Array(lawful);
  new DataView(forged.buffer).setInt32(body + 14, 20, true);
  const back = [new PaddedRow()];
  back[0].labelLength = 7; back[0].id = 1; back[0].label.fill(POISON);
  const report = new TableFixedReport();
  const n = PaddedRowFixedLoad(back, 1, forged, forged.length, PaddedRowFixedNewPlan(), report);
  assert(n === -1 && report.refused === TableFixedRefusal.LengthOverBound && !("damaged" in report),
    `the read side refuses a wire length of 20 against string(15) length_over_bound, never a clamp (n=${n}, refused=${TableFixedRefusalName(report.refused)})`);
  assert(report.oor === 0 && report.widened === 0, "the read-side refusal counts nothing");
  const fresh = new PaddedRow();
  InitPaddedRow(fresh);
  assert(back[0].labelLength === fresh.labelLength && back[0].id === fresh.id &&
    back[0].label.every((b, i) => b === fresh.label[i]),
    "the refused read hands the row back at its declared defaults (ruling R4)");
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
