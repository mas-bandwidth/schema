// CELL js/C3 — THE TEXT LENGTH BOUND (audit schema#898, matrix schema#876;
// docs/FIXED-FORM-ALGORITHM.md §4.5, the `text` row).
//
// The law, in the spec's own words (§4.5, the `text` row, line 449):
//
//   `text` | `v := SLE(4, record+src)`; if `v < 0` or `v > size` then
//   `REFUSE length_over_bound` — before any destination byte is written.
//
// This row was written when the law CLAMPED a forged length into `[0, cap]`
// and counted it. The ruling of 2026-10-16 (4edb98c8a, "a count or length
// past its bound refuses by name") retired the clamp: a forged length is not
// one this writer could have written, so it REFUSES BY NAME and lands nothing,
// and the caller's rows come back at their declared defaults (ruling R4,
// §5.3's refusal table). Every case below is the old row's case, asserted
// against the law as it stands; none is dropped.
//
// A text length is a SIGNED little-endian word, so a hostile writer can carry
// a length past the field's capacity or a negative one. Both are refusals. A
// length AT the bound is lawful and reads. §4.5's text row applies to BOTH
// plans (§4.4 runs the same loop over the identity plan and a compiled one),
// so both of this leg's sites are asserted:
//
//   1. THE PRODUCTION ENTRYPOINT, `ChainFixedLoad`
//      (build/tables-generated-js/p1/P1Table.js): form byte, hash, layout
//      compare, lineage, then the CHECK pass over every record before any
//      record lands (§5.3 step 11), where `ChainFixedCheckRead` holds each
//      text length to its bound.
//
//   2. THE RUN'S TEXT OP, `TableFixedOpText`
//      (build/tables-generated-js/p1/Tblp1Table.js, `case TableFixedOpText`),
//      reached through `TableFixedRun` with a hand-built plan over hand-built
//      bytes, the ONE loop identity and compiled both take.
//
// The vector is built with the generated unit's OWN writer (`ChainFixedSave`)
// and the forged length is planted into the record it produced, one field at
// a time. The framing: a file is the form byte at 0, seven reserved bytes, the
// layout hash at 8, the layout's u32 length at 16 and the layout at 20; one
// record is an eight-byte hash and then the body; Chain's body is `name`
// (length word at body+0, 16 payload bytes at body+4) then `link` nested at
// body+20, whose `tag` carries its length word at body+24.
//
// Run from the repository root:  node test/conformance/js/rows/C3.mjs
// SCHEMA_JS_GENERATED moves the generated tree, as the driver's does.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const p1 = await load("p1/P1Table.js");
const home = await load("p1/Tblp1Table.js");

// ---- §3's header, spelled out.
const RECORD_BODY = 8;  // a record is the layout's eight-byte hash, then the body
const AT_NAME = 0;      // name string(16): length word at body+0
const AT_LINK = 20;     // link: ChainFixedWriteBody nests Link at body+20
const AT_TAG = AT_LINK + 4; // tag string(8): its length word at link+4

// THE RUN'S CONSTANTS, SPELLED OUT (internal/codegen/jstable/fixedruntime.go):
// a plan entry is nine int32 lanes — op, src, dst, size, aux, guard, arg,
// meta, argw — TableFixedOpText is 2, the UTF-8 flavour is 1, NoGuard is -1.
const OP_TEXT = 2;
const FLAVOUR_UTF8 = 1;
const LENGTH_OVER_BOUND = home.TableFixedRefusal.LengthOverBound;

let failed = 0;
function check(ok, message) {
  if (ok) {
    process.stdout.write("ok - " + message + "\n");
  } else {
    process.stderr.write("FAIL - " + message + "\n");
    failed = 1;
  }
}

check(LENGTH_OVER_BOUND === 21 && home.TableFixedRefusalName(LENGTH_OVER_BOUND) === "length_over_bound",
  "C3: the refusal this row expects is length_over_bound (21)");

