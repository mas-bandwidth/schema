// the tables bench — the JavaScript runner.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated JavaScript table codec: same corpus, same golden gate,
// same per-variant round-trip gate before any clock, same 1 warmup + 7 measured
// runs with median/min/max/spread, same CSV v2 rows with lang=js.
//
// The JavaScript table codec names NO runtime — `make tables-js-standalone`
// gates that — so this leg imports the generated modules and nothing else.
//
// WHAT THIS NUMBER IS, said before it is read. This is the READING TIER's leg:
// the storage it decodes into is a JavaScript object, its 64-bit fields are
// BigInt because that is the only exact 64-bit integer the language has, and
// the codec runs under a JIT rather than an optimizing AOT compiler. The bar
// the ladder sets — same speed, or not significantly slower — is the bar for a
// language that can be held to it; this leg REPORTS its ratio to C++ and the
// owner reads it. A number wide of C++ here is the language's cost, stated,
// not a defect concealed.
//
// Language-specific discipline, the same choices the type leg made:
//   - escape barriers: a module-level sink accumulates observed byte counts,
//     so the JIT cannot delete the work
//   - the read path loads into ONE reused instance, reset first — the tolerant
//     wire elides a field at its default, so resetting is part of a correct
//     read into reused storage and stays inside the clock
//   - the warmup run per path doubles as the JIT warmup
//
// THIS FILE IS SHAPE-BLIND: it names the generated type at its call sites and
// nothing else — no field, no pinned value, no wire size.

import { readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const generated = process.env.SCHEMA_JS_BENCH_GENERATED ??
  resolve(here, "..", "..", "..", "generated", "bench", "tables", "js");
const unit = await import(pathToFileURL(resolve(generated, "BenchTableTable.js")).href);

const MaxNumRuns = 7;          // median of 7 (N >= 5), after 1 warmup run
let gNumRuns = MaxNumRuns;     // --round K drops this to 1 (§2.4)
const NumVariants = 64;        // read-path variant buffers

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9) per row. linkage esm — the generated codec is a set of
// ES modules loaded into this isolate and there is no runtime module to cross,
// which is the recorded packaging fact for this leg. checks contract — the
// reader's wire-contract validation is unconditional and there are no
// caller-error asserts to be dormant (§3.4). opt default (node has no
// operator-visible optimization levels). inline unknown: a JIT leg has no AOT
// artifact the §4 verdict pass could walk.
const CsvSuffix = "esm,contract,default,unknown";

const gCsvRows = [];
const gGoldensLoaded = new Map();

let gSink = 0n;
let gCsv = false;
let gWireDir = join("testdata", "wire");
let gVariantDir = join("bench", "corpus", "variants");
let failed = false;

function fail(name, what) {
  process.stderr.write("FAILED: " + name + ": " + what + "\n");
  failed = true;
}

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
  if (!gCsv) { return; }
  if (failed) {
    // §1.5: a failing run emits NO rows.
    process.stderr.write("refusing to emit CSV rows from a failing run\n");
    return;
  }
  const id = corpusId();
  process.stdout.write(
    "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," +
    "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
  for (const r of gCsvRows) {
    process.stdout.write(r.Row + "," + id + "," + r.family + "," + CsvSuffix + "\n");
  }
}

// The tolerant wire spends bytes on ids, kinds and lengths, so a table record
// is several times its equivalent type's. The record size comes from the corpus
// at run time; this is only the ceiling the runner refuses past.
const BufferSize = 65536;
const gBuffer = new Uint8Array(BufferSize);
const gTwin = new Uint8Array(BufferSize);
const gVariants = new Array(NumVariants);

function stats(rates) {
  const sorted = [...rates].sort((a, b) => a - b);
  const median = sorted[sorted.length >> 1];
  return {
    median,
    min: sorted[0],
    max: sorted[sorted.length - 1],
    spreadPct: (sorted[sorted.length - 1] - sorted[0]) / median * 100.0,
  };
}

