// schema bench — the JavaScript runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated JS modules and the serialize.js runtime: same benchmark set
// (real_packet included), same pinned corpus instances, same LCG and
// vary-function field mappings, same golden + round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=js. See
// bench/README.md for the runner contract. One module, like bench/c's one
// translation unit: the harness helpers are module-scoped.
//
// TWO GENERATED CODECS ride this runner (BENCH-STANDARD.md §5.1 codec
// column, 2026-08-18): the gen-family rows measure the FLAT tier
// (codec=flat) — THE js path under the ruling ("whichever correct
// implementation is fastest is the one we use for JavaScript") — per-call,
// §3.2's cross-language-comparable shape, each leg golden-gated AND
// cross-validated against the runtime tier (bytes, fields, verdicts, 64
// variants) before any timing. The runtime-call generated rows ride beside
// them as labeled supplementary rows (codec=runtime). Flat rows carry no
// runtime version: the flat modules import nothing, and the preamble's
// schema commit is their whole provenance (§3.5).
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
import * as wire from "../../generated/js/Wire.js";
import * as realworld from "../../generated/bench/js/realworld/RealWorld.js";

// THE js path: the flat tier (the 2026-08-18 ruling — whichever correct
// implementation is fastest is the one we use for JavaScript). The flat
// modules import no runtime; their rows carry codec=flat and the schema
// commit in the preamble is their whole provenance (§3.5). The runtime-call
// generated rows ride beside them as labeled supplementary rows
// (codec=runtime) so the compat tier stays observable; they never stand as
// the js number.
import * as typesFlat from "../../generated/js/TypesFlat.js";
import * as wireFlat from "../../generated/js/WireFlat.js";
import * as realworldFlat from "../../generated/bench/js/realworld/RealWorldFlat.js";

// bench/corpus/Bench.schema's BenchMixed as GENERATED code: the flat tier
// is THE js path for this shape exactly as for the corpus shapes above,
// cross-validated against the runtime-call tier by the same oracle.
import * as bench from "../../generated/bench/js/Bench.js";
import * as benchFlat from "../../generated/bench/js/BenchFlat.js";

