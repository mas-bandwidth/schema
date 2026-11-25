// W4 — ONLY LIVE ELEMENTS ARE READ AS VALUES; EVERY DEAD BYTE IS ZERO
// (docs/FIXED-FORM-ALGORITHM.md §4.5 line 449 and line 263, §5.3 step 11;
// the Go row test/conformance/go/rows/W4_test.go states the same law).
//
// This row was first written under SPEC-TABLES' older "slack is unspecified on
// read, and not a refusal" rule. That rule is retired: the bytes past a used
// extent are DEAD, and a non-zero dead byte refuses the whole load
// `wire_damaged`, before any record lands, moving no counter, with every row
// handed in back at its declared defaults (ruling R4). The row keeps its
// vector and every stain, and asserts the law as it stands for each.
//
// THE VECTOR. ONE RootConfig record (package tabledemo, the examples unit's
// fixed table) with every live value in bound, written by the generated
// writer, read through the production identity reader
// (TablesTable.js RootConfigFixedLoad). RootConfig's body is positional
// (SPEC §3.4) and the emitted decode walk states the offsets: version_note's
// length word at +0 and its 16-byte buffer at +4..20; the weapons count at +20
// and 22-byte WeaponConfigs at +24+i*22; the profiles count at +200. Within a
// weapon: penetration +8 (int32, declared 0..10), homing +16 (bool), the
// Effect tag +17 (two arms). The file framing is the header (16) + the u32
// layout length + the layout + the record's 8-byte hash, then the body.
//
// Run from the repository root:  node test/conformance/js/rows/W4.mjs

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs), so a control that points the
// same path at a sabotaged copy aims this row too.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const tables = await load("examples/TablesTable.js");
const home = await load("examples/TabledemoTable.js");

let failures = 0;
function check(ok, what) {
  console.log((ok ? "ok  " : "FAIL") + " " + what);
  if (!ok) { failures++; }
}

// ---- build the record: live values in bound, every slack range EMPTY ----

const HEADERS = home.TableFixedHeaderBytes + home.TableFixedLayoutHeaderBytes; // 20
const bodyAt = (k) => HEADERS + tables.RootConfigFixedLayoutBytes + 8 + k;     // body offsets

const value = new home.RootConfig();
value.versionNote.set([0x72, 0x65, 0x6c, 0x2d, 0x31], 0); // "rel-1": 5 of 16 used
value.versionNoteLength = 5;
const w0 = value.weapons[0];
w0.damage = 12.5; w0.speed = 250.0; w0.penetration = 5; w0.channel = 3;
w0.homing = true; w0.effect.type = 1; w0.effect.buff.multiplier = 2.0;
const w1 = value.weapons[1];
w1.damage = 7.25; w1.speed = 125.0; w1.penetration = 3; w1.channel = 0;
w1.homing = false; w1.effect.type = 0;
value.weaponsCount = 2;
value.profilesCount = 0;

const file = new Uint8Array(tables.RootConfigFixedMeasure(1));
check(tables.RootConfigFixedSave([value], 1, file) === file.length,
  `positive: the in-bound record saves (${file.length} bytes, body at +${bodyAt(0)})`);

// ---- read 1: the conforming peer (the writer's template zeros its slack) ----

function readRecord(bytes) {
  const back = [new home.RootConfig()];
  const report = new home.TableFixedReport();
  const n = tables.RootConfigFixedLoad(back, 1, bytes, bytes.length,
    tables.RootConfigFixedNewPlan(), report);
  return { n, v: back[0], r: report };
}

const clean = readRecord(file);
const countersOf = (r) => [r.oor, r.unknown, r.widened, r.repeated, r.mismatch];
check(clean.n === 1 && !clean.r.damaged && clean.r.refused === 0 &&
  JSON.stringify(countersOf(clean.r)) === JSON.stringify([0, 0, 0, 0, 0]),
  `baseline: the zeroed-slack record reads clean (n=${clean.n}, clamped=${clean.r.oor})`);

// ---- read 2: the same record with its DEAD bytes stained, one range at a time ----
//
// THE LAW AS IT STANDS (docs/FIXED-FORM-ALGORITHM.md §4.5, line 449, and the
// paragraph at line 263): "Dead bytes are ZERO on read: the bytes past the used
// extent — an absent optional's payload, a counted array's slots past its
// count, a text or bytes field's bytes past its length, and an inactive union
// arm — are DEAD, and a non-zero dead byte REFUSES the whole load as
// wire_damaged, checked over every record before any record lands (§5.3 step
// 11)." This row was written under the older "slack is unspecified on read"
// rule, which that ruling (fixed-dead-bytes-are-zero) retired; the Go row
// test/conformance/go/rows/W4_test.go moved with it. What the old row proved
// and still holds is asserted beside the refusal: the reader validates the
// USED units as values and NOTHING ELSE — invalid UTF-8 past the used length
// is a dead byte (wire_damaged), never text_ill_formed; a penetration of 999
// in a dead slot is a dead byte, never out_of_range — and no counter moves.

