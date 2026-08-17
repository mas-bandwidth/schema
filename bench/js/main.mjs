// schema bench — the JavaScript runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated JS modules and the serialize.js runtime: same benchmark set
// (real_packet included), same pinned corpus instances, same LCG and
// vary-function field mappings, same golden + round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=js. See
// bench/README.md for the runner contract. One module, like bench/c's one
// translation unit: the harness helpers are module-scoped, and splitting the
// rt family into a second module would force them through an export surface
// the other runners do not have.
//
// Language-specific discipline:
//   - the LCG is the C bench's uint64 LCG carried in two 32-bit lanes, the
//     exact generator serialize.js's own bench/bench.js authored: BigInt
//     never steps the generator, and BigInt values are constructed only for
//     fields whose STORAGE is BigInt (64/128-bit and flags fields) — the
//     library's real wide edge, paid exactly where a generated caller pays it
//   - streams are reused via reset() (the runtime's documented no-allocation
//     reuse path); resetting to a DIFFERENT buffer re-wraps a DataView — the
//     read loops rotate 64 variant views, so they pay that re-wrap per op,
//     the same structural cost bench/bench.js names and measures as shipped
//   - escape barriers: a module-scoped sink accumulates observed byte counts
//     (JS has no empty-asm clobber; the decoded object escapes through the
//     loop function and the sink's data dependence keeps the work alive)
//   - the driver passes write/read/vary as function values (one indirect
//     call per op, as in the Go and C# runners; Rust and C++ get this
//     inlined via generics — noted in the results)
//   - the warmup run per path doubles as the JIT warmup
//   - no alloc note: Node exposes no per-thread allocation counter the Go
//     and C# notes read; the reuse discipline here is structural (persistent
//     holders, stream reset, pre-bounded variant views)
//   - family rt holds fields as persistent { value } holder objects — the
//     serialize.js caller idiom, from the runtime's own bench — so the
//     timed loops allocate nothing of their own
//
// Both runtime modes run: NODE_ENV forks serialize.js at module load
// (src/mode.js). The standard leg is NODE_ENV=production — the caller-trust
// release shape, the family's release configuration — recorded in the CSV as
// checks=contract (caller-error validation is gone, while the checks the
// family keeps hard in release — read bounds, hostile-wire rejection, the
// sticky error latch — stay). A checked-mode run records checks=always:
// nothing is removed in that build, and the CSV says which one ran.
// linkage=esm: the runtime is ES modules loaded into the same isolate; every
// call crosses a module boundary the JIT inlines through, JavaScript's
// packaging — recorded, like every linkage value, never matched.
//
// Run from bench/js (run.sh does): the wire goldens are at
// ../../testdata/wire. The serialize.js runtime is the documented sibling
// checkout, imported by module-relative path — no npm, no install step. A
// SERIALIZE_JS override (§3.5) redirects the import; --print-runtime prints
// the resolved runtime root this process would import (node's own module
// resolution — the toolchain fact run.sh's provenance guard verifies) and
// exits.
import { readFileSync, realpathSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

import * as enums from "../../generated/js/Enums.js";
import * as types from "../../generated/js/Types.js";
import * as messages from "../../generated/js/Messages.js";
import * as wire from "../../generated/js/Wire.js";
import * as objects from "../../generated/js/Objects.js";
import * as realworld from "../../generated/bench/js/realworld/RealWorld.js";

// one namespace over the unit, the way Go sees package example — the checker
// guarantees unit-wide name uniqueness, so the merge cannot collide
const ex = { ...enums, ...types, ...messages, ...wire, ...objects };

// ---- §3.5 runtime resolution: the sibling checkout, overridable. A
// relative SERIALIZE_JS resolves against the REPO ROOT, not this process's
// cwd — the same semantics the Makefile and run.sh give every SERIALIZE_*
// path — so `SERIALIZE_JS=../serialize.js` means the same checkout from any
// invocation directory. ----
const repoRoot = fileURLToPath(new URL("../../", import.meta.url));
const runtimeRoot = process.env.SERIALIZE_JS
  ? new URL(pathToFileURL(path.resolve(repoRoot, process.env.SERIALIZE_JS)).href + "/")
  : new URL("../../../serialize.js/", import.meta.url);

if (process.argv.includes("--print-runtime")) {
  // the resolved root of the runtime this process would import — realpathed
  // through the entry module so a symlinked checkout reports its true home
  const index = realpathSync(fileURLToPath(new URL("src/index.js", runtimeRoot)));
  process.stdout.write(path.dirname(path.dirname(index)) + "\n");
  process.exit(0);
}

const { WriteStream, ReadStream, BitWriter, BitReader } = await import(
  new URL("src/index.js", runtimeRoot).href
);
const { PRODUCTION } = await import(new URL("src/mode.js", runtimeRoot).href);

const MaxNumRuns = 7; // median of 7 (N >= 5), after 1 warmup run
let gNumRuns = MaxNumRuns; // --round K drops this to 1 (§2.4: one warmup +
// one measured run per round; the driver aggregates across rounds)
const NumVariants = 64; // read-path variant buffers

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// Rows are buffered and emitted at exit so every row carries the corpus_id
// (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each
// file in sorted basename order, the basename bytes, a 0x00 byte, the
// contents. The per-runner constants: linkage esm (see the header), checks
// from the MODE THAT RAN (production = contract, checked = always — the
// header says why), opt default (node has no operator-visible optimization
// levels), inline unknown — and it stays unknown: a JIT leg has no §4.1
// AOT artifact to disassemble, so the verdict pass has no js branch and js
// rows never ratio against a row whose inline column is filled.
// family is per ROW (gen | rt | bits — §5.1).
const CsvSuffix = `esm,${PRODUCTION ? "contract" : "always"},default,unknown`;

const gCsvRows = []; // { row, family }
const gGoldensLoaded = new Map(); // basename -> Uint8Array

function fnv1a64(h, bytes) {
  for (let i = 0; i < bytes.length; i++) {
    h ^= BigInt(bytes[i]);
    h = (h * 0x100000001b3n) & 0xffffffffffffffffn;
  }
  return h;
}

function corpusId() {
  const names = [...gGoldensLoaded.keys()].sort();
  let h = 0xcbf29ce484222325n;
  const zero = new Uint8Array(1);
  for (const name of names) {
    h = fnv1a64(h, new TextEncoder().encode(name));
    h = fnv1a64(h, zero);
    h = fnv1a64(h, gGoldensLoaded.get(name));
  }
  return h.toString(16).padStart(16, "0");
}

function flushCsv() {
  if (!gCsv) {
    return;
  }
  if (failed) {
    // §1.5: a failing run emits NO rows — the exit code and stderr are the
    // whole output. Numbers from a run whose gate refused are not numbers.
    process.stderr.write("refusing to emit CSV rows from a failing run\n");
    return;
  }
  const id = corpusId();
  for (const r of gCsvRows) {
    process.stdout.write(`${r.row},${id},${r.family},${CsvSuffix}\n`);
  }
}

// buffers: write buffers must be a multiple of 8 bytes (qword-flush
// contract); reads bound themselves with pre-cut bytesPerOp views. 4096
// covers MessageMaxBytes (2008) with slack.
const BufferSize = 4096;

// §2.7 variant-buffer stride: the 64 rotating read buffers are allocated at
// BufferSize + 64 per slot, NOT packed at exact 4096 — the same stagger
// policy as the other five runners. V8 already places each Uint8Array's
// backing store independently with headers between them, so the exact
// stride was never controllable here — the pad is applied for uniformity of
// the §2.7 policy, not because this runtime's arithmetic needs it. The
// stream sees a subarray bounded at bytesPerOp; the pad is address spacing
// only.
const VariantStride = BufferSize + 64;

const gBuffer = new Uint8Array(BufferSize);
const gTwin = new Uint8Array(BufferSize);
const gVariants = [];
for (let k = 0; k < NumVariants; k++) {
  gVariants.push(new Uint8Array(VariantStride));
}

let gSink = 0; // defeats dead code elimination of computed values
let gCsv = false;
let gWireDir = "../../testdata/wire";
let failed = false;

// --------------------------------------------------------------------------
// the C bench's uint64 LCG (Knuth MMIX: rng * 6364136223846793005 +
// 1442695040888963407 mod 2^64), carried in two 32-bit lanes — the exact
// generator serialize.js/bench/bench.js authored, seedable because the
// schema runners seed it per section (1 for the per-bench variants, 12345
// for the batch builder, 999 for the batch write loop). Lane arithmetic is
// exact in the double domain: the low 32x32 product goes through 16-bit
// limbs (every partial sum stays far below 2^53), the cross products only
// matter mod 2^32 so Math.imul carries them, and the carries are recovered
// by exact subtraction. BigInt never steps the generator.
// --------------------------------------------------------------------------

const LCG_MUL_LO = 0x4c957f2d;
const LCG_MUL_HI = 0x5851f42d;
const LCG_ADD_LO = 0xf767814f;
const LCG_ADD_HI = 0x14057b7e;

const rng = { lo: 1, hi: 0 };

function lcgSeed(v) {
  rng.lo = v >>> 0;
  rng.hi = 0;
}

function lcgStep() {
  const aLo = rng.lo;
  const aHi = rng.hi;
  const aL = aLo & 0xffff;
  const aH = aLo >>> 16;
  const low = aL * (LCG_MUL_LO & 0xffff) + (aL * (LCG_MUL_LO >>> 16) + aH * (LCG_MUL_LO & 0xffff)) * 65536;
  const pLo = low >>> 0;
  const carry = (low - pLo) / 4294967296;
  const hi =
    (aH * (LCG_MUL_LO >>> 16) + carry + (Math.imul(aLo, LCG_MUL_HI) >>> 0) + (Math.imul(aHi, LCG_MUL_LO) >>> 0)) %
    4294967296;
  const sLo = pLo + LCG_ADD_LO;
  rng.lo = sLo >>> 0;
  rng.hi = ((hi + LCG_ADD_HI + (sLo >= 4294967296 ? 1 : 0)) % 4294967296) >>> 0;
}

// the low 32 bits of (rng >> s), for s in [0,63]
function shr64(s) {
  if (s === 0) {
    return rng.lo;
  }
  if (s < 32) {
    return ((rng.lo >>> s) | (rng.hi << (32 - s))) >>> 0;
  }
  return rng.hi >>> (s - 32);
}

// the full 64-bit rng as a BigInt — only for fields whose storage is BigInt
function rngBig() {
  return (BigInt(rng.hi) << 32n) | BigInt(rng.lo);
}

function fail(name, what) {
  process.stderr.write(`FAILED: ${name}: ${what}\n`);
  failed = true;
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

function checkGolden(name, data) {
  const golden = `${gWireDir}/${name}.bin`;
  let expected;
  try {
    expected = readFileSync(golden);
  } catch {
    process.stderr.write(`missing wire golden ${golden} — run from bench/js (or pass --wire-dir)\n`);
    return false;
  }
  gGoldensLoaded.set(`${name}.bin`, expected);
  if (!bytesEqual(expected, data)) {
    process.stderr.write(
      `WIRE GOLDEN MISMATCH: ${name} (${expected.length} golden vs ${data.length} actual bytes) — refusing to bench code that does not match the corpus\n`
    );
    return false;
  }
  return true;
}

function stats(rates) {
  rates.sort((a, b) => a - b);
  const n = rates.length;
  return {
    median: rates[Math.floor(n / 2)],
    min: rates[0],
    max: rates[n - 1],
    spread: ((rates[n - 1] - rates[0]) / rates[Math.floor(n / 2)]) * 100.0,
  };
}

function report(bench, path_, iters, bytesPerOp, s, family) {
  const mbps = (s.median * bytesPerOp) / (1024.0 * 1024.0);
  process.stderr.write(
    `${bench.padEnd(18)} ${path_.padEnd(5)} ${(s.median / 1e6).toFixed(2).padStart(10)} M msg/s ` +
      `${mbps.toFixed(1).padStart(10)} MB/s   (min ${(s.min / 1e6).toFixed(2)}, max ${(s.max / 1e6).toFixed(2)}, ` +
      `spread ${s.spread.toFixed(1)}%)\n`
  );
  if (gCsv) {
    gCsvRows.push({
      row:
        `js,${bench},${path_},${iters},${bytesPerOp},${gNumRuns},` +
        `${s.median.toFixed(0)},${s.min.toFixed(0)},${s.max.toFixed(0)},${mbps.toFixed(2)},${s.spread.toFixed(2)}`,
      family,
    });
  }
}

// --------------------------------------------------------------------------
// the per-message benchmark driver
// --------------------------------------------------------------------------

function benchMessage(name, golden, iters, pinned, writeFn, readFn, varyFn) {
  // self-check 1: the pinned instance matches its wire golden byte-for-byte
  const base = pinned;
  const ws = new WriteStream(gBuffer);
  if (!writeFn(ws, base)) {
    fail(name, "write of pinned instance failed");
    return;
  }
  ws.flush();
  const bytesPerOp = ws.bytesProcessed();
  if (golden !== null && !checkGolden(golden, gBuffer.subarray(0, bytesPerOp))) {
    failed = true;
    return;
  }

  // self-check 2: round-trip write -> read -> re-write -> identical bytes
  {
    const output = new pinned.constructor();
    const checkRs = new ReadStream(gBuffer.subarray(0, bytesPerOp));
    if (!readFn(checkRs, output)) {
      fail(name, "read of pinned instance failed");
      return;
    }
    const tws = new WriteStream(gTwin);
    if (!writeFn(tws, output)) {
      fail(name, "re-write of decoded instance failed");
      return;
    }
    tws.flush();
    if (tws.bytesProcessed() !== bytesPerOp || !bytesEqual(gTwin.subarray(0, bytesPerOp), gBuffer.subarray(0, bytesPerOp))) {
      fail(name, "round-trip bytes differ");
      return;
    }
  }

  // variant buffers for the read path (and proof that variation keeps
  // bytes/op constant); the views are pre-cut so the timed loop never slices
  lcgSeed(1);
  const variantViews = [];
  for (let k = 0; k < NumVariants; k++) {
    lcgStep();
    varyFn(base);
    const vs = new WriteStream(gVariants[k]);
    if (!writeFn(vs, base)) {
      fail(name, "write of varied instance failed");
      return;
    }
    vs.flush();
    if (vs.bytesProcessed() !== bytesPerOp) {
      fail(name, "variation changed bytes/op — vary must keep structure fields fixed");
      return;
    }
    variantViews.push(gVariants[k].subarray(0, bytesPerOp));
  }

  const writeRates = new Array(gNumRuns);
  const readRates = new Array(gNumRuns);

  // write path: 1 warmup (also the JIT warmup) + NumRuns measured
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      lcgStep();
      varyFn(base);
      ws.reset(gBuffer);
      if (!writeFn(ws, base)) {
        fail(name, "write failed in loop");
        return;
      }
      ws.flush();
      gSink = (gSink + ws.bytesProcessed()) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // read path: 1 warmup + NumRuns measured; ONE reused decode instance
  // hoisted out of the loop (the MessageStorage discipline — §5 zeroing
  // makes reuse equivalent on every field that rides)
  const outValue = new pinned.constructor();
  const rs = new ReadStream(variantViews[0]);
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      rs.reset(variantViews[i & (NumVariants - 1)]);
      if (!readFn(rs, outValue)) {
        fail(name, "read failed in loop");
        return;
      }
      gSink = (gSink + 1) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = iters / elapsed;
    }
  }

  report(name, "write", iters, bytesPerOp, stats(writeRates), "gen");
  report(name, "read", iters, bytesPerOp, stats(readRates), "gen");
}

