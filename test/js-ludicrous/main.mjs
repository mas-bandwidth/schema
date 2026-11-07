// The JavaScript cross-language wire test for the fixed-point + 128-bit unit
// (examples128/): the generated JS module writes the SAME pinned instance
// test/ludicrous_main.cpp pins in testdata/wire/ludicrous_state*.bin and
// byte-compares against those files — cross-language wire identity for the
// serialize-phase-1 families (fixed(I, F), int128, uint128) is the §7.2 gate
// this leg carries. Plus round-trips through the JS reader, the §5
// branch-zeroing check over a 128-bit field, the specified-defaults checks
// (one default no 64-bit literal can spell — a bare BigInt literal here),
// and the hostile-read rejections the C++ test carries (reject, never clamp
// — STANDARD.md).
//
// Prints OK and exits 0, exactly like its C++ and Go twins. Run from
// test/js-ludicrous (the Makefile does): the wire goldens are at
// ../../testdata/wire. Both runtime modes run in CI (checked, and
// NODE_ENV=production): the wire must be identical in both.
//
// Mirrors test/ludicrous_main.cpp block for block. 128-bit values are plain
// BigInt — JS needs no pair type, so Go's Int128{Lo, Hi} compositions
// collapse to literals.
import { readFileSync } from "node:fs";
import { WriteStream, ReadStream } from "../../../serialize.js/src/index.js";

import * as lud from "../../generated/js-ludicrous/Ludicrous.js";
import * as ludFlat from "../../generated/js-ludicrous/LudicrousFlat.js";

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
  return new WriteStream(new Uint8Array(256));
}

// setBits forces bits [pos, pos + n) of the stream image to 1 — the hostile
// reader's tool: smuggling an offset into the range's bit headroom, which a
// read must REJECT, never clamp (STANDARD.md).
function setBits(data, pos, n) {
  for (let i = pos; i < pos + n; i++) {
    data[(i / 8) | 0] |= 1 << i % 8;
  }
}

// makeState is test/ludicrous_main.cpp's make_state — the values must stay
// mirrored on both sides.
function makeState() {
  const inp = new lud.LudicrousState();
  inp.mode = lud.DriveMode.Ludicrous;
  inp.probe.angle = 2981888; // +45.5 * 2^16
  inp.probe.position = -809119744n; // -12346.1875 * 2^16
  inp.probe.reach = 65536000000n - 1n; // raw_max - 1
  inp.probe.ticks = 777777;
  inp.probe.samples[0] = -524288; // raw_min
  inp.probe.samples[1] = 524288; // raw_max
  inp.wide.entityId = 0x0123456789abcdeffedcba9876543210n;
  inp.wide.energy = 4999999999n;
  inp.wide.flux = (1n << 99n) + 7n;
  // wide.bias and wide.seed stay at their SPECIFIED DEFAULTS (-250 and
  // 2^65) — construction installs them, and they ride the wire as written
  inp.keysCount = 2;
  inp.keys[0] = 1n;
  inp.keys[1] = 1n << 127n;
  inp.hasTarget = true;
  inp.targetId = 42n;
  return inp;
}

// worst-case bounds, hand-derived (SPEC §6.1 item 4) — the same numbers
// test/ludicrous_main.cpp static_asserts
check(lud.FixedProbeMaxBits === 156, "FixedProbe worst case");
check(lud.WideProbeMaxBits === 403, "WideProbe worst case");
check(lud.LudicrousStateMaxBits === 1205, "LudicrousState worst case");
check(lud.ProtocolId !== 0n, "the unit has a protocol id");

