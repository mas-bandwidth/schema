// The JavaScript cross-language wire test: the generated JS modules write the
// SAME pinned instances the C++ test pins in testdata/wire/*.bin and
// byte-compare against those files — cross-language wire identity is the §7.2
// gate this leg carries (goal 3: byte-identical wire output across all
// targets, and the readers agree on what they reject). Plus round-trips
// through the JS reader, the §5 branch-zeroing checks, the specified-defaults
// checks, and the bench-corpus pins (bench_*, real_packet) the C++ bench
// pinner authored — the JS leg gates them here because the bench runner rig
// imports these exact modules.
//
// Prints OK and exits 0, exactly like its C++ and Go twins. Run from test/js
// (the Makefile does): the wire goldens are at ../../testdata/wire. The
// serialize.js runtime is the documented sibling checkout, imported by
// module-relative path — no npm, no install step, zero dependencies. Both
// runtime modes run in CI (checked, and NODE_ENV=production): the wire must
// be identical in both, and the goldens prove it.
import { readFileSync } from "node:fs";
import { WriteStream, ReadStream } from "../../../serialize.js/src/index.js";

import * as enums from "../../generated/js/Enums.js";
import * as types from "../../generated/js/Types.js";
import * as wire from "../../generated/js/Wire.js";
import * as armDefaults from "../../generated/js/ArmDefaults.js";
import * as armDefaultsFlat from "../../generated/js/ArmDefaultsFlat.js";
import * as bench from "../../generated/bench/js/Bench.js";
import * as realworld from "../../generated/bench/js/realworld/RealWorld.js";

// the flat tier — the shipped JS wire path, held byte-identical to the
// runtime tier and to the same C++-pinned goldens in the section at the
// bottom of this file
import * as degenerate from "../../generated/js/Degenerate.js";
import * as degenerateFlat from "../../generated/js/DegenerateFlat.js";
import * as clauses from "../../generated/js/Clauses.js";
import * as clausesFlat from "../../generated/js/ClausesFlat.js";
import * as joins from "../../generated/js/Joins.js";
import * as joinsFlat from "../../generated/js/JoinsFlat.js";
import * as typesFlat from "../../generated/js/TypesFlat.js";
import * as wireFlat from "../../generated/js/WireFlat.js";
import * as benchFlat from "../../generated/bench/js/BenchFlat.js";
import * as realworldFlat from "../../generated/bench/js/realworld/RealWorldFlat.js";

// BenchMixed — THE canonical benchmark shape (#184). The pin is
// test/bench/main.cpp's, transcribed exactly; it is built here once and used
// by both the runtime-tier and flat-tier gates.
function makeBenchMixedPin() {
  const mixed = new bench.BenchMixed();
  mixed.sequence = 52428;
  mixed.ackSequence = 12345;
  mixed.ackBits = 0xa5a5a5a5;
  mixed.sessionId = 0x123456789abcdef0n;
  mixed.clientId = 0xdeadbeef;
  mixed.nonce = 0xfedcba9876543210n;
  mixed.worldTime = -987654321000n;
  mixed.frameTick = 0x123456789abcn;
  mixed.serverTime = 12345678;
  mixed.entitiesCount = 8;
  for (let i = 0; i < 8; i++) {
    const e = mixed.entities[i];
    e.entityId = 2049 + i * 17;
    e.posX = -16383 + i * 4096;
    e.posY = 16383 - i * 4096;
    e.posZ = -1 + i * 2048;
    e.yaw = 511 - i * 64;
    e.pitch = i * 73;
    e.velX = -2048 + i * 512;
    e.velY = 2047 - i * 512;
    e.velZ = -1024 + i * 256;
    e.health = 1000 - i * 100;
    e.weapon = 1 + i;
    e.damage = BigInt(0x5a + i);
    e.moving = i % 2 === 0;
    e.firing = i % 3 === 0;
  }
  mixed.statsCount = 80;
  for (let i = 0; i < 80; i++) {
    mixed.stats[i].statId = (i * 3) % 256;
    mixed.stats[i].delta = -512 + ((i * 13) % 1024);
  }
  mixed.gameEvent.type = bench.MixedEventType.Hit;
  mixed.gameEvent.hit.targetId = 4095;
  mixed.gameEvent.hit.damage = 4095;
  mixed.gameEvent.hit.hitKind = 7;
  mixed.gameEvent.hit.crit = true;
  mixed.loadout.set([0x11, 0x22, 0x33, 0x44]);
  mixed.playerName.set([..."Rowan_01"].map((c) => c.charCodeAt(0)));
  mixed.playerNameLength = 8;
  mixed.payload.set([0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04]);
  mixed.payloadLength = 8;
  mixed.aimX = 0.5;
  mixed.aimY = -0.25;
  mixed.aimZ = 0.75;
  mixed.recoil = 1.5;
  mixed.drift = -3.25;
  mixed.wideKey = (0x0123456789abcdefn << 64n) | 0xfedcba9876543210n;
  mixed.flux = (1n << 99n) + 7n; // 2^99 + 7
  mixed.ping = 12345;
  mixed.crcHint = 0xabcdef;
  mixed.hasExtra = true;
  mixed.extra = 200;
  mixed.idleTicks = 9; // rides now: the branch that gated it is gone
  return mixed;
}


// one namespace over the unit, the way Go sees package example — the checker
// guarantees unit-wide name uniqueness, so the merge cannot collide
const ex = { ...enums, ...types, ...wire };
const exFlat = { ...typesFlat, ...wireFlat };

let failed = false;

function check(ok, what) {
  if (!ok) {
    console.log(`FAILED: ${what}`);
    failed = true;
  }
}

function bytesEqual(a, b) {
  if (a.length !== b.length) {
    return false;
  }
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) {
      return false;
    }
  }
  return true;
}

// deepEqual is the JS stand-in for Go's == over the generated value types:
// Number/BigInt/boolean scalars, Uint8Array buffers, arrays and nested
// generated classes, compared structurally.
function deepEqual(a, b) {
  if (a === b) {
    return true;
  }
  if (a instanceof Uint8Array && b instanceof Uint8Array) {
    return bytesEqual(a, b);
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((v, i) => deepEqual(v, b[i]));
  }
  if (a !== null && b !== null && typeof a === "object" && typeof b === "object") {
    const keys = Object.keys(a);
    return keys.length === Object.keys(b).length && keys.every((k) => deepEqual(a[k], b[k]));
  }
  return false;
}

// goldenWire byte-compares written wire against the C++-pinned golden.
function goldenWire(name, data) {
  let golden;
  try {
    golden = readFileSync(`../../testdata/wire/${name}.bin`);
  } catch (err) {
    check(false, `read wire golden ${name}: ${err.message}`);
    return;
  }
  check(bytesEqual(data, golden), `wire golden ${name} — JS bytes must equal the C++-pinned bytes`);
}

function newWriteStream() {
  return new WriteStream(new Uint8Array(2048));
}

function textBytes(s) {
  return new TextEncoder().encode(s);
}

