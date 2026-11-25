// CELL js/C9 — A UNION TAG PAST THE ARMS (SPEC-TABLES §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.6).
//
// THE LAW AS IT STANDS. §4.6 (line 1218): "an ordinal or a tag past the plan's
// set: REFUSE out_of_range"; the refusal table (line 1281) names it
// `out_of_range`, and §5.3's refusal row says a refused read "lands not one
// byte read from the file" and hands every row back at its declared defaults
// (ruling R4). The retired-clamps table records this very cell's old answer
// (line 1558, #16; line 2153, #9): a tag past the last arm used to land as
// `None` and count a clamp. That is what this row asserted before; it now
// asserts the refusal, case for case.
//
// The Effect union in WeaponConfig (package tabledemo) has two arms, Buff(1)
// and Debuff(2); tag 0 is None.
//
// THE WIRE VECTOR (body, fixed form, little-endian; the emitted decode walk
// TabledemoTable.js WeaponConfigFixedDecode states the same offsets):
//     0 damage f32, 4 speed f32, 8 penetration int32 (declared 0..10),
//     12 channel bits(6) at u32 width, 16 homing bool, 17 Effect tag u8,
//     18 the arm payload (4 bytes). Body 22 bytes.
// The record is written by the generated unit's own writer and the tag byte
// is forged in place, so the forgery is the only difference from bytes the
// positive control proves good.
//
// Run from the repository root:  node test/conformance/js/rows/C9.mjs

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const tables = await load("examples/TablesTable.js");
const home = await load("examples/TabledemoTable.js");

const OUT_OF_RANGE = home.TableFixedRefusal.OutOfRange;
const AT_TAG = 17;

let failures = 0;
function assert(condition, msg) {
  if (!condition) { console.log(`FAIL: ${msg}`); failures++; }
  else { console.log(`PASS: ${msg}`); }
}

assert(home.TableFixedRefusalName(OUT_OF_RANGE) === "out_of_range",
  "the refusal this row expects is out_of_range");

// One lawful WeaponConfig record, written by the unit's own writer, with the
// Debuff arm taken so the tag byte is non-zero before the forgery.
function lawfulFile(tag) {
  const v = new home.WeaponConfig();
  home.InitWeaponConfig(v);
  v.damage = 21.0; v.speed = 500.0; v.penetration = 5; v.channel = 2; v.homing = true;
  v.effect.type = tag;
  if (tag === 1) { v.effect.buff.multiplier = 1.5; }
  if (tag === 2) { v.effect.debuff.amount = 40; }
  const file = new Uint8Array(tables.WeaponConfigFixedMeasure(1));
  const wrote = tables.WeaponConfigFixedSave([v], 1, file);
  if (wrote !== file.length) {
    assert(false, `the writer frames the record whole (wrote ${wrote} of ${file.length})`);
    process.exit(1);
  }
  return file;
}

const bodyAt = home.TableFixedHeaderBytes + home.TableFixedLayoutHeaderBytes + tables.WeaponConfigFixedLayoutBytes + 8;

function poisoned() {
  const v = new home.WeaponConfig();
  v.damage = 99; v.speed = 99; v.penetration = 9; v.channel = 9; v.homing = true; v.effect.type = 1;
  return v;
}
function atDefaults(v) {
  const d = new home.WeaponConfig();
  home.InitWeaponConfig(d);
  return JSON.stringify(v) === JSON.stringify(d);
}
function read(file) {
  const values = [poisoned()];
  const report = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(values, 1, file, file.length, tables.WeaponConfigFixedNewPlan(), report);
  return { n, report, v: values[0] };
}
const quiet = (r) => r.oor === 0 && r.unknown === 0 && r.mismatch === 0 && r.widened === 0 && r.repeated === 0;

// ---- POSITIVE CONTROLS: every lawful tag reads, the last arm included ----
for (const tag of [0, 1, 2]) {
  const r = read(lawfulFile(tag));
  assert(r.n === 1 && r.report.refused === 0 && !("damaged" in r.report) && quiet(r.report) &&
    r.v.effect.type === tag && r.v.penetration === 5,
    `a lawful tag ${tag} reads as written (n=${r.n}, type=${r.v.effect.type}, refused=${r.report.refused})`);
}

// ---- THE LAW: a tag past the arms refuses out_of_range, landing nothing ----
for (const forged of [3, 9, 255]) {
  const file = lawfulFile(2);
  if (file[bodyAt + AT_TAG] !== 2) {
    assert(false, `the vector's tag byte sits at body+${AT_TAG} (found ${file[bodyAt + AT_TAG]})`);
    break;
  }
  file[bodyAt + AT_TAG] = forged;
  const r = read(file);
  assert(r.n === -1 && r.report.refused === OUT_OF_RANGE && !("damaged" in r.report),
    `tag ${forged} past the 2 arms refuses out_of_range, never None (n=${r.n}, refused=${home.TableFixedRefusalName(r.report.refused)})`);
  assert(quiet(r.report), `tag ${forged}: a refusal moves no counter (clamped ${r.report.oor}, unknown ${r.report.unknown})`);
  assert(atDefaults(r.v),
    `tag ${forged}: not one wire byte lands — the row handed in holds its declared defaults, its neighbours too (ruling R4; penetration=${r.v.penetration})`);
}

// ---- THE DECODE'S OWN BOUNDS CHECK, the site the old row called directly ----
{
  const file = lawfulFile(2);
  file[bodyAt + AT_TAG] = 9;
  const v = poisoned();
  const before = JSON.stringify(v);
  const report = new home.TableFixedReport();
  home.WeaponConfigFixedDecode(v, new DataView(file.buffer), bodyAt, report);
  assert(report.refused === OUT_OF_RANGE && report.oor === 0,
    `WeaponConfigFixedDecode refuses tag 9 out_of_range and counts nothing (refused=${home.TableFixedRefusalName(report.refused)})`);
  assert(JSON.stringify(v) === before,
    "WeaponConfigFixedDecode refuses before it lands a field: the value it was handed is untouched");
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
