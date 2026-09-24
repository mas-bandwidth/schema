// R9: a known hash with a different layout length or bytes → layout_malformed;
// the seven §1.1 malformations under a known hash all come back as this one name.
//
// SPEC: docs/FIXED-FORM-ALGORITHM.md:864 — "The seven §1.1 malformations under a
// KNOWN hash all come back as one name, layout_malformed"
//
// The production path: CellFixedLoad reads the header hash, looks it up in
// CellFixedKnown (the lineage), finds a match (the hash IS known), then compares
// the file's layout bytes against the known layout byte-by-byte. If they differ
// in length or content, it returns LayoutMalformed — without walking the seven
// §1.1 rules, because the hash already identified the version. The call site is
// build/tables-generated-js/v1/V1Table.js:176-178.
//
// Test vector derivation: take a valid file written by CellFixedSave, break one
// byte of the layout (or change the declared layout length), keep the header
// hash unchanged. The hash is known, the layout bytes differ → layout_malformed.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

let failed = 0;
function check(condition, message) {
  if (!condition) { console.log("FAIL: " + message); failed++; }
  else { console.log("ok: " + message); }
}

const home = await load("v1/Tblv1Table.js");
const v1 = await load("v1/V1Table.js");

const TableFixedRefusal = home.TableFixedRefusal;
const TableFixedHeaderBytes = home.TableFixedHeaderBytes;
const TableFixedLayoutHeaderBytes = home.TableFixedLayoutHeaderBytes;

// ---- a valid file for the Cell table
const value = new home.Cell();
const measure = v1.CellFixedMeasure(1);
const valid = new Uint8Array(measure);
v1.CellFixedSave([value], 1, valid);

const layoutAt = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes;

// ---- GREEN: the unbroken file opens normally (negative control)
{
  const plan = v1.CellFixedNewPlan();
  const r = new home.TableFixedReport();
  const n = v1.CellFixedLoad([new home.Cell()], 1, valid, valid.length, plan, r);
  check(n === 1 && r.refused === 0 && !r.malformed,
    "GREEN: a valid file opens (negative control)");
}

// ---- RED: a known hash with WRONG LAYOUT BYTES → layout_malformed
{
  const broken = Uint8Array.from(valid);
  broken[layoutAt + 4] ^= 0xff; // flip one byte of the layout
  const plan = v1.CellFixedNewPlan();
  const r = new home.TableFixedReport();
  const n = v1.CellFixedLoad([new home.Cell()], 1, broken, broken.length, plan, r);
  check(n === -1 && r.refused === TableFixedRefusal.LayoutMalformed,
    "RED: a known hash with a different layout byte is layout_malformed");
  check(r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && !r.malformed,
    "RED: a known hash with a different layout byte moves no counter");
}

// ---- RED: a known hash with WRONG LAYOUT LENGTH → layout_malformed
{
  const broken = Uint8Array.from(valid);
  // change the declared layout length (u32 LE at bytes 16-19)
  broken[TableFixedHeaderBytes] = 99;
  broken[TableFixedHeaderBytes + 1] = 0;
  broken[TableFixedHeaderBytes + 2] = 0;
  broken[TableFixedHeaderBytes + 3] = 0;
  const plan = v1.CellFixedNewPlan();
  const r = new home.TableFixedReport();
  const n = v1.CellFixedLoad([new home.Cell()], 1, broken, broken.length, plan, r);
  check(n === -1 && r.refused === TableFixedRefusal.LayoutMalformed,
    "RED: a known hash with a different layout length is layout_malformed");
  check(r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && !r.malformed,
    "RED: a known hash with a different layout length moves no counter");
}

// ---- EXIT
if (failed > 0) {
  console.log("\n" + failed + " test(s) FAILED");
  process.exit(1);
}
console.log("\nall tests passed");