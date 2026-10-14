// W4 — READ SLACK IS UNSPECIFIED (docs/SPEC-TABLES.md:6750, §3.4's slack rule;
// the cell's law pointer docs/FIXED-FORM-ALGORITHM.md:346 states the same law
// at §4.6: the bounds pass "walks only what a read can have written — a
// counted array's LIVE elements and never its slack").
//
// THE LAW, quoted: "UNSPECIFIED ON READ, AND NOT A REFUSAL. A reader validates
// the USED UNITS ONLY and never looks at the slack, so a peer that leaves
// garbage there is a peer this reader reads correctly. NON-ZERO SLACK IS NOT
// `malformed`, NOT A REFUSAL, AND MOVES NO COUNTER." And: "THE CONTENT RULES
// APPLY TO THE USED UNITS AND TO NOTHING ELSE. A `string(N)`'s UTF-8 validity
// (§3) is checked over its stated length, not over `N`" (docs/SPEC-TABLES.md:
// 6756). §4.6 adds why no counter may move: "clamping storage nobody wrote
// would count a clamp on every clean read."
//
// WHAT THIS TEST DOES, on the C++ row of the same audit item
// (test/conformance/cpp/rows/W4.cpp) as its shape: it builds ONE RootConfig
// record (the examples unit's fixed table, package tabledemo) whose live
// values are all in bound, reads it through the production identity reader
// (TablesTable.js's RootConfigFixedLoad), then stains every declared slack
// range of a copy with garbage a conforming writer never emits and reads THAT.
// The stain is chosen so each wrong behaviour trips a different assertion:
// invalid UTF-8 (0xC3 0x28 0xFF) past `version_note`'s used length — a reader
// validating the full 16-byte bound would flag `malformed`; out-of-range
// values in the DEAD weapon slots (penetration 999 past max 10, union tag 9
// past the 2 arms, bool 0x02) — a reader walking past the live count would
// count clamps; and a forged 999 in a dead profile's name length under a
// count of zero. The two reads must agree on every used unit, every counter
// and every verdict, and the reader must not have looked at the slack at all.
//
// THE VECTOR. RootConfig's body is 1248 bytes, positional (SPEC §3.4), and
// the emitted decode walk (TabledemoTable.js's RootConfigFixedDecode) states
// the offsets: version_note's length word at +0 and its 16-byte buffer at
// +4..20; the weapons count at +20 and 22-byte WeaponConfigs at +24+i*22;
// the profiles count at +200. Within a weapon: damage +0 (f32), speed +4
// (f32), penetration +8 (int32, declared min 0 max 10), channel +12 (bits(6)
// at uint32 width), homing +16 (bool), the Effect tag +16+1 = +17 (two arms,
// Buff(1) and Debuff(2)), the arm payload +18 (f32). The file framing is the
// header (16) + the u32 layout length + the layout + the record's 8-byte
// hash, then the body.
//
// THE NEGATIVE CONTROL is NOT run here: it breaks one constant of this law in
// the generated tree (build/tables-generated-js/examples/TabledemoTable.js,
// RootConfigFixedDecode's weapons loop bound `value.WeaponsCount` -> `8`, so
// the pass walks the slack) and this file goes RED. It is restored after; the
// red/green pair is recorded in RESULT.md.
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
value.VersionNote.set([0x72, 0x65, 0x6c, 0x2d, 0x31], 0); // "rel-1": 5 of 16 used
value.VersionNoteLength = 5;
const w0 = value.Weapons[0];
w0.Damage = 12.5; w0.Speed = 250.0; w0.Penetration = 5; w0.Channel = 3;
w0.Homing = true; w0.Effect.Type = 1; w0.Effect.Buff.Multiplier = 2.0;
const w1 = value.Weapons[1];
w1.Damage = 7.25; w1.Speed = 125.0; w1.Penetration = 3; w1.Channel = 0;
w1.Homing = false; w1.Effect.Type = 0;
value.WeaponsCount = 2;
value.ProfilesCount = 0;

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
const countersOf = (r) => [r.clamped, r.unknown, r.widened, r.duplicate, r.kindMismatch];
check(clean.n === 1 && !clean.r.malformed && clean.r.refused === 0 &&
  JSON.stringify(countersOf(clean.r)) === JSON.stringify([0, 0, 0, 0, 0]),
  `baseline: the zeroed-slack record reads clean (n=${clean.n}, clamped=${clean.r.clamped})`);

