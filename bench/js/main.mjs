// schema bench — the JavaScript runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated JS modules and the serialize.js runtime: same benchmark set,
// same variant corpus, same golden + per-variant round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=js. See
// bench/README.md for the runner contract. One module, like bench/c's one
// translation unit: the harness helpers are module-scoped.
//
// TWO GENERATED CODECS ride this runner (BENCH-STANDARD.md §5.1 codec
// column, 2026-08-18): the gen-family rows measure the FLAT tier
// (codec=flat) — THE js path under the ruling ("whichever correct
// implementation is fastest is the one we use for JavaScript") — per-call,
// §3.2's cross-language-comparable shape, golden-gated AND cross-validated
// against the runtime tier (bytes, fields, verdicts, 64 variants) before any
// timing. Flat rows carry no runtime version: the flat modules import
// nothing, and the preamble's schema commit is their whole provenance (§3.5).
//
// Language-specific discipline:
//   - the LCG is the C bench's uint64 LCG carried in two 32-bit lanes, the
//     exact generator serialize.js's own bench/bench.js authored: BigInt
//     never steps the generator
//   - streams are reused via reset() (the runtime's documented no-allocation
//     reuse path); resetting to a DIFFERENT buffer re-wraps a DataView — the
//     read loops rotate 64 variant views, so they pay that re-wrap per op,
//     the same structural cost bench/bench.js names and measures as shipped
//   - escape barriers: a module-scoped sink accumulates observed byte counts
//     (JS has no empty-asm clobber; the decoded object escapes through the
//     loop function and the sink's data dependence keeps the work alive)
//   - the driver passes write/read as function values (one indirect call per
//     op, as in the Go and C# runners; Rust and C++ get this inlined via
//     generics — noted in the results)
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

import * as bench from "../../generated/bench/js/Bench.js";
import * as benchFlat from "../../generated/bench/js/BenchFlat.js";

// one namespace over the unit, the way Go sees package example — the checker
// guarantees unit-wide name uniqueness, so the merge cannot collide
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
// The flat tier stays THE js path (codec=flat); the cross-tier oracle
// against the runtime tier is kept, because deepEqual walks the decoded
// objects by their own keys and so names no field either.
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

  // BenchMixed as GENERATED code — the flat tier is THE js entry for this
  // shape in any cross-language comparison (family gen, codec=flat),
  // cross-validated against the runtime-call tier by the same oracle. Fed
  // entirely by the committed variant corpus: no hand-written pin, vary or
  // sink code participates.
  benchDataDrivenFlat("bench_mixed", "bench_mixed", 4000000, bench.BenchMixed, bench.WriteBenchMixed, bench.ReadBenchMixed, benchFlat.WriteBenchMixedFlat, benchFlat.ReadBenchMixedFlat);

  // family bits (§1.4): the one bitpacker workload in the estate
  if (!gQuick) {
    benchBitpacker(24576);
  }

  flushCsv(); // rows carry the corpus_id of the goldens this run loaded

  if (failed) {
    process.stderr.write(`BENCH FAILED (corpus_id ${corpusId()})\n`);
    process.exit(1);
  }
  process.stderr.write(`OK (corpus_id ${corpusId()})\n`);
}

main();