// ---- ShipCreate: the bool-gated flags branch, both ways ----
// Every selection, including a repeated tag, restores the payload's
// construction values in place. The independent packet is tag 1, count 0,
// marker 5: 2 + 2 + 3 bits, low bit first, giving 0x51.
{
  const expected = Uint8Array.of(0x51);
  const fresh = new armDefaults.DefaultChoicePacket();
  fresh.choice.type = armDefaults.DefaultChoiceType.First;
  fresh.choice.first.marker = 5;
  check(fresh.choice.first.entries.every((entry) =>
    entry.retries === -1 && entry.preferred === ex.Weapon.Railgun),
    "default arm construction initializes all backing entries");

  // Explicit initialization is also available when the application reuses
  // a selected payload; assigning its public tag does not call a helper.
  const first = fresh.choice.first;
  const entries = first.entries;
  const entry0 = entries[0];
  entries[0].retries = 45;
  entries[1].preferred = ex.Weapon.Laser;
  armDefaults.InitDefaultArm(first);
  check(fresh.choice.first === first && first.entries === entries && entries[0] === entry0,
    "InitDefaultArm preserves preallocated objects and arrays");
  check(first.entriesCount === 0 && first.marker === 0 &&
    entries.every((entry) => entry.retries === -1 && entry.preferred === ex.Weapon.Railgun),
    "InitDefaultArm restores ordinary construction values");
  first.marker = 5;

  const ws = newWriteStream();
  check(armDefaults.WriteDefaultChoicePacket(ws, fresh), "runtime write initialized arm");
  check(ws.bitsProcessed() === 7, "initialized arm writes exactly seven bits");
  ws.flush();
  check(bytesEqual(ws.data(), expected), "runtime initialized arm writes independent 0x51 packet");
  const flatBuffer = new Uint8Array(armDefaults.DefaultChoicePacketMaxBytes);
  check(armDefaultsFlat.WriteDefaultChoicePacketFlat(new DataView(flatBuffer.buffer), fresh) === 1 &&
    flatBuffer[0] === 0x51, "flat initialized arm writes independent 0x51 packet");

  for (const tier of ["runtime", "flat"]) {
    const out = new armDefaults.DefaultChoicePacket();
    const selected = out.choice.first;
    const selectedEntries = selected.entries;
    const selectedObjects = [...selectedEntries];
    const unselected = out.choice.second;
    unselected.entriesCount = 1;
    unselected.marker = 6;
    unselected.entries[0].retries = 81;
    unselected.entries[1].preferred = ex.Weapon.Missile;
    const unselectedSnapshot = JSON.stringify(unselected);
    out.choice.type = armDefaults.DefaultChoiceType.Second;
    for (let pass = 0; pass < 2; pass++) {
      selected.entriesCount = 2;
      selected.marker = 7;
      for (const entry of selectedEntries) {
        entry.retries = 37 + pass;
        entry.preferred = ex.Weapon.None;
      }
      let readOK;
      let consumedOK = true;
      if (tier === "runtime") {
        const rs = new ReadStream(expected);
        readOK = armDefaults.ReadDefaultChoicePacket(rs, out);
        check(readOK, `${tier} default arm read ${pass}`);
        consumedOK = rs.bitsProcessed() === 7;
        check(consumedOK, `${tier} default arm consumes seven bits ${pass}`);
      } else {
        const buffer = new Uint8Array(expected.length + 8);
        buffer.set(expected);
        readOK = armDefaultsFlat.ReadDefaultChoicePacketFlat(new DataView(buffer.buffer), 7, out);
        check(readOK, `${tier} default arm accepts exactly seven bits ${pass}`);
      }
      const selectedOK = out.choice.type === armDefaults.DefaultChoiceType.First &&
        selected.entriesCount === 0 && selected.marker === 5;
      check(selectedOK, `${tier} default arm receives count and marker ${pass}`);
      // The control's backing-default marker requires a successful oracle decode.
      if (!readOK || !consumedOK || !selectedOK) {
        continue;
      }
      check(selectedEntries.every((entry) =>
        entry.retries === -1 && entry.preferred === ex.Weapon.Railgun),
        `${tier} default arm restores both unused entries on every selection ${pass}`);
      check(out.choice.first === selected && selected.entries === selectedEntries &&
        selectedEntries.every((entry, i) => entry === selectedObjects[i]),
        `${tier} default arm preserves selected storage ${pass}`);
      check(out.choice.second === unselected && JSON.stringify(unselected) === unselectedSnapshot,
        `${tier} default arm leaves unselected storage untouched ${pass}`);
    }
  }
  const truncated = new Uint8Array(9);
  truncated[0] = 0x51;
  check(!armDefaultsFlat.ReadDefaultChoicePacketFlat(new DataView(truncated.buffer), 6, new armDefaults.DefaultChoicePacket()), "flat default arm refuses a six-bit packet");
}

{
  const inp = new ex.ShipCreate();
  inp.shipType = ex.ShipType.Bomber;
  inp.position.x = 1000;
  inp.position.y = -2000;
  inp.position.z = 3000;
  inp.hasFlags = true;
  inp.flags = ex.ShipFlagsBoosting | ex.ShipFlagsAiming;
  inp.team = ex.Team.Blue;
  inp.health = 750;
  inp.thrust = 55;

  const ws = newWriteStream();
  check(ex.WriteShipCreate(ws, inp), "write ShipCreate");
  ws.flush();
  goldenWire("shipcreate_flags", ws.data());

  const out = new ex.ShipCreate();
  const rs = new ReadStream(ws.data());
  check(ex.ReadShipCreate(rs, out), "read ShipCreate");
  check(deepEqual(out, inp), "ShipCreate round-trips");

  // untaken branch: flags must read back ZERO (SPEC §5) — into the same
  // out value, so stale flags would be caught
  inp.hasFlags = false;
  const ws2 = newWriteStream();
  check(ex.WriteShipCreate(ws2, inp), "write ShipCreate no-flags");
  ws2.flush();
  const rs2 = new ReadStream(ws2.data());
  check(ex.ReadShipCreate(rs2, out), "read ShipCreate no-flags");
  check(!out.hasFlags && out.flags === 0n, "untaken branch reads as zero (SPEC §5)");
}

// ---- RigidBody: the back-reference example, both branch sides ----
{
  const inp = new ex.RigidBody();
  inp.position.x = 1.5;
  inp.position.y = -2.5;
  inp.position.z = 3.25;
  inp.orientation.x = 0.1;
  inp.orientation.y = 0.2;
  inp.orientation.z = 0.3;
  inp.orientation.w = 0.9;
  inp.atRest = false;
  inp.linearVelocity.x = 10.0;
  inp.linearVelocity.y = 20.0;
  inp.linearVelocity.z = -3.0;
  inp.angularVelocity.x = 0.25;
  inp.angularVelocity.y = 0.5;
  inp.angularVelocity.z = 0.75;

  const ws = newWriteStream();
  check(ex.WriteRigidBody(ws, inp), "write RigidBody moving");
  ws.flush();
  goldenWire("rigidbody_moving", ws.data());

  inp.atRest = true;
  const ws2 = newWriteStream();
  check(ex.WriteRigidBody(ws2, inp), "write RigidBody at rest");
  ws2.flush();
  goldenWire("rigidbody_at_rest", ws2.data());

  // the at-rest read must ZERO both velocities (SPEC §5), even though the
  // written value had them set
  const out = new ex.RigidBody();
  const rs = new ReadStream(ws2.data());
  check(ex.ReadRigidBody(rs, out), "read RigidBody at rest");
  check(out.atRest, "at_rest reads true");
  check(deepEqual(out.linearVelocity, new ex.Vec3()) && deepEqual(out.angularVelocity, new ex.Vec3()),
    "velocities read as zero under the taken at-rest branch (SPEC §5)");
}

// ---- Chat: the string framing == classic serialize_string over N + 1 ----
{
  const inp = new ex.Chat();
  inp.text.set(textBytes("wire parity"));
  inp.textLength = 11;

  const ws = newWriteStream();
  check(ex.WriteChat(ws, inp), "write Chat");
  ws.flush();
  goldenWire("chat", ws.data());

  const out = new ex.Chat();
  const rs = new ReadStream(ws.data());
  check(ex.ReadChat(rs, out), "read Chat");
  check(deepEqual(out, inp), "Chat round-trips");
}

// ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
{
  const inp = new ex.ProbeHeader();
  inp.version = 5;
  inp.probeId = 0x1122334455667788n;
  const ws = newWriteStream();
  check(ex.WriteProbeHeader(ws, inp), "write ProbeHeader");
  ws.flush();
  const data = ws.data();
  check(data[0] === 0xab, "const(0xAB, 8) leads the wire");
  goldenWire("probe_header", data);

  const out = new ex.ProbeHeader();
  const rs = new ReadStream(data);
  check(ex.ReadProbeHeader(rs, out), "read ProbeHeader");
  check(deepEqual(out, inp), "ProbeHeader round-trips");

  const corrupt = Uint8Array.from(data);
  corrupt[0] = 0xac;
  const rs2 = new ReadStream(corrupt);
  check(!ex.ReadProbeHeader(rs2, out), "a corrupted wire constant is REJECTED (SPEC §4.3)");
}

// makeInputPacket is the pinned InputPacket instance, shared by the
// round-trip and golden blocks below.
function makeInputPacket() {
  const p = new ex.InputPacket();
  p.synchronizeSequence = 7;
  p.currentFrame = 123456789n;
  p.startFrame = 123456780n;
  p.inputsCount = 2;
  p.inputs[0].throttle = 0.5;
  p.inputs[0].fire = true;
  p.inputs[1].stickX = -0.25;
  p.inputs[1].boost = true;
  return p;
}

// ---- InputPacket: counted array of nested structs ----
{
  const inp = makeInputPacket();
  const ws = newWriteStream();
  check(ex.WriteInputPacket(ws, inp), "write InputPacket");
  ws.flush();

  const out = new ex.InputPacket();
  const rs = new ReadStream(ws.data());
  check(ex.ReadInputPacket(rs, out), "read InputPacket");
  check(deepEqual(out, inp), "InputPacket round-trips");
}