// ---- read 2: the same record with every slack range STAINED ----
// 0xC3 0x28 0xFF is not valid UTF-8: a content rule run over the full
// 16-byte bound, not the used 5, would answer malformed. 999 is past
// penetration's declared max 10; tag 9 is past the Effect union's 2 arms;
// 0x02 is not a normalised bool; all of it sits behind a count of 2 (dead
// slots 2..7) and behind a count of 0 (a dead profile's name length 999).

const stained = new Uint8Array(file);
// eleven bytes: the buffer span body+9 .. body+20, three invalid sequences and a
// truncated lead byte, ending exactly at the buffer's last byte.
const utf8Garbage = [0xc3, 0x28, 0xff, 0xc3, 0x28, 0xff, 0xc3, 0x28, 0xff, 0xc3, 0x28];
for (let k = 0; k < utf8Garbage.length; k++) { stained[bodyAt(9) + k] = utf8Garbage[k]; }
for (let i = 2; i < 8; i++) {
  const at = bodyAt(24 + i * 22);
  const view = new DataView(stained.buffer, at, 22);
  view.setInt32(8, 999, true); // penetration, past max 10
  stained[at + 16] = 0x02;     // a bool not normalised to the language's own true
  stained[at + 17] = 9;        // an Effect tag past the 2 arms
}
new DataView(stained.buffer, bodyAt(204), 4).setInt32(0, 999, true); // a dead profile's name length

const dirty = readRecord(stained);

// THE LAW, clause by clause.
check(dirty.n === 1,
  `reads correctly: a peer that leaves garbage in the slack still loads (n=${dirty.n}, never a refusal)`);
check(!dirty.r.malformed && dirty.r.refused === 0,
  `not a refusal: non-zero slack is not malformed and refuses nothing (malformed=${dirty.r.malformed}, refused=${dirty.r.refused})`);
check(dirty.r.clamped === 0,
  `moves no counter: clamped stays 0, not one per dead slot past the live count (got ${dirty.r.clamped})`);
check(JSON.stringify(countersOf(dirty.r)) === JSON.stringify(countersOf(clean.r)),
  `moves no counter: every counter of the stained read equals the clean read (${JSON.stringify(countersOf(dirty.r))})`);

const v = dirty.v;
check(v.VersionNoteLength === 5 && clean.v.VersionNoteLength === 5 &&
  v.VersionNote.subarray(0, 5).join() === "114,101,108,45,49",
  `used units only: the string keeps its length 5 and "rel-1" behind invalid UTF-8 (len=${v.VersionNoteLength})`);
check(v.WeaponsCount === 2 && v.Weapons[0].Damage === 12.5 && v.Weapons[0].Penetration === 5 &&
  v.Weapons[0].Homing === true && v.Weapons[0].Effect.Type === 1 &&
  v.Weapons[0].Effect.Buff.Multiplier === 2.0 && v.Weapons[1].Penetration === 3 &&
  v.Weapons[1].Effect.Type === 0 && v.ProfilesCount === 0,
  "used units only: every live value lands exactly as the zeroed-slack read gave it");
check(JSON.stringify([v.VersionNoteLength, v.WeaponsCount, v.Weapons[0].Penetration, v.Weapons[1].Penetration]) ===
  JSON.stringify([clean.v.VersionNoteLength, clean.v.WeaponsCount, clean.v.Weapons[0].Penetration, clean.v.Weapons[1].Penetration]),
  "the two peers agree: zeroed slack and stained slack decode the same used extent");
check(v.Weapons[2].Penetration === 1 && v.Weapons[2].Damage === 21 && v.Weapons[3].Effect.Type === 0 &&
  v.Profiles[0].NameLength === 0,
  "never looks at the slack: dead slots keep the caller's own storage, not the wire's garbage");

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("W4: non-zero slack is not malformed, not a refusal, and moves no counter");