// --------------------------------------------------------------------------
// pinned corpus instances — the C++ reference's pin_* functions exactly
// (the same instances test/js/main.mjs pins to the wire goldens)
// --------------------------------------------------------------------------

function textBytes(s) {
  return new TextEncoder().encode(s);
}

function pinRigidBodyMoving() {
  const input = new ex.RigidBody();
  input.Position.X = 1.5;
  input.Position.Y = -2.5;
  input.Position.Z = 3.25;
  input.Orientation.X = 0.1;
  input.Orientation.Y = 0.2;
  input.Orientation.Z = 0.3;
  input.Orientation.W = 0.9;
  input.AtRest = false;
  input.LinearVelocity.X = 10.0;
  input.LinearVelocity.Y = 20.0;
  input.LinearVelocity.Z = -3.0;
  input.AngularVelocity.X = 0.25;
  input.AngularVelocity.Y = 0.5;
  input.AngularVelocity.Z = 0.75;
  return input;
}

function pinChat() {
  const input = new ex.Chat();
  input.Text.set(textBytes("wire parity"));
  input.TextLength = 11;
  return input;
}

function pinInputPacket() {
  const input = new ex.InputPacket();
  input.SynchronizeSequence = 7;
  input.CurrentFrame = 123456789n;
  input.StartFrame = 123456780n;
  input.InputsCount = 2;
  input.Inputs[0].Throttle = 0.5;
  input.Inputs[0].Fire = true;
  input.Inputs[1].StickX = -0.25;
  input.Inputs[1].Boost = true;
  return input;
}