// testDataInstance is the deterministic TestData the C++ test pins — the
// values must stay mirrored on both sides.
function testDataInstance() {
  const inp = new ex.TestData();
  inp.a = -100;
  inp.b = 100;
  inp.c = 149;
  inp.d = 0x11;
  inp.e = 0x22;
  inp.f = 0x33;
  inp.g = true;
  inp.itemsCount = 3;
  inp.items[0] = 0;
  inp.items[1] = 128;
  inp.items[2] = 255;
  inp.floatValue = Math.fround(3.1415926);
  inp.compressedFloatValue = 2.5;
  inp.doubleValue = 1.0 / 3.0;
  inp.int8Value = -128;
  inp.int16Value = -32768;
  inp.uint8Value = 255;
  inp.uint16Value = 65535;
  inp.uint32Value = 4294967295;
  inp.uint64Value = 18446744073709551615n;
  inp.int64Full = -9223372036854775808n;
  inp.int64Range = -999999999999n;
  for (let i = 0; i < inp.fixedBytes.length; i++) {
    inp.fixedBytes[i] = (i * 3) & 0xff;
  }
  inp.text.set(textBytes("the quick brown fox"));
  inp.textLength = 19;
  return inp;
}

// ---- TestData: the vanilla library's own test type, deterministic values ----
{
  const inp = testDataInstance();

  const ws = newWriteStream();
  check(ex.WriteTestData(ws, inp), "write TestData");
  ws.flush();

  const out = new ex.TestData();
  const rs = new ReadStream(ws.data());
  check(ex.ReadTestData(rs, out), "read TestData");
  check(deepEqual(out, inp), "TestData round-trips — signed narrows, full-range ints, align, fixed bytes, string");
}

// ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
// 0.005 quantizes to 1 under the float32 two-rounding law (a fused or
// double build says 0); -4.8585 over the non-zero-min range quantizes to
// 142 (a double build says 141). Same pinned instance as the C++ leg,
// against the same golden.
{
  const inp = new ex.CompressedProbe();
  inp.boundary = 0.005;
  inp.offset = -4.8585;

  const ws = newWriteStream();
  check(ex.WriteCompressedProbe(ws, inp), "write CompressedProbe");
  ws.flush();
  goldenWire("compressed_probe", ws.data());

  const out = new ex.CompressedProbe();
  const rs = new ReadStream(ws.data());
  check(ex.ReadCompressedProbe(rs, out), "read CompressedProbe");
  // per-op float32, exactly the runtime's own decode chain — Math.fround
  // around every step, the JS twin of the Go leg's through-variables trick
  check(out.boundary === Math.fround(Math.fround(1 / 1000) * 10), "boundary reconstructs integer 1");
  check(out.offset === Math.fround(Math.fround(Math.fround(142 / 10000) * 10) - 5), "offset reconstructs integer 142");
}

// ---- specified defaults: construction carries them; Zero* is the zero form ----
{
  const sample = new ex.ProbeSample();
  check(sample.active, "ProbeSample.active defaults true");
  ex.ZeroProbeSample(sample);
  check(sample.active === false, "the §5 zero form stays zero — Zero* does not reapply defaults");
  const config = new ex.ProbeConfig();
  check(config.retries === -1, "ProbeConfig.retries defaults -1");
  check(config.preferred === ex.Weapon.Railgun, "ProbeConfig.preferred defaults Railgun");
}

// ---- ProbeBits: the full-range uint32/uint64 paths, C++-pinned ----
{
  const inp = new ex.ProbeBits();
  inp.small = 0x1ff;
  inp.boundary = 0x1ffffffffn;
  inp.wide = 0xfedcba9876543210n;
  inp.sensor = 4294967295;
  inp.nonce = 18446744073709551615n;

  const ws = newWriteStream();
  check(ex.WriteProbeBits(ws, inp), "write ProbeBits");
  ws.flush();
  goldenWire("probebits", ws.data());

  const out = new ex.ProbeBits();
  const rs = new ReadStream(ws.data());
  check(ex.ReadProbeBits(rs, out), "read ProbeBits");
  check(deepEqual(out, inp), "ProbeBits round-trips — 9/33/64-bit and full-range paths");
}

// ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
// round trip, the None arm, an array of unions, and the refusal negative
// controls ----
{
  const inp = new ex.ProbeCollider();
  check(inp.shape.type === ex.ProbeShapeType.None, "construction is the empty union");
  check(ex.ProbeShapeMaxBits === 2 + 16, "MaxBits is tag + the largest arm");

  inp.armor = 7;
  inp.shape.type = ex.ProbeShapeType.Slab;
  inp.shape.slab.width = 42;
  inp.shape.slab.height = 9;
  // inp.Backup stays None — the empty arm costs the tag bits only
  inp.extrasCount = 1;
  inp.extras[0].type = ex.ProbeShapeType.Ring;
  inp.extras[0].ring.radius = 777;

  const ws = newWriteStream();
  check(ex.WriteProbeCollider(ws, inp), "write ProbeCollider");
  ws.flush();
  goldenWire("probecollider", ws.data());

  const out = new ex.ProbeCollider();
  out.backup.type = ex.ProbeShapeType.Ring; // dirty — the read must restore None
  const rs = new ReadStream(ws.data());
  check(ex.ReadProbeCollider(rs, out), "read ProbeCollider");
  check(out.armor === 7 && out.shape.type === ex.ProbeShapeType.Slab &&
        out.shape.slab.width === 42 && out.shape.slab.height === 9,
        "the selected arm round-trips");
  check(out.backup.type === ex.ProbeShapeType.None, "the None arm reads back empty");
  check(out.extrasCount === 1 && out.extras[0].type === ex.ProbeShapeType.Ring &&
        out.extras[0].ring.radius === 777, "the union array round-trips");

  // NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
  // [0, 2]; forcing both bits makes 3 and the read must refuse
  const corrupt = Uint8Array.from(ws.data());
  corrupt[1] |= 0x03;
  const bad = new ex.ProbeCollider();
  check(!ex.ReadProbeCollider(new ReadStream(corrupt), bad),
        "an out-of-range union tag is refused (SPEC §4.8)");

  // NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at bit
  // offset 10 with range [0, 100]; all seven bits decode 127
  const corrupt2 = Uint8Array.from(ws.data());
  corrupt2[1] |= 0xfc;
  corrupt2[2] |= 0x01;
  check(!ex.ReadProbeCollider(new ReadStream(corrupt2), bad),
        "a corrupt union arm payload is refused (SPEC §4.8)");

  // the write side validates the tag BEFORE it rides
  const rogue = new ex.ProbeShape();
  rogue.type = 3;
  const ws2 = newWriteStream();
  check(!ex.WriteProbeShape(ws2, rogue), "an out-of-set union tag writes nothing (SPEC §4.8)");
}

// ---- TestData and InputPacket against their C++ pins ----
{
  const inp = testDataInstance();
  const ws = newWriteStream();
  check(ex.WriteTestData(ws, inp), "write TestData (pin)");
  ws.flush();
  goldenWire("testdata", ws.data());

  const packet = makeInputPacket();
  const ws2 = newWriteStream();
  check(ex.WriteInputPacket(ws2, packet), "write InputPacket (pin)");
  ws2.flush();
  goldenWire("inputpacket", ws2.data());
}

// ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
{
  const inp = new ex.ProbeSample(); // active = true
  inp.orientation = 90.0;
  inp.rawDelta = -5;
  inp.bigDelta = -1234567890123n;
  inp.weapon = ex.Weapon.Laser;
  inp.hasTarget = true;
  inp.targetId = 777;
  inp.idleTicks = 12345; // untaken side on the wire — must read back ZERO
  inp.samplesCount = 1;
  inp.samples[0] = 42;

  const ws = newWriteStream();
  check(ex.WriteProbeSample(ws, inp), "write ProbeSample active");
  ws.flush();
  const out = new ex.ProbeSample();
  const rs = new ReadStream(ws.data());
  check(ex.ReadProbeSample(rs, out), "read ProbeSample active");
  check(out.active && out.weapon === ex.Weapon.Laser && out.hasTarget && out.targetId === 777,
    "the taken branch round-trips, nested branch included");
  check(out.idleTicks === 0, "the untaken else side reads as zero (SPEC §5)");
  check(out.orientation === 90.0, "compressed float round-trips exactly at its resolution");

  inp.active = false;
  inp.hasTarget = false;
  const ws2 = newWriteStream();
  check(ex.WriteProbeSample(ws2, inp), "write ProbeSample idle");
  ws2.flush();
  const rs2 = new ReadStream(ws2.data());
  check(ex.ReadProbeSample(rs2, out), "read ProbeSample idle");
  check(!out.active && out.idleTicks === 12345, "the else branch round-trips");
  check(out.weapon === ex.Weapon.None && !out.hasTarget && out.targetId === 0,
    "the whole untaken then side reads as zero, nested branch included (SPEC §5)");
}

