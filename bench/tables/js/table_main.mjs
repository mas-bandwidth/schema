// the tables bench — the JavaScript runner, THE FIXED FORM ONLY.
//
// A port of bench/tables/cpp/table_main.cpp (the reference implementation)
// against the generated JS table codec. It measures ONE thing: the paired
// bench's sixty-four logical records written and read in the FIXED FORM,
// form byte 3 (docs/SPEC-TABLES.md §3.4), through the generated codec.
//
// WHY THERE IS NO TOLERANT HALF HERE, and why that is not a gap this runner
// can close: JavaScript has NO FORM-1 TABLE WIRE (schema#516). The backend
// carries form 3 and only form 3, so there is no `BenchMixedSave`/`Load` on
// the tolerant wire for this leg to gate or to time. The C++ and C legs run
// the tolerant form's three correctness gates without a clock and measure the
// fixed form; this leg measures the same fixed form and cannot run those
// three. What it CAN do with the tolerant corpus, it does — see
// `checkTolerantCorpus` below: the framing arithmetic is checkable without a
// decoder, and the bytes are what the corpus id is made of.
//
// WHY THERE IS NO PACKET LEG, so `bench/paired` runs js as a TABLE-ONLY
// language and never divides a ratio for it:
//
//   - the packet runner (bench/js/main.mjs) imports the serialize.js sibling
//     runtime, which is not a checkout this repository carries;
//   - it has no `--gate` and no `--iterations`, the two flags the paired
//     driver's every invocation passes;
//   - and its generated-tier rows carry the §5.1 `codec` column, so they are
//     eighteen columns where the paired parser requires exactly seventeen.
//
// The table codec imports NO runtime at all — the generated modules import
// only each other — so the table leg has none of those three problems. A
// packet leg is a separate piece of work with a separate ruling; faking a
// packet row to fill the driver's second wire would be inventing a
// measurement, so the driver accepts a table-only row instead.
//
// THE JS COST THIS LEG DOCUMENTS: OBJECT PROJECTION. The read is a prefill of
// the reader's own image, one plan run that copies the record's body into it,
// and then `FixedTableFixedDecode`, which projects those bytes into JavaScript
// objects — a nested object per entity, an array per bounded array, a string
// per text field. C and C++ read into storage that IS the image and pay
// nothing for that step. So this leg's round_trip carries a cost the C legs do
// not have and the derived read line is where it shows; it is a property of
// the language's value model, not of the form.
//
// It follows the same contract as every other leg (BENCH-STANDARD.md): the
// committed corpus drives it, the gates run before the clock, a failing gate
// emits NO rows, and the report is 1 warmup + 7 measured runs with the median
// beside min/max/spread — `--round K` drops that to one warmup and one
// measured run so the driver aggregates across rounds itself.
//
// THIS FILE IS SHAPE-BLIND. It names the generated type and its generated
// Save/Load at those call sites and nothing else: no field, no pinned value,
// no wire size. `make shape-gate` holds that mechanically and
// bench/tables/js/SHAPE-GATE.allow carries the exact count.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
//
// Run from the repository root (the paired driver does). Module imports are
// module-relative, so the working directory only ever names the corpus.
import { readFileSync } from "node:fs";

import { FixedTable, TableFixedReport } from "../../../generated/bench/paired/js/BenchTable.js";
import {
  FixedTableFixedBlock,
  FixedTableFixedLoad,
  FixedTableFixedMeasure,
  FixedTableFixedNewPlan,
  FixedTableFixedSave,
} from "../../../generated/bench/paired/js/FixedTableTable.js";

const MaxNumRuns = 7; // median of 7 (N >= 5), after 1 warmup run
const NumVariants = 64; // the corpus is 64 records, and an OP is one record
const DefaultIterations = 400000; // the tables bench's standard table count

let gNumRuns = MaxNumRuns; // --round K drops this to 1 (§2.4)
let gIterations = 0; // explicit diagnostic count; the default stays standard
let gCsv = false;
let gGate = false;
let gIndexed = false;
let gWireDir = "testdata/wire";
let gVariantDir = "bench/corpus/variants";
let failed = false;

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9): a different wire over a different corpus, so a tools
// refusal to divide it against a `gen` row is correct and automatic.
// linkage esm — the generated table codec is ES modules loaded into the same
// isolate and names no runtime at all, which is the JavaScript spelling of the
// C++ leg's `hdr`. checks contract — this codec has no caller-error assert to
// compile out and its reader's wire-contract validation is unconditional,
// which is §3.4's word for exactly this; it is also what the paired driver
// requires of every table leg. opt default — node has no operator-visible
// optimization level. inline unknown, and it stays unknown: a JIT leg has no
// §4.1 AOT artifact to disassemble, so the verdict pass has no js branch.
//
// NO `codec` COLUMN. The type board's js rows carry one because that leg has
// two generated tiers and names which ran; the fixed form has one codec, so
// there is nothing to name and the row is the plain seventeen.
const CsvSuffix = "esm,contract,default,unknown";
const gCsvRows = [];
const gGoldensLoaded = new Map(); // basename -> bytes