// one namespace over the unit, the way Go sees package example — the checker
// guarantees unit-wide name uniqueness, so the merge cannot collide
const ex = { ...enums, ...types, ...wire };
const exFlat = { ...typesFlat, ...wireFlat };

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
let gQuick = false; // --quick: flat bench_mixed only, 3 measured runs —
// the iteration instrument, never the certification instrument
const QuickMixedIters = 4000000;
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
// family is per ROW (gen | bits — §5.1).
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
  // the CSV v2 header (§5.1), as every other runner emits — its absence
  // under --only js was the #175 cosmetic
  process.stdout.write(
    "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," +
    "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
  for (const r of gCsvRows) {
    // the §5.1 codec column is appended only on rows that carry one (the
    // generated-tier rows: flat is THE js path, runtime is supplementary)
    const codec = r.codec ? `,${r.codec}` : "";
    process.stdout.write(`${r.row},${id},${r.family},${CsvSuffix}${codec}\n`);
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

/* --------------------------------------------------------------------------
   read-side sink discipline (#175, equalized to the cpp/c reference): every
   read loop observes the FULL decoded struct per iteration. The C/C++ legs
   get this for free from an empty-asm memory clobber over the whole struct;
   JS has no clobber, so the leg's idiom is a per-iteration fold of every
   decoded field into gSink — numbers added, booleans as 0/1, byte/number
   arrays element-by-element over the decoded extent, and BigInt fields
   observed through an allocation-free comparison (a BigInt add would
   allocate per iteration and measure the allocator, not the observation).
   The folds are real work the clobber languages do not pay; the published
   number is an upper bound on the observation cost. Each sinkOf is checked
   finite at the gate, so a typo'd field name (undefined -> NaN) refuses to
   bench instead of silently poisoning the sink.
   -------------------------------------------------------------------------- */

const boolBit = (b) => (b ? 1 : 0);
const bigBit = (x) => (x !== 0n ? 1 : 0);
function sumBytes(a, n) {
  let s = 0;
  for (let i = 0; i < n; i++) {
    s += a[i];
  }
  return s;
}

function sinkOfRigidBody(d) {
  return d.Position.X + d.Position.Y + d.Position.Z +
    d.Orientation.X + d.Orientation.Y + d.Orientation.Z + d.Orientation.W +
    boolBit(d.AtRest) +
    d.LinearVelocity.X + d.LinearVelocity.Y + d.LinearVelocity.Z +
    d.AngularVelocity.X + d.AngularVelocity.Y + d.AngularVelocity.Z;
}

function sinkOfChat(d) {
  return d.TextLength + sumBytes(d.Text, d.TextLength);
}

function sinkOfTest(d) {
  return d.TestA + d.TestB + d.TestC + d.TestD;
}

function sinkOfInputPacket(d) {
  let s = d.SynchronizeSequence + bigBit(d.CurrentFrame) + bigBit(d.StartFrame) + d.InputsCount;
  for (let i = 0; i < d.InputsCount; i++) {
    const inp = d.Inputs[i];
    s += inp.StickX + inp.StickY + inp.Throttle + inp.Yaw + inp.Pitch +
      boolBit(inp.Fire) + boolBit(inp.AltFire) + boolBit(inp.Boost) + boolBit(inp.Brake) +
      boolBit(inp.Aim) + boolBit(inp.LockOn) + boolBit(inp.Zoom) + boolBit(inp.Ping);
  }
  return s;
}

function sinkOfShipCreate(d) {
  return d.ShipType +
    d.Position.X + d.Position.Y + d.Position.Z +
    d.Rotation.X + d.Rotation.Y + d.Rotation.Z + d.Rotation.W +
    d.LinearVelocity.X + d.LinearVelocity.Y + d.LinearVelocity.Z +
    boolBit(d.HasFlags) + bigBit(d.Flags) + d.Team + d.Health + d.Thrust + d.Pending;
}

function sinkOfProbeHeader(d) {
  return d.Version + bigBit(d.ProbeId);
}

function sinkOfProbeBits(d) {
  return d.Small + bigBit(d.Boundary) + bigBit(d.Wide) + d.Sensor + bigBit(d.Nonce);
}

function sinkOfProbeSample(s) {
  let v = boolBit(s.Active) + s.Orientation + s.RawDelta + bigBit(s.BigDelta) +
    s.Weapon + boolBit(s.HasTarget) + s.TargetId + s.IdleTicks + s.SamplesCount;
  for (let i = 0; i < s.SamplesCount; i++) {
    v += s.Samples[i];
  }
  return v;
}

function sinkOfProbeArray(d) {
  return sinkOfProbeSample(d.Samples[0]) + sinkOfProbeSample(d.Samples[1]) +
    d.Config.Retries + d.Config.Preferred;
}

function sinkOfTestData(d) {
  let s = d.A + d.B + d.C + d.D + d.E + d.F + boolBit(d.G) + d.ItemsCount;
  for (let i = 0; i < d.ItemsCount; i++) {
    s += d.Items[i];
  }
  s += d.FloatValue + d.CompressedFloatValue + d.DoubleValue +
    d.Int8Value + d.Int16Value + d.Uint8Value + d.Uint16Value + d.Uint32Value +
    bigBit(d.Uint64Value) + bigBit(d.Int64Full) + bigBit(d.Int64Range) +
    sumBytes(d.FixedBytes, 17) + d.TextLength + sumBytes(d.Text, d.TextLength);
  return s;
}

function sinkOfRealPacket(d) {
  return d.F001Int + d.F002F64 + d.F003Int + d.F004Cf32 + d.F005Uint +
    d.F006Int + d.F007F32 + bigBit(d.F008U64) + d.F009Int + d.F010F32 +
    d.F011Bits + boolBit(d.F012Bool) + d.F013F32 + d.F014Uint + d.F015Int +
    d.F016Fixed + d.F017Uint + d.F018Int + d.F019F64 + d.F020F32 +
    d.F021Ufixed + d.F022F32 + d.F023Bits + d.F024F32 + d.F025Fixed +
    d.F026Bits + d.F027Cf32 + d.F028Bits + bigBit(d.F029I64) + d.F030F32 +
    d.F031Bits + d.F032Int + d.F033Uint + d.F034Uint + d.F035Bits +
    d.F036Enum + boolBit(d.F037Bool) + boolBit(d.F038Bool) + d.F039Bits + d.F040Fixed +
    d.F041Int + d.F042Bits + boolBit(d.F043Bool) + d.F044F32 + d.F045Bits +
    d.F046Uint + d.F047Int + d.F048F64 + d.F049Ufixed + boolBit(d.F050Bool) +
    boolBit(d.F051Bool) + d.F052Int + d.F053F32 + d.F054Int + boolBit(d.F055Bool) +
    d.F056Int + d.F057Int + d.F058F32 + d.F059F64 + d.F060Bits +
    d.F061Cf32 + d.F062Uint + bigBit(d.F063I64) + d.F064Uint + d.F065Cf32 +
    d.F066Ufixed + d.F067Cf32 + d.F068Cf32 + d.F069Bits + d.F070Uint +
    d.F071Cf32 + d.F072Cf32 + d.F073Int + boolBit(d.F074Bool) + bigBit(d.F075U64) +
    d.F076Int + d.F077Int + d.F078Bits + d.F079Uint + boolBit(d.F080Bool) +
    d.F081Bits + d.F082Bits + d.F083Enum + d.F084Ufixed + d.F085Bits +
    d.F086Uint + d.F087F64 + d.F088Int + bigBit(d.F089Bits) + d.F090Uint +
    bigBit(d.F091Flags) + boolBit(d.F092Bool) + bigBit(d.F093Bits) + boolBit(d.F094Bool) + d.F095Fixed +
    d.F096Bits + d.F097Bits;
}

let gCsv = false;
let gWireDir = "../../testdata/wire";
let gVariantDir = "../../bench/corpus/variants";
let failed = false;

// --------------------------------------------------------------------------
// the C bench's uint64 LCG (Knuth MMIX: rng * 6364136223846793005 +
// 1442695040888963407 mod 2^64), carried in two 32-bit lanes — the exact
// generator serialize.js/bench/bench.js authored, seedable because the
// schema runners seed it per section (1 for the per-bench variants). Lane
// arithmetic is
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

function report(bench, path_, iters, bytesPerOp, s, family, codec = "") {
  const mbps = (s.median * bytesPerOp) / (1024.0 * 1024.0);
  const tag = codec ? ` [${codec}]` : "";
  process.stderr.write(
    `${(bench + tag).padEnd(18)} ${path_.padEnd(5)} ${(s.median / 1e6).toFixed(2).padStart(10)} M msg/s ` +
      `${mbps.toFixed(1).padStart(10)} MB/s   (min ${(s.min / 1e6).toFixed(2)}, max ${(s.max / 1e6).toFixed(2)}, ` +
      `spread ${s.spread.toFixed(1)}%)\n`
  );
  if (gCsv) {
    gCsvRows.push({
      row:
        `js,${bench},${path_},${iters},${bytesPerOp},${gNumRuns},` +
        `${s.median.toFixed(0)},${s.min.toFixed(0)},${s.max.toFixed(0)},${mbps.toFixed(2)},${s.spread.toFixed(2)}`,
      family,
      codec,
    });
  }
}

// deepEqual is the cross-tier field comparison (the test legs' helper): the
// flat oracle holds reads FIELD-identical to the runtime tier, not just
// byte-identical on re-write.
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

// --------------------------------------------------------------------------
// the per-shape benchmark drivers. benchMessageFlat measures THE js path
// (the flat tier, codec=flat); benchMessage measures the runtime-call
// generated tier, riding as labeled supplementary rows (codec=runtime).
// --------------------------------------------------------------------------

// benchMessageFlat: the §1.5 oracle gate binds the flat leg to the corpus
// pins AND to the runtime tier (the probe's cross-validation, standing:
// bytes, fields and verdicts, pinned instance plus all 64 variants) before
// any timing. Timed loops are per-call flat functions — §3.2's comparable
// call shape — over caller-owned DataViews, no stream object at all.
function benchMessageFlat(name, golden, iters, pinned, writeFn, readFn, flatWriteFn, flatReadFn, varyFn, sinkOf) {
  const base = pinned;
  const gView = new DataView(gBuffer.buffer);

  // oracle 1: the pinned instance through the FLAT writer matches its golden
  const bytesPerOp = flatWriteFn(base, gView);
  if (bytesPerOp < 0) {
    fail(name, "flat write of pinned instance refused");
    return;
  }
  if (golden !== null && !checkGolden(golden, gBuffer.subarray(0, bytesPerOp))) {
    failed = true;
    return;
  }

  // oracle 2: cross-tier — the runtime tier writes identical bytes
  {
    const ws = new WriteStream(gTwin);
    if (!writeFn(ws, base)) {
      fail(name, "runtime write of pinned instance failed");
      return;
    }
    ws.flush();
    if (ws.bytesProcessed() !== bytesPerOp || !bytesEqual(gTwin.subarray(0, bytesPerOp), gBuffer.subarray(0, bytesPerOp))) {
      fail(name, "flat and runtime tiers disagree on pinned bytes");
      return;
    }
  }

  // oracle 3: flat read is field-identical to the runtime read, and the
  // flat re-write reproduces the bytes
  {
    const flOut = new pinned.constructor();
    if (!flatReadFn(flOut, gView, bytesPerOp * 8)) {
      fail(name, "flat read of pinned instance failed");
      return;
    }
    const rtOut = new pinned.constructor();
    if (!readFn(new ReadStream(gBuffer.subarray(0, bytesPerOp)), rtOut)) {
      fail(name, "runtime read of pinned instance failed");
      return;
    }
    if (!deepEqual(flOut, rtOut)) {
      fail(name, "flat and runtime reads disagree on fields");
      return;
    }
    if (!Number.isFinite(sinkOf(flOut))) {
      fail(name, "sinkOf not finite on the decoded pinned instance (typo'd field?)");
      return;
    }
    if (flatWriteFn(flOut, new DataView(gTwin.buffer)) !== bytesPerOp ||
      !bytesEqual(gTwin.subarray(0, bytesPerOp), gBuffer.subarray(0, bytesPerOp))) {
      fail(name, "flat round-trip bytes differ");
      return;
    }
  }

  // 64 variants: cross-tier equivalence on every one — write bytes, read
  // fields, read verdicts — and bytes/op constant under variation
  lcgSeed(1);
  const variantViews = [];
  for (let k = 0; k < NumVariants; k++) {
    lcgStep();
    varyFn(base);
    const vview = new DataView(gVariants[k].buffer);
    if (flatWriteFn(base, vview) !== bytesPerOp) {
      fail(name, "variation changed bytes/op — vary must keep structure fields fixed");
      return;
    }
    const ws = new WriteStream(gTwin);
    if (!writeFn(ws, base)) {
      fail(name, "runtime write of varied instance failed");
      return;
    }
    ws.flush();
    if (!bytesEqual(gTwin.subarray(0, bytesPerOp), gVariants[k].subarray(0, bytesPerOp))) {
      fail(name, "flat and runtime tiers disagree on varied bytes");
      return;
    }
    const flOut = new pinned.constructor();
    const rtOut = new pinned.constructor();
    const flOk = flatReadFn(flOut, vview, bytesPerOp * 8);
    const rtOk = readFn(new ReadStream(gVariants[k].subarray(0, bytesPerOp)), rtOut);
    if (flOk !== rtOk) {
      fail(name, "flat and runtime read verdicts disagree on a variant");
      return;
    }
    if (!flOk || !deepEqual(flOut, rtOut)) {
      fail(name, "flat and runtime reads disagree on a variant's fields");
      return;
    }
    variantViews.push(vview);
  }

  const writeRates = new Array(gNumRuns);
  const readRates = new Array(gNumRuns);

  // write path: 1 warmup (also the JIT warmup) + NumRuns measured
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      lcgStep();
      varyFn(base);
      const n = flatWriteFn(base, gView);
      if (n < 0) {
        fail(name, "flat write refused in loop");
        return;
      }
      gSink = (gSink + n) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // read path: pre-cut DataViews rotate; ONE reused decode instance
  const outValue = new pinned.constructor();
  const numBits = bytesPerOp * 8;
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      if (!flatReadFn(outValue, variantViews[i & (NumVariants - 1)], numBits)) {
        fail(name, "flat read failed in loop");
        return;
      }
      gSink = (gSink + sinkOf(outValue)) >>> 0; // full-struct observation (#175)
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = iters / elapsed;
    }
  }

  report(name, "write", iters, bytesPerOp, stats(writeRates), "gen", "flat");
  report(name, "read", iters, bytesPerOp, stats(readRates), "gen", "flat");
}