// ---- ProbeArray: transitive defaults and its C++ pin ----
{
  const fresh = new ex.ProbeArray();
  check(fresh.samples[0].active && fresh.samples[1].active, "defaults reach through a fixed array");
  check(fresh.config.retries === -1 && fresh.config.preferred === ex.Weapon.Railgun,
    "defaults reach through a plain member");

  const inp = new ex.ProbeArray();
  inp.samples[0].orientation = 90.0;
  inp.samples[0].rawDelta = -5;
  inp.samples[0].bigDelta = -1234567890123n;
  inp.samples[0].weapon = ex.Weapon.Laser;
  inp.samples[0].hasTarget = true;
  inp.samples[0].targetId = 777;
  inp.samples[0].samplesCount = 1;
  inp.samples[0].samples[0] = 42;
  inp.samples[1].active = false;
  inp.samples[1].orientation = -45.5;
  inp.samples[1].rawDelta = 7;
  inp.samples[1].bigDelta = 99n;
  inp.samples[1].idleTicks = 1000;
  inp.samples[1].samplesCount = 2;
  inp.samples[1].samples[0] = 7;
  inp.samples[1].samples[1] = 8;
  inp.config.retries = 3;
  inp.config.preferred = ex.Weapon.Missile;

  const ws = newWriteStream();
  check(ex.WriteProbeArray(ws, inp), "write ProbeArray");
  ws.flush();
  goldenWire("probearray", ws.data());

  const out = new ex.ProbeArray();
  const rs = new ReadStream(ws.data());
  check(ex.ReadProbeArray(rs, out), "read ProbeArray");
  check(!out.samples[1].active && out.samples[1].idleTicks === 1000, "nested else branch round-trips");
  check(out.samples[1].weapon === ex.Weapon.None && !out.samples[1].hasTarget,
    "nested untaken side reads as zero (SPEC §5)");
  check(out.config.retries === 3 && out.config.preferred === ex.Weapon.Missile, "config round-trips");
}

// ---- ProbeReport: nested composition, and the widened flags wire ----
{
  const inp = new ex.ProbeReport();
  inp.header.version = 3;
  inp.header.probeId = 0xcafebaben;
  inp.flags = ex.ProbeFlagsArmed | ex.ProbeFlagsDamaged;
  inp.echo.testA = 555;
  inp.echo.testB = 1000;

  const ws = newWriteStream();
  check(ex.WriteProbeReport(ws, inp), "write ProbeReport");
  ws.flush();
  const out = new ex.ProbeReport();
  const rs = new ReadStream(ws.data());
  check(ex.ReadProbeReport(rs, out), "read ProbeReport");
  check(deepEqual(out, inp), "ProbeReport round-trips — a named type as an ordinary field");

  // a mask bit above the widened 8-bit wire is refused, not truncated
  inp.flags = 1n << 9n;
  const ws2 = newWriteStream();
  check(!ex.WriteProbeReport(ws2, inp), "a mask bit above the flags wire width is refused");
}

// ---- Block: the bytes(N) framing ----
{
  const inp = new ex.Block();
  for (let i = 0; i < 100; i++) {
    inp.data[i] = i;
  }
  inp.dataLength = 100;

  const ws = newWriteStream();
  check(ex.WriteBlock(ws, inp), "write Block");
  ws.flush();
  const out = new ex.Block();
  const rs = new ReadStream(ws.data());
  check(ex.ReadBlock(rs, out), "read Block");
  check(deepEqual(out, inp), "Block round-trips — bytes(N) framing");
}

// ---- the readers agree on what they REJECT (goal 3's second half) ----
// The two refusal channels: generated validation returns false and latches
// NOTHING (stream.error stays None); stream failures latch on the stream —
// the JS split of Go's ErrValidation-vs-stream-error distinction.
{
  // an interior null in a string is content the read refuses
  const chatGolden = readFileSync("../../testdata/wire/chat.bin");
  const corrupt = Uint8Array.from(chatGolden);
  corrupt[4] = 0; // inside the text bytes (length rides bytes 0-1, align pads to byte 2)
  const out = new ex.Chat();
  const rs = new ReadStream(corrupt);
  check(!ex.ReadChat(rs, out) && rs.error === null, "an interior null is rejected as validation");

  // a truncated stream is the stream's own error, never a content verdict
  const truncated = chatGolden.subarray(0, 3);
  const out2 = new ex.Chat();
  const rs2 = new ReadStream(truncated);
  check(!ex.ReadChat(rs2, out2) && rs2.error !== null, "truncation surfaces as the stream error");

  // a nonzero reserved bit is rejected
  const probeGolden = readFileSync("../../testdata/wire/probe_header.bin");
  const corrupt2 = Uint8Array.from(probeGolden);
  corrupt2[1] |= 0x08; // the first reserved bit above version's 3
  const out3 = new ex.ProbeHeader();
  const rs3 = new ReadStream(corrupt2);
  check(!ex.ReadProbeHeader(rs3, out3) && rs3.error === null, "a nonzero reserved bit is rejected");

  // an out-of-range array count is refused before any element rides —
  // corrupt the count bits INSIDE a complete valid wire (the preamble is
  // 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4),
  // so the refusal is the RANGE check, not a truncation overflow
  const packetGolden = readFileSync("../../testdata/wire/inputpacket.bin");
  const corrupt3 = Uint8Array.from(packetGolden);
  corrupt3[18] = (corrupt3[18] & ~0x1f) | 17; // count 2 -> 17, over [0, 16]
  const out4 = new ex.InputPacket();
  const rs4 = new ReadStream(corrupt3);
  check(!ex.ReadInputPacket(rs4, out4) && rs4.error !== null, "an out-of-range count is refused before the loop");
}

// ---- RigidBody: the moving branch read back whole ----
{
  const inp = new ex.RigidBody();
  inp.position.x = 1.5;
  inp.position.y = -2.5;
  inp.position.z = 3.25;
  inp.orientation.x = 0.1;
  inp.orientation.y = 0.2;
  inp.orientation.z = 0.3;
  inp.orientation.w = 0.9;
  inp.atRest = false;
  inp.linearVelocity.x = 10.0;
  inp.linearVelocity.y = 20.0;
  inp.linearVelocity.z = -3.0;
  inp.angularVelocity.x = 0.25;
  inp.angularVelocity.y = 0.5;
  inp.angularVelocity.z = 0.75;

  const ws = newWriteStream();
  check(ex.WriteRigidBody(ws, inp), "write RigidBody moving (read-back)");
  ws.flush();
  const out = new ex.RigidBody();
  const rs = new ReadStream(ws.data());
  check(ex.ReadRigidBody(rs, out), "read RigidBody moving");
  check(deepEqual(out, inp), "the moving branch round-trips with velocities intact");
}

// ================= THE BENCH CORPUS (BENCH-STANDARD.md §1.5) =================
// The same pinned instances test/bench/main.cpp authored into testdata/wire/
// {bench_*,real_packet}.bin — the oracle gate every bench runner carries. The
// JS leg gates them here (the other checked-runtime legs only build their
// bench corpus; the JS bench runner imports these exact modules, so the leg
// carries the oracle): write -> golden -> read -> re-write -> byte-compare,
// and the documented wire size checked against reality.

function pinShape(name, expectedBytes, mod, shape, inp) {
  const ws = newWriteStream();
  check(mod[`Write${shape}`](ws, inp), `write ${shape}`);
  ws.flush();
  const wire = ws.data();
  check(wire.length === expectedBytes, `${name} is ${expectedBytes} bytes`);
  goldenWire(name, wire);

  const out = new mod[shape]();
  const rs = new ReadStream(wire);
  check(mod[`Read${shape}`](rs, out), `read ${shape}`);
  const ws2 = newWriteStream();
  check(mod[`Write${shape}`](ws2, out), `re-write ${shape}`);
  ws2.flush();
  check(bytesEqual(ws2.data(), wire), `${shape} round-trips to identical bytes`);
}

{
  const packet = new bench.BenchPacket(); // serialize/bench.cpp BenchPacket::Init(), verbatim
  packet.a = -37;
  packet.b = 12345;
  packet.c = 987654;
  packet.bits7 = 97;
  packet.bits13 = 5000;
  packet.bits23 = 1234567;
  packet.flag = true;
  packet.x = 1.5;
  packet.y = -3.25;
  packet.z = 100.125;
  packet.big = 0x123456789abcdef0n;
  for (let i = 0; i < 17; i++) {
    packet.blob[i] = (i * 31) & 0xff;
  }
  pinShape("bench_packet", 49, bench, "BenchPacket", packet);

  const ints = new bench.BenchInts();
  ints.f0 = -37;
  ints.f1 = 12345;
  ints.f2 = 987654;
  ints.f3 = 2;
  ints.f4 = -15;
  ints.f5 = 777;
  ints.f6 = -2048;
  ints.f7 = 200;
  ints.f8 = -543210;
  ints.f9 = 99;
  pinShape("bench_ints", 14, bench, "BenchInts", ints);

  const bits = new bench.BenchBits();
  bits.b7 = 97;
  bits.b13 = 5000;
  bits.b23 = 1234567;
  bits.b3 = 5;
  bits.b32 = 0xdeadbeef;
  bits.b11 = 1024;
  bits.b19 = 333333;
  bits.b48 = 0xfedcba987654n;
  pinShape("bench_bits", 20, bench, "BenchBits", bits);

  const mixed = makeBenchMixedPin();
  pinShape("bench_mixed", 438, bench, "BenchMixed", mixed);

  // RealPacket pins the ALL-DEFAULTS instance: constructed and serialized
  // unmodified, every field at its declared default — 1629 bits = 204 bytes
  pinShape("real_packet", 204, realworld, "RealPacket", new realworld.RealPacket());
}

