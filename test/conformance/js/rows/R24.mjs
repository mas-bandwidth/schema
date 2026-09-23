// row js/R24 — "ill-formed text in the USED units refuses by name
// (text_ill_formed); text is counted in bytes with the clamp in units;
// slack is unspecified on read and not a refusal"
// (docs/roadmap.sexp js/R24; docs/FIXED-FORM-ALGORITHM.md:1682).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:897 and :333:
//   "ill-formed text in the USED units | the name fix 11 owes (§4.5) |
//    nothing decoded past it; the reference sets `malformed` instead"
//   "The CONTENT RULES apply to the USED UNITS and nothing else: UTF-8
//    validity over v bytes, never over N; wide code units over v, never 2N,
//    an astral pair counting two (fix 7). A content violation REFUSES BY
//    NAME, the verdict the packet reader gives, this form having no L to
//    continue past (fix 11)."
//
// This row has THREE sub-claims:
//   A. ill-formed text in the USED units refuses by name (text_ill_formed)
//   B. text is counted in bytes with the clamp in units
//   C. slack is unspecified on read and not a refusal
//
// The JS leg does NOT implement sub-claim A: the text op (Tblv2Table.js:1389,
// TabledemoTable.js:362-377) copies bytes without UTF-8 validation.
//
// The JS leg DOES implement sub-claim B: the length field is clamped into
// [0, cap] where cap = size / unit (unit=1 for UTF-8), and clamped is counted
// (see C3.mjs for the proof).
//
// The JS leg DOES implement sub-claim C: the decode loop reads all `size`
// bytes (the declared field size, not the used length) — Tblv2Table.js:1392
// reads 8 bytes regardless of LabelLength, so slack bytes are copied but not
// validated (they are "unspecified" and not a refusal).
//
// THE TEST:
//   - Sub-claim A: forge ill-formed UTF-8 in used units → expect refuse by name.
//     JS does NOT implement this, so this assertion will fail (RED).
//   - Sub-claim B: forge a length past cap → expect clamped. Already proven by C3.
//   - Sub-claim C: forge varying slack bytes → expect no refusal, they are copied
//     as-is regardless of used length.

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

// Build a 16-byte body for Cell with specified length, label bytes, and slack.
function makeCellBody(lengthValue, labelBytes) {
  const buf = new ArrayBuffer(16);
  const view = new DataView(buf);
  view.setInt32(0, 42, true);
  view.setInt32(4, lengthValue, true);
  const arr = new Uint8Array(buf);
  for (let i = 0; i < labelBytes.length && i < 8; i++) {
    arr[8 + i] = labelBytes[i];
  }
  return new DataView(buf);
}

// ---- Sub-claim A: ill-formed text in USED units refuses by name ----
// 0xFF is never valid UTF-8. If the leg implements this law, report.refused != 0.
{
  const cell = new Cell();
  InitCell(cell);
  const report = new TableFixedReport();
  CellFixedDecode(cell, makeCellBody(3, [0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00]), 0, report);
  assert(report.refused !== 0, "ill-formed UTF-8 in used units → report.refused != 0 (got " + report.refused + ")");
  assert(report.malformed === false, "ill-formed UTF-8 → report.malformed is false (got " + report.malformed + ")");
  assert(report.clamped === 0, "a refusal moves no counters, clamped == 0 (got " + report.clamped + ")");
}

// ---- Sub-claim B: text counted in bytes with clamp in units ----
// Already proven by C3, but repeat one assertion here for completeness.
{
  const cell = new Cell();
  InitCell(cell);
  const report = new TableFixedReport();
  CellFixedDecode(cell, makeCellBody(20, [0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41]), 0, report);
  assert(cell.LabelLength === 8, "length 20 clamps to cap 8 (got " + cell.LabelLength + ")");
  assert(report.clamped === 1, "clamped == 1 for length past cap (got " + report.clamped + ")");
}

// ---- Sub-claim C: slack is unspecified on read and not a refusal ----
// The field size is 8 bytes. If used = 3, bytes 3..7 are slack.
// They should be copied without causing any refusal or clamp.
{
  const cell = new Cell();
  InitCell(cell);
  const report = new TableFixedReport();
  // used = 3, slack bytes are 0xDE 0xAD 0xBE 0xEF 0xCA (arbitrary values)
  CellFixedDecode(cell, makeCellBody(3, [0x41, 0x42, 0x43, 0xDE, 0xAD, 0xBE, 0xEF, 0xCA]), 0, report);
  assert(cell.LabelLength === 3, "used length = 3 (got " + cell.LabelLength + ")");
  // Slack bytes are copied: the storage is always 8 bytes, and the decoder
  // reads all 8 bytes regardless of used length.
  assert(cell.Label[3] === 0xDE, "slack byte at index 3 = 0xDE (got 0x" + cell.Label[3].toString(16) + ")");
  assert(cell.Label[7] === 0xCA, "slack byte at index 7 = 0xCA (got 0x" + cell.Label[7].toString(16) + ")");
  assert(report.refused === 0, "slack is not a refusal (refused=" + report.refused + ")");
  assert(report.malformed === false, "slack is not malformed (malformed=" + report.malformed + ")");
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