// --------------------------------------------------------------------------
// the DATA-DRIVEN benchmark driver (issue #191)
// --------------------------------------------------------------------------
//
// THE PROPERTY: nothing below names a field of the shape it measures. Shape
// knowledge lives in the committed variant DATA (bench/corpus/variants,
// emitted by bench/tools/variantgen) and in the generated codec, and nowhere
// else — so this driver cannot drift from another language's driver in what
// it measures, which is the whole reason the design exists. If a change here
// ever needs a field name, the design has failed and that is the finding.
//
// It replaces benchMessageFlat for bench_mixed only. The flat tier stays THE
// js path (codec=flat); the cross-tier oracle against the runtime tier is
// kept, because deepEqual walks the decoded objects by their own keys and so
// names no field either.
//
// The §2.7 read-side sink deviation this leg carried — the JS BigInt observed
// as one bit where java/dart/elixir fold the full value, which made the js
// read row a FLOOR — has nothing left to deviate on: the round-trip's decode
// is observed by its own re-encode.

// Loads <variant-dir>/<name>.variants.bin into the NumVariants §2.7-staggered
// slots and returns the record size, or -1. The records are fixed-width by
// construction (§2.7 pins every structure field), so the file needs no index:
// the record size IS file size / NumVariants, and a file that does not divide
// evenly is a refusal.
function loadVariants(name) {
  const path_ = `${gVariantDir}/${name}.variants.bin`;
  let packed;
  try {
    packed = readFileSync(path_);
  } catch {
    process.stderr.write(
      `missing variant data ${path_} — run \`make bench-variants\`, and run the bench from bench/js (or pass --variant-dir)\n`
    );
    return -1;
  }
  if (packed.length === 0 || packed.length % NumVariants !== 0) {
    process.stderr.write(
      `variant data ${path_} is ${packed.length} bytes, not a multiple of ${NumVariants} records — refusing to bench data whose stride is not the record size\n`
    );
    return -1;
  }
  const record = packed.length / NumVariants;
  if (record > BufferSize) {
    process.stderr.write(`variant data ${path_} has ${record}-byte records, over the ${BufferSize}-byte buffer\n`);
    return -1;
  }
  for (let k = 0; k < NumVariants; k++) {
    gVariants[k].set(packed.subarray(k * record, (k + 1) * record), 0);
  }
  // The variant data is corpus (§1.6): it defines the work inside the timed
  // loops, so it rides in corpus_id exactly as the wire goldens do.
  gGoldensLoaded.set(`${name}.variants.bin`, packed);
  return record;
}