// ======================= THE FLAT TIER (§7.2 + design §5) =======================
// The shipped JS wire path: every pinned instance above rides again through
// the FLAT writer and reader — byte-compared against the same C++-pinned
// goldens AND against the runtime tier (the cross-tier equivalence gate:
// bytes, fields and verdicts), both directions. The leg runs in BOTH runtime
// modes (the Makefile's NODE_ENV pair), and both modes compare to the same
// goldens — so the checked and production flat writers are held
// byte-identical transitively, every run.

const CHECKED_MODE = process.env.NODE_ENV !== "production";

// flatCross: runtime write -> flat write byte-compare (+ golden), flat read
// vs runtime read field-compare, flat re-write byte-compare.
function flatCross(name, mod, flatMod, shape, inp, golden) {
  const ws = new WriteStream(new Uint8Array(4096));
  if (!mod[`Write${shape}`](ws, inp)) {
    check(false, `${name}: runtime write failed`);
    return;
  }
  ws.flush();
  const rtBytes = Uint8Array.from(ws.data());

  const fbuf = new Uint8Array(4096);
  const fview = new DataView(fbuf.buffer);
  const n = flatMod[`Write${shape}Flat`](fview, inp);
  check(n === rtBytes.length, `${name}: flat write length ${n} vs runtime ${rtBytes.length}`);
  check(bytesEqual(fbuf.subarray(0, rtBytes.length), rtBytes), `${name}: flat bytes === runtime bytes`);
  if (golden) {
    goldenWire(golden, fbuf.subarray(0, rtBytes.length));
  }

  const rtOut = new mod[shape]();
  check(mod[`Read${shape}`](new ReadStream(rtBytes), rtOut), `${name}: runtime read`);
  const flOut = new mod[shape]();
  const rbuf = new Uint8Array(rtBytes.length + 8); // FLAT_READ_SLACK
  rbuf.set(rtBytes);
  check(flatMod[`Read${shape}Flat`](new DataView(rbuf.buffer), rtBytes.length * 8, flOut), `${name}: flat read verdict`);
  check(deepEqual(flOut, rtOut), `${name}: flat read fields === runtime read fields`);

  const n2 = flatMod[`Write${shape}Flat`](fview, flOut);
  check(n2 === rtBytes.length && bytesEqual(fbuf.subarray(0, rtBytes.length), rtBytes),
    `${name}: flat round-trips to identical bytes`);
}

{
  const inp = new ex.ShipCreate();
  inp.shipType = ex.ShipType.Bomber;
  inp.position.x = 1000;
  inp.position.y = -2000;
  inp.position.z = 3000;
  inp.hasFlags = true;
  inp.flags = ex.ShipFlagsBoosting | ex.ShipFlagsAiming;
  inp.team = ex.Team.Blue;
  inp.health = 750;
  inp.thrust = 55;
  flatCross("flat shipcreate", ex, exFlat, "ShipCreate", inp, "shipcreate_flags");
  inp.hasFlags = false;
  flatCross("flat shipcreate-noflags", ex, exFlat, "ShipCreate", inp, null);

  // §5 branch zeroing through the flat reader, into a dirty instance
  const fbuf = new Uint8Array(4096);
  const n = exFlat.WriteShipCreateFlat(new DataView(fbuf.buffer), inp);
  const dirty = new ex.ShipCreate();
  dirty.flags = 0xfn;
  check(exFlat.ReadShipCreateFlat(new DataView(fbuf.buffer), n * 8, dirty), "flat read no-flags");
  check(!dirty.hasFlags && dirty.flags === 0n, "flat untaken branch reads as zero (SPEC §5)");
}
{
  const inp = new ex.RigidBody();
  inp.position.x = 1.5;
  inp.position.y = -2.5;
  inp.position.z = 3.25;
  inp.orientation.x = 0.1;
  inp.orientation.y = 0.2;
  inp.orientation.z = 0.3;
  inp.orientation.w = 0.9;
  inp.atRest = false;
  inp.linearVelocity.x = 10.0;
  inp.linearVelocity.y = 20.0;
  inp.linearVelocity.z = -3.0;
  inp.angularVelocity.x = 0.25;
  inp.angularVelocity.y = 0.5;
  inp.angularVelocity.z = 0.75;
  flatCross("flat rigidbody_moving", ex, exFlat, "RigidBody", inp, "rigidbody_moving");
  inp.atRest = true;
  flatCross("flat rigidbody_at_rest", ex, exFlat, "RigidBody", inp, "rigidbody_at_rest");
}
{
  const inp = new ex.Chat();
  inp.text.set(textBytes("wire parity"));
  inp.textLength = 11;
  flatCross("flat chat", ex, exFlat, "Chat", inp, "chat");
}
{
  const inp = new ex.ProbeHeader();
  inp.version = 5;
  inp.probeId = 0x1122334455667788n;
  flatCross("flat probe_header", ex, exFlat, "ProbeHeader", inp, "probe_header");
}
flatCross("flat inputpacket", ex, exFlat, "InputPacket", makeInputPacket(), "inputpacket");
flatCross("flat testdata", ex, exFlat, "TestData", testDataInstance(), "testdata");
{
  const inp = new ex.CompressedProbe();
  inp.boundary = 0.005;
  inp.offset = -4.8585;
  flatCross("flat compressed_probe", ex, exFlat, "CompressedProbe", inp, "compressed_probe");
}
{
  const inp = new ex.ProbeBits();
  inp.small = 0x1ff;
  inp.boundary = 0x1ffffffffn;
  inp.wide = 0xfedcba9876543210n;
  inp.sensor = 4294967295;
  inp.nonce = 18446744073709551615n;
  flatCross("flat probebits", ex, exFlat, "ProbeBits", inp, "probebits");
}
{
  const inp = new ex.ProbeSample();
  inp.orientation = 90.0;
  inp.rawDelta = -5;
  inp.bigDelta = -1234567890123n;
  inp.weapon = ex.Weapon.Laser;
  inp.hasTarget = true;
  inp.targetId = 777;
  inp.idleTicks = 12345;
  inp.samplesCount = 1;
  inp.samples[0] = 42;
  flatCross("flat probesample-active", ex, exFlat, "ProbeSample", inp, null);
  inp.active = false;
  inp.hasTarget = false;
  flatCross("flat probesample-idle", ex, exFlat, "ProbeSample", inp, null);
}
{
  const inp = new ex.ProbeArray();
  inp.samples[0].orientation = 90.0;
  inp.samples[0].rawDelta = -5;
  inp.samples[0].bigDelta = -1234567890123n;
  inp.samples[0].weapon = ex.Weapon.Laser;
  inp.samples[0].hasTarget = true;
  inp.samples[0].targetId = 777;
  inp.samples[0].samplesCount = 1;
  inp.samples[0].samples[0] = 42;
  inp.samples[1].active = false;
  inp.samples[1].orientation = -45.5;
  inp.samples[1].rawDelta = 7;
  inp.samples[1].bigDelta = 99n;
  inp.samples[1].idleTicks = 1000;
  inp.samples[1].samplesCount = 2;
  inp.samples[1].samples[0] = 7;
  inp.samples[1].samples[1] = 8;
  inp.config.retries = 3;
  inp.config.preferred = ex.Weapon.Missile;
  flatCross("flat probearray", ex, exFlat, "ProbeArray", inp, "probearray");
}
{
  const inp = new ex.ProbeReport();
  inp.header.version = 3;
  inp.header.probeId = 0xcafebaben;
  inp.flags = ex.ProbeFlagsArmed | ex.ProbeFlagsDamaged;
  inp.echo.testA = 555;
  inp.echo.testB = 1000;
  flatCross("flat probereport", ex, exFlat, "ProbeReport", inp, null);
}
{
  const inp = new ex.Block();
  for (let i = 0; i < 100; i++) {
    inp.data[i] = i + 1;
  }
  inp.dataLength = 100;
  flatCross("flat block", ex, exFlat, "Block", inp, null);
}
{
  // unions through the flat tier — including the shape that convicted the
  // union-as-static run-fusing defect: every arm None, so the actual wire is
  // far shorter than MaxBits and a fused bound counting MaxBitsUnion would
  // refuse valid wire
  const inp = new ex.ProbeCollider();
  inp.armor = 7;
  flatCross("flat probecollider-none", ex, exFlat, "ProbeCollider", inp, null);
  inp.shape.type = ex.ProbeShapeType.Slab;
  inp.shape.slab.width = 42;
  inp.shape.slab.height = 9;
  inp.extrasCount = 1;
  inp.extras[0].type = ex.ProbeShapeType.Ring;
  inp.extras[0].ring.radius = 777;
  flatCross("flat probecollider", ex, exFlat, "ProbeCollider", inp, "probecollider");
}

