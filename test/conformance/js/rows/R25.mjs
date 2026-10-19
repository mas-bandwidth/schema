// R25.mjs — test/conformance/js/rows/R25.mjs
//
// plan_too_large when the plan does not fit the caller's capacity
// (docs/FIXED-FORM-ALGORITHM.md:891, §5.9 #45).
//
// THE LAW: when the plan does not fit the caller's capacity, the refusal is
// reported as plan_too_large BY NAME. There are two production checks:
// 1. At V1Table.js:180: if (plan === null) { report.refused = PlanTooLarge; return -1; }
// 2. At V1Table.js:190: if (lane.count > plan.capacity) { report.refused = PlanTooLarge; return -1; }
//
// This test focuses on case 1 (plan === null), which is the clearest case of
// "the plan does not fit the caller's capacity" — the caller has no plan storage at all.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

let passed = 0, failed = 0;
function assert(ok, msg) {
  if (ok) { passed++; process.stdout.write("PASS " + msg + "\n"); }
  else    { failed++; process.stderr.write("FAIL " + msg + "\n"); }
}

// Load the V1 table runtime
const v1 = await load("v1/V1Table.js");
const tbl = await load("v1/Tblv1Table.js");
const {
  CellFixedLoad,
  CellFixedLayout,
  CellFixedLayoutBytes,
  CellFixedRecordBytes,
  CellFixedHashLo,
  CellFixedHashHi,
} = v1;
const {
  TableFixedForm,
  TableFixedHeaderBytes,
  TableFixedHashAt,
  TableFixedLayoutHeaderBytes,
  TableFixedReport,
  TableFixedRefusal,
  TableFixedRefusalName,
} = tbl;

// Build a minimal valid file for Cell table
// File structure: form(1) + reserved(7) + hash(8) + layout_len(4) + layout + record
function buildCellFile() {
  const headerLen = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes;
  const recordBytes = CellFixedRecordBytes;
  const totalLen = headerLen + CellFixedLayoutBytes + recordBytes;
  
  const bytes = new Uint8Array(totalLen);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);
  
  // Form byte
  bytes[0] = TableFixedForm;
  
  // Reserved bytes [1..7] must be zero
  for (let i = 1; i < TableFixedHashAt; i++) bytes[i] = 0;
  
  // Hash in header
  view.setUint32(TableFixedHashAt, CellFixedHashLo, true);
  view.setUint32(TableFixedHashAt + 4, CellFixedHashHi, true);
  
  // Layout length at bytes[16:20]
  view.setUint32(TableFixedHeaderBytes, CellFixedLayoutBytes, true);
  
  // Layout bytes
  bytes.set(CellFixedLayout, TableFixedHeaderBytes + TableFixedLayoutHeaderBytes);
  
  // Record: hash + body (16 bytes for Cell)
  const recAt = headerLen + CellFixedLayoutBytes;
  view.setUint32(recAt, CellFixedHashLo, true);
  view.setUint32(recAt + 4, CellFixedHashHi, true);
  // Body: power(4) + label(8) = 12 bytes, but record is 24 total (hash 8 + body 16)
  for (let i = recAt + 8; i < recAt + recordBytes; i++) {
    bytes[i] = 0;
  }
  
  return bytes;
}

// Test: plan_too_large when plan is null
// This is the check at V1Table.js:180: if (plan === null) { report.refused = PlanTooLarge; return -1; }
{
  const bytes = buildCellFile();
  const report = new TableFixedReport();
  
  // Pass null as the plan parameter - the caller has no plan storage
  const values = [{}];
  const result = CellFixedLoad(values, 1, bytes, bytes.length, null, report);
  
  // EXPECTED: result === -1, report.refused === PlanTooLarge (13)
  assert(result === -1, "plan_too_large (null plan): result = -1");
  assert(report.refused === TableFixedRefusal.PlanTooLarge,
    "plan_too_large (null plan): report.refused = PlanTooLarge, got " + TableFixedRefusalName(report.refused));
  assert(!report.malformed, "plan_too_large (null plan): report.malformed = false");
}

// Test: with a valid plan, it should NOT refuse with plan_too_large
{
  const bytes = buildCellFile();
  const report = new TableFixedReport();
  
  const plan = {
    capacity: 1,
    image: new Uint8Array(256),
    view: new DataView(new ArrayBuffer(256)),
    cover: new Uint8Array(256),
    holes: [],
  };
  
  const values = [{ Power: 0, LabelLength: 0, Label: new Uint8Array(8) }];
  const result = CellFixedLoad(values, 1, bytes, bytes.length, plan, report);
  
  // Should succeed (return 1 record)
  assert(result === 1, "plan_valid: result = 1 (one record loaded)");
  assert(report.refused === 0, "plan_valid: report.refused = 0 (no refusal)");
}

// ---- done ---
if (failed > 0) {
  process.stderr.write(failed + " FAILED\n");
  process.exit(1);
}
process.stdout.write(passed + " passed\n");
process.exit(0);
