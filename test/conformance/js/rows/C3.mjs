// CELL js/C3 — THE TEXT LENGTH CLAMP (audit schema#898, matrix schema#876;
// docs/FIXED-FORM-ALGORITHM.md §4.5, the `text` row).
//
// The law, in the spec's own words:
//
//   `text` | `unit := (meta == wide) ? 2 : 1`; `cap := size / unit`;
//   `v := SLE(4, record+src)` clamped into `[0, cap]`, `COUNT clamped` if it
//   fired. Copy the payload, and terminate at the used length where the
//   language stores a terminator — never for `bytes`.
//
// A text length is a SIGNED little-endian word the writer stores beside the
// payload, so a hostile or buggy writer can carry anything — a length past
// the field's own capacity, or a negative one. The reader clamps it into
// `[0, cap]` — cap is THIS reader's own capacity, `size / unit`, in ELEMENTS —
// the record still reads, and the clamp counts ONCE (`COUNT clamped`); §4.5:
// "the reader validates on load in every build, release included, and a clamp
// counts ONCE for the field". §4.5's text row applies to BOTH plans — §4.4
// runs the same loop over the identity plan and a compiled one — so both of
// this leg's clamp sites are asserted here:
//
//   1. THE PRODUCTION ENTRYPOINT, `ChainFixedLoad`
//      (build/tables-generated-js/p1/P1Table.js), the read a caller of the
//      generated unit makes: form byte, hash, layout compare, lineage, then
//      per record `TableFixedRun` (P1Table.js:451) and `ChainFixedDecode`
//      (P1Table.js:452). On the identity path the plan is ONE copy of the
//      whole body, so the clamp lives in the projection `ChainFixedDecode`
//      (build/tables-generated-js/p1/Tblp1Table.js) — `n` over `string(16)`
//      clamped into `[0, 16]`, one `clamped` when it fires.
//
//   2. THE RUN'S TEXT OP, `TableFixedOpText`
//      (build/tables-generated-js/p1/Tblp1Table.js, `case TableFixedOpText`),
//      reached the way this leg's own fixed-form suite reaches the runtime
//      (test/js-tables/fixedform.mjs, `wideTextCodeUnits`): a hand-built plan
//      over hand-built bytes through `TableFixedRun`, the ONE loop identity
//      and compiled both take. This file asserts the NARROW flavour;
//      `wideTextCodeUnits` holds the WIDE one — together they pin
//      `unit := (meta == wide) ? 2 : 1`.
//
// The vector is built with the generated unit's OWN writer (`ChainFixedSave`)
// and the forged length is planted into the record it produced, one field at
// a time — the same instrument this leg's identity-clamp suite uses — so the
// only thing that differs from bytes already proved good is the forgery. The
// framing is the emitter's own, stated where the emitter states it
// (build/tables-generated-js/p1/P1Table.js, `ChainFixedSave` and
// `ChainFixedWriteBody`): a file is the form byte at 0, seven reserved bytes,
// the layout hash at 8, the layout's u32 length at 16 and the layout at 20;
// one record is an eight-byte hash and then the body; Chain's body is `name`
// (length word at body+0, 16 payload bytes at body+4) then `link` nested at
// body+20, whose `tag` carries its length word at body+24.
//
// Run from the repository root, which is the contract:
//
//   node test/conformance/js/rows/C3.mjs
//
// The generated tree is a PATH, not a static import, exactly as the driver
// keeps it (test/conformance/js/main.mjs): the default is the tree
// `make build/tables-generated-js/.stamp` writes, and SCHEMA_JS_GENERATED
// moves it — the same variable the driver reads.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const p1 = await load("p1/P1Table.js");
const home = await load("p1/Tblp1Table.js");

// ---- §3's header, spelled out: a driver that read the offsets out of the
// code under test would agree with whatever that code happened to write.
const LAYOUT_AT = 20;   // form byte 0, reserved 1..7, hash 8..15, length 16..19
const RECORD_BODY = 8;  // a record is the layout's eight-byte hash, then the body
const AT_NAME = 0;      // name string(16): length word at body+0
const AT_LINK = 20;     // link: ChainFixedWriteBody nests Link at body+20
const AT_TAG = AT_LINK + 4; // tag string(8): LinkFixedWriteBody puts its length at link+4