// a union on its own: the flat tier carries the runtime tier's standalone
// Write<Union>/Read<Union> pair, every arm and None
{
  const shape = new ex.ProbeShape();
  flatCross("flat probeshape-none", ex, exFlat, "ProbeShape", shape, null);
  shape.type = ex.ProbeShapeType.Ring;
  shape.ring.radius = 777;
  flatCross("flat probeshape-ring", ex, exFlat, "ProbeShape", shape, null);
  shape.type = ex.ProbeShapeType.Slab;
  shape.slab.width = 42;
  shape.slab.height = 9;
  flatCross("flat probeshape-slab", ex, exFlat, "ProbeShape", shape, null);
  const choice = new armDefaults.DefaultChoice();
  choice.type = armDefaults.DefaultChoiceType.Second;
  choice.second.entriesCount = 1;
  choice.second.marker = 5;
  flatCross("flat defaultchoice", armDefaults, armDefaultsFlat, "DefaultChoice", choice, null);
  const rogue = new ex.ProbeShape();
  rogue.type = 3;
  if (CHECKED_MODE) {
    check(exFlat.WriteProbeShapeFlat(new DataView(new ArrayBuffer(64)), rogue) === -1,
      "the checked flat writer refuses an out-of-set union tag (SPEC §4.8)");
  }
  const tagOnly = new Uint8Array(8 + 8);
  tagOnly[0] = 3; // a two-bit tag of 3 is past the count
  check(!exFlat.ReadProbeShapeFlat(new DataView(tagOnly.buffer), 2, new ex.ProbeShape()),
    "the flat reader refuses an out-of-set union tag (SPEC §4.8)");
}

// ---- the bench-corpus pins through flat (the §1.5 oracle instances) ----
{
  const packet = new bench.BenchPacket();
  packet.a = -37;
  packet.b = 12345;
  packet.c = 987654;
  packet.bits7 = 97;
  packet.bits13 = 5000;
  packet.bits23 = 1234567;
  packet.flag = true;
  packet.x = 1.5;
  packet.y = -3.25;
  packet.z = 100.125;
  packet.big = 0x123456789abcdef0n;
  for (let i = 0; i < 17; i++) {
    packet.blob[i] = (i * 31) & 0xff;
  }
  flatCross("flat bench_packet", bench, benchFlat, "BenchPacket", packet, "bench_packet");

  const ints = new bench.BenchInts();
  ints.f0 = -37;
  ints.f1 = 12345;
  ints.f2 = 987654;
  ints.f3 = 2;
  ints.f4 = -15;
  ints.f5 = 777;
  ints.f6 = -2048;
  ints.f7 = 200;
  ints.f8 = -543210;
  ints.f9 = 99;
  flatCross("flat bench_ints", bench, benchFlat, "BenchInts", ints, "bench_ints");

  const bits = new bench.BenchBits();
  bits.b7 = 97;
  bits.b13 = 5000;
  bits.b23 = 1234567;
  bits.b3 = 5;
  bits.b32 = 0xdeadbeef;
  bits.b11 = 1024;
  bits.b19 = 333333;
  bits.b48 = 0xfedcba987654n;
  flatCross("flat bench_bits", bench, benchFlat, "BenchBits", bits, "bench_bits");

  const mixed = makeBenchMixedPin();
  flatCross("flat bench_mixed", bench, benchFlat, "BenchMixed", mixed, "bench_mixed");

  flatCross("flat real_packet", realworld, realworldFlat, "RealPacket", new realworld.RealPacket(), "real_packet");
}

// ---- refusal vectors THROUGH the flat reader (refusal rules are format) ----
{
  const withSlack = (bytes) => {
    const b = new Uint8Array(bytes.length + 8);
    b.set(bytes);
    return b;
  };

  // an interior null in a string is content the read refuses
  const chatGolden = readFileSync("../../testdata/wire/chat.bin");
  const corrupt = withSlack(chatGolden);
  corrupt[4] = 0;
  const out = new ex.Chat();
  check(!exFlat.ReadChatFlat(new DataView(corrupt.buffer), chatGolden.length * 8, out),
    "flat: an interior null is rejected");

  // a truncated stream is refused by the fused bounds checks
  check(!exFlat.ReadChatFlat(new DataView(withSlack(chatGolden.subarray(0, 3)).buffer), 3 * 8, out),
    "flat: truncation is refused");

  // a corrupted wire constant and a nonzero reserved bit are rejected
  const probeGolden = readFileSync("../../testdata/wire/probe_header.bin");
  const c2 = withSlack(probeGolden);
  c2[0] = 0xac;
  const out3 = new ex.ProbeHeader();
  check(!exFlat.ReadProbeHeaderFlat(new DataView(c2.buffer), probeGolden.length * 8, out3),
    "flat: a corrupted wire constant is REJECTED (SPEC §4.3)");
  c2[0] = 0xab;
  c2[1] |= 0x08;
  check(!exFlat.ReadProbeHeaderFlat(new DataView(c2.buffer), probeGolden.length * 8, out3),
    "flat: a nonzero reserved bit is rejected");

  // an out-of-range array count is refused before any element rides
  const packetGolden = readFileSync("../../testdata/wire/inputpacket.bin");
  const c3 = withSlack(packetGolden);
  c3[18] = (c3[18] & ~0x1f) | 17;
  const out4 = new ex.InputPacket();
  check(!exFlat.ReadInputPacketFlat(new DataView(c3.buffer), packetGolden.length * 8, out4),
    "flat: an out-of-range count is refused before the loop");
}

// ---- checked-mode write refusals: verdict-identical across tiers ----
// (production mode: the flat writer is the trusted-writer release shape and
// these vectors are caller misuse by contract — asserted in checked runs
// only, which the Makefile's mode pair guarantees happen)
if (CHECKED_MODE) {
  const fview = new DataView(new Uint8Array(4096).buffer);

  const bad = new ex.ShipCreate();
  bad.health = 5000; // above [0, MaxHealth]
  const ws = newWriteStream();
  check(!ex.WriteShipCreate(ws, bad) && exFlat.WriteShipCreateFlat(fview, bad) === -1,
    "both tiers refuse an out-of-range ranged write");

  const bad2 = new ex.ProbeReport();
  bad2.flags = 1n << 9n;
  const ws2 = newWriteStream();
  check(!ex.WriteProbeReport(ws2, bad2) && exFlat.WriteProbeReportFlat(fview, bad2) === -1,
    "both tiers refuse a mask bit above the flags wire width");

  const bad3 = new ex.Chat();
  bad3.textLength = 999; // above string(MaxChatLength)
  const ws3 = newWriteStream();
  check(!ex.WriteChat(ws3, bad3) && exFlat.WriteChatFlat(fview, bad3) === -1,
    "both tiers refuse an out-of-range string length");

  const bad4 = new ex.InputPacket();
  bad4.inputsCount = 17; // above [0, MaxInputsPerPacket]
  const ws4 = newWriteStream();
  check(!ex.WriteInputPacket(ws4, bad4) && exFlat.WriteInputPacketFlat(fview, bad4) === -1,
    "both tiers refuse an out-of-range array count");

  const bad5 = new ex.ShipCreate();
  bad5.shipType = 99; // enum headroom above the wire range
  const ws5 = newWriteStream();
  check(!ex.WriteShipCreate(ws5, bad5) && exFlat.WriteShipCreateFlat(fview, bad5) === -1,
    "both tiers refuse enum headroom above the wire range");
}

// ---- FlagName / FlagNames: per-bit names and the set renderer ----
{
  check(ex.FlagNameShipFlags(0) === "FiringLaser", "FlagName names bit 0");
  check(ex.FlagNameShipFlags(9) === "???", "FlagName is out-of-range safe");
  check(ex.FlagNamesShipFlags(0n) === "0", "FlagNames renders the empty set as 0");
  check(ex.FlagNamesShipFlags(ex.ShipFlagsFiringLaser | ex.ShipFlagsBraking) === "FiringLaser|Braking",
    "FlagNames renders the set bits");
  check(ex.FlagNamesShipFlags(ex.ShipFlagsAiming | (1n << 63n)) === "Aiming|0x8000000000000000",
    "FlagNames renders unknown high bits as hex");
}

// ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
//
// Twelve shapes written back to back into ONE runtime-tier stream against
// the one C++-pinned golden, in the C++ test's order, then each shape
// crossed through the flat tier. A fixed scalar array whose elements an
// emitter places TWICE is invisible to a same-language round trip; only the
// byte compare against another language's bytes names it.
{
  const dg = degenerate;

  const vec2 = new dg.Vec2();
  vec2.x = 1.5;
  vec2.y = -2.25;

  const spanF64 = new dg.SpanF64();
  spanF64.values[0] = 3.5;
  spanF64.values[1] = -4.75;

  const spanU64 = new dg.SpanU64();
  spanU64.values[0] = 0xdeadbeefcafebaben;
  spanU64.values[1] = 1n;

  const spanI64 = new dg.SpanI64();
  spanI64.values[0] = -1234567890123n;
  spanI64.values[1] = 42n;

  const spanOne = new dg.SpanOne();
  spanOne.values[0] = 0x0123456789abcdefn;

  const spanChunk = new dg.SpanChunk();
  spanChunk.values[0] = 0x1111;
  spanChunk.values[1] = 0x2222;
  spanChunk.values[2] = 0x3333;
  spanChunk.values[3] = 0x4444;

  const spanTail = new dg.SpanTail();
  spanTail.values[0] = 6.125;
  spanTail.values[1] = -7.0;
  spanTail.tail = 0xfeedface;

  const spanTwice = new dg.SpanTwice();
  spanTwice.a[0] = 8.5;
  spanTwice.a[1] = 9.5;
  spanTwice.b[0] = -10.5;
  spanTwice.b[1] = -11.5;

  const trio = new dg.Trio();
  trio.a = 0xabcde;
  trio.b = 0x12345;
  trio.c = 0xfffff;

  const trioSole = new dg.TrioSole();
  trioSole.inner.a = 1;
  trioSole.inner.b = 2;
  trioSole.inner.c = 3;

  const trioFirst = new dg.TrioFirst();
  trioFirst.inner.a = 0xaaaaa;
  trioFirst.inner.b = 0x55555;
  trioFirst.inner.c = 0xf0f0f;
  trioFirst.trailer = 0xbeef;

  const straddle = new dg.TrioStraddle();
  straddle.pad0 = 0x0011223344556677n;
  straddle.pad1 = 0x8899aabbccddeeffn;
  straddle.pad2 = 0xffffffffffffffffn;
  straddle.pad3 = 0n;
  straddle.pad4 = 0x123456789abcdef0n;
  straddle.pad5 = 0xabcdef;
  straddle.inner.a = 0x11111;
  straddle.inner.b = 0x22222;
  straddle.inner.c = 0x33333;

  const shapes = [
    ["Vec2", vec2],
    ["SpanF64", spanF64],
    ["SpanU64", spanU64],
    ["SpanI64", spanI64],
    ["SpanOne", spanOne],
    ["SpanChunk", spanChunk],
    ["SpanTail", spanTail],
    ["SpanTwice", spanTwice],
    ["Trio", trio],
    ["TrioSole", trioSole],
    ["TrioFirst", trioFirst],
    ["TrioStraddle", straddle],
  ];

  const ws = newWriteStream();
  for (const [name, inp] of shapes) {
    check(dg[`Write${name}`](ws, inp), `write ${name}`);
  }
  check(ws.bitsProcessed() === 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
    "the twelve degenerate shapes ride their declared widths and nothing more");
  ws.flush();
  goldenWire("degenerate", ws.data());

  const rs = new ReadStream(ws.data());
  for (const [name, inp] of shapes) {
    const out = new dg[name]();
    check(dg[`Read${name}`](rs, out), `read ${name}`);
    check(deepEqual(out, inp), `${name} round-trips`);
  }

  // and every shape through the flat tier, held to the runtime tier's bytes
  for (const [name, inp] of shapes) {
    flatCross(`flat ${name.toLowerCase()}`, dg, degenerateFlat, name, inp, null);
  }
}

// ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
//
// Degenerate.schema is every-type-a-whole-number-of-bytes by construction,
// so no clause boundary in it lands mid-byte. These two units are chosen so
// they do. Each shape is written to its OWN stream and flushed, and the
// golden is those concatenated — the shapes are not byte-aligned, so a
// shared stream would not equal the concatenation every emitter can produce.
// Every shape also crosses the flat tier against the runtime tier's bytes.
{
  const seq = (n) => Array.from({ length: n }, (_, i) => i);

  // [shapeName, typeName, value, bits, expectedAfterRoundTrip]
  const cl = clauses;
  const clauseShapes = [];
  const w13At = (c) => {
    const v = new cl.W13();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = 8191 - i * 733;
    return v;
  };
  for (const c of [0, 1, 3, 4, 5, 7, 12]) clauseShapes.push([`W13/${c}`, "W13", w13At(c), 4 + 13 * c, w13At(c)]);

  const w17At = (c) => {
    const v = new cl.W17();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = 131071 - i * 11117;
    return v;
  };
  for (const c of [0, 1, 2, 3, 4, 9]) clauseShapes.push([`W17/${c}`, "W17", w17At(c), 4 + 17 * c, w17At(c)]);

  const w26At = (c) => {
    const v = new cl.W26();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = 67108863 - i * 5555555;
    return v;
  };
  for (const c of [0, 1, 2, 3, 6]) clauseShapes.push([`W26/${c}`, "W26", w26At(c), 3 + 26 * c, w26At(c)]);

  const w1At = (c) => {
    const v = new cl.W1();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = i % 2;
    return v;
  };
  for (const c of [0, 1, 3, 4, 5, 20]) clauseShapes.push([`W1/${c}`, "W1", w1At(c), 5 + c, w1At(c)]);

  const w52At = (c) => {
    const v = new cl.W52();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = 4503599627370495n - BigInt(i) * 123456789n;
    return v;
  };
  for (const c of [0, 1, 2, 3]) clauseShapes.push([`W52/${c}`, "W52", w52At(c), 2 + 52 * c, w52At(c)]);

  const w50At = (c) => {
    const v = new cl.W50();
    v.itemsCount = c;
    for (const i of seq(c)) v.items[i] = 1125899906842623n - BigInt(i) * 987654321n;
    return v;
  };
  for (const c of [0, 1, 2, 3]) clauseShapes.push([`W50/${c}`, "W50", w50At(c), 2 + 50 * c, w50At(c)]);

  const f13 = new cl.F13();
  for (const i of seq(7)) f13.items[i] = 8191 - i * 911;
  clauseShapes.push(["F13", "F13", f13, 91, f13]);

  const triAt = (c) => {
    const v = new cl.ArrTri3();
    v.itemsCount = c;
    for (const i of seq(c)) {
      v.items[i].a = i % 2;
      v.items[i].b = i % 4;
    }
    return v;
  };
  for (const c of [0, 1, 3, 4, 5, 10]) clauseShapes.push([`ArrTri3/${c}`, "ArrTri3", triAt(c), 4 + 3 * c, triAt(c)]);

  const arrEleven = new cl.ArrEleven();
  for (const i of seq(9)) {
    arrEleven.items[i].a = i % 8;
    arrEleven.items[i].b = 255 - i * 17;
  }
  clauseShapes.push(["ArrEleven", "ArrEleven", arrEleven, 99, arrEleven]);

  // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
  for (const [arm, tag] of [["None", cl.EmptyUnionType.None], ["A", cl.EmptyUnionType.A], ["B", cl.EmptyUnionType.B]]) {
    const v = new cl.HoldsEmptyUnion();
    v.lead = 21;
    v.tail = 99;
    v.u.type = tag;
    clauseShapes.push([`HoldsEmptyUnion/${arm}`, "HoldsEmptyUnion", v, 14, v]);
  }

  // lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
  // b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The 5-bit
  // lead is what puts the align at a non-zero offset.
  const strsAt = (s, b) => {
    const v = new cl.Strs();
    v.lead = 21;
    v.tail = 5;
    v.s.set(s);
    v.sLength = s.length;
    v.b.set(b);
    v.bLength = b.length;
    return v;
  };
  const strsSpecs = [
    ["empty", [], [], 27],
    ["full", [...Buffer.from("abcdefgh")], [0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7], 155],
    ["part", [...Buffer.from("xyz")], [1, 2, 3], 75],
  ];
  for (const [tag, s, b, bits] of strsSpecs) {
    const v = strsAt(s, b);
    clauseShapes.push([`Strs/${tag}`, "Strs", v, bits, v]);
  }

  const nestedAt = (c) => {
    const v = new cl.ArrNested();
    v.lead = 21;
    v.tail = 5;
    v.itemsCount = c;
    for (const i of seq(c)) {
      v.items[i].a = i % 8;
      v.items[i].b = 200 - i * 7;
    }
    return v;
  };
  for (const c of [0, 1, 2, 3, 4]) clauseShapes.push([`ArrNested/${c}`, "ArrNested", nestedAt(c), 11 + 11 * c, nestedAt(c)]);

  const sole = new cl.Sole();
  sole.only = 5555;
  clauseShapes.push(["Sole", "Sole", sole, 13, sole]);

  // Joins.schema. Every branch is written on BOTH arms, so no path is pinned
  // by omission — and the expected value after a round trip is not the value
  // written: the untaken side reads back as zero (SPEC §5).
  const jn = joins;
  const joinShapes = [];
  for (const f of [false, true]) {
    // the arms agree on WIDTH but not on value, so a join that keeps the
    // wrong arm is a value mismatch and not just a width one
    const agree = new jn.ArmsAgree();
    Object.assign(agree, { lead: 21, flag: f, a: 1234, b: 1500, tail: 99 });
    const agreeWant = new jn.ArmsAgree();
    Object.assign(agreeWant, { lead: 21, flag: f, a: f ? 1234 : 0, b: f ? 0 : 1500, tail: 99 });
    joinShapes.push([`ArmsAgree/${f}`, "ArmsAgree", agree, 24, agreeWant]);

    const disagree = new jn.ArmsDisagree();
    Object.assign(disagree, { lead: 21, flag: f, a: 1234, b: 5, tail: 99 });
    const disagreeWant = new jn.ArmsDisagree();
    Object.assign(disagreeWant, { lead: 21, flag: f, a: f ? 1234 : 0, b: f ? 0 : 5, tail: 99 });
    joinShapes.push([`ArmsDisagree/${f}`, "ArmsDisagree", disagree, f ? 24 : 16, disagreeWant]);

    const armEmpty = new jn.ArmEmpty();
    Object.assign(armEmpty, { lead: 21, flag: f, a: 456789, tail: 99 });
    const armEmptyWant = new jn.ArmEmpty();
    Object.assign(armEmptyWant, { lead: 21, flag: f, a: f ? 456789 : 0, tail: 99 });
    joinShapes.push([`ArmEmpty/${f}`, "ArmEmpty", armEmpty, f ? 32 : 13, armEmptyWant]);

    const alignStr = new jn.ArmAlign();
    Object.assign(alignStr, { lead: 21, flag: f, sLength: 4, b: 1000, tail: 99 });
    alignStr.s.set([...Buffer.from("abcd")]);
    const alignStrWant = new jn.ArmAlign();
    Object.assign(alignStrWant, { lead: 21, flag: f, tail: 99 });
    if (f) {
      alignStrWant.s.set([...Buffer.from("abcd")]);
      alignStrWant.sLength = 4;
    } else {
      alignStrWant.b = 1000;
    }
    joinShapes.push([`ArmAlign/${f}`, "ArmAlign", alignStr, f ? 55 : 23, alignStrWant]);

    const alignEmpty = new jn.ArmAlign();
    Object.assign(alignEmpty, { lead: 21, flag: f, b: 1000, tail: 99 });
    const alignEmptyWant = new jn.ArmAlign();
    Object.assign(alignEmptyWant, { lead: 21, flag: f, b: f ? 0 : 1000, tail: 99 });
    joinShapes.push([`ArmAlignEmptyStr/${f}`, "ArmAlign", alignEmpty, 23, alignEmptyWant]);
  }

  for (const o of [false, true]) {
    for (const i of [false, true]) {
      const v = new jn.ArmsNested();
      Object.assign(v, { lead: 5, outer: o, inner: i, x: 500000000, y: 17, z: 4000, tail: 33 });
      const want = new jn.ArmsNested();
      Object.assign(want, { lead: 5, outer: o, tail: 33 });
      if (o) {
        want.inner = i;
        if (i) want.x = 500000000;
        else want.y = 17;
      } else {
        want.z = 4000;
      }
      joinShapes.push([`ArmsNested/${o}${i}`, "ArmsNested", v, o ? (i ? 40 : 16) : 23, want]);
    }
  }

  for (const f of [false, true]) {
    for (const c of [0, 1, 2, 3]) {
      const v = new jn.ArmArray();
      Object.assign(v, { lead: 21, flag: f, itemsCount: c, b: 300, tail: 99 });
      for (const i of seq(c)) v.items[i] = 8191 - i * 777;
      const want = new jn.ArmArray();
      Object.assign(want, { lead: 21, flag: f, tail: 99 });
      if (f) {
        want.itemsCount = c;
        for (const i of seq(c)) want.items[i] = 8191 - i * 777;
      } else {
        want.b = 300;
      }
      joinShapes.push([`ArmArray/${f}/${c}`, "ArmArray", v, f ? 15 + 13 * c : 22, want]);
    }
  }

  // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
  for (const [arm, tag, bits] of [["None", jn.UnevenType.None, 18], ["N", jn.UnevenType.Narrow, 21], ["W", jn.UnevenType.Wide, 55]]) {
    const v = new jn.HoldsUneven();
    v.lead = 21;
    v.tail = 1500;
    v.u.type = tag;
    if (tag === jn.UnevenType.Narrow) v.u.narrow.n = 5;
    if (tag === jn.UnevenType.Wide) v.u.wide.w = 123456789012n;
    joinShapes.push([`HoldsUneven/${arm}`, "HoldsUneven", v, bits, v]);
  }

  // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
  const unevenItemBits = [0, 5, 44, 49];
  const arrUnevenAt = (c) => {
    const v = new jn.ArrUneven();
    v.lead = 21;
    v.tail = 5;
    v.itemsCount = c;
    for (const i of seq(c)) {
      if (i % 2 === 0) {
        v.items[i].type = jn.UnevenType.Narrow;
        v.items[i].narrow.n = i % 8;
      } else {
        v.items[i].type = jn.UnevenType.Wide;
        v.items[i].wide.w = 99887766554n + BigInt(i);
      }
    }
    return v;
  };
  for (const c of [0, 1, 2, 3]) joinShapes.push([`ArrUneven/${c}`, "ArrUneven", arrUnevenAt(c), 10 + unevenItemBits[c], arrUnevenAt(c)]);

  // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s, then a
  // 32 + 29 + 19 + 4 static run after the align regains it
  const regainAt = (c, sl) => {
    const v = new jn.RegainAfterAlign();
    Object.assign(v, { lead: 21, itemsCount: c, sLength: sl, p: 0xdeadbeef, q: (1 << 29) - 7, r: (1 << 19) - 3, tail: 9 });
    if (sl) v.s.set([...Buffer.from("wxyz")]);
    for (const i of seq(c)) v.items[i] = 8191 - i * 999;
    return v;
  };
  for (const c of [0, 1, 2, 3]) {
    for (const sl of [0, 4]) {
      const afterAlign = Math.ceil((5 + 2 + 13 * c + 3) / 8) * 8;
      joinShapes.push([`Regain/${c}/${sl}`, "RegainAfterAlign", regainAt(c, sl), afterAlign + 8 * sl + 84, regainAt(c, sl)]);
    }
  }

  // write each shape on its own stream, concatenate, byte-compare, read each
  // back out of its own slice, and cross every one through the flat tier
  const pinArrangements = (goldenName, mod, flatMod, shapes) => {
    const parts = [];
    for (const [shapeName, typeName, value, bits] of shapes) {
      const ws = newWriteStream();
      check(mod[`Write${typeName}`](ws, value), `write ${shapeName}`);
      check(ws.bitsProcessed() === bits, `${shapeName} rides its declared width`);
      ws.flush();
      const bytes = Uint8Array.from(ws.data());
      check(bytes.length === Math.ceil(bits / 8), `${shapeName} byte width`);
      parts.push(bytes);
    }
    const total = parts.reduce((n, p) => n + p.length, 0);
    const stream = new Uint8Array(total);
    let at = 0;
    for (const p of parts) {
      stream.set(p, at);
      at += p.length;
    }
    goldenWire(goldenName, stream);

    let off = 0;
    for (const [shapeName, typeName, , bits, want] of shapes) {
      const n = Math.ceil(bits / 8);
      const out = new mod[typeName]();
      check(mod[`Read${typeName}`](new ReadStream(stream.subarray(off, off + n)), out), `read ${shapeName}`);
      check(deepEqual(out, want), `${shapeName} round-trips (untaken sides zeroed, SPEC §5)`);
      off += n;
    }
    check(off === stream.length, `the ${goldenName} reads consume the whole golden`);

    for (const [shapeName, typeName, value] of shapes) {
      flatCross(`flat ${shapeName}`, mod, flatMod, typeName, value, null);
    }
  };

  pinArrangements("clauses", clauses, clausesFlat, clauseShapes);
  pinArrangements("joins", joins, joinsFlat, joinShapes);
}

if (failed) {
  process.exit(1);
}
console.log("OK");
