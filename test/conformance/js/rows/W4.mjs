// W4 — EVERY DEAD BYTE IS ZERO, AND A NON-ZERO DEAD BYTE REFUSES THE WHOLE
// LOAD (docs/SPEC-TABLES.md, THE SLACK RULE: "EVERY SLACK BYTE IS DEAD, AND A
// NON-ZERO DEAD BYTE REFUSES THE WHOLE LOAD"; docs/FIXED-FORM-ALGORITHM.md
// §4.5, §5.3). The Go row test/conformance/go/rows/W4_test.go holds the same
// law. This row once held the old slack rule (non-zero slack read, refused
// nothing and moved no counter); the spec replaced it, and the row with it.
//
// THE LAW, clause by clause, as this row checks it:
//   - ZERO ON WRITE: a writer zero-fills every byte of slack, so the record
//     this build saves reads clean and saves back byte identically;
//   - the four kinds of dead byte (a string(N)'s bytes past its used length,
//     a counted array's slots past its live count, the arm bytes under union
//     tag 0, and a dead nested record's bytes) each REFUSE `wire_damaged`
//     when one byte of them is not zero;
//   - the refusal lands nothing: the destination holds its declared defaults
//     after it (ruling R4), and no counter moves.
//
// THE VECTOR. RootConfig (the examples unit, package tabledemo) has a 1248
// byte body, positional (SPEC §3.4). The emitted dead-byte pass
// (TabledemoTable.js's RootConfigFixedCheckDead) states the offsets:
// version_note's length word at +0 and its 16-byte buffer at +4..20; the
// weapons count at +20 and 22-byte WeaponConfigs at +24+i*22, the Effect tag
// at +17 of a weapon and its 4-byte arm at +18; the profiles count at +200
// and 261-byte ProfileConfigs from +204. The file framing is the header, the
// u32 layout length, the layout and the record's 8-byte hash, then the body.
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

// The first path at which a and b differ, or null. A union at None (b.type
// === 0) compares its tag only: Init sets the tag to None and the dormant
// arms keep their storage, by the generated Init's own rule.
function deepDiff(a, b, path = "") {
  if (a !== null && b !== null && typeof a === "object" && typeof b === "object" &&
      !ArrayBuffer.isView(b) && typeof b.type === "number" && Object.keys(b).length > 1 && b.type === 0) {
    return a.type === 0 ? null : `${path}.type (${a.type} vs 0)`;
  }
  if (typeof a !== typeof b) { return `${path} (${typeof a} vs ${typeof b})`; }
  if (a === null || typeof a !== "object") { return Object.is(a, b) ? null : `${path} (${a} vs ${b})`; }
  if (ArrayBuffer.isView(a)) {
    if (!ArrayBuffer.isView(b) || a.length !== b.length) { return `${path} (length)`; }
    for (let i = 0; i < a.length; i++) { if (!Object.is(a[i], b[i])) { return `${path}[${i}] (${a[i]} vs ${b[i]})`; } }
    return null;
  }
  for (const k of Object.keys(a)) {
    const d = deepDiff(a[k], b[k], `${path}.${k}`);
    if (d !== null) { return d; }
  }
  return null;
}

// ---- the record: live values in bound, every dead byte zero ----

const HEADERS = home.TableFixedHeaderBytes + home.TableFixedLayoutHeaderBytes;
const bodyAt = (k) => HEADERS + tables.RootConfigFixedLayoutBytes + 8 + k;

const value = new home.RootConfig();
value.versionNote.set([0x72, 0x65, 0x6c, 0x2d, 0x31], 0); // "rel-1": 5 of 16 used
value.versionNoteLength = 5;
const w0 = value.weapons[0];
w0.damage = 12.5; w0.speed = 250.0; w0.penetration = 5; w0.channel = 3;
w0.homing = true; w0.effect.type = 1; w0.effect.buff.multiplier = 2.0;
const w1 = value.weapons[1];
w1.damage = 7.25; w1.speed = 125.0; w1.penetration = 3; w1.channel = 0;
w1.homing = false; w1.effect.type = 0; // tag None: its 4 arm bytes are dead
value.weaponsCount = 2;
value.profilesCount = 0;

