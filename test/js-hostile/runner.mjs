// THE JAVASCRIPT PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated readers underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock, no outside service. The runner prints one FIX-CARD line
// per carried disagreement, a FAIL line per disagreement no FIX-CARD names, a
// STALE line per FIX-CARD whose row has gone green, and one summary line; it
// exits 1 on any FAIL or STALE, so the carried list cannot rot into silence.
//
//   node test/js-hostile/runner.mjs generate <corpus-dir> <generated-dir> <schema-binary>
//   node test/js-hostile/runner.mjs run <corpus-dir> <generated-dir> <serialize.js-dir> [known-findings.tsv]
//   node test/js-hostile/runner.mjs show <corpus-dir> <generated-dir> <serialize.js-dir> <row>
//
// The working directory is the repository root. `generate` writes one unit per
// schema the manifest names, under <generated-dir>/<slug>; a schema the
// checkout does not carry (schemas/probe.schema) is the copy beside this file.
//
// ISOLATION AND THE BUDGET. Every row runs on a worker thread from a small
// pool, each worker with a capped heap. A row has HOSTILE_CASE_BUDGET_MS
// (default 5000) from the moment it is handed over: past it the worker is
// terminated and the row FAILS as over budget, so a hang or a quadratic path is
// a failure and not a slow pass. A throw, a stack overflow or a heap exhaustion
// fails that row alone; the pool replaces the worker and the run goes on.
// HOSTILE_JS_JOBS sets the pool size (default: half the cores, at most 4).
//
// A carrier maps to the port's public readers:
//   text              <T>FromJson(text, report)          (<Unit>Variable.js)
//   wire              <T>Load(bytes, report)
//   wire_retain       <T>LoadRetain(bytes, retain, report)
//   message           AnnounceRead(own announcement), then <T>LoadMessages
//                     into one value (capacity 1)          (<Unit>Table.js)
//   message_novocab   <T>LoadMessages with no announcement read
//   announce          AnnounceRead(vocabulary, input, report)
//   announce_second   AnnounceRead twice on one vocabulary
//   announce_bytes    AnnounceRead with MaxBytes 8
//   announce_entries  AnnounceRead into an entry store of 1
//   packet            Read<T>(ReadStream, value)           the runtime tier
//                     Read<T>Flat(view, numBits, value)    the flat tier
// A packet case is four rows, <name>@<tier>-<mode>: both tiers, each in a
// worker whose NODE_ENV is development and in one whose NODE_ENV is production
// (serialize.js and the flat modules fix their mode at module load). Every
// other carrier is one row, <name>, in development.
//
// VERDICTS. A table read's refusal is the reason its report names, cut at its
// first ": " (the port appends the field, `non_finite_float: ShipEntry.health`);
// a malformed read, or one that hands back no value, is the corpus's `damaged`.
// An accepted text, wire or wire_retain read is digested as the engine's is:
// SHA-256 of the port's text writer with its one trailing newline removed. After
// that accept the runner also drives the value through Measure, Save and ToJson
// (and the retaining pair on a retaining read): a throw or hang there FAILS the
// row, a clean refusal (-1, null) does not. A message accept decodes into the
// fixed class, which has no text writer; the runner proves the body set no
// field by comparing its fixed record with a freshly initialized one, byte for
// byte, and digests the defaults the text read of `{}` states. A packet accept
// is digested as SHA-256 of the bytes the same tier's own writer emits when it
// re-writes the decoded value; a writer that refuses or throws is an error.
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { availableParallelism } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { isMainThread, parentPort, Worker, workerData } from "node:worker_threads";

const HERE = dirname(new URL(import.meta.url).pathname);
const MODES = ["development", "production"];
const TIERS = ["runtime", "flat"];

// ---- the manifest and the carried findings ----

function rows(path) {
  return readFileSync(path, "utf8")
    .split("\n")
    .filter((l) => l !== "" && !l.startsWith("#"))
    .map((l) => l.split("\t"));
}

function manifestCases(corpus) {
  return rows(join(corpus, "manifest.tsv")).map((f) => ({
    name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
    verdict: f[6], reason: f[7], digest: f[8],
  }));
}