// Ctor — the generated message type — is named once at the call site. A TYPE
// name is not a field name; the driver knows nothing about the contents.
function benchDataDrivenFlat(name, golden, iters, Ctor, writeFn, readFn, flatWriteFn, flatReadFn) {
  const bytesPerOp = loadVariants(name);
  if (bytesPerOp < 0) {
    failed = true;
    return;
  }

  // gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
  // file is bound to the wire golden by one byte-compare.
  if (!checkGolden(golden, gVariants[0].subarray(0, bytesPerOp))) {
    failed = true;
    return;
  }

  // gate 2: every variant decodes through the FLAT tier, re-encodes, and
  // comes back byte-identical at the same length; and the runtime tier
  // agrees on both verdict and fields. §1.5's named residual (the 64 varied
  // buffers length-checked but never value-checked) closes here.
  const variantViews = [];
  const instances = [];
  const numBits = bytesPerOp * 8;
  const gView = new DataView(gBuffer.buffer);
  const twinView = new DataView(gTwin.buffer);
  for (let k = 0; k < NumVariants; k++) {
    // the view spans the whole slot, not just the packet: the flat reader's
    // 64-bit window loads up to 8 bytes past the last word (§2.7's stride pad)
    const vview = new DataView(gVariants[k].buffer);
    const flOut = new Ctor();
    if (!flatReadFn(flOut, vview, numBits)) {
      fail(name, "flat decode of a variant failed");
      return;
    }
    const rtOut = new Ctor();
    if (!readFn(new ReadStream(gVariants[k].subarray(0, bytesPerOp)), rtOut)) {
      fail(name, "runtime decode of a variant failed");
      return;
    }
    if (!deepEqual(flOut, rtOut)) {
      fail(name, "flat and runtime reads disagree on a variant's fields");
      return;
    }
    if (flatWriteFn(flOut, twinView) !== bytesPerOp ||
      !bytesEqual(gTwin.subarray(0, bytesPerOp), gVariants[k].subarray(0, bytesPerOp))) {
      fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
      return;
    }
    variantViews.push(vview);
    instances.push(flOut);
  }

  const writeRates = new Array(gNumRuns);
  const roundtripRates = new Array(gNumRuns);

  // WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
  // instances is what §2.7's per-iteration LCG mutation bought — the encoder
  // never sees the same input twice in a row and cannot precompute scratch
  // words — with none of the per-language mutation code, and with bytes/op
  // constant by construction rather than by assertion.
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      const n = flatWriteFn(instances[i & (NumVariants - 1)], gView);
      if (n < 0) {
        fail(name, "flat write refused in loop");
        return;
      }
      gSink = (gSink + n) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // ROUND-TRIP: decode a variant view, then re-encode what came out. The
  // decode needs no sink discipline of its own — its output IS the encode's
  // input, so every decoded field is observed by construction, with no
  // per-language fold to audit.
  const outValue = new Ctor();
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      if (!flatReadFn(outValue, variantViews[i & (NumVariants - 1)], numBits)) {
        fail(name, "flat read failed in loop");
        return;
      }
      const n = flatWriteFn(outValue, gView);
      if (n < 0) {
        fail(name, "flat re-write refused in loop");
        return;
      }
      gSink = (gSink + n) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      roundtripRates[run] = iters / elapsed;
    }
  }

  const w = stats(writeRates);
  const rt = stats(roundtripRates);
  report(name, "write", iters, bytesPerOp, w, "gen", "flat");
  report(name, "round_trip", iters, bytesPerOp, rt, "gen", "flat");

  // READ is DERIVED, never measured: round-trip time minus write time. It
  // prints for continuity with the read rows the rest of the corpus still
  // reports and is NOT a CSV row — a derived number in the CSV would be
  // divided as if it had been measured.
  const readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    process.stderr.write(
      `${name.padEnd(18)} ${"read".padEnd(5)} ${(1e-6 / readTime).toFixed(2).padStart(10)} M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n`
    );
  }
}