function stainText(bytes) {
  // eleven bytes over the buffer span body+9 .. body+20, past "rel-1"'s 5 used
  // bytes: invalid UTF-8 a content rule run over all 16 would call ill formed.
  const utf8Garbage = [0xc3, 0x28, 0xff, 0xc3, 0x28, 0xff, 0xc3, 0x28, 0xff, 0xc3, 0x28];
  for (let k = 0; k < utf8Garbage.length; k++) { bytes[bodyAt(9) + k] = utf8Garbage[k]; }
}
function stainSlots(bytes) {
  // the dead weapon slots 2..7, behind a count of 2: values a bounds pass
  // walking past the live count would refuse as out_of_range or wire_damaged.
  for (let i = 2; i < 8; i++) {
    const at = bodyAt(24 + i * 22);
    new DataView(bytes.buffer, at, 22).setInt32(8, 999, true); // penetration, past max 10
    bytes[at + 16] = 0x02;                                      // not a bool
    bytes[at + 17] = 9;                                         // an Effect tag past 2 arms
  }
}
function stainProfile(bytes) {
  // a dead profile's name length, behind a profiles count of 0
  new DataView(bytes.buffer, bodyAt(204), 4).setInt32(0, 999, true);
}

const fresh = new home.RootConfig();
home.InitRootConfig(fresh);
const big = (k, x) => (typeof x === "bigint" ? "big:" + x.toString() : x);
const atDefaults = (v) => JSON.stringify(v, big) === JSON.stringify(fresh, big);

function poisonedRead(bytes) {
  const back = [new home.RootConfig()];
  back[0].versionNoteLength = 3;
  back[0].weaponsCount = 1;
  back[0].weapons[0].penetration = 9;
  const report = new home.TableFixedReport();
  const n = tables.RootConfigFixedLoad(back, 1, bytes, bytes.length, tables.RootConfigFixedNewPlan(), report);
  return { n, v: back[0], r: report };
}

for (const [what, stain] of [
  ["invalid UTF-8 past version_note's used length", stainText],
  ["out-of-range values in the dead weapon slots past the count", stainSlots],
  ["a forged name length in a dead profile behind a count of zero", stainProfile],
  ["every dead range at once", (b) => { stainText(b); stainSlots(b); stainProfile(b); }],
]) {
  const stained = new Uint8Array(file);
  stain(stained);
  const dirty = poisonedRead(stained);
  check(dirty.n === -1 && dirty.r.refused === home.TableFixedRefusal.WireDamaged && !dirty.r.damaged,
    `dead bytes are zero: ${what} refuses the whole load wire_damaged (n=${dirty.n}, refused=${home.TableFixedRefusalName(dirty.r.refused)})`);
  check(JSON.stringify(countersOf(dirty.r)) === JSON.stringify([0, 0, 0, 0, 0]),
    `moves no counter: ${what} counts nothing (${JSON.stringify(countersOf(dirty.r))})`);
  check(atDefaults(dirty.v),
    `lands nothing: after ${what} the row handed in holds its declared defaults (ruling R4)`);
}

// USED UNITS ONLY, the old row's point, through the refusal's NAME: the text
// stain is a dead-byte refusal and never text_ill_formed, so the content rule
// ran over the used 5 bytes and not over all 16; the slot stain is never
// out_of_range, so the bounds pass walked only the live count.
{
  const t = new Uint8Array(file); stainText(t);
  const s = new Uint8Array(file); stainSlots(s);
  check(poisonedRead(t).r.refused !== home.TableFixedRefusal.TextIllFormed &&
        poisonedRead(s).r.refused !== home.TableFixedRefusal.OutOfRange,
    "used units only: dead garbage is never text_ill_formed and never out_of_range — content and bounds rules walk the used extent");
}

// AND THE LIVE BYTES ARE WHAT IS VALIDATED: the same invalid UTF-8 INSIDE the
// used length is text_ill_formed by name.
{
  const live = new Uint8Array(file);
  live[bodyAt(4)] = 0xff; // the first used byte of "rel-1"
  const r = poisonedRead(live);
  check(r.n === -1 && r.r.refused === home.TableFixedRefusal.TextIllFormed,
    `used units are validated: 0xFF inside version_note's used length refuses text_ill_formed (refused=${home.TableFixedRefusalName(r.r.refused)})`);
}

// THE CLEAN READ lands every live value exactly as written.
{
  const v = clean.v;
  check(v.versionNoteLength === 5 && v.versionNote.subarray(0, 5).join() === "114,101,108,45,49" &&
    v.weaponsCount === 2 && v.weapons[0].damage === 12.5 && v.weapons[0].penetration === 5 &&
    v.weapons[0].homing === true && v.weapons[0].effect.type === 1 &&
    v.weapons[0].effect.buff.multiplier === 2.0 && v.weapons[1].penetration === 3 &&
    v.weapons[1].effect.type === 0 && v.profilesCount === 0,
    "the zeroed-slack read lands every live value exactly as written");
}

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("W4: every dead byte is zero on read; non-zero refuses wire_damaged, moves no counter, lands nothing");