// THE RUN'S CONSTANTS, SPELLED OUT (internal/codegen/jstable/fixedruntime.go,
// exactly as fixedform.mjs's C5 spells them): a plan entry is nine int32
// lanes — op, src, dst, size, aux, guard, arg, meta, argw — TableFixedOpText
// is 2, and the text flavours are UTF-8 (narrow) 1 and wide 2. NoGuard is -1.
const OP_TEXT = 2;
const FLAVOUR_UTF8 = 1;

let failed = 0;
function check(ok, message) {
  if (ok) {
    process.stdout.write("ok - " + message + "\n");
  } else {
    process.stderr.write("FAIL - " + message + "\n");
    failed = 1;
  }
}

// One lawful Chain record, framed as a file of its own, written by the
// generated unit's own writer.
function lawfulFile() {
  const v = new home.Chain();
  // THE PAYLOAD IS SEQUENTIAL so the landed bytes are checkable by value:
  // name carries 0x68..0x77, tag carries "tag12345".
  for (let i = 0; i < 16; i++) { v.Name[i] = 0x68 + i; }
  v.NameLength = 16;
  v.Link.Value = 7;
  v.Link.Tag.set(new Uint8Array([0x74, 0x61, 0x67, 0x31, 0x32, 0x33, 0x34, 0x35]));
  v.Link.TagLength = 8;
  const bytes = new Uint8Array(p1.ChainFixedMeasure(1));
  const wrote = p1.ChainFixedSave([v], 1, bytes);
  if (wrote !== bytes.length) {
    check(false, "C3: the writer framed the file whole (wrote " + wrote + " of " + bytes.length + ")");
    return null;
  }
  return bytes;
}

// The body of record 0 starts past the header, the layout and the record's
// own hash; a forged length is planted at body+`at`, one field at a time.
const bodyAt = LAYOUT_AT + p1.ChainFixedLayoutBytes + RECORD_BODY;

function plant(bytes, at, value) {
  new DataView(bytes.buffer).setInt32(bodyAt + at, value, true);
}

// THE PRODUCTION READ: every caller of the generated unit lands here —
// ChainFixedLoad runs the plan's loop (identity on a file of this build's own
// layout) and then the projection, which is where this path's clamp lives.
function readProduction(bytes) {
  const values = [new home.Chain()];
  const report = new home.TableFixedReport();
  const n = p1.ChainFixedLoad(values, 1, bytes, bytes.length, p1.ChainFixedNewPlan(), report);
  return { n, report, v: values[0] };
}

// ---- THE IDENTITY PATH, THROUGH THE PRODUCTION ENTRYPOINT ------------------

{
  // THE POSITIVE CONTROL FIRST: an untouched record clamps nothing, so a
  // clamp that fired on every read below would read as a pass.
  const bytes = lawfulFile();
  const r = readProduction(bytes);
  check(r.n === 1 && r.report.clamped === 0 && r.report.refused === 0 && !r.report.malformed,
    "C3 IDENTITY: a lawful record of this build's own layout reads and clamps nothing");
  check(r.v.NameLength === 16 && r.v.Link.TagLength === 8 && r.v.Link.Value === 7,
    "C3 IDENTITY: the lawful lengths land as written (name 16, tag 8, value 7)");
}

{
  // THE FORGED LENGTH PAST THE CAP: 9999 is past string(16)'s own capacity,
  // so it lands the reader's own bound 16 — the record still reads, not a
  // refusal — and the clamp counts exactly once.
  const bytes = lawfulFile();
  plant(bytes, AT_NAME, 9999);
  const r = readProduction(bytes);
  check(r.n === 1 && r.report.refused === 0 && !r.report.malformed,
    "C3 IDENTITY: a forged name length past the cap is a clamp and not a refusal — the record still reads");
  check(r.v.NameLength === 16,
    "C3 IDENTITY: a forged name length of 9999 over string(16) lands the reader's own bound 16 (got " + r.v.NameLength + ")");
  check(r.report.clamped === 1,
    "C3 IDENTITY: a forged name length past the cap moves clamped exactly once (got " + r.report.clamped + ")");
  // THE CLAMP IS THE USED LENGTH AND NOTHING ELSE: the payload's copy is
  // whole, so all sixteen planted bytes sit behind the clamped length.
  let whole = true;
  for (let i = 0; i < 16; i++) { whole = whole && r.v.Name[i] === 0x68 + i; }
  check(whole,
    "C3 IDENTITY: the payload lands whole behind the clamped length — the clamp is the USED length, never the copy");
}

