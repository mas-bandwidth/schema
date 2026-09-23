// row js/NestedKeyedArraysValidData — "Nested enum-keyed arrays: valid-data
// write/read acceptance" (docs/roadmap.sexp nested-keyed-arrays/js/valid-data;
// docs/FIXED-FORM-ALGORITHM.md:1682 §7 keyed corpus).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:1682 (§7 proof 1):
//   "`keyed` (keyed arrays nesting keyed arrays) ... Read a file and save it
//    back; the bytes must be identical — a byte a port encodes differently is
//    a byte that does not come back."
//
// The schema is tables/examples/Keyed.schema: HullConfig carries
// `turrets [Weapon]TurretConfig` — an enum-keyed array whose element is a
// fixed table that itself carries an optional section. KeyedConfig carries
// `[Hull]HullConfig` — an enum-keyed array of records that carry enum-keyed
// arrays. The test writes two records with values set in every nested keyed
// slot, saves them, reads them back, and asserts every turret value survived.

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const tmod = await import(pathToFileURL(join(generated, "examples/TabledemoTable.js")).href);
const kmod = await import(pathToFileURL(join(generated, "examples/KeyedTable.js")).href);

const {
  Hull, Weapon, Team,
  HullConfig, InitHullConfig,
  TurretConfig, InitTurretConfig,
  KeyedConfig, InitKeyedConfig,
  TableFixedPlan, TableFixedReport,
} = tmod;

const {
  HullConfigFixedSave, HullConfigFixedLoad, HullConfigFixedMeasure, HullConfigFixedBodyBytes,
  KeyedConfigFixedSave, KeyedConfigFixedLoad, KeyedConfigFixedMeasure, KeyedConfigFixedBodyBytes,
} = kmod;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log("FAIL " + msg);
    failures++;
  } else {
    console.log("OK   " + msg);
  }
}

// float32 round-trip tolerance: the wire stores f32 and JS reads as f64
function f32eq(a, b) {
  return Math.fround(a) === Math.fround(b);
}

// ---- helper: make a TurretConfig with given values ----
function makeTurret(damage, cooldown, gunnerPresent, reaction, tracking) {
  const t = new TurretConfig();
  InitTurretConfig(t);
  t.Damage = damage;
  t.Cooldown = cooldown;
  t.GunnerPresent = gunnerPresent ? 1 : 0;
  if (gunnerPresent) {
    t.Gunner.Reaction = reaction;
    t.Gunner.Tracking = tracking ? 1 : 0;
  }
  return t;
}

// ---- helper: assert two TurretConfig values match ----
function assertTurret(a, b, label) {
  assert(f32eq(a.Damage, b.Damage), label + ": damage " + a.Damage + " === " + b.Damage);
  assert(f32eq(a.Cooldown, b.Cooldown), label + ": cooldown " + a.Cooldown + " === " + b.Cooldown);
  // GunnerPresent and Tracking are stored as bytes (0/1)
  assert(Number(a.GunnerPresent) === Number(b.GunnerPresent), label + ": gunnerPresent " + Number(a.GunnerPresent) + " === " + Number(b.GunnerPresent));
  if (a.GunnerPresent && b.GunnerPresent) {
    assert(f32eq(a.Gunner.Reaction, b.Gunner.Reaction), label + ": gunner.reaction " + a.Gunner.Reaction + " === " + b.Gunner.Reaction);
    assert(Number(a.Gunner.Tracking) === Number(b.Gunner.Tracking), label + ": gunner.tracking " + Number(a.Gunner.Tracking) + " === " + Number(b.Gunner.Tracking));
  }
}

// ---- helper: assert two HullConfig values match, including nested keyed turrets ----
function assertHull(a, b, hullName) {
  assert(f32eq(a.Health, b.Health), hullName + ": health " + a.Health + " === " + b.Health);
  assert(f32eq(a.Mass, b.Mass), hullName + ": mass " + a.Mass + " === " + b.Mass);
  // [Weapon]TurretConfig — 3 slots: 0=Cannon, 1=Missile, 2=Mine
  const wNames = ["Cannon", "Missile", "Mine"];
  for (let w = 0; w < 3; w++) {
    assertTurret(a.Turrets[w], b.Turrets[w], hullName + "." + wNames[w]);
  }
}

// ---- THE TEST: two KeyedConfig records, every nested keyed slot set ----

const cfg = [];
for (let i = 0; i < 2; i++) { cfg.push(new KeyedConfig()); InitKeyedConfig(cfg[i]); }

// Record 0: set all three hulls, each with all three turrets
cfg[0].Teams[0].SpawnCount = 5;
cfg[0].Teams[1].SpawnCount = 3;
cfg[0].Teams[2].SpawnCount = 7;

// Hull 0 (Interceptor) — every turret with different gunner configs
cfg[0].Hulls[0].Health = 100.0;
cfg[0].Hulls[0].Mass = 1.0;
cfg[0].Hulls[0].Turrets[0] = makeTurret(10.0, 0.5, true, 0.3, true);
cfg[0].Hulls[0].Turrets[1] = makeTurret(20.0, 1.0, false, 0, false);
cfg[0].Hulls[0].Turrets[2] = makeTurret(5.0, 0.25, true, 0.1, false);