// One lawful Chain record, framed as a file of its own, written by the
// generated unit's own writer.
function lawfulFile() {
  const v = new home.Chain();
  for (let i = 0; i < 16; i++) { v.name[i] = 0x68 + i; }
  v.nameLength = 16;
  v.link.value = 7;
  v.link.tag.set(new Uint8Array([0x74, 0x61, 0x67, 0x31, 0x32, 0x33, 0x34, 0x35]));
  v.link.tagLength = 8;
  const bytes = new Uint8Array(p1.ChainFixedMeasure(1));
  const wrote = p1.ChainFixedSave([v], 1, bytes);
  if (wrote !== bytes.length) {
    check(false, "C3: the writer framed the file whole (wrote " + wrote + " of " + bytes.length + ")");
    process.exit(1);
  }
  return bytes;
}

const bodyAt = home.TableFixedHeaderBytes + home.TableFixedLayoutHeaderBytes + p1.ChainFixedLayoutBytes + RECORD_BODY;

function plant(bytes, at, value) {
  new DataView(bytes.buffer).setInt32(bodyAt + at, value, true);
}

// THE PRODUCTION READ. The destination is POISONED first, so a refusal that
// failed to reset it (ruling R4) or a read that landed wire bytes shows.
function poisoned() {
  const v = new home.Chain();
  v.name.fill(0x5a);
  v.nameLength = 0x5a5a;
  v.link.value = 0x5a5a;
  v.link.tag.fill(0x5a);
  v.link.tagLength = 0x5a5a;
  return v;
}

function atDefaults(v) {
  const d = new home.Chain();
  home.InitChain(d);
  return JSON.stringify([Array.from(v.name), v.nameLength, v.link.value, Array.from(v.link.tag), v.link.tagLength]) ===
    JSON.stringify([Array.from(d.name), d.nameLength, d.link.value, Array.from(d.link.tag), d.link.tagLength]);
}

function readProduction(bytes) {
  const values = [poisoned()];
  const report = new home.TableFixedReport();
  const n = p1.ChainFixedLoad(values, 1, bytes, bytes.length, p1.ChainFixedNewPlan(), report);
  return { n, report, v: values[0] };
}

const quiet = (r) => r.oor === 0 && r.widened === 0 && r.unknown === 0 && r.mismatch === 0 && r.repeated === 0;

// ---- THE IDENTITY PATH, THROUGH THE PRODUCTION ENTRYPOINT ------------------

{
  // THE POSITIVE CONTROL FIRST: an untouched record reads, so a refusal that
  // fired on every read below would not read as a pass.
  const r = readProduction(lawfulFile());
  check(r.n === 1 && r.report.refused === 0 && !r.report.damaged && quiet(r.report),
    "C3 IDENTITY: a lawful record of this build's own layout reads and moves no counter");
  check(r.v.nameLength === 16 && r.v.link.tagLength === 8 && r.v.link.value === 7,
    "C3 IDENTITY: the lawful lengths land as written (name 16, tag 8, value 7)");
}

function refusesWhole(at, value, what) {
  const bytes = lawfulFile();
  plant(bytes, at, value);
  const r = readProduction(bytes);
  check(r.n === -1 && r.report.refused === LENGTH_OVER_BOUND && !r.report.damaged,
    "C3 IDENTITY: " + what + " refuses length_over_bound, never a clamp (n=" + r.n + ", refused=" +
    home.TableFixedRefusalName(r.report.refused) + ")");
  check(quiet(r.report),
    "C3 IDENTITY: " + what + " moves no counter — a refusal counts nothing (clamped " + r.report.oor + ")");
  check(atDefaults(r.v),
    "C3 IDENTITY: " + what + " lands not one byte; the row handed in holds its declared defaults (ruling R4)");
}

// THE FORGED LENGTH PAST THE CAP: 9999 over string(16).
refusesWhole(AT_NAME, 9999, "a forged name length of 9999 over string(16)");
// ONE PAST THE BOUND: 17 is the first length the cap does not admit.
refusesWhole(AT_NAME, 17, "a forged name length of 17, one past string(16)'s bound");
// THE NEGATIVE LENGTH: the word is signed, so -7 is negative, never 2^32-7.
refusesWhole(AT_NAME, -7, "a forged NEGATIVE name length (-7)");
// THE NESTED FIELD'S OWN CAP: link.tag is string(8), so 9 past it refuses
// though it is well inside name's 16 — the bound is the field's own.
refusesWhole(AT_TAG, 9, "a forged tag length of 9 over the nested string(8)");