function pinShipCreate() {
  const input = new ex.ShipCreate();
  input.ShipType = ex.ShipType.Bomber;
  input.Position.X = 1000;
  input.Position.Y = -2000;
  input.Position.Z = 3000;
  input.HasFlags = true;
  input.Flags = ex.ShipFlagsBoosting | ex.ShipFlagsAiming;
  input.Team = ex.Team.Blue;
  input.Health = 750;
  input.Thrust = 55;
  return input;
}

function pinShipShallow() {
  const interp = new ex.ShipData_Interpolate();
  interp.ShipType = ex.ShipType.Corvette;
  interp.Position.X = 1.5;
  interp.Position.Y = -2.25;
  interp.Position.Z = 100.0;
  interp.Rotation.X = 0.0;
  interp.Rotation.Y = 0.0;
  interp.Rotation.Z = 0.0;
  interp.Rotation.W = 1.0;
  interp.LinearVelocity.X = 3.0;
  interp.LinearVelocity.Y = 0.0;
  interp.LinearVelocity.Z = -1.0;
  interp.Flags = ex.ShipFlagsBoosting;
  interp.Team = ex.Team.Red;
  interp.Health = 750;
  interp.Thrust = 55;
  const q = new ex.ShipData_Shallow();
  ex.QuantizeShip(interp, q);
  return q;
}

function pinProbeHeader() {
  const h = new ex.ProbeHeader();
  h.Version = 5;
  h.ProbeId = 0x1122334455667788n;
  return h;
}

function pinProbeBits() {
  const input = new ex.ProbeBits();
  input.Small = 0x1ff;
  input.Boundary = 0x1ffffffffn;
  input.Wide = 0xfedcba9876543210n;
  input.Sensor = 4294967295;
  input.Nonce = 18446744073709551615n;
  return input;
}

function pinProbeArray() {
  // construction carries the SPECIFIED defaults (active = true, retries =
  // -1, preferred = Railgun) exactly as C++ construction does — the pinned
  // instance overrides only what test/main.cpp overrides
  const input = new ex.ProbeArray();
  input.Samples[0].Orientation = 90.0;
  input.Samples[0].RawDelta = -5;
  input.Samples[0].BigDelta = -1234567890123n;
  input.Samples[0].Weapon = ex.Weapon.Laser;
  input.Samples[0].HasTarget = true;
  input.Samples[0].TargetId = 777;
  input.Samples[0].SamplesCount = 1;
  input.Samples[0].Samples[0] = 42;
  input.Samples[1].Active = false;
  input.Samples[1].Orientation = -45.5;
  input.Samples[1].RawDelta = 7;
  input.Samples[1].BigDelta = 99n;
  input.Samples[1].IdleTicks = 1000;
  input.Samples[1].SamplesCount = 2;
  input.Samples[1].Samples[0] = 7;
  input.Samples[1].Samples[1] = 8;
  input.Config.Retries = 3;
  input.Config.Preferred = ex.Weapon.Missile;
  return input;
}

function pinTestData() {
  const input = new ex.TestData();
  input.A = -100;
  input.B = 100;
  input.C = 149;
  input.D = 0x11;
  input.E = 0x22;
  input.F = 0x33;
  input.G = true;
  input.ItemsCount = 3;
  input.Items[0] = 0;
  input.Items[1] = 128;
  input.Items[2] = 255;
  input.FloatValue = Math.fround(3.1415926);
  input.CompressedFloatValue = 2.5;
  input.DoubleValue = 1.0 / 3.0;
  input.Int8Value = -128;
  input.Int16Value = -32768;
  input.Uint8Value = 255;
  input.Uint16Value = 65535;
  input.Uint32Value = 4294967295;
  input.Uint64Value = 18446744073709551615n;
  input.Int64Full = -9223372036854775808n;
  input.Int64Range = -999999999999n;
  for (let i = 0; i < 17; i++) {
    input.FixedBytes[i] = (i * 3) & 0xff;
  }
  input.Text.set(textBytes("the quick brown fox"));
  input.TextLength = 19;
  return input;
}

// --------------------------------------------------------------------------
// vary functions — the C++ reference's vary_* field mappings exactly: VALUE
// fields mutate within wire ranges through the LCG; structure fields
// (counts, lengths, branch bools) stay fixed so bytes/op is constant. Each
// reads the lanes the loop just stepped.
// --------------------------------------------------------------------------

function varyRigidBody(m) {
  m.Position.X = (shr64(8) & 0xffff) * 0.25;
  m.Position.Y = (shr64(16) & 0xffff) * 0.5;
  m.Position.Z = (shr64(24) & 0xffff) * 0.125;
  m.Orientation.X = (rng.lo & 0xff) * 0.001;
  m.LinearVelocity.X = (shr64(32) & 0xfff) * 0.25;
  m.AngularVelocity.Z = (shr64(40) & 0xfff) * 0.125;
}

// the at-rest twin varies only the fields its taken branch still writes
function varyRigidBodyAtRest(m) {
  m.Position.X = (shr64(8) & 0xffff) * 0.25;
  m.Position.Y = (shr64(16) & 0xffff) * 0.5;
  m.Orientation.X = (rng.lo & 0xff) * 0.001;
}

function varyChat(m) {
  for (let i = 0; i < m.TextLength; i++) {
    m.Text[i] = 97 + ((rng.lo >>> (i & 7)) & 15); // 'a' + …, never zero
  }
}

function varyTest(m) {
  m.TestA = rng.lo & 0xffff;
  m.TestB = shr64(16) & 511; // within [0, 1000]
  m.TestC = shr64(25) & 511;
  m.TestD = shr64(34) & 511;
}

function varyInputPacket(m) {
  m.SynchronizeSequence = rng.lo & 0xffff;
  m.CurrentFrame = rngBig();
  m.StartFrame = rngBig() >> 1n;
  m.Inputs[0].Throttle = (rng.lo & 0xff) / 256.0;
  m.Inputs[0].Fire = (rng.lo & 1) !== 0;
  m.Inputs[1].StickX = (shr64(8) & 0xff) / 256.0 - 0.5;
  m.Inputs[1].Boost = (rng.lo & 2) !== 0;
}

function varyShipCreate(m) {
  m.Position.X = (shr64(8) & 0xfffff) - 0x80000; // within [-8388608, 8388608]
  m.Position.Y = (shr64(16) & 0xfffff) - 0x80000;
  m.Position.Z = (shr64(24) & 0xfffff) - 0x80000;
  m.Rotation.X = (rng.lo & 0x7ff) - 1024; // within [-1024, 1024]
  m.LinearVelocity.X = (shr64(32) & 0x3fffff) - 2097152;
  m.Flags = rngBig() & 15n; // 4 wire bits, has_flags stays true
  m.Health = shr64(5) & 511; // within [0, 1000]
  m.Thrust = shr64(14) & 63; // within [0, 100]
}

function varyShipShallow(m) {
  m.PositionX = (shr64(8) & 0xfffff) - 0x80000;
  m.PositionY = (shr64(16) & 0xfffff) - 0x80000;
  m.PositionZ = (shr64(24) & 0xfffff) - 0x80000;
  m.RotationX = (rng.lo & 0x7ff) - 1024;
  m.RotationW = (shr64(11) & 0x7ff) - 1024;
  m.LinearVelocityX = (shr64(32) & 0x3fffff) - 2097152;
  m.Flags = rngBig() & 15n;
  m.Health = shr64(5) & 511;
  m.Thrust = shr64(14) & 63;
}

function varyProbeHeader(m) {
  m.Version = rng.lo & 7; // 3 wire bits
  m.ProbeId = rngBig();
}

