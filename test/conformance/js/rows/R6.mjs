// R6 — A TABLE PAST §3.4's 65536 CEILING IS NOT A FIXED-FORM ROOT: the refusal
// names the table, no form is emitted, and no lineage entry is parsed for it
// even when the lock carries one.
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:740-754 (§5.2 COMPILE): "A TABLE PAST
// §3.4's CEILING IS NOT A FIXED-FORM ROOT, SO COMPILE CONSULTS NO LINEAGE FOR
// IT ... the generator parses no entry of it, even when the LOCK carries one
// ... The only thing the generated module owes such a table is the line naming
// why the form is not there." The hurt it records is THIS leg (#931): the JS
// ceiling refusal did not name the table, WideBlob was a fixed-form root here
// and nowhere else, and the lineage parse held the lock's CORRECT entry to the
// form's rules — where a root past 65536 does not parse — and failed the build
// over a lock that was right. Beside it, :2088 (§6): "a record body of 65536
// bytes is lawful and 65537 is not ... the compiler names the table and its
// size". The codegen pin is TestJSFixedNoFormMeansNoLineageToParse
// (internal/codegen/jstable/fixedform_test.go); this row drives the shipped
// compiler end to end, the way a user does.
//
// THE VECTOR, derived from the law. A unit of two fixed tables:
//
//   Small { keep uint32 }       — a 4-byte body, a fixed-form root: THE POSITIVE
//                                 CONTROL, so the row cannot pass on a module
//                                 that emitted nothing.
//   Huge  { payload bytes(70000) } — 4 (the length word) + 70000 = 70004 body
//                                 bytes, past 65536; every field a plain bounded
//                                 one, so the ceiling is the ONLY reason it is
//                                 not a root.
//
// `bin/schema lock` writes the unit's lock, and the lock DOES carry a lineage
// entry for Huge (it records every fixed table's layout). The row then shows
// that entry, held to the form's rules by this leg's own TableFixedParseLayout,
// does NOT parse (layout_record_too_large: its root is past 65536) — so a
// generator that parsed it would fail the build — and that `bin/schema generate
// --lang js` over that very lock succeeds, names Huge, and emits nothing of the
// entry: not its hash, not its layout bytes, no fixed-form surface.
//
// run: node test/conformance/js/rows/R6.mjs   (from the repository root)
// exit: 0 = every assertion passed, 1 = one or more failed
// depends on: bin/schema (a prerequisite of make build/tables-generated-js/.stamp)

import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const CEILING = 65536; // §3.4, spelled here and never read from the code under test

let pass = 0;
let fail = 0;
function check(ok, msg) {
  if (ok) { console.log("PASS " + msg); pass++; }
  else { console.log("FAIL " + msg); fail++; }
}
function finish() {
  if (fail > 0) {
    console.log(`\nFAILED: ${fail} of ${pass + fail} assertions`);
    process.exit(1);
  }
  console.log(`\nPASSED: ${pass} assertions`);
  process.exit(0);
}

const schema = resolve("bin/schema");
const work = mkdtempSync(join(tmpdir(), "schema-js-R6-"));
try {
  await main();
} catch (e) {
  check(false, "the row ran to its end without a throw: " + (e && e.stack ? e.stack : e));
} finally {
  rmSync(work, { recursive: true, force: true });
}
finish();

function run(args) {
  const r = spawnSync(schema, args, { encoding: "utf8" });
  if (r.error) { throw new Error(`${schema} ${args.join(" ")}: ${r.error.message}`); }
  return r;
}

// One table's lineage lines out of the lock: [{ wire, record, bytes }].
function lineageOf(lock, table) {
  const out = [];
  let inside = false;
  for (const line of lock.split("\n")) {
    if (/^\S/.test(line)) { inside = line.startsWith(`fixed table ${table} `); continue; }
    const m = inside && line.match(/^\s+lineage wire=0x([0-9a-f]{16}) record=(\d+) bytes=([0-9a-f]+)/);
    if (m) { out.push({ wire: m[1], record: Number(m[2]), bytes: m[3] }); }
  }
  return out;
}

