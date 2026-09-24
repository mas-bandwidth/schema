// THE UNION TAG PAST ARMS → NONE ASSERTION (SPEC §3.4, FIXED-FORM-ALGORITHM.md §4.6)
//
// The Effect union in WeaponConfig has two arms: Buff(1), Debuff(2).
// Tag 0 = None, tags > 2 are past the arm count and MUST land as None (0)
// with report.clamped incremented by exactly 1.
//
// DERIVATION OF THE WIRE VECTOR:
//   WeaponConfig body layout (fixed-form, little-endian):
//     offset 0: damage      float32 (4 bytes)
//     offset 4: speed       float32 (4 bytes)
//     offset 8: penetration int32   (4 bytes)
//     offset 12: channel    bits(6) stored as uint32 (4 bytes)
//     offset 16: homing     bool (1 byte)
//     offset 17: Effect.Type tag (1 byte) — FORGED to 9 here, past arm count 2
//     offset 18: payload    float32 (4 bytes, Buff.Debuff.Multiplier or Debuff.Amount)
//   Total body size: 22 bytes. No length/count/terminator — fixed form is positional only.
//
// This is a CONFORMANCE TEST for cell js/C9 in the schema matrix.
// It uses the same generated JS tables code path that the driver imports from
// build/tables-generated-js/, directly calling the decode function.

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const mod = await import(pathToFileURL(join(generated, "examples/TabledemoTable.js")).href);

const { WeaponConfig, InitWeaponConfig, WeaponConfigFixedDecode, TableFixedReport } = mod;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log(`FAIL: ${msg}`);
    failures++;
  } else {
    console.log(`PASS: ${msg}`);
  }
}

// Construct the 22-byte body vector with EFFECT TAG = 9 (past arms count of 2).
// The needle value 0x09 at offset 17 names no arm: the reader knows none, so
// it must refuse-to-decode this arm and fall back to None.
function makeBody(tag) {
  const buf = new ArrayBuffer(22);
  const view = new DataView(buf);
  view.setFloat32(0, 21.0, true);   // damage (any valid value)
  view.setFloat32(4, 500.0, true);  // speed
  view.setInt32(8, 5, true);        // penetration
  view.setUint32(12, 2, true);      // channel
  view.setUint8(16, 1);             // homing = true
  view.setUint8(17, tag);           // Effect.Type — FORGED VALUE
  view.setFloat32(18, 1.5, true);   // payload
  return new Uint8Array(buf);
}

// ---- THE LAW: union tag past arms → None, clamped == 1 ----

const body = makeBody(9); // tag = 9, well past arm count of 2

const wc = new WeaponConfig();
InitWeaponConfig(wc);
const report = new TableFixedReport();

WeaponConfigFixedDecode(wc, new DataView(body.buffer), 0, report);

assert(wc.Effect.Type === 0, `tag 9 lands as None (Type=${wc.Effect.Type})`);
assert(report.clamped === 1, `report.clamped is exactly 1 (got ${report.clamped})`);
assert(report.refused === 0, `not a refusal (refused=${report.refused})`);
assert(!report.malformed, "not malformed");
assert(report.unknown === 0, "unknown = 0");
assert(report.kindMismatch === 0, "kindMismatch = 0");
assert(report.widened === 0, "widened = 0");
assert(report.duplicate === 0, "duplicate = 0");

// Verify neighbouring scalar after the union field is untouched.
// Penetration starts at 5 (valid range [0,10]) and should be preserved.
assert(wc.Penetration === 5, `neighbour penetration = 5 (${wc.Penetration})`);

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