const file = new Uint8Array(tables.RootConfigFixedMeasure(1));
check(tables.RootConfigFixedSave([value], 1, file) === file.length,
  `positive: the in-bound record saves (${file.length} bytes, body at +${bodyAt(0)})`);

function readRecord(bytes, back) {
  const report = new home.TableFixedReport();
  const n = tables.RootConfigFixedLoad(back, 1, bytes, bytes.length,
    tables.RootConfigFixedNewPlan(), report);
  return { n, r: report };
}

// ---- zero on write: every dead byte this build writes is zero ----

let deadNonZero = 0;
for (let k = 5; k < 16; k++) { if (file[bodyAt(4 + k)] !== 0) { deadNonZero++; } }
for (let k = 2 * 22; k < 8 * 22; k++) { if (file[bodyAt(24 + k)] !== 0) { deadNonZero++; } }
for (let k = 0; k < 4; k++) { if (file[bodyAt(24 + 22 + 18 + k)] !== 0) { deadNonZero++; } }
for (let k = 0; k < 4 * 261; k++) { if (file[bodyAt(204 + k)] !== 0) { deadNonZero++; } }
check(deadNonZero === 0, `zero on write: every dead byte the writer emits is zero (${deadNonZero} non-zero)`);

const cleanBack = [new home.RootConfig()];
const clean = readRecord(file, cleanBack);
check(clean.n === 1 && !clean.r.damaged && clean.r.refused === 0 && clean.r.oor === 0,
  `baseline: the zero-slack record reads clean (n=${clean.n}, refused=${clean.r.refused})`);
check(cleanBack[0].versionNoteLength === 5 && cleanBack[0].weaponsCount === 2 &&
  cleanBack[0].weapons[0].penetration === 5 && cleanBack[0].weapons[0].effect.type === 1 &&
  cleanBack[0].weapons[1].effect.type === 0 && cleanBack[0].profilesCount === 0,
  "baseline: every live value lands");
const again = new Uint8Array(file.length);
check(tables.RootConfigFixedSave(cleanBack, 1, again) === again.length &&
  again.every((b, i) => b === file[i]), "baseline: the read record saves back byte identically");

// ---- each kind of dead byte, stained alone, refuses the whole load ----

const defaults = new home.RootConfig();
home.InitRootConfig(defaults);

const stains = [
  ["a string(16)'s byte past its used length 5", bodyAt(4 + 5)],
  ["the last byte of the string's buffer", bodyAt(4 + 15)],
  ["a counted array's slot past its live count 2 (weapon 2's penetration)", bodyAt(24 + 2 * 22 + 8)],
  ["the array's last dead byte (weapon 7's arm)", bodyAt(24 + 7 * 22 + 21)],
  ["an arm byte under union tag 0 (weapon 1's Effect)", bodyAt(24 + 22 + 18)],
  ["a dead nested record under a count of 0 (profile 0's name length)", bodyAt(204)],
];
for (const [what, at] of stains) {
  const stained = new Uint8Array(file);
  stained[at] = 0x01;
  const back = [new home.RootConfig()];
  // a stale destination, so a refusal that lands anything is visible
  back[0].versionNoteLength = 3; back[0].versionNote[0] = 0x7a;
  back[0].weaponsCount = 1; back[0].weapons[0].penetration = 9;
  const { n, r } = readRecord(stained, back);
  check(n === -1 && r.refused === home.TableFixedRefusal.WireDamaged,
    `${what}: a non-zero dead byte refuses wire_damaged (n=${n}, refused=${home.TableFixedRefusalName(r.refused)})`);
  const diff = deepDiff(back[0], defaults);
  check(diff === null, `${what}: the refusal leaves the destination at its declared defaults (R4)${diff === null ? "" : `: ${diff}`}`);
  check(r.oor === 0 && r.widened === 0 && r.unknown === 0 && r.repeated === 0 && r.mismatch === 0,
    `${what}: a refused load moves no counter`);
}

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("W4: every dead byte is zero, and a non-zero one refuses the whole load");
