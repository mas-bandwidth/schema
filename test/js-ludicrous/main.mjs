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
  inp.Mode = lud.DriveMode.Ludicrous;
  inp.Probe.Angle = 2981888; // +45.5 * 2^16
  inp.Probe.Position = -809119744n; // -12346.1875 * 2^16
  inp.Probe.Reach = 65536000000n - 1n; // raw_max - 1
  inp.Probe.Ticks = 777777;
  inp.Probe.Samples[0] = -524288; // raw_min
  inp.Probe.Samples[1] = 524288; // raw_max
  inp.Wide.EntityId = 0x0123456789abcdeffedcba9876543210n;
  inp.Wide.Energy = 4999999999n;
  inp.Wide.Flux = (1n << 99n) + 7n;
  // wide.bias and wide.seed stay at their SPECIFIED DEFAULTS (-250 and
  // 2^65) — construction installs them, and they ride the wire as written
  inp.KeysCount = 2;
  inp.Keys[0] = 1n;
  inp.Keys[1] = 1n << 127n;
  inp.HasTarget = true;
  inp.TargetId = 42n;
  return inp;
}

// worst-case bounds, hand-derived (SPEC §6.1 item 4) — the same numbers
// test/ludicrous_main.cpp static_asserts
check(lud.FixedProbeMaxBits === 156, "FixedProbe worst case");
check(lud.WideProbeMaxBits === 403, "WideProbe worst case");
check(lud.LudicrousStateMaxBits === 1205, "LudicrousState worst case");
check(lud.MessageMaxBits === 1206, "message-level bound");
check(lud.ProtocolId !== 0n, "the unit has a protocol id");

// zero initialization with specified defaults (SPEC §4.2), sentinel-zero
// composition: construction starts at DriveMode None — the null rides
// in-band — and the two defaulted 128-bit fields construct to their declared
// values, one of which no 64-bit literal can spell
{
  const zero = new lud.LudicrousState();
  check(zero.Mode === lud.DriveMode.None, "a fresh state starts at DriveMode None");
  check(zero.Probe.Reach === 0n, "reach starts zero");
  check(zero.Wide.EntityId === 0n, "entity_id starts zero");
  check(zero.Wide.Bias === -250n, "bias defaults -250");
  check(zero.Wide.Seed === 1n << 65n, "seed defaults 2^65");
  check(zero.KeysCount === 0, "keys start empty");
  check(zero.TargetId === 0n, "target_id starts zero");
  // Zero* is the §5 zero form; construction alone installs the defaults
  // (SPEC §4.2) — the JS twin of Go's plain-zero-value check
  lud.ZeroLudicrousState(zero);
  check(zero.Wide.Bias === 0n, "the §5 zero form stays zero");
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
  check(out.Mode === lud.DriveMode.Ludicrous, "mode round-trips");
  check(out.Probe.Angle === inp.Probe.Angle, "angle round-trips");
  check(out.Probe.Position === inp.Probe.Position, "position round-trips");
  check(out.Probe.Reach === inp.Probe.Reach, "reach round-trips");
  check(out.Probe.Ticks === inp.Probe.Ticks, "ticks round-trips");
  check(deepEqual(out.Probe.Samples, inp.Probe.Samples), "samples round-trip");
  check(out.Wide.EntityId === inp.Wide.EntityId, "entity_id round-trips");
  check(out.Wide.Energy === inp.Wide.Energy, "energy round-trips");
  check(out.Wide.Flux === inp.Wide.Flux, "flux round-trips");
  check(out.Wide.Bias === -250n, "the bias default rides the wire");
  check(out.Wide.Seed === 1n << 65n, "the seed default rides the wire");
  check(out.KeysCount === 2, "keys_count round-trips");
  check(out.Keys[0] === inp.Keys[0] && out.Keys[1] === inp.Keys[1], "keys round-trip");
  check(out.HasTarget && out.TargetId === 42n, "the taken branch round-trips");
}