async function main() {
  const unit = join(work, "unit");
  const gen = join(work, "gen");
  mkdirSync(unit);
  writeFileSync(join(unit, "Probe.schema"), `package probe
fixed table Small
{
    keep uint32 = 0
}
fixed table Huge
{
    payload bytes(70000)
}
`);

  // ---- 1. THE LOCK CARRIES AN ENTRY FOR HUGE ----------------------------------
  const locked = run(["lock", unit]);
  check(locked.status === 0, `schema lock writes the unit's lock (exit ${locked.status})${locked.status === 0 ? "" : "\n" + locked.stderr.trim()}`);
  const lock = readFileSync(join(unit, "schema.lock"), "utf8");
  const huge = lineageOf(lock, "Huge");
  const small = lineageOf(lock, "Small");
  check(huge.length >= 1, `the lock carries ${huge.length} lineage entr${huge.length === 1 ? "y" : "ies"} for Huge`);
  check(small.length >= 1, `the lock carries ${small.length} lineage entr${small.length === 1 ? "y" : "ies"} for Small`);
  check(huge.every((e) => e.record > CEILING),
    `Huge's recorded body (${huge.map((e) => e.record).join(", ")}) is past the ${CEILING}-byte ceiling`);
  check(small.every((e) => e.record <= CEILING), "Small's recorded body is within the ceiling");

  // ---- 2. GENERATE OVER THAT LOCK SUCCEEDS AND NAMES THE TABLE ----------------
  const g = run(["generate", "--lang", "js", "--out", gen, unit]);
  check(g.status === 0,
    `a lineage entry for a table with NO FORM does not fail the build (exit ${g.status})${g.status === 0 ? "" : "\n" + g.stderr.trim()}`);
  check(/table Huge\b[^\n]*70004 bytes[^\n]*65536/.test(g.stderr),
    "the compiler's refusal names the table Huge, its body size and the ceiling");
  check(!/table Small\b/.test(g.stderr), "the compiler refuses nothing about Small");

  const files = readdirSync(gen).filter((f) => f.endsWith(".js"));
  const src = files.map((f) => readFileSync(join(gen, f), "utf8")).join("\n");
  check(files.includes("ProbeTable.js"), "the unit's fixed-form module ProbeTable.js is written");

  // ---- 3. THE NAMED REFUSAL LINE IN THE MODULE --------------------------------
  check(/table Huge has NO FIXED FORM in JavaScript[^\n]*65536/.test(src),
    "NAMED, NEVER SILENT: the module carries the line naming Huge and the 65536 ceiling");

  // ---- 4. THE POSITIVE CONTROL: Small HAS its form, and its lineage is laid ---
  const mod = await import(pathToFileURL(join(gen, "ProbeTable.js")).href);
  for (const name of ["SmallFixedMeasure", "SmallFixedSave", "SmallFixedLoad", "SmallFixedNewPlan", "SmallFixedLayoutCopy"]) {
    check(typeof mod[name] === "function", `positive control: Small exports ${name}`);
  }
  for (const name of ["SmallFixedBodyBytes", "SmallFixedRecordBytes", "SmallFixedHashLo", "SmallFixedHashHi", "SmallFixedLayoutBytes"]) {
    check(typeof mod[name] === "number", `positive control: Small exports ${name}`);
  }
  const s = small[small.length - 1];
  check(mod.SmallFixedHashLo >>> 0 === parseInt(s.wire.slice(8), 16) &&
        mod.SmallFixedHashHi >>> 0 === parseInt(s.wire.slice(0, 8), 16),
    "positive control: Small's own hash is the lock's lineage hash for it");
  {
    const v = new mod.Small();
    v.keep = 0xC0FFEE;
    const buf = new Uint8Array(mod.SmallFixedMeasure(1));
    const wrote = mod.SmallFixedSave([v], 1, buf);
    const back = [new mod.Small()];
    const report = new mod.TableFixedReport();
    const n = mod.SmallFixedLoad(back, 1, buf, buf.length, mod.SmallFixedNewPlan(), report);
    check(wrote === buf.length && n === 1 && report.refused === 0 && back[0].keep === 0xC0FFEE,
      "positive control: a Small record saves and loads back through its fixed form");
  }

  // ---- 5. NOTHING OF HUGE'S FORM IS EMITTED -----------------------------------
  const surfaces = [
    "HugeFixedBodyBytes", "HugeFixedRecordBytes", "HugeFixedHashLo", "HugeFixedHashHi",
    "HugeFixedLayout", "HugeFixedLayoutBytes", "HugeFixedLayoutCopy", "HugeFixedDst",
    "HugeFixedPrefill", "HugeFixedIdentity", "HugeFixedKnown", "HugeFixedFloor",
    "HugeFixedLineagePlans", "HugeFixedNewPlan", "HugeFixedMeasure", "HugeFixedSave",
    "HugeFixedLoad", "HugeFixedWriteBody", "HugeFixedDecode",
  ];
  const named = surfaces.filter((n) => new RegExp(`\\b${n}\\b`).test(src));
  check(named.length === 0, `no fixed-form surface for Huge in any module${named.length ? ": found " + named.join(", ") : ""}`);
  const exported = Object.keys(mod).filter((k) => surfaces.includes(k));
  check(exported.length === 0, `ProbeTable.js exports no fixed-form surface for Huge${exported.length ? ": " + exported.join(", ") : ""}`);
  // The class itself stays (§3.4: the fixed keyword still buys the by-value storage).
  check(typeof mod.Huge === "function", "Huge keeps its by-value class");

  // ---- 6. NO LINEAGE ENTRY OF HUGE IS PARSED OR EMITTED -----------------------
  for (const e of huge) {
    const lo = "0x" + e.wire.slice(8);
    const hi = "0x" + e.wire.slice(0, 8);
    const b64 = Buffer.from(e.bytes, "hex").toString("base64");
    check(!src.toLowerCase().includes(lo) && !src.toLowerCase().includes(hi),
      `Huge's lineage hash 0x${e.wire} appears in no module`);
    check(!src.includes(b64), "Huge's locked layout bytes appear in no module");

    // ... and that entry, held to the FORM'S rules, would not have parsed: the
    // generator that consulted it is the build #931 failed over a correct lock.
    const bytes = Uint8Array.from(Buffer.from(e.bytes, "hex"));
    const view = new mod.TableFixedLayoutView();
    const ok = mod.TableFixedParseLayout(bytes, 0, bytes.length, view);
    check(!ok && view.refusal === mod.TableFixedRefusal.LayoutRecordTooLarge,
      "Huge's correct lock entry does not parse under the form's rules (layout_record_too_large), so it was never consulted");
  }
  // The control for 6: Small's entry DOES parse under the same rules.
  for (const e of small) {
    const bytes = Uint8Array.from(Buffer.from(e.bytes, "hex"));
    const view = new mod.TableFixedLayoutView();
    check(mod.TableFixedParseLayout(bytes, 0, bytes.length, view),
      "control: Small's lock entry parses under the form's rules");
  }
}
