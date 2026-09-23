// row js/C3 — "text length clamp" (docs/roadmap.sexp js/C3;
// docs/FIXED-FORM-ALGORITHM.md:333, the 'text' row of §4.5's scatter).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:333:
//   "v := SLE(4, record+src) clamped into [0, cap], COUNT clamped if it fired."
//
// The JS leg's fixed-form reader lands text length in the Cell type
// (V2.schema, tblv2.Cell: `label string(8)`). CellFixedDecode
// (build/tables-generated-js/v2/Tblv2Table.js:1384) reads a little-endian
// int32 length at offset 4 of the body and clamps it into [0, 8],
// incrementing report.clamped when the raw value is negative or past the cap.
//
// THE TEST: forge a record with a length field of 20 (past cap 8) and one
// with -1 (raw < 0), run CellFixedDecode, and assert that the length lands
// at the cap/zero respectively and that report.clamped is exactly 1 for each.
//
// WIRE VECTOR DERIVATION (Cell record, form-3 body, 16 bytes):
//   offset  0: power     int32 = 42
//   offset  4: length    int32 = forged value (20 or -1)
//   offset  8: label[0..7] = 8 bytes of 'A'
// The record body is 16 bytes; a full form-3 record prefixes an 8-byte hash,
// but CellFixedDecode works on the body portion alone (it receives view + at
// pointing at offset 8 of the full record).

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const mod = await import(pathToFileURL(join(generated, "v2/V2Table.js")).href);
const tbl = await import(pathToFileURL(join(generated, "v2/Tblv2Table.js")).href);

const { Cell, InitCell, CellFixedDecode } = tbl;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log("FAIL: " + msg);
    failures++;
  } else {
    console.log("PASS: " + msg);
  }
}

// Build a 16-byte body for Cell with a given length value and a given label byte.
function makeCellBody(lengthValue, labelByte) {
  const buf = new ArrayBuffer(16);
  const view = new DataView(buf);
  view.setInt32(0, 42, true);        // power
  view.setInt32(4, lengthValue, true); // length (forged)
  for (let i = 0; i < 8; i++) {
    view.setUint8(8 + i, labelByte);
  }
  return new DataView(buf);
}

// ---- THE LAW: text length clamped to cap, clamped incremented ----

// Case 1: length = 20, cap = 8 → must clamp to 8, clamped == 1
{
  const cell = new Cell();
  InitCell(cell);
  const report = new tbl.TableFixedReport();
  CellFixedDecode(cell, makeCellBody(20, 0x41), 0, report);
  assert(cell.LabelLength === 8, "length 20 clamps to cap 8 (got " + cell.LabelLength + ")");
  assert(report.clamped === 1, "report.clamped is exactly 1 (got " + report.clamped + ")");
}

// Case 2: length = -1 → must clamp to 0, clamped == 1
{
  const cell = new Cell();
  InitCell(cell);
  const report = new tbl.TableFixedReport();
  CellFixedDecode(cell, makeCellBody(-1, 0x42), 0, report);
  assert(cell.LabelLength === 0, "length -1 clamps to 0 (got " + cell.LabelLength + ")");
  assert(report.clamped === 1, "report.clamped is exactly 1 (got " + report.clamped + ")");
}

// Case 3: length = 5 (within [0, 8]) → must NOT clamp, clamped == 0
{
  const cell = new Cell();
  InitCell(cell);
  const report = new tbl.TableFixedReport();
  CellFixedDecode(cell, makeCellBody(5, 0x43), 0, report);
  assert(cell.LabelLength === 5, "length 5 is accepted as-is (got " + cell.LabelLength + ")");
  assert(report.clamped === 0, "report.clamped is 0 (got " + report.clamped + ")");
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