// ---- the untaken branch: identical prefix, and the 128-bit field under
// it reads back ZERO into a dirty object (SPEC §5) ----
{
  const inp = makeState();
  inp.HasTarget = false;
  const ws = newWriteStream();
  check(lud.WriteLudicrousState(ws, inp), "write LudicrousState untargeted");
  ws.flush();
  goldenWire("ludicrous_state_untargeted", ws.data());

  const out = new lud.LudicrousState();
  out.TargetId = 0xdeadn; // dirty — the read must zero it
  const rs = new ReadStream(ws.data());
  check(lud.ReadLudicrousState(rs, out), "read LudicrousState untargeted");
  check(!out.HasTarget, "has_target reads false");
  check(out.TargetId === 0n, "the untaken 128-bit field reads as zero (SPEC §5)");
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

// ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6, 2026-08-15) ----
// The whole wire is the tail byte; a port that emits ANY bits for a
// degenerate range shifts it and fails the golden compare.
{
  check(lud.DegenerateProbeMaxBits === 8, "three degenerate fields cost zero bits");

  const inp = new lud.DegenerateProbe();
  inp.LockedFixed = -196608; // -3 * 2^16, the ONE legal raw
  inp.LockedInt = 7;
  inp.LockedWide = -12345678901234n;
  inp.Tail = 0xa5;

  const ws = newWriteStream();
  check(lud.WriteDegenerateProbe(ws, inp), "write DegenerateProbe");
  ws.flush();
  goldenWire("degenerate_probe", ws.data());

  const out = new lud.DegenerateProbe();
  const rs = new ReadStream(ws.data());
  check(lud.ReadDegenerateProbe(rs, out), "read DegenerateProbe");
  check(deepEqual(out, inp), "DegenerateProbe round-trips — every value materialized from its range");
}

// ---- NarrowBody: the narrowed fixed shallow (SPEC §4.8 rule 2b) ----
// The pinned tie semantics: quantize rounds to nearest, ties AWAY FROM
// ZERO — the one fixed-point rounding rule (SPEC §4.8, decided
// 2026-08-15) — and unquantize is the left shift back. The wire
// bytes are the C++-pinned goldens; the values mirror
// test/ludicrous_main.cpp block for block.
{
  check(lud.NarrowBodyData_ShallowMaxBits === 228, "narrowed shallow worst case");
  check(lud.NarrowBodyData_DeepMaxBits === 332, "narrow deep worst case");

  const inp = new lud.NarrowBodyData_Interpolate();
  inp.Position.X = 384n; // +1.5 eighths: tie, rounds AWAY to 2
  inp.Position.Y = -384n; // -1.5 eighths: tie, rounds AWAY to -2 — THE distinguishing value
  inp.Position.Z = -6586368n; // -100.5 * 2^16, exact in 8 kept bits
  inp.Rotation.W = 1 << 30; // identity, hits the +1024 bound exactly
  inp.Velocity.X = 1n;
  inp.Velocity.Y = -1n;
  inp.Velocity.Z = 123456789n;

  const sh = new lud.NarrowBodyData_Shallow();
  lud.QuantizeNarrowBody(inp, sh);
  check(sh.PositionX === 2, "+1.5 eighths ties away to 2");
  check(sh.PositionY === -2, "-1.5 eighths ties away from zero to -2 (the bare shift would say -1)");
  check(sh.PositionZ === -25728, "-100.5 units exact in 8 kept bits");
  check(sh.RotationX === 0 && sh.RotationY === 0 && sh.RotationZ === 0, "identity xyz quantize to 0");
  check(sh.RotationW === 1024, "identity w hits the +1024 bound exactly");
  check(sh.Velocity.X === 1n && sh.Velocity.Y === -1n && sh.Velocity.Z === 123456789n,
    "full-precision velocity copies");

  const back = new lud.NarrowBodyData_Interpolate();
  lud.UnquantizeNarrowBody(sh, back);
  check(back.Position.X === 512n, "narrowing loss, 384 -> 2 -> 512");
  check(back.Position.Y === -512n, "narrowing loss, -384 -> -2 -> -512");
  check(back.Position.Z === -6586368n, "exact multiple of 2^8 restores exactly");
  check(back.Rotation.W === 1 << 30, "the identity survives the round trip");

  const ws = newWriteStream();
  check(lud.WriteNarrowBodyData_Shallow(ws, sh), "write NarrowBodyData_Shallow");
  ws.flush();
  const shWire = Uint8Array.from(ws.data());
  goldenWire("narrow_body_shallow", shWire);

  const shOut = new lud.NarrowBodyData_Shallow();
  const rs = new ReadStream(shWire);
  check(lud.ReadNarrowBodyData_Shallow(rs, shOut), "read NarrowBodyData_Shallow");
  check(shOut.PositionY === -2 && shOut.RotationW === 1024 && shOut.Velocity.Z === 123456789n,
    "shallow round trip");

  const deep = new lud.NarrowBodyData_Deep();
  deep.Position.X = inp.Position.X;
  deep.Position.Y = inp.Position.Y;
  deep.Position.Z = inp.Position.Z;
  deep.Rotation.X = inp.Rotation.X;
  deep.Rotation.Y = inp.Rotation.Y;
  deep.Rotation.Z = inp.Rotation.Z;
  deep.Rotation.W = inp.Rotation.W;
  deep.Velocity.X = inp.Velocity.X;
  deep.Velocity.Y = inp.Velocity.Y;
  deep.Velocity.Z = inp.Velocity.Z;
  const wsd = newWriteStream();
  check(lud.WriteNarrowBodyData_Deep(wsd, deep), "write NarrowBodyData_Deep");
  wsd.flush();
  const deepWire = Uint8Array.from(wsd.data());
  goldenWire("narrow_body_deep", deepWire);

  const deepOut = new lud.NarrowBodyData_Deep();
  const rsd = new ReadStream(deepWire);
  check(lud.ReadNarrowBodyData_Deep(rsd, deepOut), "read NarrowBodyData_Deep");
  check(deepOut.Position.Z === -6586368n && deepOut.Rotation.W === 1 << 30, "deep full precision round trip");

  // hostile shallow read: position_x's 26 offset bits all-ones =
  // 67108863, above the range size 51200000 — reject, never clamp
  const hostile = Uint8Array.from(shWire);
  setBits(hostile, 0, 26);
  const hOut = new lud.NarrowBodyData_Shallow();
  const hrs = new ReadStream(hostile);
  check(!lud.ReadNarrowBodyData_Shallow(hrs, hOut), "a smuggled narrowed offset is REJECTED");
}

// ---- UnsignedProbe: ufixed(I, F), the unsigned sibling (SPEC §4.3;
// Glenn 2026-08-15: "ufixed is fine", closing §9 q17) ----
// span's raw value fills uint64's HIGH HALF (above 2^63) — BigInt carries
// it directly (no bit-cast dance), and the C++-pinned golden is the gate.
// Values mirror test/ludicrous_main.cpp.
{
  check(lud.UnsignedProbeMaxBits === 196, "UnsignedProbe worst case");

  const inp = new lud.UnsignedProbe();
  inp.Angle = 2981888; // +45.5 * 2^16
  inp.Span = 0xffffffffffff0000n; // raw_max — the uint64 HIGH HALF
  inp.Reach = 131071999999n; // raw_max - 1
  inp.Ticks = 777777;
  inp.Samples[0] = 0; // raw_min
  inp.Samples[1] = 1048576; // raw_max
  inp.Locked = 196608; // 3 * 2^16, the ONE legal raw
  inp.Tail = 0xa5;

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
  bad.Samples = [...inp.Samples];
  bad.Locked = 196609;
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

// ---- the message dispatch surface over the new unit ----
{
  const inp = makeState();
  const ws = newWriteStream();
  check(lud.WriteMessage(ws, inp), "write Message LudicrousState");
  ws.flush();

  const storage = new lud.MessageStorage();
  const holder = { value: null };
  const rs = new ReadStream(ws.data());
  check(lud.ReadMessage(rs, storage, holder), "read Message LudicrousState");
  const out = holder.value;
  check(out instanceof lud.LudicrousState, "the message is the LudicrousState");
  if (out instanceof lud.LudicrousState) {
    check(out.Wide.Flux === inp.Wide.Flux, "flux rides the dispatch surface");
    check(out.Probe.Angle === 2981888, "angle rides the dispatch surface");
  }
}

if (failed) {
  process.exit(1);
}
console.log("OK");