function pad(s, width) {
  s = String(s);
  return s.length >= width ? s : s + " ".repeat(width - s.length);
}

function padLeft(s, width) {
  s = String(s);
  return s.length >= width ? s : " ".repeat(width - s.length) + s;
}

function report(bench, path, iters, bytesPerOp, s) {
  const mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
  process.stderr.write(
    pad(bench, 18) + " " + pad(path, 11) + " " +
    padLeft((s.median / 1e6).toFixed(3), 10) + " M msg/s " +
    padLeft(mbps.toFixed(1), 10) + " MB/s   (min " + (s.min / 1e6).toFixed(3) +
    ", max " + (s.max / 1e6).toFixed(3) + ", spread " + s.spreadPct.toFixed(1) + "%)\n");
  if (gCsv) {
    gCsvRows.push({
      Row: "js," + bench + "," + path + "," + iters + "," + bytesPerOp + "," + gNumRuns + "," +
        s.median.toFixed(0) + "," + s.min.toFixed(0) + "," + s.max.toFixed(0) + "," +
        mbps.toFixed(2) + "," + s.spreadPct.toFixed(2),
      family: "table",
    });
  }
}

function checkGolden(name, data, bytes) {
  const path = join(gWireDir, name + ".bin");
  if (!existsSync(path)) {
    process.stderr.write("missing wire golden " + path + " — run from the schema repo root (or pass --wire-dir)\n");
    return false;
  }
  const expected = new Uint8Array(readFileSync(path));
  gGoldensLoaded.set(name + ".bin", expected);
  if (expected.length !== bytes) {
    process.stderr.write("WIRE GOLDEN MISMATCH: " + path + " (" + expected.length + " golden vs " + bytes + " actual bytes)\n");
    return false;
  }
  for (let i = 0; i < bytes; i++) {
    if (expected[i] !== data[i]) {
      process.stderr.write("WIRE GOLDEN MISMATCH: " + path + " differs at byte " + i +
        " — refusing to bench code that does not match the corpus\n");
      return false;
    }
  }
  return true;
}

// Records are fixed-width by construction — test/bench/table_main.cpp refuses
// to emit a corpus whose records differ — so the record size IS file size /
// NumVariants.
function loadVariants(name) {
  const path = join(gVariantDir, name + ".variants.bin");
  if (!existsSync(path)) {
    process.stderr.write("missing variant data " + path +
      " — run `make bench-table-corpus`, and run from the schema repo root (or pass --variant-dir)\n");
    return -1;
  }
  const packed = new Uint8Array(readFileSync(path));
  if (packed.length === 0 || packed.length % NumVariants !== 0) {
    process.stderr.write("variant data " + path + " is " + packed.length +
      " bytes, not a multiple of " + NumVariants + " records\n");
    return -1;
  }
  const record = packed.length / NumVariants;
  if (record > BufferSize) {
    process.stderr.write("variant data " + path + " has " + record + "-byte records, over the " +
      BufferSize + "-byte buffer\n");
    return -1;
  }
  for (let k = 0; k < NumVariants; k++) {
    gVariants[k] = packed.subarray(k * record, (k + 1) * record);
  }
  gGoldensLoaded.set(name + ".variants.bin", packed);
  return record;
}

// ------------------------------------------------------------------
// the data-driven table driver
// ------------------------------------------------------------------