// zero initialization with specified defaults (SPEC §4.2), sentinel-zero
// composition: construction starts at DriveMode None — the null rides
// in-band — and the two defaulted 128-bit fields construct to their declared
// values, one of which no 64-bit literal can spell
{
  const zero = new lud.LudicrousState();
  check(zero.mode === lud.DriveMode.None, "a fresh state starts at DriveMode None");
  check(zero.probe.reach === 0n, "reach starts zero");
  check(zero.wide.entityId === 0n, "entity_id starts zero");
  check(zero.wide.bias === -250n, "bias defaults -250");
  check(zero.wide.seed === 1n << 65n, "seed defaults 2^65");
  check(zero.keysCount === 0, "keys start empty");
  check(zero.targetId === 0n, "target_id starts zero");
  // Zero* is the §5 zero form; construction alone installs the defaults
  // (SPEC §4.2) — the JS twin of Go's plain-zero-value check
  lud.ZeroLudicrousState(zero);
  check(zero.wide.bias === 0n, "the §5 zero form stays zero");
}

// ---- the taken-branch wire: generated bytes == the C++-pinned golden ----
let takenWire;
{
  const inp = makeState();
  const ws = newWriteStream();
  check(lud.WriteLudicrousState(ws, inp), "write LudicrousState");
  ws.flush();
  takenWire = Uint8Array.from(ws.data());
  goldenWire("ludicrous_state", takenWire);

  const out = new lud.LudicrousState();
  const rs = new ReadStream(takenWire);
  check(lud.ReadLudicrousState(rs, out), "read LudicrousState");
  check(out.mode === lud.DriveMode.Ludicrous, "mode round-trips");
  check(out.probe.angle === inp.probe.angle, "angle round-trips");
  check(out.probe.position === inp.probe.position, "position round-trips");
  check(out.probe.reach === inp.probe.reach, "reach round-trips");
  check(out.probe.ticks === inp.probe.ticks, "ticks round-trips");
  check(deepEqual(out.probe.samples, inp.probe.samples), "samples round-trip");
  check(out.wide.entityId === inp.wide.entityId, "entity_id round-trips");
  check(out.wide.energy === inp.wide.energy, "energy round-trips");
  check(out.wide.flux === inp.wide.flux, "flux round-trips");
  check(out.wide.bias === -250n, "the bias default rides the wire");
  check(out.wide.seed === 1n << 65n, "the seed default rides the wire");
  check(out.keysCount === 2, "keys_count round-trips");
  check(out.keys[0] === inp.keys[0] && out.keys[1] === inp.keys[1], "keys round-trip");
  check(out.hasTarget && out.targetId === 42n, "the taken branch round-trips");
}

// ---- the untaken branch: identical prefix, and the 128-bit field under
// it reads back ZERO into a dirty object (SPEC §5) ----
{
  const inp = makeState();
  inp.hasTarget = false;
  const ws = newWriteStream();
  check(lud.WriteLudicrousState(ws, inp), "write LudicrousState untargeted");
  ws.flush();
  goldenWire("ludicrous_state_untargeted", ws.data());

  const out = new lud.LudicrousState();
  out.targetId = 0xdeadn; // dirty — the read must zero it
  const rs = new ReadStream(ws.data());
  check(lud.ReadLudicrousState(rs, out), "read LudicrousState untargeted");
  check(!out.hasTarget, "has_target reads false");
  check(out.targetId === 0n, "the untaken 128-bit field reads as zero (SPEC §5)");
}

// ---- hostile reads REJECT, never clamp (STANDARD.md, SPEC §5) ----
{
  // fixed: angle's 25 offset bits start at bit 2; all-ones = 33554431,
  // above the raw range 360 * 2^16 = 23592960
  const hostile = Uint8Array.from(takenWire);
  setBits(hostile, 2, 25);
  const out = new lud.LudicrousState();
  const rs = new ReadStream(hostile);
  check(!lud.ReadLudicrousState(rs, out), "a smuggled fixed offset is REJECTED");
}
{
  // int128: energy's 34 offset bits start at bit 286 (2+156+128);
  // all-ones = 2^34 - 1 = 17179869183, above the range 10^10
  const hostile = Uint8Array.from(takenWire);
  setBits(hostile, 286, 34);
  const out = new lud.LudicrousState();
  const rs = new ReadStream(hostile);
  check(!lud.ReadLudicrousState(rs, out), "a smuggled int128 offset is REJECTED");
}
{
  // truncation: running out of input mid-read is a read failure (SPEC §5)
  const out = new lud.LudicrousState();
  const rs = new ReadStream(takenWire.subarray(0, 4));
  check(!lud.ReadLudicrousState(rs, out), "a truncated stream is a read failure");
}

// ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6) ----
// The whole wire is the tail byte; a port that emits ANY bits for a
// degenerate range shifts it and fails the golden compare.
{
  check(lud.DegenerateProbeMaxBits === 8, "three degenerate fields cost zero bits");

  const inp = new lud.DegenerateProbe();
  inp.lockedFixed = -196608; // -3 * 2^16, the ONE legal raw
  inp.lockedInt = 7;
  inp.lockedWide = -12345678901234n;
  inp.tail = 0xa5;

  const ws = newWriteStream();
  check(lud.WriteDegenerateProbe(ws, inp), "write DegenerateProbe");
  ws.flush();
  goldenWire("degenerate_probe", ws.data());

  const out = new lud.DegenerateProbe();
  const rs = new ReadStream(ws.data());
  check(lud.ReadDegenerateProbe(rs, out), "read DegenerateProbe");
  check(deepEqual(out, inp), "DegenerateProbe round-trips — every value materialized from its range");
}

// ---- UnsignedProbe: ufixed(I, F), the unsigned sibling (SPEC §4.3;
//) ----
// span's raw value fills uint64's HIGH HALF (above 2^63) — BigInt carries
// it directly (no bit-cast dance), and the C++-pinned golden is the gate.
// Values mirror test/ludicrous_main.cpp.
{
  check(lud.UnsignedProbeMaxBits === 196, "UnsignedProbe worst case");

  const inp = new lud.UnsignedProbe();
  inp.angle = 2981888; // +45.5 * 2^16
  inp.span = 0xffffffffffff0000n; // raw_max — the uint64 HIGH HALF
  inp.reach = 131071999999n; // raw_max - 1
  inp.ticks = 777777;
  inp.samples[0] = 0; // raw_min
  inp.samples[1] = 1048576; // raw_max
  inp.locked = 196608; // 3 * 2^16, the ONE legal raw
  inp.tail = 0xa5;

  const ws = newWriteStream();
  check(lud.WriteUnsignedProbe(ws, inp), "write UnsignedProbe");
  ws.flush();
  const uWire = Uint8Array.from(ws.data());
  goldenWire("unsigned_probe", uWire);

  const out = new lud.UnsignedProbe();
  const rs = new ReadStream(uWire);
  check(lud.ReadUnsignedProbe(rs, out), "read UnsignedProbe");
  check(deepEqual(out, inp), "UnsignedProbe round-trips — the uint64 high half bit-exact");

  // the write-side degenerate refusal: any raw but 3 * 2^16 is refused
  // before a single bit is written
  const bad = new lud.UnsignedProbe();
  Object.assign(bad, inp);
  bad.samples = [...inp.samples];
  bad.locked = 196609;
  const wsb = newWriteStream();
  check(!lud.WriteUnsignedProbe(wsb, bad), "a wrong degenerate ufixed raw is REFUSED on write");

  // hostile: span's 64 offset bits (starting at bit 25) all-ones =
  // 2^64 - 1, above the raw range 0xFFFFFFFFFFFF0000 — the headroom is
  // exactly the low 16 bits, and the reject must fire in the UNSIGNED
  // domain (a signed compare would call the smuggled value negative)
  const hostile = Uint8Array.from(uWire);
  setBits(hostile, 25, 64);
  const hOut = new lud.UnsignedProbe();
  const hrs = new ReadStream(hostile);
  check(!lud.ReadUnsignedProbe(hrs, hOut), "a smuggled ufixed high-half offset is REJECTED");
}

console.log("OK");