function varyProbeBits(m) {
  m.Small = rng.lo & 511; // 9 bits
  m.Boundary = rngBig() & ((1n << 33n) - 1n); // 33 bits
  m.Wide = BigInt.asUintN(64, rngBig() * 3n);
  m.Sensor = shr64(16);
  m.Nonce = rngBig() ^ 0x5555555555555555n;
}

function varyProbeArray(m) {
  m.Samples[0].Orientation = -180.0 + (rng.lo & 0x3fff) * 0.02;
  m.Samples[0].RawDelta = shr64(8) | 0;
  m.Samples[0].BigDelta = BigInt.asIntN(64, rngBig() * 5n);
  m.Samples[0].TargetId = shr64(24) & 0xffff;
  m.Samples[0].Samples[0] = shr64(40) & 0xffff;
  m.Samples[1].Orientation = -180.0 + (shr64(3) & 0x3fff) * 0.02;
  m.Samples[1].IdleTicks = shr64(32);
  m.Samples[1].Samples[0] = shr64(4) & 0xffff;
  m.Samples[1].Samples[1] = shr64(12) & 0xffff;
  m.Config.Retries = shr64(20) | 0;
}

function varyTestData(m) {
  m.A = (rng.lo & 127) - 64; // within [-100, 100]
  m.B = (shr64(7) & 127) - 64;
  m.C = (shr64(14) & 127) - 64; // within [-100, 150]
  m.D = rng.lo & 255;
  m.E = shr64(8) & 255;
  m.F = shr64(16) & 255;
  m.Items[0] = rng.lo & 255; // items_count stays 3
  m.Items[1] = shr64(8) & 255;
  m.Items[2] = shr64(16) & 255;
  m.FloatValue = rng.lo & 0xffff;
  m.CompressedFloatValue = (rng.lo & 1023) * 0.005; // within [0, 10] (max 5.115)
  m.DoubleValue = (shr64(16) & 0xffffff) * 0.5;
  m.Int8Value = (rng.lo << 24) >> 24;
  m.Int16Value = (shr64(8) << 16) >> 16;
  m.Uint8Value = shr64(16) & 255;
  m.Uint16Value = shr64(24) & 0xffff;
  m.Uint32Value = shr64(32);
  m.Uint64Value = BigInt.asUintN(64, rngBig() * 7n);
  m.Int64Full = BigInt.asIntN(64, rngBig() * 11n);
  m.Int64Range = ((rngBig() >> 24n) & ((1n << 37n) - 1n)) - (1n << 36n); // within +/- 1e12
  m.FixedBytes[0] = rng.lo & 0xff;
  m.FixedBytes[16] = shr64(8) & 0xff;
  for (let i = 0; i < m.TextLength; i++) {
    m.Text[i] = 97 + ((rng.lo >>> (i & 7)) & 15); // never zero
  }
}

// real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured
// through the GENERATED code (bench/corpus/RealWorld.schema ->
// generated/bench/js/realworld). The pinned instance is the ALL-DEFAULTS
// instance: new RealPacket() serialized unmodified, 1629 bits = 204 wire
// bytes, pinned to testdata/wire/real_packet.bin by test/bench/main.cpp.
// The four branch gates (f012 true, f043 false, f050 true, f074 false) are
// STRUCTURE (§2.7): they keep their schema defaults here, so the same
// branch bodies ride every iteration and bytes/op is constant. These
// mappings reproduce bench/cpp/bench_main.cpp's vary_real_packet exactly —
// fields under the false gates do not ride and are not varied; every
// mapping keeps its field inside its declared wire range.
function varyRealPacket(m) {
  // ranged ints, assorted widths, signed and unsigned
  m.F001Int = (shr64(8) & 0xfffff) - 0x80000; // +/-2^19 within +/-805495
  m.F003Int = (shr64(12) & 0xfffff) - 0x80000; // within +/-835897
  m.F005Uint = shr64(20) & 0xfff; // <=4095 within [0, 7316]
  m.F006Int = (shr64(26) & 0x7ff) - 1024; // +/-1024 within +/-1513
  m.F009Int = (shr64(33) & 31) - 16; // +/-16 within +/-22
  m.F033Uint = shr64(37) & 0x1ffff; // <=131071 within [0, 142780]
  m.F041Int = (shr64(42) & 63) - 32; // +/-32 within +/-55
  m.F062Uint = shr64(47) & 255; // <=255 within [0, 503]
  m.F088Int = (shr64(52) & 0x3ff) - 512; // +/-512 within +/-694
  m.F090Uint = shr64(57) & 127; // <=127 within [0, 214]
  // bits(N), narrow and wide
  m.F011Bits = rng.lo & 0x3ff; // 10 bits
  m.F023Bits = shr64(5) & 0x1ffffff; // 25 bits
  m.F042Bits = shr64(3) & 0x3fffffff; // 30 bits
  m.F081Bits = shr64(7) & 0x1fffffff; // 29 bits
  m.F089Bits = rngBig() & 0xffffffffffffn; // 48 bits
  m.F093Bits = rngBig() ^ 0x5555555555555555n; // 64 bits
  m.F097Bits = shr64(11) & 0xfff; // 12 bits
  // bools (NEVER the four branch gates — those are structure, §2.7)
  m.F037Bool = (rng.lo & 1) !== 0;
  m.F055Bool = (rng.lo & 2) !== 0;
  m.F092Bool = (rng.lo & 4) !== 0;
  // float32 / float64
  m.F007F32 = rng.lo & 0xffff;
  m.F020F32 = (shr64(16) & 0xffff) * 0.5;
  m.F058F32 = (shr64(24) & 0xffff) * 0.25;
  m.F002F64 = (shr64(8) & 0xffffff) * 0.5;
  m.F059F64 = (shr64(16) & 0xffffff) * 0.25;
  m.F087F64 = (shr64(24) & 0xffffff) * 0.125;
  // compressed floats (in range by construction)
  m.F004Cf32 = (rng.lo & 0x3fff) * 0.1; // <=1638.3 within [0, 2000]
  m.F061Cf32 = -90.0 + (shr64(9) & 255) * 0.5; // within [-90, 90] (max 37.5)
  m.F067Cf32 = -100.0 + (shr64(18) & 511) * 0.25; // within [-100, 100] (max 27.75)
  m.F072Cf32 = (shr64(27) & 8191) * 0.01; // <=81.91 within [0, 100]
  // fixed / ufixed (raw storage scaled by 2^F; bounds are whole units)
  m.F016Fixed = (shr64(10) & 0x3ffffff) - 0x2000000; // +/-2^25 within +/-36*2^20
  m.F025Fixed = (shr64(18) & 0x7fff) - 0x4000; // +/-2^14 within +/-119*2^8
  m.F095Fixed = (shr64(22) & 0x7ffffff) - 0x4000000; // +/-2^26 within +/-1577*2^16
  m.F021Ufixed = shr64(30) & 0x3ffffff; // <=2^26-1 within 25141*2^12
  m.F049Ufixed = shr64(36) & 0x7fff; // <=32767 within 3*2^14
  m.F084Ufixed = shr64(44) & 0x7f; // <=127 within 1*2^7
  // enum / flags (wire-valid by construction)
  m.F036Enum = shr64(30) & 3; // within wire range [0, 5]
  m.F083Enum = shr64(34) & 3;
  m.F091Flags = rngBig() & 31n; // 5 wire bits
  // full-width 64-bit
  m.F008U64 = rngBig();
  m.F029I64 = BigInt.asIntN(64, rngBig() * 3n);
  m.F063I64 = BigInt.asIntN(64, rngBig() * 5n);
  // fields riding inside the TAKEN branches (f012 true, f050 true)
  m.F013F32 = shr64(4) & 0xffff;
  m.F014Uint = shr64(21) & 511; // <=511 within [0, 775]
  m.F015Int = (shr64(40) & 31) - 16; // +/-16 within +/-21
  m.F017Uint = shr64(29) & 0xfff; // <=4095 within [0, 4606]
  m.F051Bool = (rng.lo & 8) !== 0;
  m.F052Int = (shr64(38) & 63) - 32; // +/-32 within +/-57
  m.F053F32 = (shr64(40) & 0xffff) * 0.125;
  m.F054Int = (shr64(45) & 63) - 32; // +/-32 within +/-35
}

