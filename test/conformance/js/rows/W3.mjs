// test/conformance/js/rows/W3.mjs — js/W3 "zero behind a narrower arm"
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:204): "**A union** writes the tag and
// the taken arm only, zero behind a narrower one." (The card's mechanical
// pointer 201 lands two lines up, on the slack-rule sentence; the union's own
// sentence is at 204.) docs/SPEC-TABLES.md:6736: a union is "the tag ordinal
// at its tag type's storage width + `max` over the arms of `C(arm)` — tag,
// then the WIDEST ARM; **tag `0` is `None`**...; ZERO on write behind a
// narrower arm, IGNORED on read, never a refusal". test/tables/FN1.schema:60
// names the same law for the widest-arm shape.
//
// THE CORPUS FACT this file works inside: every fixed-form union the js tree
// generates (Tabledemo's Effect, v1's and v2's Cfg Effect) has 4-byte arms,
// so the narrowest arm this leg's generated code can reach is NONE — tag 0,
// no arm store at all — and the union's whole widest-arm extent is "behind a
// narrower arm". The widest-vs-narrower arm PAIR the law also names (FN1's
// CellB over CellA) has no generated module in this tree, and generating one
// is the emitter's business (internal/codegen/), out of this card's scope.
// So the assertion here is the None tag: the tag lands, NO arm store runs,
// and the four bytes behind the tag come out as the TEMPLATE's zeros — not
// the pre-stain, and not either dormant arm's poisoned storage.
//
// DERIVATION OF THE WIRE LAYOUT (fixed form 3, little-endian; the generated
// WeaponConfigFixedDst rows state the same numbers):
//   file: 16-byte header, u32 layout length, the 191-byte layout, records;
//   record: 8-byte layout hash + the 22-byte body:
//     body  0: damage float32
//     body  4: speed float32
//     body  8: penetration int32
//     body 12: channel bits(6), stored as uint32
//     body 16: homing bool (1 byte)
//     body 17: Effect tag (1 byte; 0 = None, 1 = Buff, 2 = Debuff)
//     body 18: the arm payload, max over the arms of C(arm) = 4 bytes
//              (Buff float32 multiplier / Debuff int32 amount), 18..22
//
// THE VECTOR: three records — None with both dormant arms' storage poisoned,
// Buff with payload 2.5, Debuff with payload 77. The output buffer is
// pre-stained 0xFF so the template's zero-fill shows as a WRITTEN zero.
// float32 2.5 = 0x40200000 → 00 00 20 40 LE; int32 77 → 4D 00 00 00 LE.
//
// Run from the repository root:  node test/conformance/js/rows/W3.mjs
// Green prints one line per assertion and exits 0; any red prints FAIL and
// exits 1.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs), so the harness's negative
// controls can aim this test at a sabotaged copy too.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const runtime = await load("examples/TabledemoTable.js");
const tables = await load("examples/TablesTable.js");

const { WeaponConfig, TableFixedHeaderBytes, TableFixedLayoutHeaderBytes } = runtime;
const { WeaponConfigFixedSave, WeaponConfigFixedMeasure, WeaponConfigFixedLayoutBytes,
  WeaponConfigFixedRecordBytes } = tables;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log(`FAIL: ${msg}`);
    failures++;
  } else {
    console.log(`PASS: ${msg}`);
  }
}

// One weapon per union tag. BOTH dormant arms' storage is poisoned in every
// record: a store that runs behind a tag it must not run behind — or a fill
// that does not cover the arm slot — puts non-zero bytes where the law says
// the template's zeros stand.
function makeWeapon(tag) {
  const w = new WeaponConfig();
  w.Damage = 21;
  w.Speed = 500;
  w.Penetration = 5;
  w.Channel = 2;
  w.Homing = true;
  w.Effect.Type = tag;
  w.Effect.Buff.Multiplier = 1234567.5;  // dormant-arm garbage
  w.Effect.Debuff.Amount = 2000000000;   // dormant-arm garbage
  if (tag === 1) { w.Effect.Buff.Multiplier = 2.5; }
  if (tag === 2) { w.Effect.Debuff.Amount = 77; }
  return w;
}

const values = [makeWeapon(0), makeWeapon(1), makeWeapon(2)];
const file = new Uint8Array(WeaponConfigFixedMeasure(3));
file.fill(0xFF);
assert(WeaponConfigFixedSave(values, 3, file) === file.length,
  "the writer answers its measure for all three records");

const dv = new DataView(file.buffer, file.byteOffset, file.length);
const recordAt = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes + WeaponConfigFixedLayoutBytes;
const bodyOf = (k) => recordAt + 8 + k * WeaponConfigFixedRecordBytes;
const NONE = bodyOf(0), BUFF = bodyOf(1), DEBUFF = bodyOf(2);

// ---- THE LAW: the tag and the taken arm only, zero behind a narrower one
assert(dv.getUint8(NONE + 17) === 0,
  "None: the tag lands at body+17 as 0");
assert(file[NONE + 18] === 0 && file[NONE + 19] === 0 && file[NONE + 20] === 0 && file[NONE + 21] === 0,
  "None: zero behind the narrower arm — the whole widest-arm extent is the template's zeros, and neither dormant arm's poisoned storage rides");

assert(dv.getUint8(BUFF + 17) === 1,
  "Buff: the tag lands as 1");
assert(file[BUFF + 18] === 0x00 && file[BUFF + 19] === 0x00 && file[BUFF + 20] === 0x20 && file[BUFF + 21] === 0x40,
  "Buff: the taken arm's payload lands whole at the widest-arm slot (float32 2.5)");

assert(dv.getUint8(DEBUFF + 17) === 2,
  "Debuff: the tag lands as 2");
assert(file[DEBUFF + 18] === 0x4D && file[DEBUFF + 19] === 0x00 && file[DEBUFF + 20] === 0x00 && file[DEBUFF + 21] === 0x00,
  "Debuff: the taken arm's payload lands whole at the widest-arm slot (int32 77)");

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
