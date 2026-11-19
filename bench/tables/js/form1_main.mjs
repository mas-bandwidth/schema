// the tables bench — the JavaScript Form-1 runner (bench/tables/README.md).
//
// Shape-blind: names the generated root once (`rootName`) and resolves the
// generated Load/Save functions dynamically from the staged module.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const NumVariants = 64;
const MaxRuns = 7;
const DefaultIterations = 25600;
const rootName = "TableMixed";

const genDir = process.argv[2];
if (!genDir || genDir.startsWith("--")) {
  process.stderr.write("usage: node form1_main.mjs <generated-dir> [--gate] [--csv] [--round K] [--iterations N] [--wire-dir D] [--variant-dir D]\n");
  process.exit(1);
}

let gCsv = false;
let gGate = false;
let gNumRuns = MaxRuns;
let gIterations = 0;
let gWireDir = "testdata/wire";
let gVariantDir = "bench/corpus/variants";

for (let i = 3; i < process.argv.length; i++) {
  const a = process.argv[i];
  if (a === "--csv") gCsv = true;
  else if (a === "--gate") gGate = true;
  else if (a === "--wire-dir" && i + 1 < process.argv.length) gWireDir = process.argv[++i];
  else if (a === "--variant-dir" && i + 1 < process.argv.length) gVariantDir = process.argv[++i];
  else if (a === "--round" && i + 1 < process.argv.length) {
    const k = Number(process.argv[++i]);
    if (!Number.isInteger(k) || k < 0) {
      process.stderr.write("--round expects a non-negative integer\n");
      process.exit(1);
    }
    gNumRuns = 1;
  } else if (a === "--iterations" && i + 1 < process.argv.length) {
    const n = Number(process.argv[++i]);
    if (!Number.isInteger(n) || n <= 0 || n % NumVariants !== 0) {
      process.stderr.write("--iterations expects a positive multiple of 64\n");
      process.exit(1);
    }
    gIterations = n;
  } else {
    process.stderr.write(`unknown argument ${a}\n`);
    process.exit(1);
  }
}

const mod = await import(pathToFileURL(resolve(genDir, "BenchTableVariable.js")).href);
const loadFn = mod[`${rootName}Load`];
const saveFn = mod[`${rootName}Save`];
const ReportCtor = mod.TableVariableReport;

function bytesEqual(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

function fnv1a64(h, bytes) {
  for (let i = 0; i < bytes.length; i++) {
    h ^= BigInt(bytes[i]);
    h = (h * 0x100000001b3n) & 0xffffffffffffffffn;
  }
  return h;
}

function corpusId(files) {
  let h = 0xcbf29ce484222325n;
  const zero = new Uint8Array(1);
  for (const name of Object.keys(files).sort()) {
    h = fnv1a64(h, new TextEncoder().encode(name));
    h = fnv1a64(h, zero);
    h = fnv1a64(h, files[name]);
  }
  return h.toString(16).padStart(16, "0");
}

const golden = new Uint8Array(readFileSync(`${gWireDir}/bench_table.bin`));
const packed = new Uint8Array(readFileSync(`${gVariantDir}/bench_table.variants.bin`));
if (packed.length === 0 || packed.length % NumVariants !== 0) {
  process.stderr.write("FAILED: bench_table: invalid variant corpus size\n");
  process.exit(1);
}
const record = packed.length / NumVariants;
const variants = [];
for (let k = 0; k < NumVariants; k++) {
  variants.push(packed.subarray(k * record, (k + 1) * record));
}

if (!bytesEqual(variants[0], golden)) {
  process.stderr.write("WIRE GOLDEN MISMATCH: bench_table\n");
  process.exit(1);
}

const instances = [];
for (let k = 0; k < NumVariants; k++) {
  const rep = ReportCtor ? new ReportCtor() : {};
  const val = loadFn(variants[k], rep);
  if (val === null || val === undefined || rep.damaged || rep.refused) {
    process.stderr.write(`FAILED: bench_table: load of variant ${k} failed\n`);
    process.exit(1);
  }
  const out = saveFn(val);
  if (!(out instanceof Uint8Array) || !bytesEqual(out, variants[k])) {
    process.stderr.write(`FAILED: bench_table: variant ${k} round-trip bytes differ\n`);
    process.exit(1);
  }
  instances.push(val);
}

for (let step = 0; step < 2 * NumVariants; step++) {
  const k = step % NumVariants;
  const rep = ReportCtor ? new ReportCtor() : {};
  const val = loadFn(variants[k], rep);
  const out = val ? saveFn(val) : null;
  if (!out || !bytesEqual(out, variants[k])) {
    process.stderr.write("FAILED: bench_table: repeated load round-trip bytes differ\n");
    process.exit(1);
  }
}

const cid = corpusId({ "bench_table.bin": golden, "bench_table.variants.bin": packed });
if (gGate) {
  process.stderr.write(`OK (gate only, corpus_id ${cid})\n`);
  process.exit(0);
}

const iters = gIterations > 0 ? gIterations : DefaultIterations;
let sink = 0;
const rows = [];

for (const pathName of ["write", "round_trip"]) {
  const rates = [];
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i++) {
      const k = i & (NumVariants - 1);
      let v = instances[k];
      if (pathName === "round_trip") {
        const rep = ReportCtor ? new ReportCtor() : {};
        v = loadFn(variants[k], rep);
        if (!v) {
          process.stderr.write("FAILED: bench_table: load failed in timed loop\n");
          process.exit(1);
        }
      }
      const out = saveFn(v);
      if (!out || out.length !== record) {
        process.stderr.write("FAILED: bench_table: save failed in timed loop\n");
        process.exit(1);
      }
      sink = (sink + out.length) >>> 0;
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) rates.push(iters / elapsed);
  }
  rates.sort((a, b) => a - b);
  const med = rates[rates.length >> 1];
  const lo = rates[0];
  const hi = rates[rates.length - 1];
  rows.push(
    `js,bench_table,${pathName},${iters},${record},${gNumRuns},${Math.round(med)},${Math.round(lo)},${Math.round(hi)},${((med * record) / 1048576).toFixed(2)},${(((hi - lo) / med) * 100).toFixed(2)},${cid},table,esm,always,default,unknown`
  );
}

if (gCsv) {
  process.stdout.write("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
  process.stdout.write(rows.join("\n") + "\n");
} else {
  process.stderr.write(rows.join("\n") + "\n");
}
process.stderr.write(`OK (corpus_id ${cid}, sink ${sink})\n`);