// --------------------------------------------------------------------------
// the synthetic steady-state batch: NumBatchMessages messages through the
// Message dispatch surface (WriteMessage/ReadMessage) plus the None
// terminator, mixed types driven by the LCG — the C++ batch builder exactly.
// --------------------------------------------------------------------------

const NumBatchMessages = 4096;
const BatchPasses = 6400;

function buildBatch(batchBuffer) {
  const batch = new Array(NumBatchMessages);

  lcgSeed(12345);
  for (let k = 0; k < NumBatchMessages; k++) {
    lcgStep();
    const pick = shr64(32) % 20;
    if (pick < 5) {
      // 25% Chat
      const m = new ex.Chat();
      m.TextLength = 16 + (rng.lo & 15);
      for (let i = 0; i < m.TextLength; i++) {
        m.Text[i] = 97 + ((rng.lo >>> (i & 7)) & 15);
      }
      batch[k] = m;
    } else if (pick < 10) {
      // 25% Test
      const m = new ex.Test();
      m.TestA = rng.lo & 0xffff;
      m.TestB = shr64(16) & 511;
      m.TestC = shr64(25) & 511;
      m.TestD = shr64(34) & 511;
      batch[k] = m;
    } else if (pick < 13) {
      // 15% Synchronize
      const m = new ex.Synchronize();
      m.SyncFrame = rngBig();
      m.SyncSequence = shr64(8) & 0xffff;
      batch[k] = m;
    } else if (pick < 16) {
      // 15% Timescale
      const m = new ex.Timescale();
      m.Scale = (rng.lo & 0xffff) / 65536.0;
      m.FrameA = shr64(16);
      m.FrameB = shr64(24);
      batch[k] = m;
    } else if (pick < 18) {
      // 10% Heartbeat
      batch[k] = new ex.Heartbeat();
    } else {
      // 10% Block
      const m = new ex.Block();
      m.DataLength = 64 + (rng.lo & 127);
      for (let i = 0; i < m.DataLength; i++) {
        m.Data[i] = shr64(i & 31) & 0xff;
      }
      batch[k] = m;
    }
  }

  const ws = new WriteStream(batchBuffer);
  for (let k = 0; k < NumBatchMessages; k++) {
    if (!ex.WriteMessage(ws, batch[k])) {
      fail("message_batch", "batch write failed during setup");
      return null;
    }
  }
  if (!ex.WriteMessage(ws, null)) {
    // null is the None terminator
    fail("message_batch", "terminator write failed during setup");
    return null;
  }
  ws.flush();
  return { batch, batchBytes: ws.bytesProcessed() };
}

function benchBatch() {
  // worst case is NumBatchMessages * MessageMaxBytes; actual is far less.
  // + 8 read slack, and the size is a multiple of 8 (write contract).
  const batchBufferSize = (NumBatchMessages + 1) * ex.MessageMaxBytes + 8;
  const batchBuffer = new Uint8Array(batchBufferSize);

  const built = buildBatch(batchBuffer);
  if (built === null) {
    return;
  }
  const batch = built.batch;
  let batchBytes = built.batchBytes;

  const passes = BatchPasses;
  const totalMsgs = passes * NumBatchMessages;

  const writeRates = new Array(gNumRuns);
  const readRates = new Array(gNumRuns);

  // write path: whole batch per pass; one message mutates per pass so the
  // batch is never loop-invariant
  lcgSeed(999);
  const ws = new WriteStream(batchBuffer);
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let pass = 0; pass < passes; pass++) {
      lcgStep();
      const mutate = batch[shr64(16) & (NumBatchMessages - 1)];
      if (mutate instanceof ex.Synchronize) {
        mutate.SyncFrame = rngBig();
      } else if (mutate instanceof ex.Test) {
        mutate.TestA = rng.lo & 0xffff;
      }
      ws.reset(batchBuffer);
      for (let k = 0; k < NumBatchMessages; k++) {
        if (!ex.WriteMessage(ws, batch[k])) {
          fail("message_batch", "write failed in loop");
          return;
        }
      }
      ex.WriteMessage(ws, null);
      ws.flush();
      gSink = (gSink + ws.bytesProcessed()) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = totalMsgs / elapsed;
    }
  }

  // the read buffer: rebuild once from the deterministic batch state so
  // bytes match
  const rebuilt = buildBatch(batchBuffer);
  if (rebuilt === null) {
    return;
  }
  batchBytes = rebuilt.batchBytes;

  // read path: read messages until the None terminator, whole batch per
  // pass; reads land in pre-allocated storage — no heap per message
  const storage = new ex.MessageStorage();
  const holder = { value: null };
  const batchView = batchBuffer.subarray(0, batchBytes);
  const rs = new ReadStream(batchView);
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let pass = 0; pass < passes; pass++) {
      rs.reset(batchView);
      let count = 0;
      for (;;) {
        if (!ex.ReadMessage(rs, storage, holder)) {
          fail("message_batch", "read failed in loop");
          return;
        }
        if (holder.value === null) {
          break;
        }
        count++;
      }
      if (count !== NumBatchMessages) {
        fail("message_batch", "batch message count mismatch on read");
        return;
      }
      gSink = (gSink + count) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = totalMsgs / elapsed;
    }
  }

  const bytesPerMsg = Math.floor(batchBytes / NumBatchMessages);
  report("message_batch", "write", totalMsgs, bytesPerMsg, stats(writeRates), "gen");
  report("message_batch", "read", totalMsgs, bytesPerMsg, stats(readRates), "gen");
}

// --------------------------------------------------------------------------
// family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.js runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the plain WriteStream/ReadStream serialize* surface, the way a game would
// write them. Fields are persistent { value } holders (the serialize.js
// caller idiom, from the runtime's own bench), created once and mutated in
// place, so the loops allocate nothing of their own. The §1.5 oracle gate
// byte-compares the hand-written wire against the goldens the GENERATED
// code pinned (testdata/wire/bench_*.bin) and round-trips before any
// number. Per §3.2 every benched op has EXACTLY two call sites: its untimed
// once-helper and its timed loop — per-shape functions, as in the Go and C#
// runners.
// --------------------------------------------------------------------------

function makeRtPacket() {
  return {
    a: { value: 0 },
    b: { value: 0 },
    c: { value: 0 },
    bits7: { value: 0 },
    bits13: { value: 0 },
    bits23: { value: 0 },
    flag: { value: false },
    x: { value: 0 },
    y: { value: 0 },
    z: { value: 0 },
    big: { value: 0n },
    blob: new Uint8Array(17),
  };
}

function writeRtPacket(s, p) {
  return (
    s.serializeInt(p.a, -100, 100) &&
    s.serializeInt(p.b, 0, 65535) &&
    s.serializeInt(p.c, -1000000, 1000000) &&
    s.serializeBits(p.bits7, 7) &&
    s.serializeBits(p.bits13, 13) &&
    s.serializeBits(p.bits23, 23) &&
    s.serializeBool(p.flag) &&
    s.serializeFloat(p.x) &&
    s.serializeFloat(p.y) &&
    s.serializeFloat(p.z) &&
    s.serializeUint64(p.big) &&
    s.serializeBytes(p.blob) // aligns internally — the schema says `align` out loud
  );
}

function readRtPacket(s, p) {
  return (
    s.serializeInt(p.a, -100, 100) &&
    s.serializeInt(p.b, 0, 65535) &&
    s.serializeInt(p.c, -1000000, 1000000) &&
    s.serializeBits(p.bits7, 7) &&
    s.serializeBits(p.bits13, 13) &&
    s.serializeBits(p.bits23, 23) &&
    s.serializeBool(p.flag) &&
    s.serializeFloat(p.x) &&
    s.serializeFloat(p.y) &&
    s.serializeFloat(p.z) &&
    s.serializeUint64(p.big) &&
    s.serializeBytes(p.blob)
  );
}

function makeRtInts() {
  return {
    f0: { value: 0 },
    f1: { value: 0 },
    f2: { value: 0 },
    f3: { value: 0 },
    f4: { value: 0 },
    f5: { value: 0 },
    f6: { value: 0 },
    f7: { value: 0 },
    f8: { value: 0 },
    f9: { value: 0 },
  };
}