// a schema path the checkout carries is used as-is; the one the corpus names
// outside it (schemas/probe.schema) is the copy beside this runner
function schemaPath(schema) {
  return existsSync(schema) ? schema : join(HERE, basename(schema));
}

function slug(schema) {
  return schema.replace(/[^A-Za-z0-9]+/g, "_");
}

// ---- generate: one unit per schema the manifest names ----

function generateOne(bin, out, path) {
  return new Promise((done) => {
    execFile(bin, ["generate", "--lang", "js", "--out", out, path], (err, _stdout, stderr) => {
      done(err ? String(stderr || err.message).trim() : null);
    });
  });
}

async function generate(corpus, outDir, bin) {
  const schemas = [...new Set(manifestCases(corpus).map((c) => c.schema))];
  const failures = await Promise.all(schemas.map(async (schema) => {
    const path = schemaPath(schema);
    const out = join(outDir, slug(schema));
    let err = await generateOne(bin, out, path);
    // a packet schema file that imports its siblings is generated as its unit
    if (err !== null && path.endsWith(".schema") && statSync(path).isFile()) {
      err = await generateOne(bin, out, dirname(path));
    }
    return err === null ? null : `${schema}: ${err}`;
  }));
  const bad = failures.filter((f) => f !== null);
  for (const f of bad) {
    console.error(`generate failed: ${f}`);
  }
  if (bad.length > 0) {
    process.exit(1);
  }
}

// ---- the rows: one per case, four per packet case ----

function caseRows(cases) {
  const out = [];
  for (const c of cases) {
    if (c.carrier === "packet") {
      for (const tier of TIERS) {
        for (const mode of MODES) {
          out.push({ ...c, id: `${c.name}@${tier}-${mode}`, tier, mode });
        }
      }
    } else {
      out.push({ ...c, id: c.name, tier: "", mode: "development" });
    }
  }
  return out;
}

const CARRIERS = new Set(["text", "wire", "wire_retain", "message", "message_novocab",
  "announce", "announce_second", "announce_bytes", "announce_entries", "packet"]);

// ---- the worker: the port's readers, one row at a time ----

