// R15.mjs — test/conformance/js/rows/R15.mjs
//
// The four forward-read clamps are retired:
//   - count clamp across bounds
//   - range clamp across versions
//   - remap of an unknown variant to None
//   - drop-and-count of an unknown field
//
// Each is layout_newer now. This test verifies that an unknown layout
// refuses with LayoutNewer and does NOT perform forward reads.
//
// SPEC: docs/FIXED-FORM-ALGORITHM.md:967-973
//
// The test:
// 1. Constructs a minimal valid form-1 header with an unknown hash
// 2. Calls CellFixedLoad with that buffer
// 3. Verifies report.refused === TableFixedRefusal.LayoutNewer (15)

import { CellFixedLoad } from "../../../../build/tables-generated-js/v1/V1Table.js";
import { TableFixedResetReport } from "../../../../build/tables-generated-js/v1/Tblv1Table.js";
import { CellFixedLayout, CellFixedLayoutBytes } from "../../../../build/tables-generated-js/v1/V1Table.js";

// Constants for building a minimal form-1 file
const FORM_1 = 3;               // TableFixedForm (not TableFixedVariableForm=1)
const HEADER_BYTES = 10;        // Form byte + hash (8) + reserved (1)
const RESERVE_BYTES = 1;        // bytes[1..8] must be zero

// The report structure expected by CellFixedLoad
function newReport() {
  return {
    malformed: false,
    refused: 0,
    unknown: 0,
    kindMismatch: 0,
    layoutHash: 0n,
  };
}

// Build a minimal form-1 file with an UNKNOWN hash (0x12345678, 0xABCDEF01)
// SPEC: TableFixedHeaderBytes=16, TableFixedHashAt=8, TableFixedLayoutHeaderBytes=4
function buildUnknownLayoutFile() {
  // CellFixedLayoutBytes = 55 for V1 Cfg table
  const layoutLen = CellFixedLayoutBytes;
  // HEADER_BYTES=10 includes form(1)+hash(8)+reserved(1) = 10 bytes
  // But TableFixedHeaderBytes=16, so we need to account for that
  // Header: [0]form, [1..7]hash_hi_part? no... let me check
  // TableFixedHashAt=8 means hashLo at bytes[8:12], hashHi at bytes[12:16]
  // Form byte at bytes[0], reserved bytes[1..7] must be zero
  // Layout length at bytes[16:20] (little-endian uint32)
  // Layout starts at bytes[20]
  
  const totalLen = 16 + 4 + layoutLen + 8;  // header(16) + layout_header(4) + layout + record_header(8)

  const bytes = new Uint8Array(totalLen);

  // Form byte
  bytes[0] = FORM_1;

  // Reserved bytes [1..7] must be zero
  for (let i = 1; i < 8; i++) bytes[i] = 0;

  // Hash in header (UNKNOWN: 0x12345678, 0xABCDEF01)
  const headerHashLo = 0x12345678;
  const headerHashHi = 0xABCDEF01;
  bytes[8] = headerHashLo & 0xff;
  bytes[9] = (headerHashLo >> 8) & 0xff;
  bytes[10] = (headerHashLo >> 16) & 0xff;
  bytes[11] = (headerHashLo >> 24) & 0xff;
  bytes[12] = headerHashHi & 0xff;
  bytes[13] = (headerHashHi >> 8) & 0xff;
  bytes[14] = (headerHashHi >> 16) & 0xff;
  bytes[15] = (headerHashHi >> 24) & 0xff;

  // Layout length at bytes[16:20] (little-endian uint32)
  bytes[16] = layoutLen & 0xff;
  bytes[17] = (layoutLen >> 8) & 0xff;
  bytes[18] = (layoutLen >> 16) & 0xff;
  bytes[19] = (layoutLen >> 24) & 0xff;

  // Layout bytes (copy from CellFixedLayout)
  for (let i = 0; i < layoutLen; i++) {
    bytes[20 + i] = CellFixedLayout[i];
  }

  // Record header at the end (unknown hash again)
  const recAt = 20 + layoutLen;
  bytes[recAt] = headerHashLo & 0xff;
  bytes[recAt + 1] = (headerHashLo >> 8) & 0xff;
  bytes[recAt + 2] = (headerHashLo >> 16) & 0xff;
  bytes[recAt + 3] = (headerHashLo >> 24) & 0xff;
  bytes[recAt + 4] = headerHashHi & 0xff;
  bytes[recAt + 5] = (headerHashHi >> 8) & 0xff;
  bytes[recAt + 6] = (headerHashHi >> 16) & 0xff;
  bytes[recAt + 7] = (headerHashHi >> 24) & 0xff;

  return bytes;
}

// Test: unknown layout refuses with LayoutNewer
function testUnknownLayoutLayoutNewer() {
  const bytes = buildUnknownLayoutFile();
  const report = newReport();

  const plan = {
    capacity: 10,
    image: new Uint8Array(256),
    view: new DataView(new ArrayBuffer(256)),
    cover: 0,
    holes: [],
  };

  const result = CellFixedLoad(plan.image, plan.capacity, bytes, bytes.length, plan, report);

  // EXPECTED: result === -1, report.refused === LayoutNewer (15)
  // The forward reads are RETIRED — an unknown layout must refuse immediately
  // with layout_newer, not clamp counts or ranges or remap unknowns.
  if (result !== -1) {
    console.log("FAIL: result = " + result + ", expected -1");
    return false;
  }
  if (report.refused !== 15) {
    console.log("FAIL: report.refused = " + report.refused + ", expected 15 (LayoutNewer)");
    return false;
  }
  if (report.malformed) {
    console.log("FAIL: report.malformed = true, expected false");
    return false;
  }
  if (report.unknown !== 0) {
    console.log("FAIL: report.unknown = " + report.unknown + ", expected 0");
    return false;
  }
  if (report.kindMismatch !== 0) {
    console.log("FAIL: report.kindMismatch = " + report.kindMismatch + ", expected 0");
    return false;
  }

  console.log("PASS: unknown layout refuses with LayoutNewer");
  return true;
}

// Entry point
const passed = testUnknownLayoutLayoutNewer();
process.exit(passed ? 0 : 1);