let gSink = 0; // defeats dead code elimination of computed values
let gProbe = 0; // the rotating byte the write loop observes; see the loops

function fnv1a64(h, bytes) {
  for (let i = 0; i < bytes.length; i++) {
    h ^= BigInt(bytes[i]);
    h = (h * 0x100000001b3n) & 0xffffffffffffffffn;
  }
  return h;
}

// §1.6: FNV-1a-64 over the goldens THIS RUN LOADED — for each file in sorted
// basename order, the basename bytes, a 0x00 byte, then the contents. The
// paired driver computes the same hash over the same five files and refuses a
// row whose id does not match it.
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
    // §1.5: a failing run emits NO rows. Numbers from a run whose gate
    // refused are not numbers.
    process.stderr.write("refusing to emit CSV rows from a failing run\n");
    return;
  }
  const id = corpusId();
  process.stdout.write(
    "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," +
      "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n"
  );
  for (const row of gCsvRows) {
    process.stdout.write(`${row},${id},table,${CsvSuffix}\n`);
  }
}

function fail(name, what) {
  process.stderr.write(`FAILED: ${name}: ${what}\n`);
  failed = true;
}

function readCorpus(dir, basename) {
  const path = `${dir}/${basename}`;
  let bytes;
  try {
    bytes = new Uint8Array(readFileSync(path));
  } catch {
    process.stderr.write(
      `missing corpus ${path} — run from the schema repo root (or pass --wire-dir/--variant-dir)\n`
    );
    return null;
  }
  gGoldensLoaded.set(basename, bytes);
  return bytes;
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

function report(bench, path_, iters, bytesPerOp, s) {
  const mbps = (s.median * bytesPerOp) / (1024.0 * 1024.0);
  process.stderr.write(
    `${bench.padEnd(18)} ${path_.padEnd(11)} ${(s.median / 1e6).toFixed(3).padStart(10)} M msg/s ` +
      `${mbps.toFixed(1).padStart(10)} MB/s   (min ${(s.min / 1e6).toFixed(3)}, ` +
      `max ${(s.max / 1e6).toFixed(3)}, spread ${s.spread.toFixed(1)}%)\n`
  );
  if (gCsv) {
    gCsvRows.push(
      `js,${bench},${path_},${iters},${bytesPerOp},${gNumRuns},` +
        `${s.median.toFixed(0)},${s.min.toFixed(0)},${s.max.toFixed(0)},` +
        `${mbps.toFixed(2)},${s.spread.toFixed(2)}`
    );
  }
}

// THE TOLERANT CORPUS, read for what this leg can honestly say about it.
//
// The corpus is ONE corpus (docs/SPEC-TABLES.md §3.4): the same sixty-four
// logical records ride both forms, and the corpus id is over all five files,
// which is what makes a fixed-form row and a tolerant-form row answerable to
// each other and to one packet row. So this leg loads all five. It has no
// form-1 decoder to gate the tolerant three with, and it does not pretend to:
// what it checks is the FRAMING, which is arithmetic over the length index and
// needs no codec —
//
//   - the index is 64 little-endian uint32 lengths,
//   - they sum to exactly the concatenation's size, none of them zero,
//   - and variant 0 IS the pinned instance, byte for byte.
//
// A leg that skipped these files would report a corpus id the paired driver
// could not match, which is §1.6 doing its job rather than a workaround.
function checkTolerantCorpus(name, golden) {
  const packed = readCorpus(gVariantDir, `${name}.variants.bin`);
  const pinned = readCorpus(gWireDir, `${golden}.bin`);
  if (packed === null || pinned === null) {
    return false;
  }
  const lengths = new Array(NumVariants);
  if (gIndexed) {
    const index = readCorpus(gVariantDir, `${name}.lengths`);
    if (index === null || index.length !== NumVariants * 4) {
      fail(name, "the length index is not 64 little-endian uint32 lengths");
      return false;
    }
    for (let k = 0; k < NumVariants; k++) {
      lengths[k] =
        (index[k * 4] | (index[k * 4 + 1] << 8) | (index[k * 4 + 2] << 16) | (index[k * 4 + 3] << 24)) >>> 0;
    }
  } else {
    // the historical fixed-stride format, retained exactly as the C++ leg
    // retains it
    if (packed.length === 0 || packed.length % NumVariants !== 0) {
      fail(name, "the unindexed corpus is not 64 equal records");
      return false;
    }
    lengths.fill(packed.length / NumVariants);
  }
  let offset = 0;
  for (let k = 0; k < NumVariants; k++) {
    if (lengths[k] <= 0 || offset + lengths[k] > packed.length) {
      fail(name, `variant ${k}'s length does not fit the corpus`);
      return false;
    }
    offset += lengths[k];
  }
  if (offset !== packed.length) {
    fail(name, "the lengths do not sum to the corpus size");
    return false;
  }
  if (!bytesEqual(pinned, packed.subarray(0, lengths[0]))) {
    process.stderr.write(
      `WIRE GOLDEN MISMATCH: ${golden} (${pinned.length} golden vs ${lengths[0]} corpus bytes) — ` +
        "refusing to bench against a corpus whose variant 0 is not the pinned instance\n"
    );
    failed = true;
    return false;
  }
  return true;
}

// ------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ------------------------------------------------------------------------
//
// A FILE is the unit here and not a record: the vocabulary block rides once,
// the records follow it to the end, and every one of them is the same size —
// so the write is one call for all 64 and the read is one call back, and an OP
// is one RECORD of that call.
//
// The gates before the clock are the C++ leg's four, in the same order and for
// the same reasons. The first is the one this form adds: THE BLOCK THIS BUILD
// EMITS IS THE CORPUS'S BLOCK, byte for byte. Every other fact about the form
// — the positions, the ids, the kinds, the record's size, the hash every
// record carries — is settled by those bytes, so it is checked first and by
// itself, and a leg that matches it is speaking the form and not a near miss.
function benchFixed(name, baseIters) {
  const iters = gIterations || baseIters;
  const file = readCorpus(gVariantDir, "bench_fixed.bin");
  const vocab = readCorpus(gVariantDir, "bench_fixed.vocab");
  if (file === null || vocab === null || file.length === 0) {
    failed = true;
    return;
  }
  const bytesPerOp = file.length / NumVariants;
  const twin = new Uint8Array(file.length);

  // gate 1: THE BLOCK IS THE CORPUS'S BLOCK.
  if (!bytesEqual(vocab, FixedTableFixedBlock)) {
    fail(name, "this build's vocabulary block is not the corpus's, byte for byte");
    return;
  }

  // The measured shape, named once — the generated type at the point this
  // driver declares its storage and hands it to the generated Save/Load, and
  // nothing else about it (bench/tables/js/SHAPE-GATE.allow).
  const values = Array.from({ length: NumVariants }, () => new FixedTable());
  const out = Array.from({ length: NumVariants }, () => new FixedTable());
  const plan = FixedTableFixedNewPlan();
  const outPlan = FixedTableFixedNewPlan();
  const report_ = new TableFixedReport();

  // gate 2: the whole file loads clean.
  if (FixedTableFixedLoad(values, NumVariants, file, file.length, plan, report_) !== NumVariants) {
    fail(name, "the fixed corpus did not load");
    return;
  }

  // gate 3: writing them back reproduces the file, byte for byte.
  if (FixedTableFixedSave(values, NumVariants, twin) !== file.length || !bytesEqual(twin, file)) {
    fail(name, "round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus");
    return;
  }

  // gate 4: reused storage round-trips too. The load owns the prefill, so
  // nothing is reset between the two passes.
  for (let k = 0; k < 2; k++) {
    if (
      FixedTableFixedLoad(out, NumVariants, file, file.length, outPlan, report_) !== NumVariants ||
      FixedTableFixedSave(out, NumVariants, twin) !== file.length ||
      !bytesEqual(twin, file)
    ) {
      fail(name, "reused target round-trip bytes differ");
      return;
    }
  }

  if (gGate) {
    return;
  }

  const need = FixedTableFixedMeasure(NumVariants);
  const writeRates = new Array(gNumRuns);
  const roundTripRates = new Array(gNumRuns);

  // WRITE: one call lays down all 64 records; an op is one record.
  //
  // The escape barrier is the sink, and the sink OBSERVES A WRITTEN BYTE.
  // JavaScript has no empty-asm memory clobber, and the byte count this form
  // returns is a constant of the record count — a sink fed only by that would
  // let the stores themselves be dead. So the sink reads one byte back out of
  // the buffer per call, at a rotating offset, which is one load per 64
  // records and is the closest thing this runtime has to the clobber.
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i += NumVariants) {
      const wrote = FixedTableFixedSave(values, NumVariants, twin);
      if (wrote !== file.length) {
        fail(name, "save failed in loop");
        return;
      }
      gProbe = gProbe + 1 < need ? gProbe + 1 : 0;
      gSink = gSink + wrote + twin[gProbe];
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // ROUND-TRIP: read the file back, then write what came out. The read needs
  // no sink discipline of its own — its output IS the write's input, so every
  // decoded field is observed by construction. This is the arm that carries
  // the object projection: the plan run fills the reader's image and the
  // decode projects it into JavaScript objects, per record, every time.
  for (let run = -1; run < gNumRuns; run++) {
    const start = performance.now();
    for (let i = 0; i < iters; i += NumVariants) {
      if (FixedTableFixedLoad(out, NumVariants, file, file.length, outPlan, report_) !== NumVariants) {
        fail(name, "load failed in loop");
        return;
      }
      const wrote = FixedTableFixedSave(out, NumVariants, twin);
      if (wrote !== file.length) {
        fail(name, "re-save failed in loop");
        return;
      }
      gProbe = gProbe + 1 < need ? gProbe + 1 : 0;
      gSink = gSink + wrote + twin[gProbe];
    }
    const elapsed = (performance.now() - start) / 1000.0;
    if (run >= 0) {
      roundTripRates[run] = iters / elapsed;
    }
  }

  const w = stats(writeRates);
  const rt = stats(roundTripRates);
  report(name, "write", iters, bytesPerOp, w);
  report(name, "round_trip", iters, bytesPerOp, rt);

  // READ is DERIVED, never measured: round-trip time minus write time. It
  // prints for continuity and is NOT a CSV row — a derived number in the CSV
  // would be divided as if it had been measured (§2.9). For this leg it is
  // also where the object projection shows.
  const readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    process.stderr.write(
      `${name.padEnd(18)} ${"read".padEnd(11)} ${(1e-6 / readTime).toFixed(3).padStart(10)} M msg/s   ` +
        "(DERIVED: round-trip minus write, informational — not a measured row)\n"
    );
  }
}