{
  // THE NEGATIVE LENGTH: the length word is signed, so a hostile writer can
  // carry -7; the law clamps it to 0 and counts once, never letting a
  // negative length index this reader's storage backwards.
  const bytes = lawfulFile();
  plant(bytes, AT_NAME, -7);
  const r = readProduction(bytes);
  check(r.v.NameLength === 0 && r.report.clamped === 1,
    "C3 IDENTITY: a forged NEGATIVE name length lands 0 with one clamp (got length " + r.v.NameLength + ", clamped " + r.report.clamped + ")");
}

{
  // A LENGTH AT THE BOUND IS NOT A CLAMP: 16 is string(16)'s own cap, so the
  // value stands and the counter does not move — a clamp that fired on a
  // legal length would be a counter nobody could read.
  const bytes = lawfulFile();
  plant(bytes, AT_NAME, 16);
  const r = readProduction(bytes);
  check(r.v.NameLength === 16 && r.report.clamped === 0,
    "C3 IDENTITY: a name length AT the bound 16 is not a clamp and moves no counter (got length " + r.v.NameLength + ", clamped " + r.report.clamped + ")");
}

{
  // THE NESTED FIELD'S OWN CAP: link.tag is string(8), and its clamp is that
  // field's own — one forged length, one clamp, the other field untouched.
  const bytes = lawfulFile();
  plant(bytes, AT_TAG, 9999);
  const r = readProduction(bytes);
  check(r.v.Link.TagLength === 8 && r.report.clamped === 1 && r.v.NameLength === 16,
    "C3 IDENTITY: a forged tag length over the nested string(8) lands that field's own cap 8 with one clamp (got " + r.v.Link.TagLength + ", clamped " + r.report.clamped + ")");
}

// ---- THE RUN'S TEXT OP, THE NARROW FLAVOUR --------------------------------
//
// op=TEXT src=0 dst=0 size=16 aux=4 guard=NONE(-1) arg=0 meta=UTF8(1) argw=1
// — a string(16): the length word at the record's head, 16 bytes of payload
// behind it, and the same twenty bytes in the image. size is the PAYLOAD
// span in bytes, so cap = size / unit is 16 ELEMENTS for the narrow flavour;
// a cap taken in the whole entry's bytes (20) would admit a forged 20 into
// 16 bytes of storage.
const textPlan = new Int32Array([OP_TEXT, 0, 0, 16, 4, -1, 0, FLAVOUR_UTF8, 1]);

function runText(length) {
  const src = new Uint8Array(20);
  const sv = new DataView(src.buffer);
  sv.setInt32(0, length, true);
  for (let i = 0; i < 16; i++) { src[4 + i] = 0x61 + i; }
  const dst = new Uint8Array(20);
  const dv = new DataView(dst.buffer);
  const r = new home.TableFixedReport();
  home.TableFixedRun(textPlan, 1, src, sv, 0, dst, dv, null, r);
  return { length: dv.getInt32(0, true), clamped: r.clamped, payload: dst.subarray(4) };
}

{
  // THE FORGED LENGTH PAST THE CAP, through the run's own text op: 9999 is
  // past 16, so it clamps to 16 and counts once — the same bound the
  // identity path lands, from the other half of §4.4's one loop.
  const got = runText(9999);
  check(got.length === 16 && got.clamped === 1,
    "C3 RUN: a forged length of 9999 over a narrow text entry of cap 16 lands 16 with one clamped (got length " + got.length + ", clamped " + got.clamped + ")");
  let whole = true;
  for (let i = 0; i < 16; i++) { whole = whole && got.payload[i] === 0x61 + i; }
  check(whole,
    "C3 RUN: the payload lands whole behind the clamped length, at the aux lane the entry names");
}

{
  // THE NEGATIVE LENGTH through the op: the word is read through a signed
  // view, so -1 is negative, not a cap of four billion (0xFFFFFFFF).
  const got = runText(-1);
  check(got.length === 0 && got.clamped === 1,
    "C3 RUN: a forged NEGATIVE length lands 0 with one clamp — the word is signed, never 2^32-1 (got length " + got.length + ", clamped " + got.clamped + ")");
}

{
  // AT THE BOUND the op counts nothing: 16 is the entry's own cap.
  const got = runText(16);
  check(got.length === 16 && got.clamped === 0,
    "C3 RUN: a length AT the bound 16 is not a clamp and moves no counter (got length " + got.length + ", clamped " + got.clamped + ")");
}

process.exit(failed);
