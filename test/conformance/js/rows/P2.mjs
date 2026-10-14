// test/conformance/js/rows/P2.mjs — js/P2 "write-read-write byte-identical"
// (docs/roadmap.sexp:3855; docs/FIXED-FORM-ALGORITHM.md:1682 §7 proof 1;
// docs/SPEC-TABLES.md:6120 §3.3 round trip).
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:1682:
//   "Read a file and save it back; the bytes must be identical — a byte a port
//    encodes differently is a byte that does not come back."
// and docs/SPEC-TABLES.md:6120:
//   "The round trip across the forms. … Red if one byte differs in either
//    direction, which is the negative control on every rule here that says the
//    VALUE does not move."
//
// THE PRODUCTION PATH. The fixed form's generated JS: this file calls the SAME
// emitter output the conformance driver reaches through SCHEMA_JS_GENERATED —
// build/tables-generated-js/p1/P1Table.js — at its fixed-form half:
//
//   ChainFixedMeasure -> ChainFixedSave -> ChainFixedLoad -> ChainFixedSave
//
// The test drives Write -> Read -> Write over a real value and requires the two
// writes to be byte-identical, and requires the decoded values to match the
// values written — so the identity is not the identity of two empty buffers.
//
// THE VECTOR. P1 is the pre-pointer side of the pointer evolution pair: a
// string(16) and a nested fixed table Link { int32 value | min = 0, max = 1000;
// string(8) tag }. The vector is CONSTRUCTED here and exercises the two slack
// rules the round trip is about: a name at its exact bound beside one short of
// it, and a non-default value beside a default one, across two records.

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const p1 = await import(pathToFileURL(join(generated, "p1/P1Table.js")).href);
const home = await import(pathToFileURL(join(generated, "p1/Tblp1Table.js")).href);

const {
  InitChain, Chain, InitLink, Link,
  TableFixedReport, TableFixedRefusal,
} = home;

const {
  ChainFixedMeasure, ChainFixedSave, ChainFixedLoad, ChainFixedNewPlan,
  ChainFixedBodyBytes, ChainFixedRecordBytes,
} = p1;

let failures = 0;

function check(ok, msg) {
  console.log(ok ? `PASS: ${msg}` : `FAIL: ${msg}`);
  if (!ok) { failures++; }
}

// BYTE-LEVEL COMPARISON: name the first difference.
function sameBytes(got, want, what) {
  if (got.length !== want.length) {
    check(false, `${what}: ${got.length} bytes, first write was ${want.length}`);
    return false;
  }
  for (let i = 0; i < want.length; i++) {
    if (got[i] !== want[i]) {
      check(false, `${what}: first byte differs at offset ${i} (second write ${got[i]}, first write ${want[i]})`);
      return false;
    }
  }
  return true;
}

function putText(buf, str) {
  for (let i = 0; i < str.length; i++) { buf[i] = str.charCodeAt(i); }
}

// THE TWO RECORDS. Record 0 spends the string(16) to its exact bound and
// carries link.value = 7 with a non-empty tag; record 1 is short of the bound,
// carries the declared default (0), and has an empty tag.
function makeValues() {
  const full = new Chain();
  InitChain(full);
  putText(full.Name, "write-read-write"); // 16 bytes, the bound exactly
  full.NameLength = 16;
  full.Link.Value = 7;
  putText(full.Link.Tag, "p2-row");
  full.Link.TagLength = 6;

  const short = new Chain();
  InitChain(short);
  putText(short.Name, "p2");
  short.NameLength = 2;
  short.Link.Value = 0; // the declared default
  short.Link.TagLength = 0;

  return [full, short];
}

function write(values) {
  const need = ChainFixedMeasure(values.length);
  const bytes = new Uint8Array(need);
  const n = ChainFixedSave(values, values.length, bytes);
  check(n === need, `write: ChainFixedSave answers the measured ${need} bytes (got ${n})`);
  return bytes;
}

function read(bytes, count) {
  const values = Array.from({ length: count }, () => { const v = new Chain(); InitChain(v); return v; });
  const report = new TableFixedReport();
  const plan = ChainFixedNewPlan(4096, 4096);
  const n = ChainFixedLoad(values, count, bytes, bytes.length, plan, report);
  check(n === count, `read: ChainFixedLoad answers ${count} records (got ${n})`);
  check(!report.malformed && report.refused === TableFixedRefusal.None,
    "read: a clean read moves no refusal");
  return values;
}

// ---- THE ROUND TRIP ----
//
// Values are written, the bytes are read into fresh values, those values are
// written again. The second write must reproduce the first byte for byte, and
// the values read must match — so a reader that drops a byte or a writer that
// reorders one is caught even where the two agree.

const written = makeValues();
const first = write(written);

const back = read(first, written.length);

// Verify the values moved correctly.
check(back[0].NameLength === 16, "record 0 NameLength = 16");
check(back[0].Link.Value === 7, "record 0 Link.Value did not move");
check(back[0].Link.TagLength === 6, "record 0 Link.TagLength = 6");
check(back[1].NameLength === 2, "record 1 NameLength = 2");
check(back[1].Link.Value === 0, "record 1 Link.Value = default 0");
check(back[1].Link.TagLength === 0, "record 1 Link.TagLength = 0 (empty tag)");

// Slack behind a short name must be zero (the template's zeros).
let slackZero = true;
for (let i = back[1].NameLength; i < back[1].Name.length; i++) {
  if (back[1].Name[i] !== 0) { slackZero = false; break; }
}
check(slackZero, "record 1: slack bytes behind Name are zero");

// THE LAW: second write matches the first.
const second = write(back);
sameBytes(second, first, "P2 write-read-write byte-identical");

if (failures === 0) {
  console.log("");
  console.log("P2: write-read-write byte-identical — GREEN");
}
process.exit(failures > 0 ? 1 : 0);