function usage() {
  process.stderr.write(
    "usage: node bench/tables/js/table_main.mjs [--indexed] [--gate] [--csv] [--round K] " +
      "[--iterations N] [--wire-dir <dir>] [--variant-dir <dir>]\n"
  );
  process.exit(1);
}

function main() {
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--indexed") {
      gIndexed = true;
    } else if (args[i] === "--gate") {
      gGate = true;
    } else if (args[i] === "--csv") {
      gCsv = true;
    } else if (args[i] === "--wire-dir" && i + 1 < args.length) {
      gWireDir = args[++i];
    } else if (args[i] === "--variant-dir" && i + 1 < args.length) {
      gVariantDir = args[++i];
    } else if (args[i] === "--iterations" && i + 1 < args.length) {
      const n = Number(args[++i]);
      if (!Number.isInteger(n) || n <= 0 || n > 2147483584 || n % NumVariants !== 0) {
        process.stderr.write("--iterations requires a positive multiple of 64 up to 2147483584\n");
        process.exit(1);
      }
      gIterations = n;
    } else if (args[i] === "--round" && i + 1 < args.length) {
      // §2.4: one warmup + one measured run, then exit. K only identifies the
      // round to the interleaved driver, which aggregates across rounds itself.
      const k = Number(args[++i]);
      if (!Number.isInteger(k) || k < 0) {
        process.stderr.write(`--round takes a non-negative integer, got '${args[i]}'\n`);
        process.exit(1);
      }
      gNumRuns = 1;
    } else {
      usage();
    }
  }

  process.stderr.write(`schema tables bench (js, node ${process.versions.node}, the FIXED FORM)\n`);

  // The tolerant half's bytes, and every check this leg can make of them
  // without a form-1 decoder it does not have (schema#516).
  if (checkTolerantCorpus("bench_table", "bench_table")) {
    benchFixed("bench_fixed", DefaultIterations);
  }

  flushCsv(); // rows carry the corpus_id of the goldens this run loaded

  if (failed) {
    process.stderr.write(`TABLES BENCH FAILED (corpus_id ${corpusId()})\n`);
    process.exit(1);
  }
  process.stderr.write(`OK (corpus_id ${corpusId()}, sink ${gSink === 0 ? "0" : "nonzero"})\n`);
}

main();