// Hull 1 (Gunship)
cfg[0].Hulls[1].Health = 200.0;
cfg[0].Hulls[1].Mass = 3.0;
cfg[0].Hulls[1].Turrets[0] = makeTurret(15.0, 0.75, true, 0.5, true);
cfg[0].Hulls[1].Turrets[1] = makeTurret(25.0, 1.5, true, 0.2, false);
cfg[0].Hulls[1].Turrets[2] = makeTurret(8.0, 0.3, false, 0, false);

// Hull 2 (Freighter)
cfg[0].Hulls[2].Health = 500.0;
cfg[0].Hulls[2].Mass = 10.0;
cfg[0].Hulls[2].Turrets[0] = makeTurret(1.0, 2.0, false, 0, false);
cfg[0].Hulls[2].Turrets[1] = makeTurret(2.0, 3.0, true, 0.8, true);
cfg[0].Hulls[2].Turrets[2] = makeTurret(0.5, 0.1, true, 0.9, true);

// Scores
cfg[0].Scores.PerTeam[0] = 100;
cfg[0].Scores.PerTeam[1] = 200;
cfg[0].Scores.PerTeam[2] = 150;

// Record 1: a second record with different values
cfg[1].Teams[0].SpawnCount = 10;
cfg[1].Teams[1].SpawnCount = 8;
cfg[1].Teams[2].SpawnCount = 12;

cfg[1].Hulls[0].Health = 150.0;
cfg[1].Hulls[0].Mass = 2.0;
cfg[1].Hulls[0].Turrets[0] = makeTurret(30.0, 0.1, true, 0.9, true);
cfg[1].Hulls[0].Turrets[1] = makeTurret(40.0, 0.2, true, 0.7, false);
cfg[1].Hulls[0].Turrets[2] = makeTurret(10.0, 0.5, false, 0, false);

cfg[1].Hulls[1].Health = 300.0;
cfg[1].Hulls[1].Mass = 5.0;
cfg[1].Hulls[1].Turrets[0] = makeTurret(12.0, 0.6, false, 0, false);
cfg[1].Hulls[1].Turrets[1] = makeTurret(18.0, 0.9, true, 0.4, true);
cfg[1].Hulls[1].Turrets[2] = makeTurret(6.0, 0.15, true, 0.6, false);

cfg[1].Hulls[2].Health = 400.0;
cfg[1].Hulls[2].Mass = 8.0;
cfg[1].Hulls[2].Turrets[0] = makeTurret(3.0, 1.5, true, 0.3, true);
cfg[1].Hulls[2].Turrets[1] = makeTurret(4.0, 2.0, false, 0, false);
cfg[1].Hulls[2].Turrets[2] = makeTurret(1.0, 0.2, true, 0.95, true);

cfg[1].Scores.PerTeam[0] = 300;
cfg[1].Scores.PerTeam[1] = 250;
cfg[1].Scores.PerTeam[2] = 400;

// ---- WRITE ----

const need = KeyedConfigFixedMeasure(2);
assert(need > 0, "KeyedConfigFixedMeasure(2) returns a positive byte count");

const buf = new ArrayBuffer(need);
const bytes = new Uint8Array(buf);
const written = KeyedConfigFixedSave(cfg, 2, bytes);
assert(written === need, "KeyedConfigFixedSave wrote " + written + " bytes, expected " + need);

// ---- READ ----

const back = [];
for (let i = 0; i < 2; i++) { back.push(new KeyedConfig()); InitKeyedConfig(back[i]); }

const plan = new TableFixedPlan(10, KeyedConfigFixedBodyBytes, 10);
const report = new TableFixedReport();

const n = KeyedConfigFixedLoad(back, 2, bytes, written, plan, report);
assert(n === 2, "KeyedConfigFixedLoad returned " + n + " records, expected 2");
assert(!report.malformed, "read is not malformed");
assert(report.refused === 0, "read is not refused (refused=" + report.refused + ")");
assert(report.unknown === 0, "unknown=" + report.unknown);
assert(report.kindMismatch === 0, "kindMismatch=" + report.kindMismatch);

// ---- ASSERT: every nested keyed slot survived the round trip ----

for (let r = 0; r < 2; r++) {
  const label = "record" + r;
  // Teams: [Team]TeamConfig — 3 slots
  for (let t = 0; t < 3; t++) {
    const tName = ["Red", "Blue", "Green"][t];
    const a = cfg[r].Teams[t], b = back[r].Teams[t];
    assert(a.SpawnCount === b.SpawnCount, label + ".Team." + tName + ": spawnCount " + a.SpawnCount + " === " + b.SpawnCount);
  }
  // Hulls: [Hull]HullConfig — 3 slots, each with nested [Weapon]TurretConfig
  for (let h = 0; h < 3; h++) {
    const hName = ["Interceptor", "Gunship", "Freighter"][h];
    assertHull(cfg[r].Hulls[h], back[r].Hulls[h], label + "." + hName);
  }
  // Scores
  for (let s = 0; s < 3; s++) {
    const tName = ["Red", "Blue", "Green"][s];
    assert(cfg[r].Scores.PerTeam[s] === back[r].Scores.PerTeam[s], label + ".Score." + tName + ": " + cfg[r].Scores.PerTeam[s] + " === " + back[r].Scores.PerTeam[s]);
  }
}

if (failures === 0) {
  console.log("row js/NestedKeyedArraysValidData: nested enum-keyed arrays round-trip — 2 records, 3 hulls × 3 turrets each, every slot lands (docs/FIXED-FORM-ALGORITHM.md:1682)");
}
process.exit(failures === 0 ? 0 : 1);
