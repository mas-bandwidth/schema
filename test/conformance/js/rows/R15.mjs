// R15 — the four forward-read clamps are retired (docs/roadmap.sexp R15): the
// count clamp across bounds, the range clamp across versions, the remap of an
// unknown variant to None, the drop-and-count of an unknown field. Each was a
// read of a layout the lineage does not hold, and each is now ONE refusal at
// §5.3 step 5, before any record is touched.
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md §5.3 step 5: "if none: REFUSE
// layout_unknown, reporting h AND NOTHING ELSE"; and "STEP 5's ANSWER IS
// `layout_unknown`, IN EVERY LEG, AND `layout_newer` IS RETIRED" (decided
// 2026-11-03). Ruling R4 (commit fa1d6bdea): a refused read leaves every value
// handed in at its declared default.
//
// Ported from the Go reference row test/conformance/go/rows/R15_test.go, on the
// real generated API: the build's own Cell file, written by CellFixedSave, is
// the positive control (it reads, so the refusal below is not vacuous); then
// the same file with its header hash (and every record's) moved off the
// lineage, its record bodies filled with 0xFF so a count word, a ranged scalar,
// a union tag and an unknown field id would all be past any bound, must refuse
// layout_unknown with n = -1, layoutHash = the file's hash, every counter zero,
// and every poisoned value back at its default.
//
// Run alone from the repo root: node test/conformance/js/rows/R15.mjs

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const rt = await load("v1/Tblv1Table.js");
const v1 = await load("v1/V1Table.js");
const { Cell, TableFixedForm, TableFixedHashAt, TableFixedHeaderBytes, TableFixedLayoutHeaderBytes,
  TableFixedRefusal, TableFixedRefusalName, TableFixedReport } = rt;
const { CellFixedLoad, CellFixedSave, CellFixedMeasure, CellFixedNewPlan, CellFixedLayoutBytes,
  CellFixedRecordBytes, CellFixedHashLo, CellFixedHashHi } = v1;

let failed = 0;
function check(ok, msg) {
  if (ok) { process.stdout.write("PASS " + msg + "\n"); }
  else { failed++; process.stdout.write("FAIL " + msg + "\n"); }
}

// Every refusal must be a name this leg knows: a missing LayoutUnknown would
// make every comparison below undefined === undefined.
if (typeof TableFixedRefusal.LayoutUnknown !== "number") {
  process.stdout.write("FAIL TableFixedRefusal.LayoutUnknown is not defined\n");
  process.exit(1);
}

const COUNT = 2;
const UNKNOWN_HASH = 0xdeadbeefcafe0001n; // in no lineage entry
const OWN_HASH = (BigInt(CellFixedHashHi >>> 0) << 32n) | BigInt(CellFixedHashLo >>> 0);
check(UNKNOWN_HASH !== OWN_HASH, "the planted hash is not the build's own");

function poison(c) {
  c.power = 0x5a5a5a5a;
  c.label.fill(0x7e);
  c.labelLength = 5;
}
function isDefault(c) {
  const fresh = new Cell();
  return c.power === fresh.power && c.labelLength === fresh.labelLength &&
    c.label.length === fresh.label.length && c.label.every((b, i) => b === fresh.label[i]);
}
function countersZero(r) {
  return r.unknown === 0 && r.mismatch === 0 && r.oor === 0 && r.widened === 0 && r.repeated === 0;
}

// ---- THE BUILD'S OWN FILE ----
const written = [];
for (let i = 0; i < COUNT; i++) {
  const c = new Cell();
  c.power = 100 + i;
  c.label.set([0x68, 0x69]); // "hi"
  c.labelLength = 2;
  written.push(c);
}
const need = CellFixedMeasure(COUNT);
const file = new Uint8Array(need);
const wrote = CellFixedSave(written, COUNT, file);
check(wrote === need, "the build's own file writes (" + wrote + " of " + need + " bytes)");
check(file[0] === TableFixedForm, "form byte is TableFixedForm (" + file[0] + ")");

// ---- POSITIVE CONTROL: the build's own file reads ----
{
  const values = [new Cell(), new Cell(), new Cell()];
  const plan = CellFixedNewPlan();
  const report = new TableFixedReport();
  const n = CellFixedLoad(values, values.length, file, file.length, plan, report);
  check(n === COUNT && report.refused === TableFixedRefusal.None,
    "positive control: own hash reads " + COUNT + " records (n=" + n + ", refused=" +
    TableFixedRefusalName(report.refused) + ")");
  check(values[0].power === 100 && values[1].power === 101 && values[1].labelLength === 2,
    "positive control: the values landed (power " + values[0].power + "," + values[1].power + ")");
  check(report.layoutHash === 0n, "positive control: layoutHash is zero on a read that lands");
}

// ---- THE UNKNOWN HASH: header and every record, bodies 0xFF ----
const stranger = file.slice();
const dv = new DataView(stranger.buffer);
const firstRecord = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes + CellFixedLayoutBytes;
dv.setBigUint64(TableFixedHashAt, UNKNOWN_HASH, true);
for (let i = 0; i < COUNT; i++) {
  const at = firstRecord + i * CellFixedRecordBytes;
  dv.setBigUint64(at, UNKNOWN_HASH, true);
  stranger.fill(0xff, at + 8, at + CellFixedRecordBytes);
}
{
  const values = [new Cell(), new Cell(), new Cell()];
  for (const c of values) { poison(c); }
  check(!values.some(isDefault), "the values handed in are poisoned before the read");
  const plan = CellFixedNewPlan();
  const report = new TableFixedReport();
  const n = CellFixedLoad(values, values.length, stranger, stranger.length, plan, report);
  check(n === -1, "unknown hash: n = -1 (got " + n + ")");
  check(report.refused === TableFixedRefusal.LayoutUnknown,
    "unknown hash refuses layout_unknown at step 5 (got " + TableFixedRefusalName(report.refused) + ")");
  check(TableFixedRefusalName(report.refused) === "layout_unknown",
    "the refusal's name is layout_unknown, never layout_newer");
  check(report.layoutHash === UNKNOWN_HASH,
    "layoutHash is the file's hash 0x" + UNKNOWN_HASH.toString(16) + " (got 0x" + report.layoutHash.toString(16) + ")");
  check(countersZero(report) && report.damaged === false,
    "every counter zero, not damaged: no clamp, remap or drop ran (unknown=" + report.unknown +
    " mismatch=" + report.mismatch + " oor=" + report.oor + " widened=" + report.widened +
    " repeated=" + report.repeated + " damaged=" + report.damaged + ")");
  check(values.every(isDefault), "ruling R4: every value handed in holds its declared default after the refusal");
}

if (failed > 0) {
  process.stdout.write("R15: RED, " + failed + " failed\n");
  process.exit(1);
}
process.stdout.write("R15: GREEN\n");