function benchTable(name, golden, baseIters, make, reset, save, load) {
  const iters = baseIters;

  const bytesPerOp = loadVariants(name);
  if (bytesPerOp < 0) { failed = true; return; }

  // gate 1 (§1.5): variant 0 IS the pinned instance.
  if (!checkGolden(golden, gVariants[0], bytesPerOp)) { failed = true; return; }

  // gate 2: every variant loads, re-saves, and comes back byte-identical at the
  // same length — before any clock starts.
  const instances = new Array(NumVariants);
  for (let k = 0; k < NumVariants; k++) {
    instances[k] = make();
    reset(instances[k]);
    if (!load(instances[k], gVariants[k])) { fail(name, "load of a variant failed"); return; }
    const wrote = save(instances[k], gTwin);
    if (wrote !== bytesPerOp) {
      fail(name, "variant round-trip length differs — refusing to bench a codec that does not reproduce the corpus");
      return;
    }
    for (let i = 0; i < bytesPerOp; i++) {
      if (gTwin[i] !== gVariants[k][i]) {
        fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
        return;
      }
    }
  }

  const writeRates = new Array(gNumRuns);
  const roundtripRates = new Array(gNumRuns);

  // WRITE: save the 64 pre-loaded instances round-robin (§2.7 variation).
  for (let run = -1; run < gNumRuns; run++) {
    const started = process.hrtime.bigint();
    for (let i = 0; i < iters; i++) {
      const wrote = save(instances[i & (NumVariants - 1)], gBuffer);
      if (wrote !== bytesPerOp) { fail(name, "save failed in loop"); return; }
      gSink += BigInt(wrote);
    }
    const elapsed = Number(process.hrtime.bigint() - started) / 1e9;
    if (run >= 0) { writeRates[run] = iters / elapsed; }
  }

  // ROUND-TRIP: reset, load a variant buffer, re-save what came out. The load's
  // output IS the save's input, so every loaded field is observed by
  // construction (§2.7's read-side sink problem dissolved).
  const outValue = make();
  for (let run = -1; run < gNumRuns; run++) {
    const started = process.hrtime.bigint();
    for (let i = 0; i < iters; i++) {
      reset(outValue);
      if (!load(outValue, gVariants[i & (NumVariants - 1)])) { fail(name, "load failed in loop"); return; }
      const wrote = save(outValue, gBuffer);
      if (wrote !== bytesPerOp) { fail(name, "re-save failed in loop"); return; }
      gSink += BigInt(wrote);
    }
    const elapsed = Number(process.hrtime.bigint() - started) / 1e9;
    if (run >= 0) { roundtripRates[run] = iters / elapsed; }
  }

  const w = stats(writeRates);
  const rt = stats(roundtripRates);
  report(name, "write", iters, bytesPerOp, w);
  report(name, "round_trip", iters, bytesPerOp, rt);

  // READ is DERIVED, never measured (§2.9): stderr only, never a row.
  const readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    process.stderr.write(pad(name, 18) + " " + pad("read", 11) + " " +
      padLeft((1e-6 / readTime).toFixed(3), 10) +
      " M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n");
  }
}

// ---- main

const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--csv") { gCsv = true; }
  else if (args[i] === "--wire-dir" && i + 1 < args.length) { gWireDir = args[++i]; }
  else if (args[i] === "--variant-dir" && i + 1 < args.length) { gVariantDir = args[++i]; }
  else if (args[i] === "--round" && i + 1 < args.length) {
    const k = Number(args[++i]);
    if (!Number.isInteger(k) || k < 0) {
      process.stderr.write("--round takes a non-negative integer, got '" + args[i] + "'\n");
      process.exit(1);
    }
    gNumRuns = 1;
  } else {
    process.stderr.write("usage: main.mjs [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>]\n");
    process.exit(1);
  }
}

process.stderr.write("schema tables bench (js)\n");

// The one measured shape, named once — the generated type at the call site and
// nothing else about it (bench/SHAPE-GATE.allow).
const report0 = new unit.TableReport();
benchTable(
  "bench_table", "bench_table", 100000,
  () => new unit.TableMixed(),
  (v) => unit.TableMixedReset(v),
  (v, b) => unit.TableMixedSave(v, b),
  (v, b) => unit.TableMixedLoad(v, b, report0) && !report0.Malformed);

flushCsv();

if (failed) {
  process.stderr.write("TABLES BENCH FAILED (corpus_id " + corpusId() + ")\n");
  process.exit(1);
}
process.stderr.write("OK (corpus_id " + corpusId() + ", sink " + gSink + ")\n");