function writeRtInts(s, f) {
  return (
    s.serializeInt(f.f0, -100, 100) &&
    s.serializeInt(f.f1, 0, 65535) &&
    s.serializeInt(f.f2, -1000000, 1000000) &&
    s.serializeInt(f.f3, 0, 3) &&
    s.serializeInt(f.f4, -15, 15) &&
    s.serializeInt(f.f5, 0, 1000) &&
    s.serializeInt(f.f6, -2048, 2047) &&
    s.serializeInt(f.f7, 0, 255) &&
    s.serializeInt(f.f8, -600000, 600000) &&
    s.serializeInt(f.f9, 0, 100)
  );
}

function readRtInts(s, f) {
  return (
    s.serializeInt(f.f0, -100, 100) &&
    s.serializeInt(f.f1, 0, 65535) &&
    s.serializeInt(f.f2, -1000000, 1000000) &&
    s.serializeInt(f.f3, 0, 3) &&
    s.serializeInt(f.f4, -15, 15) &&
    s.serializeInt(f.f5, 0, 1000) &&
    s.serializeInt(f.f6, -2048, 2047) &&
    s.serializeInt(f.f7, 0, 255) &&
    s.serializeInt(f.f8, -600000, 600000) &&
    s.serializeInt(f.f9, 0, 100)
  );
}

function makeRtBits() {
  return {
    b7: { value: 0 },
    b13: { value: 0 },
    b23: { value: 0 },
    b3: { value: 0 },
    b32: { value: 0 },
    b11: { value: 0 },
    b19: { value: 0 },
    b48: { value: 0n },
  };
}

function writeRtBits(s, f) {
  return (
    s.serializeBits(f.b7, 7) &&
    s.serializeBits(f.b13, 13) &&
    s.serializeBits(f.b23, 23) &&
    s.serializeBits(f.b3, 3) &&
    s.serializeBits(f.b32, 32) &&
    s.serializeBits(f.b11, 11) &&
    s.serializeBits(f.b19, 19) &&
    s.serializeBits64(f.b48, 48)
  );
}

function readRtBits(s, f) {
  return (
    s.serializeBits(f.b7, 7) &&
    s.serializeBits(f.b13, 13) &&
    s.serializeBits(f.b23, 23) &&
    s.serializeBits(f.b3, 3) &&
    s.serializeBits(f.b32, 32) &&
    s.serializeBits(f.b11, 11) &&
    s.serializeBits(f.b19, 19) &&
    s.serializeBits64(f.b48, 48)
  );
}

function makeRtMixed() {
  return {
    sequence: { value: 0 },
    ackBits: { value: 0 },
    entityId: { value: 0 },
    posX: { value: 0 },
    posY: { value: 0 },
    posZ: { value: 0 },
    yaw: { value: 0 },
    moving: { value: false },
    firing: { value: false },
    timestamp: { value: 0n },
    weapon: { value: 0 },
  };
}

function writeRtMixed(s, f) {
  return (
    s.serializeInt(f.sequence, 0, 65535) &&
    s.serializeBits(f.ackBits, 32) &&
    s.serializeBits(f.entityId, 12) &&
    s.serializeInt(f.posX, -16384, 16383) &&
    s.serializeInt(f.posY, -16384, 16383) &&
    s.serializeInt(f.posZ, -16384, 16383) &&
    s.serializeBits(f.yaw, 9) &&
    s.serializeBool(f.moving) &&
    s.serializeBool(f.firing) &&
    s.serializeBits64(f.timestamp, 48) &&
    s.serializeInt(f.weapon, 0, 15)
  );
}

function readRtMixed(s, f) {
  return (
    s.serializeInt(f.sequence, 0, 65535) &&
    s.serializeBits(f.ackBits, 32) &&
    s.serializeBits(f.entityId, 12) &&
    s.serializeInt(f.posX, -16384, 16383) &&
    s.serializeInt(f.posY, -16384, 16383) &&
    s.serializeInt(f.posZ, -16384, 16383) &&
    s.serializeBits(f.yaw, 9) &&
    s.serializeBool(f.moving) &&
    s.serializeBool(f.firing) &&
    s.serializeBits64(f.timestamp, 48) &&
    s.serializeInt(f.weapon, 0, 15)
  );
}

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

function pinRtPacket() {
  const p = makeRtPacket();
  p.a.value = -37;
  p.b.value = 12345;
  p.c.value = 987654;
  p.bits7.value = 97;
  p.bits13.value = 5000;
  p.bits23.value = 1234567;
  p.flag.value = true;
  p.x.value = 1.5;
  p.y.value = -3.25;
  p.z.value = 100.125;
  p.big.value = 0x123456789abcdef0n;
  for (let i = 0; i < 17; i++) {
    p.blob[i] = (i * 31) & 0xff;
  }
  return p;
}

function pinRtInts() {
  const f = makeRtInts();
  f.f0.value = -37;
  f.f1.value = 12345;
  f.f2.value = 987654;
  f.f3.value = 2;
  f.f4.value = -15;
  f.f5.value = 777;
  f.f6.value = -2048;
  f.f7.value = 200;
  f.f8.value = -543210;
  f.f9.value = 99;
  return f;
}

function pinRtBits() {
  const f = makeRtBits();
  f.b7.value = 97;
  f.b13.value = 5000;
  f.b23.value = 1234567;
  f.b3.value = 5;
  f.b32.value = 0xdeadbeef;
  f.b11.value = 1024;
  f.b19.value = 333333;
  f.b48.value = 0xfedcba987654n;
  return f;
}

function pinRtMixed() {
  const f = makeRtMixed();
  f.sequence.value = 52428;
  f.ackBits.value = 0xa5a5a5a5;
  f.entityId.value = 2049;
  f.posX.value = -16384;
  f.posY.value = 16383;
  f.posZ.value = -1;
  f.yaw.value = 511;
  f.moving.value = true;
  f.firing.value = false;
  f.timestamp.value = 0x123456789abcn;
  f.weapon.value = 15;
  return f;
}

// ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

function varyRtPacket(p) {
  p.a.value = (shr64(8) & 63) - 32;
  p.b.value = shr64(16) & 65535;
  p.c.value = (shr64(24) & 0xfffff) - 500000;
  p.bits7.value = rng.lo & 127;
  p.bits13.value = shr64(3) & 8191;
  p.bits23.value = shr64(5) & 8388607;
  p.flag.value = (rng.lo & 1) !== 0;
  p.x.value = rng.lo & 0xffff;
  p.big.value = rngBig();
  p.blob[0] = shr64(32) & 0xff;
}

function varyRtInts(f) {
  f.f0.value = (shr64(8) & 63) - 32;
  f.f1.value = shr64(16) & 65535;
  f.f2.value = (shr64(24) & 0xfffff) - 500000;
  f.f3.value = shr64(2) & 3;
  f.f4.value = (shr64(11) & 15) - 8;
  f.f5.value = shr64(22) & 511;
  f.f6.value = (shr64(33) & 2047) - 1024;
  f.f7.value = shr64(40) & 255;
  f.f8.value = (shr64(30) & 0xfffff) - 500000;
  f.f9.value = shr64(57) & 63;
}

function varyRtBits(f) {
  f.b7.value = rng.lo & 127;
  f.b13.value = shr64(3) & 8191;
  f.b23.value = shr64(5) & 8388607;
  f.b3.value = shr64(29) & 7;
  f.b32.value = shr64(16);
  f.b11.value = shr64(37) & 2047;
  f.b19.value = shr64(44) & 524287;
  f.b48.value = rngBig() & 0xffffffffffffn;
}

function varyRtMixed(f) {
  f.sequence.value = shr64(8) & 65535;
  f.ackBits.value = shr64(16);
  f.entityId.value = rng.lo & 4095;
  f.posX.value = (shr64(20) & 32767) - 16384;
  f.posY.value = (shr64(25) & 32767) - 16384;
  f.posZ.value = (shr64(30) & 32767) - 16384;
  f.yaw.value = shr64(3) & 511;
  f.moving.value = (rng.lo & 1) !== 0;
  f.firing.value = (rng.lo & 2) !== 0;
  f.timestamp.value = rngBig() & 0xffffffffffffn;
  f.weapon.value = shr64(60) & 15;
}

// ---- the single untimed call sites (§3.2), one pair per shape ----

function rtOnceWritePacket(p, buf) {
  const ws = new WriteStream(buf);
  if (!writeRtPacket(ws, p)) {
    return -1;
  }
  ws.flush();
  return ws.bytesProcessed();
}

