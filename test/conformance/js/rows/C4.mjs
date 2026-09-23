// row js/C4 — "text content refuses BY NAME" (docs/roadmap.sexp js/C4;
// docs/FIXED-FORM-ALGORITHM.md:333, the 'text' row of §4.5's scatter).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:333:
//   "The CONTENT RULES apply to the USED UNITS and nothing else: UTF-8
//    validity over v bytes, never over N. A content violation REFUSES BY
//    NAME, the verdict the packet reader gives, this form having no L to
//    continue past (fix 11)."
//
// The JS leg's fixed-form reader (Tblv2Table.js CellFixedDecode, and
// TabledemoTable.js's TableFixedRun over TableFixedOpText) copies the text
// bytes but does NOT validate UTF-8 content — no TextDecoder, no isWellFormed,
// no byte-by-byte check. The law says a content violation must set
// report.refused to the named refusal and leave report.malformed false, with
// nothing decoded and no counter moving.
//
// THE TEST: forge a Cell record with a length of 3 and three bytes that are
// NOT valid UTF-8 (0xFF 0x00 0x00). Run CellFixedDecode and assert that
// report.refused is non-zero AND report.malformed is false.
//
// If the JS leg implements this law, the test goes GREEN; if it does not
// (as is the case on the tip), the test is a committed RED test and the
// item is recorded as RED in RESULT.md.
//
// WIRE VECTOR DERIVATION (Cell record body, 16 bytes):
//   offset  0: power     int32 = 42
//   offset  4: length    int32 = 3
//   offset  8: label[0]  = 0xFF (invalid UTF-8 lead byte)
//   offset  9: label[1]  = 0x00
//   offset 10: label[2]  = 0x00
//   offset 11..15: slack  = 0

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const tbl = await import(pathToFileURL(join(generated, "v2/Tblv2Table.js")).href);

const { Cell, InitCell, CellFixedDecode, TableFixedReport, TableFixedRefusal } = tbl;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log("FAIL: " + msg);
    failures++;
  } else {
    console.log("PASS: " + msg);
  }
}

// Build a 16-byte body for Cell with invalid UTF-8 in the label.
// 0xFF is never a valid UTF-8 byte, so this is guaranteed ill-formed.
function makeCellBodyWithBadUtf8() {
  const buf = new ArrayBuffer(16);
  const view = new DataView(buf);
  view.setInt32(0, 42, true);       // power
  view.setInt32(4, 3, true);        // length = 3 used units
  view.setUint8(8, 0xFF);           // invalid UTF-8 lead byte
  view.setUint8(9, 0x00);
  view.setUint8(10, 0x00);
  view.setUint8(11, 0x00);          // slack
  view.setUint8(12, 0x00);
  view.setUint8(13, 0x00);
  view.setUint8(14, 0x00);
  view.setUint8(15, 0x00);
  return new DataView(buf);
}

// ---- THE LAW: ill-formed UTF-8 in used units → refuse by name ----

{
  const cell = new Cell();
  InitCell(cell);
  const report = new TableFixedReport();
  CellFixedDecode(cell, makeCellBodyWithBadUtf8(), 0, report);

  // The law says: refused != 0, malformed == false, nothing decoded.
  // If the leg implements this, report.refused will be non-zero.
  // If it does not (current state), the label bytes will be copied anyway.
  assert(report.refused !== 0, "report.refused is non-zero for ill-formed UTF-8 (got " + report.refused + ")");
  assert(report.malformed === false, "report.malformed is false (got " + report.malformed + ")");
  assert(report.clamped === 0, "report.clamped is 0 — a refusal moves no counters (got " + report.clamped + ")");
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