function benchMessage(name, golden, iters, pinned, writeFn, readFn, varyFn, sinkOf, codec = "runtime") {
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
    if (!Number.isFinite(sinkOf(output))) {
      fail(name, "sinkOf not finite on the decoded pinned instance (typo'd field?)");
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
      gSink = (gSink + sinkOf(outValue)) >>> 0; // full-struct observation (#175)
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      readRates[run] = iters / elapsed;
    }
  }

  report(name, "write", iters, bytesPerOp, stats(writeRates), "gen", codec);
  report(name, "read", iters, bytesPerOp, stats(readRates), "gen", codec);
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

function main() {
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--csv") {
      gCsv = true;
    } else if (args[i] === "--wire-dir" && i + 1 < args.length) {
      gWireDir = args[++i];
    } else if (args[i] === "--variant-dir" && i + 1 < args.length) {
      gVariantDir = args[++i];
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
    } else if (args[i] === "--quick") {
      gQuick = true;
    } else {
      process.stderr.write("usage: node main.mjs [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>] [--print-runtime]\n");
      process.exit(1);
    }
  }
  if (gQuick && gNumRuns === MaxNumRuns) {
    gNumRuns = 3;
  }

  process.stderr.write(`schema bench (js, node ${process.versions.node}, ${PRODUCTION ? "production" : "checked"} mode${gQuick ? ", --quick: iteration instrument, not certification" : ""})\n`);

  if (gQuick) {
    // --quick: the flat bench_mixed leg only (THE js path for the shape),
    // golden-gated and cross-validated like every flat leg
    benchDataDrivenFlat("bench_mixed", "bench_mixed", QuickMixedIters, bench.BenchMixed, bench.WriteBenchMixed, bench.ReadBenchMixed, benchFlat.WriteBenchMixedFlat, benchFlat.ReadBenchMixedFlat);
    flushCsv();
    if (failed) {
      process.stderr.write(`BENCH FAILED (corpus_id ${corpusId()})\n`);
      process.exit(1);
    }
    process.stderr.write(`OK (corpus_id ${corpusId()})\n`);
    return;
  }


  // rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
  const atRest = pinRigidBodyMoving();
  atRest.AtRest = true;

  // THE js rows: the flat tier, per-call (§3.2's comparable shape), each
  // leg bound to the corpus pins AND cross-validated against the runtime
  // tier (bytes/fields/verdicts, 64 variants) before any timing
  benchMessageFlat("rigidbody_moving", "rigidbody_moving", 24000000, pinRigidBodyMoving(), ex.WriteRigidBody, ex.ReadRigidBody, exFlat.WriteRigidBodyFlat, exFlat.ReadRigidBodyFlat, varyRigidBody, sinkOfRigidBody);
  benchMessageFlat("rigidbody_at_rest", "rigidbody_at_rest", 32000000, atRest, ex.WriteRigidBody, ex.ReadRigidBody, exFlat.WriteRigidBodyFlat, exFlat.ReadRigidBodyFlat, varyRigidBodyAtRest, sinkOfRigidBody);
  benchMessageFlat("chat", "chat", 48000000, pinChat(), ex.WriteChat, ex.ReadChat, exFlat.WriteChatFlat, exFlat.ReadChatFlat, varyChat, sinkOfChat);
  benchMessageFlat("test", null, 192000000, new ex.Test(), ex.WriteTest, ex.ReadTest, exFlat.WriteTestFlat, exFlat.ReadTestFlat, varyTest, sinkOfTest);
  benchMessageFlat("inputpacket", "inputpacket", 16000000, pinInputPacket(), ex.WriteInputPacket, ex.ReadInputPacket, exFlat.WriteInputPacketFlat, exFlat.ReadInputPacketFlat, varyInputPacket, sinkOfInputPacket);
  benchMessageFlat("shipcreate", "shipcreate_flags", 32000000, pinShipCreate(), ex.WriteShipCreate, ex.ReadShipCreate, exFlat.WriteShipCreateFlat, exFlat.ReadShipCreateFlat, varyShipCreate, sinkOfShipCreate);
  benchMessageFlat("probe_header", "probe_header", 256000000, pinProbeHeader(), ex.WriteProbeHeader, ex.ReadProbeHeader, exFlat.WriteProbeHeaderFlat, exFlat.ReadProbeHeaderFlat, varyProbeHeader, sinkOfProbeHeader);
  benchMessageFlat("probebits", "probebits", 128000000, pinProbeBits(), ex.WriteProbeBits, ex.ReadProbeBits, exFlat.WriteProbeBitsFlat, exFlat.ReadProbeBitsFlat, varyProbeBits, sinkOfProbeBits);
  benchMessageFlat("probearray", "probearray", 20000000, pinProbeArray(), ex.WriteProbeArray, ex.ReadProbeArray, exFlat.WriteProbeArrayFlat, exFlat.ReadProbeArrayFlat, varyProbeArray, sinkOfProbeArray);
  benchMessageFlat("testdata", "testdata", 8000000, pinTestData(), ex.WriteTestData, ex.ReadTestData, exFlat.WriteTestDataFlat, exFlat.ReadTestDataFlat, varyTestData, sinkOfTestData);

  // real_packet (§1.7): the realistic snapshot — ~93 riding individually
  // serialized small fields, 204 wire bytes, 0% bulk share by bits. The
  // pin is the ALL-DEFAULTS instance: construction installs the SPECIFIED
  // defaults (the four branch gates: f012 true, f043 false, f050 true,
  // f074 false) exactly as C++ RealPacket{} does. base_iters sized in the
  // C++ reference (§2.1).
  benchMessageFlat("real_packet", "real_packet", 8000000, new realworld.RealPacket(), realworld.WriteRealPacket, realworld.ReadRealPacket, realworldFlat.WriteRealPacketFlat, realworldFlat.ReadRealPacketFlat, varyRealPacket, sinkOfRealPacket);

  // the runtime-call generated tier: labeled supplementary rows
  // (codec=runtime) so the compat tier's regressions stay visible — never
  // the js number
  const atRestRt = pinRigidBodyMoving(); // fresh pin — the flat leg's vary mutated atRest
  atRestRt.AtRest = true;
  benchMessage("rigidbody_moving", "rigidbody_moving", 24000000, pinRigidBodyMoving(), ex.WriteRigidBody, ex.ReadRigidBody, varyRigidBody, sinkOfRigidBody);
  benchMessage("rigidbody_at_rest", "rigidbody_at_rest", 32000000, atRestRt, ex.WriteRigidBody, ex.ReadRigidBody, varyRigidBodyAtRest, sinkOfRigidBody);
  benchMessage("chat", "chat", 48000000, pinChat(), ex.WriteChat, ex.ReadChat, varyChat, sinkOfChat);
  benchMessage("test", null, 192000000, new ex.Test(), ex.WriteTest, ex.ReadTest, varyTest, sinkOfTest);
  benchMessage("inputpacket", "inputpacket", 16000000, pinInputPacket(), ex.WriteInputPacket, ex.ReadInputPacket, varyInputPacket, sinkOfInputPacket);
  benchMessage("shipcreate", "shipcreate_flags", 32000000, pinShipCreate(), ex.WriteShipCreate, ex.ReadShipCreate, varyShipCreate, sinkOfShipCreate);
  benchMessage("probe_header", "probe_header", 256000000, pinProbeHeader(), ex.WriteProbeHeader, ex.ReadProbeHeader, varyProbeHeader, sinkOfProbeHeader);
  benchMessage("probebits", "probebits", 128000000, pinProbeBits(), ex.WriteProbeBits, ex.ReadProbeBits, varyProbeBits, sinkOfProbeBits);
  benchMessage("probearray", "probearray", 20000000, pinProbeArray(), ex.WriteProbeArray, ex.ReadProbeArray, varyProbeArray, sinkOfProbeArray);
  benchMessage("testdata", "testdata", 8000000, pinTestData(), ex.WriteTestData, ex.ReadTestData, varyTestData, sinkOfTestData);
  benchMessage("real_packet", "real_packet", 8000000, new realworld.RealPacket(), realworld.WriteRealPacket, realworld.ReadRealPacket, varyRealPacket, sinkOfRealPacket);

  // BenchMixed as GENERATED code — the flat tier is THE js entry for this
  // shape in any cross-language comparison, exactly as for the corpus shapes
  // above (family gen, codec=flat), cross-validated against the runtime-call
  // tier by the same oracle. Fed entirely by the committed variant corpus:
  // no hand-written pin, vary or sink code participates.
  benchDataDrivenFlat("bench_mixed", "bench_mixed", 4000000, bench.BenchMixed, bench.WriteBenchMixed, bench.ReadBenchMixed, benchFlat.WriteBenchMixedFlat, benchFlat.ReadBenchMixedFlat);

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