function rtOnceReadPacket(p, view) {
  const rs = new ReadStream(view);
  return readRtPacket(rs, p);
}

function rtOnceWriteInts(f, buf) {
  const ws = new WriteStream(buf);
  if (!writeRtInts(ws, f)) {
    return -1;
  }
  ws.flush();
  return ws.bytesProcessed();
}

function rtOnceReadInts(f, view) {
  const rs = new ReadStream(view);
  return readRtInts(rs, f);
}

function rtOnceWriteBits(f, buf) {
  const ws = new WriteStream(buf);
  if (!writeRtBits(ws, f)) {
    return -1;
  }
  ws.flush();
  return ws.bytesProcessed();
}

function rtOnceReadBits(f, view) {
  const rs = new ReadStream(view);
  return readRtBits(rs, f);
}

function rtOnceWriteMixed(f, buf) {
  const ws = new WriteStream(buf);
  if (!writeRtMixed(ws, f)) {
    return -1;
  }
  ws.flush();
  return ws.bytesProcessed();
}

function rtOnceReadMixed(f, view) {
  const rs = new ReadStream(view);
  return readRtMixed(rs, f);
}

// ---- the timed loops, one function per (shape, path) as in the Go and C#
// runners; streams reuse via reset — the runtime's documented no-allocation
// path, as the gen benches do ----

function rtBenchPacketWriteLoop(base, iters) {
  const ws = new WriteStream(gBuffer);
  for (let i = 0; i < iters; i++) {
    lcgStep();
    varyRtPacket(base);
    ws.reset(gBuffer);
    if (!writeRtPacket(ws, base)) {
      return false;
    }
    ws.flush();
    gSink = (gSink + ws.bytesProcessed()) >>> 0;
  }
  return true;
}

function rtBenchPacketReadLoop(outValue, iters, views) {
  const rs = new ReadStream(views[0]);
  for (let i = 0; i < iters; i++) {
    rs.reset(views[i & (NumVariants - 1)]);
    if (!readRtPacket(rs, outValue)) {
      return false;
    }
    gSink = (gSink + 1) >>> 0;
  }
  return true;
}

function rtBenchIntsWriteLoop(base, iters) {
  const ws = new WriteStream(gBuffer);
  for (let i = 0; i < iters; i++) {
    lcgStep();
    varyRtInts(base);
    ws.reset(gBuffer);
    if (!writeRtInts(ws, base)) {
      return false;
    }
    ws.flush();
    gSink = (gSink + ws.bytesProcessed()) >>> 0;
  }
  return true;
}

function rtBenchIntsReadLoop(outValue, iters, views) {
  const rs = new ReadStream(views[0]);
  for (let i = 0; i < iters; i++) {
    rs.reset(views[i & (NumVariants - 1)]);
    if (!readRtInts(rs, outValue)) {
      return false;
    }
    gSink = (gSink + 1) >>> 0;
  }
  return true;
}

function rtBenchBitsWriteLoop(base, iters) {
  const ws = new WriteStream(gBuffer);
  for (let i = 0; i < iters; i++) {
    lcgStep();
    varyRtBits(base);
    ws.reset(gBuffer);
    if (!writeRtBits(ws, base)) {
      return false;
    }
    ws.flush();
    gSink = (gSink + ws.bytesProcessed()) >>> 0;
  }
  return true;
}

function rtBenchBitsReadLoop(outValue, iters, views) {
  const rs = new ReadStream(views[0]);
  for (let i = 0; i < iters; i++) {
    rs.reset(views[i & (NumVariants - 1)]);
    if (!readRtBits(rs, outValue)) {
      return false;
    }
    gSink = (gSink + 1) >>> 0;
  }
  return true;
}

function rtBenchMixedWriteLoop(base, iters) {
  const ws = new WriteStream(gBuffer);
  for (let i = 0; i < iters; i++) {
    lcgStep();
    varyRtMixed(base);
    ws.reset(gBuffer);
    if (!writeRtMixed(ws, base)) {
      return false;
    }
    ws.flush();
    gSink = (gSink + ws.bytesProcessed()) >>> 0;
  }
  return true;
}

function rtBenchMixedReadLoop(outValue, iters, views) {
  const rs = new ReadStream(views[0]);
  for (let i = 0; i < iters; i++) {
    rs.reset(views[i & (NumVariants - 1)]);
    if (!readRtMixed(rs, outValue)) {
      return false;
    }
    gSink = (gSink + 1) >>> 0;
  }
  return true;
}

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

function benchRt(name, iters, base, outValue, onceWrite, onceRead, writeLoop, readLoop, vary) {
  // oracle 1: the hand-written wire must equal the generated-code golden
  const bytesPerOp = onceWrite(base, gBuffer);
  if (bytesPerOp < 0) {
    fail(name, "write of pinned instance failed");
    return;
  }
  if (!checkGolden(name, gBuffer.subarray(0, bytesPerOp))) {
    failed = true;
    return;
  }

  // oracle 2: round-trip write -> read -> re-write -> identical bytes
  if (!onceRead(outValue, gBuffer.subarray(0, bytesPerOp))) {
    fail(name, "read of pinned instance failed");
    return;
  }
  if (onceWrite(outValue, gTwin) !== bytesPerOp || !bytesEqual(gTwin.subarray(0, bytesPerOp), gBuffer.subarray(0, bytesPerOp))) {
    fail(name, "round-trip bytes differ");
    return;
  }

  // variant buffers (and proof that variation keeps bytes/op constant)
  lcgSeed(1);
  const views = [];
  for (let k = 0; k < NumVariants; k++) {
    lcgStep();
    vary(base);
    if (onceWrite(base, gVariants[k]) !== bytesPerOp) {
      fail(name, "variation changed bytes/op — vary must keep structure fields fixed");
      return;
    }
    views.push(gVariants[k].subarray(0, bytesPerOp));
  }

  const writeRates = new Array(gNumRuns);
  const readRates = new Array(gNumRuns);

  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    if (!writeLoop(base, iters)) {
      fail(name, "write failed in loop");
      return;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    if (!readLoop(outValue, iters, views)) {
      fail(name, "read failed in loop");
      return;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = iters / elapsed;
    }
  }

  report(name, "write", iters, bytesPerOp, stats(writeRates), "rt");
  report(name, "read", iters, bytesPerOp, stats(readRates), "rt");
}

// --------------------------------------------------------------------------
// family bits (BENCH-STANDARD.md §1.4): the raw BitWriter/BitReader with
// the 16-width table (227 bits/group) over a 65536-byte buffer, the ONE
// bitpacker workload in the estate. Values vary per pass through the LCG
// (widths are the structure and stay fixed; bytes/pass asserted constant);
// reads rotate 64 pre-written variant buffers, each verified to read back
// exactly what was written before any number is produced.
// --------------------------------------------------------------------------

const BitsNumWidths = 16;
const BitsBufferSize = 65536;
const bitsWidths = [1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22]; // 227 bits/group

const gBitsBuffer = new Uint8Array(BitsBufferSize);
const gBitsVariants = [];
for (let k = 0; k < NumVariants; k++) {
  gBitsVariants.push(new Uint8Array(BitsBufferSize));
}

function bitsMask(width) {
  return width === 32 ? 0xffffffff : ((1 << width) >>> 0) - 1;
}

// the per-pass value variation: one LCG step per pass, values from its bits.
// The unsigned re-cast matters: `& 0xffffffff` alone lands in ToInt32 and a
// value with bit 31 set would go negative, where the C runner stores uint32
// — readBits returns unsigned, so the read-back gate would refuse.
function varyBitsValues(values) {
  for (let i = 0; i < BitsNumWidths; i++) {
    values[i] = (shr64(i) & bitsMask(bitsWidths[i])) >>> 0;
  }
}

// the single untimed writeBits call site (§3.2)
function bitsWritePass(buffer, values) {
  const w = new BitWriter(buffer);
  while (w.bitsAvailable() >= 256) {
    for (let i = 0; i < BitsNumWidths; i++) {
      w.writeBits(values[i], bitsWidths[i]);
    }
  }
  w.flushBits();
  return w.bytesWritten();
}

// the single untimed readBits call site (§3.2): the buffer must read back
// exactly the values written — the bits family's refusal gate
function bitsReadVerify(buffer, values) {
  const r = new BitReader(buffer);
  while (r.bitsRemaining() >= 256) {
    for (let i = 0; i < BitsNumWidths; i++) {
      if (r.readBits(bitsWidths[i]) !== values[i]) {
        return false;
      }
    }
  }
  return true;
}