function sha256Hex(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

class Unbound extends Error {}

const DAMAGED = { verdict: "refuse", detail: "damaged" };

function reasonName(r) {
  const i = r.indexOf(": ");
  return i < 0 ? r : r.slice(0, i);
}

// A unit is every module the generator wrote for one schema, loaded once.
// Names collide across modules (a table's fixed class and its variable class
// share a name), so a lookup names the function it needs and takes its
// companions from the module that exports it.
class Unit {
  constructor(mods) { this.mods = mods; }
  with(...names) {
    for (const m of this.mods) {
      if (names.every((n) => typeof m[n] !== "undefined")) {
        return m;
      }
    }
    return null;
  }
  need(what, ...names) {
    const m = this.with(...names);
    if (m === null) {
      throw new Unbound(`no ${what} bound: ${names.join(", ")}`);
    }
    return m;
  }
}

async function loadUnit(dir) {
  const files = readdirSync(dir).filter((f) => f.endsWith(".js")).sort();
  const mods = [];
  for (const f of files) {
    mods.push(await import(pathToFileURL(resolve(dir, f)).href));
  }
  return new Unit(mods);
}

class Reader {
  constructor(genDir, runtime) {
    this.genDir = genDir;
    this.runtime = runtime;
    this.units = new Map();
    this.show = false;
  }

  async preload(schemas) {
    for (const s of schemas) {
      this.units.set(s, await loadUnit(join(this.genDir, slug(s))));
    }
  }

  unit(schema) {
    const u = this.units.get(schema);
    if (u === undefined) {
      throw new Unbound(`no generated unit for ${schema}`);
    }
    return u;
  }

  accepted(written) {
    if (written === null || written === undefined) {
      return { verdict: "error", detail: "ToJson refused the decoded value" };
    }
    if (this.show) {
      process.stdout.write(written);
    }
    const canonical = written.endsWith("\n") ? written.slice(0, -1) : written;
    return { verdict: "accept", detail: sha256Hex(Buffer.from(canonical, "utf8")) };
  }

  // A table read's verdict off its report: a named refusal is its reason, a
  // malformed read or one with no value is `damaged`, and a clean read is
  // driven through the writers and accepted with its digest.
  tableVerdict(m, T, value, report, after) {
    if (report.reason) {
      return { verdict: "refuse", detail: reasonName(report.reason) };
    }
    if (value === null || value === undefined || report.malformed) {
      return DAMAGED;
    }
    m[`${T}Measure`](value);
    m[`${T}Save`](value);
    if (after) {
      after();
    }
    return this.accepted(m[`${T}ToJson`](value));
  }

  text(schema, T, payload) {
    const m = this.unit(schema).need("text reader", `${T}FromJson`, `${T}ToJson`, "TableVariableReport");
    let text;
    try {
      // a text read takes a JS string: bytes that are not UTF-8 are not text,
      // and a byte-order mark is handed to the reader rather than eaten here
      text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(payload);
    } catch {
      return DAMAGED;
    }
    const report = new m.TableVariableReport();
    return this.tableVerdict(m, T, m[`${T}FromJson`](text, report), report, null);
  }

  wire(schema, T, payload) {
    const m = this.unit(schema).need("wire reader", `${T}Load`, `${T}ToJson`, "TableVariableReport");
    const report = new m.TableVariableReport();
    return this.tableVerdict(m, T, m[`${T}Load`](payload, report), report, null);
  }

  wireRetain(schema, T, payload) {
    const m = this.unit(schema).need("retaining wire reader", `${T}LoadRetain`, `${T}MeasureRetain`,
      `${T}SaveRetain`, "TableVariableRetain", "TableVariableReport");
    const retain = new m.TableVariableRetain(new Uint8Array(64 * 1024), new Array(8 * 1024).fill(null));
    const report = new m.TableVariableReport();
    const value = m[`${T}LoadRetain`](payload, retain, report);
    return this.tableVerdict(m, T, value, report, () => {
      m[`${T}MeasureRetain`](value, retain);
      m[`${T}SaveRetain`](value, retain, new m.TableVariableReport());
    });
  }

  vocabulary(m, capacity) {
    const v = new m.TableVocabulary();
    v.Init(new Array(capacity).fill(null));
    return v;
  }

  message(schema, T, payload, announced) {
    const u = this.unit(schema);
    const batch = u.need("message reader", `${T}LoadMessages`, `${T}MeasureMessages`, `${T}SaveMessages`,
      `${T}FixedMeasure`, `${T}FixedSave`);
    const fixed = u.need("fixed class", `Init${T}`, T, "TableVocabulary", "AnnounceRead", "Announce",
      "AnnounceMeasure", "TableMessageOpenRefused");
    const vocab = this.vocabulary(fixed, 4096);
    if (announced) {
      const own = new Uint8Array(fixed.AnnounceMeasure());
      fixed.Announce(own);
      const ar = {};
      if (!fixed.AnnounceRead(vocab, own, ar) || ar.Malformed) {
        return { verdict: "error", detail: "the unit could not read its own announcement" };
      }
    }
    const value = new fixed[T]();
    fixed[`Init${T}`](value);
    const report = {};
    const n = batch[`${T}LoadMessages`]([value], vocab, payload, report);
    if (report.Verdict === fixed.TableMessageOpenRefused) {
      return { verdict: "refuse", detail: reasonName(report.Reason || "") || "damaged" };
    }
    if (report.Malformed || n !== 1) {
      return DAMAGED;
    }
    const size = batch[`${T}MeasureMessages`]([value], {});
    if (size > 0) {
      batch[`${T}SaveMessages`]([value], new Uint8Array(size), {});
    }
    const fresh = new fixed[T]();
    fixed[`Init${T}`](fresh);
    const a = new Uint8Array(batch[`${T}FixedMeasure`](1));
    const b = new Uint8Array(batch[`${T}FixedMeasure`](1));
    batch[`${T}FixedSave`]([value], 1, a);
    batch[`${T}FixedSave`]([fresh], 1, b);
    if (!a.every((x, i) => x === b[i])) {
      return { verdict: "error", detail: "the decoded body set a field: no text writer to digest it" };
    }
    const variable = u.need("text reader", `${T}FromJson`, `${T}ToJson`, "TableVariableReport");
    const defaults = variable[`${T}FromJson`]("{}", new variable.TableVariableReport());
    return this.accepted(variable[`${T}ToJson`](defaults));
  }

  announce(schema, carrier, payload) {
    const m = this.unit(schema).need("announcement reader", "TableVocabulary", "AnnounceRead",
      "TableMessageOpenRefused");
    const vocab = this.vocabulary(m, carrier === "announce_entries" ? 1 : 4096);
    if (carrier === "announce_bytes") {
      vocab.MaxBytes = 8;
    }
    if (carrier === "announce_second") {
      m.AnnounceRead(vocab, payload, {});
    }
    const report = {};
    const ok = m.AnnounceRead(vocab, payload, report);
    if (report.Verdict === m.TableMessageOpenRefused) {
      return { verdict: "refuse", detail: reasonName(report.Reason || "") || "damaged" };
    }
    if (report.Malformed || !ok) {
      return DAMAGED;
    }
    return { verdict: "accept", detail: "-" };
  }

  packet(schema, T, payload, tier) {
    const u = this.unit(schema);
    if (tier === "runtime") {
      const m = u.need("runtime-tier packet reader", `Read${T}`, `Write${T}`, T);
      const value = new m[T]();
      if (!m[`Read${T}`](new this.runtime.ReadStream(payload), value)) {
        return { verdict: "refuse", detail: "packet_refused" };
      }
      const ws = new this.runtime.WriteStream(new Uint8Array(Math.max(4096, payload.length * 4 + 64)));
      if (!m[`Write${T}`](ws, value)) {
        return { verdict: "error", detail: "the runtime writer refused the value its reader accepted" };
      }
      ws.flush();
      return { verdict: "accept", detail: sha256Hex(ws.data()) };
    }
    const flat = u.need("flat-tier packet reader", `Read${T}Flat`, `Write${T}Flat`);
    const cls = u.need("packet class", `Read${T}`, T);
    const value = new cls[T]();
    // the flat reader's buffer extends FlatReadSlack (8) bytes past the payload
    const buf = new Uint8Array(payload.length + 8);
    buf.set(payload);
    if (!flat[`Read${T}Flat`](new DataView(buf.buffer), payload.length * 8, value)) {
      return { verdict: "refuse", detail: "packet_refused" };
    }
    const out = new Uint8Array(Math.max(4096, payload.length * 4 + 64));
    const n = flat[`Write${T}Flat`](new DataView(out.buffer), value);
    if (n < 0) {
      return { verdict: "error", detail: "the flat writer refused the value its reader accepted" };
    }
    return { verdict: "accept", detail: sha256Hex(out.subarray(0, n)) };
  }

  run(row, payload) {
    switch (row.carrier) {
      case "text":
        return this.text(row.schema, row.table, payload);
      case "wire":
        return this.wire(row.schema, row.table, payload);
      case "wire_retain":
        return this.wireRetain(row.schema, row.table, payload);
      case "message":
        return this.message(row.schema, row.table, payload, true);
      case "message_novocab":
        return this.message(row.schema, row.table, payload, false);
      case "announce":
      case "announce_second":
      case "announce_bytes":
      case "announce_entries":
        return this.announce(row.schema, row.carrier, payload);
      case "packet":
        return this.packet(row.schema, row.table, payload, row.tier);
    }
    throw new Unbound(`unknown carrier ${row.carrier}`);
  }

  // a throw from the port is an error answer for the row; an unbound reader is
  // the runner's own defect and is reported as such
  answer(row, payload) {
    try {
      return this.run(row, payload);
    } catch (e) {
      if (e instanceof Unbound) {
        return { verdict: "unbound", detail: e.message };
      }
      const what = e instanceof Error ? `${e.name}: ${e.message}` : String(e);
      return { verdict: "error", detail: `threw ${what.split("\n")[0]}` };
    }
  }
}

async function loadRuntime(dir) {
  return import(pathToFileURL(resolve(dir, "src", "index.js")).href);
}

async function workerMain() {
  const { genDir, serializeJs, schemas } = workerData;
  const reader = new Reader(genDir, await loadRuntime(serializeJs));
  await reader.preload(schemas);
  parentPort.on("message", (job) => {
    parentPort.postMessage(reader.answer(job.row, job.payload));
  });
  parentPort.postMessage({ ready: true });
}

// ---- the pool ----

class Slot {
  constructor(pool) {
    this.pool = pool;
    this.job = null;
    this.timer = null;
    this.ready = false;
    this.worker = new Worker(new URL(import.meta.url), {
      workerData: pool.data,
      env: { ...process.env, NODE_ENV: pool.mode },
      resourceLimits: { maxOldGenerationSizeMb: 1024 },
      stdout: false,
      stderr: false,
    });
    this.worker.on("message", (msg) => this.message(msg));
    this.worker.on("error", (err) => this.died(`crashed: ${String(err && err.message || err).split("\n")[0]}`));
    this.worker.on("exit", (code) => this.died(`exited with code ${code}`));
  }

  message(msg) {
    if (msg.ready) {
      this.ready = true;
      this.pool.next(this);
      return;
    }
    const job = this.job;
    clearTimeout(this.timer);
    this.job = null;
    job.done(msg);
    this.pool.next(this);
  }

  start(job) {
    this.job = job;
    this.timer = setTimeout(() => {
      this.died(`over budget: no answer in ${this.pool.budget} ms`);
    }, this.pool.budget);
    this.worker.postMessage({ row: job.row, payload: job.payload });
  }

  died(why) {
    if (this.dead) {
      return;
    }
    this.dead = true;
    clearTimeout(this.timer);
    this.worker.removeAllListeners();
    this.worker.on("error", () => {});
    this.worker.terminate();
    const job = this.job;
    this.job = null;
    if (!this.ready) {
      this.pool.fatal(`a ${this.pool.mode} worker died before it was ready: ${why}`);
      return;
    }
    if (job !== null) {
      job.done({ verdict: "error", detail: why });
    }
    this.pool.replace(this);
  }
}

class Pool {
  constructor(mode, size, data, budget, fatal) {
    this.mode = mode;
    this.data = data;
    this.budget = budget;
    this.fatal = fatal;
    this.queue = [];
    this.slots = [];
    this.size = size;
    this.started = false;
  }
  submit(row, payload) {
    return new Promise((done) => {
      this.queue.push({ row, payload, done });
      if (!this.started) {
        this.started = true;
        for (let i = 0; i < this.size; i++) {
          this.slots.push(new Slot(this));
        }
      }
    });
  }
  next(slot) {
    const job = this.queue.shift();
    if (job === undefined) {
      slot.idle = true;
      this.maybeClose();
      return;
    }
    slot.start(job);
  }
  replace(slot) {
    this.slots = this.slots.filter((s) => s !== slot);
    if (this.queue.length > 0) {
      this.slots.push(new Slot(this));
    } else {
      this.maybeClose();
    }
  }
  maybeClose() {
    if (this.queue.length === 0 && this.slots.every((s) => s.idle)) {
      for (const s of this.slots) {
        s.dead = true;
        s.worker.removeAllListeners();
        s.worker.terminate();
      }
      this.slots = [];
    }
  }
}

// ---- run ----

function want(c) {
  return c.verdict === "accept"
    ? { verdict: "accept", detail: c.digest }
    : { verdict: "refuse", detail: c.reason };
}

function show(a) {
  return `${a.verdict} ${a.detail}`;
}

async function run(corpus, genDir, serializeJs, findingsPath) {
  if (sha256Hex(Buffer.from("abc")) !== "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") {
    console.error("the runner's SHA-256 fails its own known-answer test");
    process.exit(1);
  }
  const budget = Number(process.env.HOSTILE_CASE_BUDGET_MS || 5000);
  const jobs = Number(process.env.HOSTILE_JS_JOBS || Math.max(1, Math.min(4, Math.floor(availableParallelism() / 2))));
  const cases = manifestCases(corpus);
  const findings = new Map();
  if (findingsPath && existsSync(findingsPath)) {
    for (const f of rows(findingsPath)) {
      findings.set(f[0], f);
    }
  }
  const all = caseRows(cases);
  const skipped = all.filter((r) => !CARRIERS.has(r.carrier));
  const live = all.filter((r) => CARRIERS.has(r.carrier));
  const schemas = [...new Set(live.map((r) => r.schema))];
  const data = { genDir: resolve(genDir), serializeJs: resolve(serializeJs), schemas };
  const fatal = (why) => {
    console.error(why);
    process.exit(1);
  };
  const pools = new Map(MODES.map((mode) => [mode, new Pool(mode, jobs, data, budget, fatal)]));
  const answers = await Promise.all(live.map((row) =>
    pools.get(row.mode).submit(row, readFileSync(join(corpus, row.input)))));

  let passed = 0, carried = 0;
  const bad = [], stale = [], unbound = [];
  const seen = new Set(all.map((r) => r.id));
  for (const r of skipped) {
    console.log(`SKIP ${r.id}: carrier ${r.carrier} has no JavaScript reader`);
  }
  live.forEach((row, i) => {
    const got = answers[i];
    if (got.verdict === "unbound") {
      console.log(`UNBOUND ${row.id}: ${got.detail}`);
      unbound.push(row.id);
      return;
    }
    const w = want(row);
    const agrees = got.verdict === w.verdict && got.detail === w.detail;
    const finding = findings.get(row.id);
    if (agrees) {
      if (finding !== undefined) {
        console.log(`STALE ${row.id}: FIX-CARD ${finding[1]} no longer reproduces`);
        stale.push(row.id);
      } else {
        passed++;
      }
    } else if (finding !== undefined) {
      console.log(`FIX-CARD: ${finding[1]}: ${finding[2]} ${finding[3]} (got ${show(got)})`);
      carried++;
    } else {
      console.log(`FAIL ${row.id} carrier=${row.carrier} got=${show(got)} want=${show(w)}`);
      bad.push(row.id);
    }
  });
  for (const name of findings.keys()) {
    if (!seen.has(name)) {
      console.log(`STALE ${name}: FIX-CARD names no row the manifest carries`);
      stale.push(name);
    }
  }
  console.log(`${cases.length} cases, ${all.length} rows: ${passed} pass, ${carried} carried as FIX-CARD, ` +
    `${bad.length} unexplained, ${stale.length} stale, ${skipped.length} skipped, ${unbound.length} unbound`);
  process.exit(bad.length > 0 || stale.length > 0 || unbound.length > 0 ? 1 : 0);
}

// `show <row>` runs one row on this thread and prints the port's canonical
// text for it, to diff by hand against the engine's when a FIX-CARD is worked.
// A packet row runs in the mode this process was started in.
async function showRow(corpus, genDir, serializeJs, id) {
  const row = caseRows(manifestCases(corpus)).find((r) => r.id === id);
  if (row === undefined) {
    console.error(`no row ${id}`);
    process.exit(2);
  }
  const reader = new Reader(resolve(genDir), await loadRuntime(serializeJs));
  await reader.preload([row.schema]);
  reader.show = true;
  const got = reader.answer(row, readFileSync(join(corpus, row.input)));
  console.error(`\ngot ${show(got)}\nwant ${show(want(row))}`);
}

async function main(args) {
  if (args[0] === "generate" && args.length === 4) {
    return generate(args[1], args[2], args[3]);
  }
  if (args[0] === "run" && (args.length === 4 || args.length === 5)) {
    return run(args[1], args[2], args[3], args[4]);
  }
  if (args[0] === "show" && args.length === 5) {
    return showRow(args[1], args[2], args[3], args[4]);
  }
  console.error("usage: runner.mjs generate <corpus-dir> <generated-dir> <schema-binary>");
  console.error("       runner.mjs run <corpus-dir> <generated-dir> <serialize.js-dir> [known-findings.tsv]");
  console.error("       runner.mjs show <corpus-dir> <generated-dir> <serialize.js-dir> <row>");
  process.exit(2);
}

if (isMainThread) {
  await main(process.argv.slice(2));
} else {
  await workerMain();
}