{
  // A LENGTH AT THE BOUND IS LAWFUL: 16 is string(16)'s own cap, so the value
  // stands, nothing refuses and no counter moves.
  const bytes = lawfulFile();
  plant(bytes, AT_NAME, 16);
  const r = readProduction(bytes);
  check(r.n === 1 && r.report.refused === 0 && r.v.nameLength === 16 && quiet(r.report),
    "C3 IDENTITY: a name length AT the bound 16 reads and moves no counter (n=" + r.n + ", length " + r.v.nameLength + ")");
  let whole = true;
  for (let i = 0; i < 16; i++) { whole = whole && r.v.name[i] === 0x68 + i; }
  check(whole, "C3 IDENTITY: at the bound the whole payload lands");
}

{
  // A SHORTER LAWFUL LENGTH reads too, and the bytes past it on the wire are
  // DEAD: a writer zeroes them, so this vector zeroes them before it plants.
  const bytes = lawfulFile();
  plant(bytes, AT_NAME, 3);
  for (let i = 3; i < 16; i++) { bytes[bodyAt + AT_NAME + 4 + i] = 0; }
  const r = readProduction(bytes);
  check(r.n === 1 && r.report.refused === 0 && r.v.nameLength === 3 &&
    r.v.name[0] === 0x68 && r.v.name[1] === 0x69 && r.v.name[2] === 0x6a,
    "C3 IDENTITY: a lawful shorter length (3) lands as written (n=" + r.n + ", length " + r.v.nameLength + ")");
}

// ---- THE RUN'S TEXT OP ------------------------------------------------------
//
// op=TEXT src=0 dst=0 size=16 aux=4 guard=NONE(-1) arg=0 meta=UTF8(1) argw=1
// — a string(16): the length word at the record's head, 16 bytes of payload
// behind it, and the same twenty bytes in the image. size is the PAYLOAD
// span, so the bound is 16; a bound taken over the whole entry (20) would
// admit a forged 20 into 16 bytes of storage.
const textPlan = new Int32Array([OP_TEXT, 0, 0, 16, 4, -1, 0, FLAVOUR_UTF8, 1]);

function runText(length) {
  const src = new Uint8Array(20);
  const sv = new DataView(src.buffer);
  sv.setInt32(0, length, true);
  for (let i = 0; i < 16; i++) { src[4 + i] = 0x61 + i; }
  // THE IMAGE IS POISONED, so a refusal that wrote a byte shows.
  const dst = new Uint8Array(20).fill(0x5a);
  const dv = new DataView(dst.buffer);
  const r = new home.TableFixedReport();
  home.TableFixedRun(textPlan, 1, src, sv, 0, dst, dv, null, r);
  return { refused: r.refused, oor: r.oor, length: dv.getInt32(0, true), dst };
}

function runRefuses(length, what) {
  const got = runText(length);
  check(got.refused === LENGTH_OVER_BOUND && got.oor === 0,
    "C3 RUN: " + what + " refuses length_over_bound and counts nothing (refused=" +
    home.TableFixedRefusalName(got.refused) + ", clamped " + got.oor + ")");
  check(got.dst.every((b) => b === 0x5a),
    "C3 RUN: " + what + " writes not one destination byte (§4.5: before any destination byte is written)");
}

runRefuses(9999, "a forged length of 9999 over a text entry of bound 16");
runRefuses(17, "a forged length of 17, one past the bound 16");
runRefuses(-1, "a forged NEGATIVE length (-1) — the word is signed, never 2^32-1");

{
  // AT THE BOUND the op lands the length and the whole payload at aux.
  const got = runText(16);
  let whole = true;
  for (let i = 0; i < 16; i++) { whole = whole && got.dst[4 + i] === 0x61 + i; }
  check(got.refused === 0 && got.oor === 0 && got.length === 16 && whole,
    "C3 RUN: a length AT the bound 16 lands, payload whole at the aux lane, no counter moved (refused=" +
    got.refused + ", length " + got.length + ")");
}

process.exit(failed);