function bitpackerWriteLoop(passes, bytesPerPass, values) {
  const w = new BitWriter(gBitsBuffer);
  for (let pass = 0; pass < passes; pass++) {
    lcgStep();
    varyBitsValues(values);
    w.reset(gBitsBuffer);
    while (w.bitsAvailable() >= 256) {
      for (let i = 0; i < BitsNumWidths; i++) {
        w.writeBits(values[i], bitsWidths[i]);
      }
    }
    w.flushBits();
    if (w.bytesWritten() !== bytesPerPass) {
      return false; // the bytes_per_op assertion (§2.7)
    }
    gSink = (gSink + w.bytesWritten()) >>> 0;
  }
  return true;
}

function bitpackerReadLoop(passes) {
  const r = new BitReader(gBitsVariants[0]);
  for (let pass = 0; pass < passes; pass++) {
    r.reset(gBitsVariants[pass & (NumVariants - 1)]);
    let sum = 0;
    while (r.bitsRemaining() >= 256) {
      for (let i = 0; i < BitsNumWidths; i++) {
        sum += r.readBits(bitsWidths[i]);
      }
    }
    gSink = (gSink + sum) >>> 0;
  }
  return true;
}

function benchBitpacker(passes) {
  const values = new Array(BitsNumWidths).fill(0);

  lcgSeed(1);
  let bytesPerPass = -1;
  for (let k = 0; k < NumVariants; k++) {
    lcgStep();
    varyBitsValues(values);
    const wrote = bitsWritePass(gBitsVariants[k], values);
    if (bytesPerPass < 0) {
      bytesPerPass = wrote;
    }
    if (wrote !== bytesPerPass) {
      fail("bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed");
      return;
    }
    if (!bitsReadVerify(gBitsVariants[k], values)) {
      fail("bitpacker", "read-back disagrees with written values — refusing to bench");
      return;
    }
  }

  const writeRates = new Array(gNumRuns);
  const readRates = new Array(gNumRuns);

  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    if (!bitpackerWriteLoop(passes, bytesPerPass, values)) {
      fail("bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)");
      return;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = passes / elapsed;
    }
  }

  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    if (!bitpackerReadLoop(passes)) {
      fail("bitpacker", "read loop failed");
      return;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = passes / elapsed;
    }
  }

  report("bitpacker", "write", passes, bytesPerPass, stats(writeRates), "bits");
  report("bitpacker", "read", passes, bytesPerPass, stats(readRates), "bits");
}

// --------------------------------------------------------------------------

// the message_stream golden: dispatch wire self-check (not a benchmark)
function checkMessageStreamGolden() {
  const chat = new ex.Chat();
  chat.Text.set(textBytes("dispatch"));
  chat.TextLength = 8;
  const test = new ex.Test();
  test.TestB = 42;

  const ws = new WriteStream(gBuffer);
  if (!ex.WriteMessage(ws, chat) || !ex.WriteMessage(ws, test) || !ex.WriteMessage(ws, null)) {
    fail("message_stream", "dispatch write failed");
    return;
  }
  ws.flush();
  if (!checkGolden("message_stream", gBuffer.subarray(0, ws.bytesProcessed()))) {
    failed = true;
  }
}

// --------------------------------------------------------------------------

function main() {
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--csv") {
      gCsv = true;
    } else if (args[i] === "--wire-dir" && i + 1 < args.length) {
      gWireDir = args[++i];
    } else if (args[i] === "--round" && i + 1 < args.length) {
      // §2.4: one warmup + one measured run of every benchmark, then exit.
      // K only identifies the round to the interleaved driver, which
      // aggregates max/median/min/spread across rounds itself.
      const k = Number(args[++i]);
      if (!Number.isInteger(k) || k < 0) {
        process.stderr.write(`--round takes a non-negative integer, got '${args[i]}'\n`);
        process.exit(1);
      }
      gNumRuns = 1;
    } else {
      process.stderr.write("usage: node main.mjs [--csv] [--round K] [--wire-dir <dir>] [--print-runtime]\n");
      process.exit(1);
    }
  }

  process.stderr.write(`schema bench (js, node ${process.versions.node}, ${PRODUCTION ? "production" : "checked"} mode)\n`);

  checkMessageStreamGolden();

  // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
  const atRest = pinRigidBodyMoving();
  atRest.AtRest = true;

  benchMessage("rigidbody_moving", "rigidbody_moving", 24000000, pinRigidBodyMoving(), ex.WriteRigidBody, ex.ReadRigidBody, varyRigidBody);
  benchMessage("rigidbody_at_rest", "rigidbody_at_rest", 32000000, atRest, ex.WriteRigidBody, ex.ReadRigidBody, varyRigidBodyAtRest);
  benchMessage("chat", "chat", 48000000, pinChat(), ex.WriteChat, ex.ReadChat, varyChat);
  benchMessage("test", null, 192000000, new ex.Test(), ex.WriteTest, ex.ReadTest, varyTest);
  benchMessage("inputpacket", "inputpacket", 16000000, pinInputPacket(), ex.WriteInputPacket, ex.ReadInputPacket, varyInputPacket);
  benchMessage("shipcreate", "shipcreate_flags", 32000000, pinShipCreate(), ex.WriteShipCreate, ex.ReadShipCreate, varyShipCreate);
  benchMessage("ship_shallow", "ship_shallow", 32000000, pinShipShallow(), ex.WriteShipData_Shallow, ex.ReadShipData_Shallow, varyShipShallow);
  benchMessage("probe_header", "probe_header", 256000000, pinProbeHeader(), ex.WriteProbeHeader, ex.ReadProbeHeader, varyProbeHeader);
  benchMessage("probebits", "probebits", 128000000, pinProbeBits(), ex.WriteProbeBits, ex.ReadProbeBits, varyProbeBits);
  benchMessage("probearray", "probearray", 20000000, pinProbeArray(), ex.WriteProbeArray, ex.ReadProbeArray, varyProbeArray);
  benchMessage("testdata", "testdata", 8000000, pinTestData(), ex.WriteTestData, ex.ReadTestData, varyTestData);

  // real_packet (§1.7): the realistic snapshot — ~93 riding individually
  // serialized small fields, 204 wire bytes, 0% bulk share by bits. The
  // pin is the ALL-DEFAULTS instance: construction installs the SPECIFIED
  // defaults (the four branch gates: f012 true, f043 false, f050 true,
  // f074 false) exactly as C++ RealPacket{} does. base_iters sized in the
  // C++ reference (§2.1).
  benchMessage("real_packet", "real_packet", 8000000, new realworld.RealPacket(), realworld.WriteRealPacket, realworld.ReadRealPacket, varyRealPacket);

  benchBatch();

  // family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
  // the goldens the generated code pinned. Iteration counts are fixed and
  // identical across all six runners (§2.1; sized in the C++ reference).
  benchRt("bench_packet", 32000000, pinRtPacket(), makeRtPacket(), rtOnceWritePacket, rtOnceReadPacket, rtBenchPacketWriteLoop, rtBenchPacketReadLoop, varyRtPacket);
  benchRt("bench_ints", 40000000, pinRtInts(), makeRtInts(), rtOnceWriteInts, rtOnceReadInts, rtBenchIntsWriteLoop, rtBenchIntsReadLoop, varyRtInts);
  benchRt("bench_bits", 48000000, pinRtBits(), makeRtBits(), rtOnceWriteBits, rtOnceReadBits, rtBenchBitsWriteLoop, rtBenchBitsReadLoop, varyRtBits);
  benchRt("bench_mixed", 40000000, pinRtMixed(), makeRtMixed(), rtOnceWriteMixed, rtOnceReadMixed, rtBenchMixedWriteLoop, rtBenchMixedReadLoop, varyRtMixed);

  // family bits (§1.4): the one bitpacker workload in the estate
  benchBitpacker(24576);

  flushCsv(); // rows carry the corpus_id of the goldens this run loaded

  if (failed) {
    process.stderr.write(`BENCH FAILED (corpus_id ${corpusId()})\n`);
    process.exit(1);
  }
  process.stderr.write(`OK (corpus_id ${corpusId()})\n`);
}

main();
