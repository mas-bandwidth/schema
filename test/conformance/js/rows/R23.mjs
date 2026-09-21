// R23 — the static data's member names and order (§5.9 #19, #15):
// TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes;
// the report's layout_hash last and zero on every other path.
//
// Standalone test: node test/conformance/js/rows/R23.mjs
// Exits 0 (green) on pass, 1 (red) on failure.
// Imports the generated JS runtime the same way the conformance driver does.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const runtime = await load("v1/Tblv1Table.js");

let ok = 0;
let total = 0;
function check(condition, msg) {
  total++;
  if (condition) { ok++; }
  else { process.stderr.write("RED: " + msg + "\n"); }
}

// ---- §5.9 #19: TableFixedKnownLayout members and order ----
// JS spelling: hash splits into two uint32 lanes (hashLo, hashHi). The
// semantic order from the spec is: hash, layout, layout_bytes, record_bytes.
const ctor = runtime.TableFixedKnownLayout.toString();

check(typeof runtime.TableFixedKnownLayout === "function",
      "TableFixedKnownLayout is exported");

// Verify constructor param order matches spec: hash → hashLo,hashHi;
// layout, layout_bytes, record_bytes
check(ctor.includes("constructor(hashLo, hashHi, layout, layoutBytes, recordBytes)"),
      "constructor param order: hashLo, hashHi, layout, layoutBytes, recordBytes");

// Instance property order from constructor assignments
const inst = new runtime.TableFixedKnownLayout(0x12345678, 0x9abcdef0,
    new Uint8Array([1,2,3]), 3, 100);
const keys = Object.keys(inst);
check(keys.length === 5, "TableFixedKnownLayout has 5 own members");
check(keys[0] === "hashLo", "member 0 is hashLo");
check(keys[1] === "hashHi", "member 1 is hashHi");
check(keys[2] === "layout", "member 2 is layout");
check(keys[3] === "layoutBytes", "member 3 is layoutBytes");
check(keys[4] === "recordBytes", "member 4 is recordBytes");

check((inst.hashLo >>> 0) === 0x12345678, "hashLo assigned correctly");
check((inst.hashHi >>> 0) === 0x9abcdef0, "hashHi assigned correctly");
check(inst.layout instanceof Uint8Array && inst.layout.length === 3,
      "layout assigned correctly");
check(inst.layoutBytes === 3, "layoutBytes assigned correctly");
check(inst.recordBytes === 100, "recordBytes assigned correctly");

// Check the generated FixedKnown arrays use this exact class
// (existing generated constant in the module)
const v1Table = await load("v1/V1Table.js");
for (const key of Object.keys(v1Table)) {
  if (key.endsWith("FixedKnown")) {
    const arr = v1Table[key];
    check(Array.isArray(arr) && arr.length > 0,
          key + " is a non-empty array of TableFixedKnownLayout");
    for (let i = 0; i < arr.length; i++) {
      const e = arr[i];
      check(e instanceof runtime.TableFixedKnownLayout,
            key + "[" + i + "] is a TableFixedKnownLayout instance");
      check(typeof e.hashLo === "number", key + "[" + i + "].hashLo is number");
      check(typeof e.hashHi === "number", key + "[" + i + "].hashHi is number");
      check(e.layout instanceof Uint8Array,
            key + "[" + i + "].layout is Uint8Array");
      check(typeof e.layoutBytes === "number",
            key + "[" + i + "].layoutBytes is number");
      check(typeof e.recordBytes === "number",
            key + "[" + i + "].recordBytes is number");
    }
  }
}

// ---- §5.9 #15: layout_hash last on the report, zero on every other path ----
const report = new runtime.TableFixedReport();
const reportKeys = Object.keys(report);
check(report.layoutHash === 0n,
      "layoutHash starts as 0n");
check(reportKeys[reportKeys.length - 1] === "layoutHash",
      "layoutHash is the last field on TableFixedReport");

runtime.TableFixedResetReport(report);
check(report.layoutHash === 0n,
      "layoutHash is 0n after TableFixedResetReport");

runtime.TableFixedRefuseHash(report, 1, 0xdeadbeef, 0xcafef00d);
check(report.layoutHash === ((BigInt(0xcafef00d) << 32n) |
      BigInt(0xdeadbeef >>> 0)),
      "layoutHash set to the file's hash by TableFixedRefuseHash");

runtime.TableFixedResetReport(report);
check(report.layoutHash === 0n,
      "layoutHash back to 0n after second reset — zero on every non-refusal path");

if (total !== ok) {
  process.stderr.write("FAILED " + (total - ok) + " of " + total + "\n");
  process.exit(1);
}
process.stdout.write("R23: " + ok + " assertions passed\n");