// THE FIXED FORM'S JAVASCRIPT LEG (docs/SPEC-TABLES.md §3.4), form byte 3.
//
// Three things are held here and the first two are the whole of the form:
//
//   1. THE BYTES ARE THE C++ REFERENCE'S. The reference writes the paired
//      bench's own sixty-four logical records with its form-3 writer; this
//      leg reads that file, and writes it back, and the bytes must be
//      IDENTICAL. The block and its fnv1a64 hash are checked against the same
//      file, because two writers whose blocks agree byte for byte agree on
//      every id, kind, size and position in the closure — and if they do not,
//      nothing else in this file means anything.
//
//   2. THE DECODED VALUES ARE CHECKED INDEPENDENTLY. A reader and a writer
//      that share one offset mistake round trip perfectly and are both wrong,
//      so the reference states the values too, and every field of every record
//      is compared against them.
//
//   3. THE STRICT READ OF THIS BUILD'S OWN LAYOUT: the form header, the
//      per-record hash, the live counts and the hostile bytes, each refused by
//      name and never damage. The VERSIONING conformance is not here: a fixed
//      table reads backward and never forward (docs/FIXED-FORM-ALGORITHM.md
//      §5), and its rows live on the lineage harness
//      (internal/codegen/jstable/fixedversioning_test.go and siblings), where
//      the older layout is handed to the reader as its locked entry.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

// §3'S HEADER, ONE RULE FOR ALL FIVE FORMS: the form byte at 0, seven reserved
// bytes that are zero, the LAYOUT HASH at 8, and the body at 16. The fixed
// form's body opens with the layout behind its own u32 length, so the layout
// itself starts at 20 and the records follow it.
//
// These are SPELLED OUT rather than imported from the generated module on
// purpose: a driver that read the offsets out of the code under test would
// agree with whatever header that code happened to write, which is the one
// thing this file exists to refuse.
const HEADER_BYTES = 16;
const HASH_AT = 8;
const LAYOUT_LENGTH_AT = 16;
const LAYOUT_AT = 20;

//   4. THE OPTIONALS, whose bytes are the reference's too. `?T` is the ONE
//      place this form departs from §2.3 — a present byte in front of a
//      payload that rides whole — so the flag's POSITION is a wire fact, and a
//      port that placed it one byte off would round trip its own records
//      perfectly and disagree with every peer. The reference writes the corpus
//      (test/js-tables/fixedoptional_corpus.cpp) at every place a flag can
//      ride, this leg matches it byte for byte, reads it back, and lands the
//      same records through a plan compiled from the OTHER generation's block.
//
//   node test/js-tables/fixedform.mjs <generated> <corpus> [optional-corpus]
//
// It is its own driver rather than a mode of main.mjs because it shares nothing
// with that leg: main.mjs opens the two ACCELERATORS, which this form has no
// symbol of, and a unit carrying the fixed form need not have a block at all.
//
// Run from the repository root, which is where the Makefile runs it.

// WHAT §5.6 RETIRED IS NOT HERE. Every case that read a file written under
// ANOTHER schema's layout without that layout in the reader's lineage — a plan
// compiled from a file's bytes, the seven §1.1 names fired at read time, a cache
// keyed by hash, a forward read — is gone from this driver: under §5 every one of
// those files comes back layout_unknown, and a test that does not run must not
// exist (docs/rulings.json, no-skips). Their coverage is held where §5 puts it:
// the lineage harness (internal/codegen/jstable/fixedversioning_test.go,
// fixedversioning_retired_controls_test.go, fixedroadmap_t01_test.go), where the
// peer's entry is handed to the reader as its lock, and §1.1's seven rules as the
// LOCK's validation of what it records (internal/lockfile/layout_test.go). The
// IDENTITY halves of the three mixed functions — referenceOracleIdentity,
// holePrefillIdentity and bytesArrayConventionIdentity — run here, because §5
// does not touch this build's own layout.
export async function checkFixedForm(check, generated, corpusDir, optionalDir, oracleDir) {
  const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

  const bench = await load("bench/BenchTable.js");
  const fixed = await load("bench/FixedTableTable.js");
  const fx1 = await load("fx1/FX1Table.js");
  const fx1home = await load("fx1/Tblfx1Table.js");
  const ut1 = await load("ut1/UT1Table.js");
  const ut1home = await load("ut1/Tblut1Table.js");

  await pairedCorpus(check, bench, fixed, corpusDir);
  forgedCounts(check, bench, fixed, corpusDir);
  duplicateNeverRaised(check, bench, fixed);
  fixedPointShiftBits(check, bench, fixed);
  formHeaderProbes(check, fx1, fx1home);
  refuseTotalProbe(check, fx1, fx1home);
  guardComparedAtArgW(check, fx1home);
  planPartition(check, ut1, ut1home, fx1, fx1home);
  ordinalWidth8ReadWhole(check, fx1home);
  bytesArrayConventionIdentity(check, fx1, fx1home);
  liveCountSlack(check, fx1, fx1home);
  hostileByteSweep(check, fx1, fx1home);
  holePrefillIdentity(check, fx1, fx1home);
  if (oracleDir) {
    referenceOracleIdentity(check, fx1, fx1home, oracleDir);
  }

  if (optionalDir) {
    const o = {
      p1: await load("p1/P1Table.js"), p1home: await load("p1/Tblp1Table.js"),
      p3: await load("p3/P3Table.js"), p3home: await load("p3/Tblp3Table.js"),
      fo1: await load("fo1/FO1Table.js"), fo1home: await load("fo1/Tblfo1Table.js"),
    };
    optionalBytes(check, o, optionalDir);
    rangedScalarClamp(check, o);
  }
}

// ---------------------------------------------------------------------------
// THE STRICT READ'S TWO HELPERS: poison a destination, and compare it with a
// freshly constructed one
// ---------------------------------------------------------------------------
//
// Poison every destination field so a refusal's required untouched behavior
// (§5.3) is visible even when a reset would restore construction defaults.
function poisonDeep(v) {
  if (v === null || typeof v !== "object") { return; }
  if (ArrayBuffer.isView(v)) {
    if (v instanceof BigInt64Array || v instanceof BigUint64Array) { v.fill(0x5An); } else { v.fill(0x5A); }
    return;
  }
  for (const k of Object.keys(v)) {
    const x = v[k];
    if (typeof x === "number") { v[k] = x === 0x5A ? 0x5B : 0x5A; }
    else if (typeof x === "boolean") { v[k] = !x; }
    else if (typeof x === "bigint") { v[k] = x === 0x5An ? 0x5Bn : 0x5An; }
    else if (typeof x === "object") { poisonDeep(x); }
  }
}

// a union value: a numeric `type` beside one object per arm
function isUnion(v) {
  const keys = Object.keys(v);
  return keys.includes("type") && typeof v.type === "number" && keys.length > 1 &&
    keys.every((k) => k === "type" || (v[k] !== null && typeof v[k] === "object" && !ArrayBuffer.isView(v[k])));
}

// the first path at which a and b differ, or null when they are the same. A
// union at None (b.type === 0) compares its tag only: Init sets the tag to None
// and its dormant arms keep their storage, by the generated Init's own rule.
function deepDiff(a, b, path = "") {
  if (a !== null && b !== null && typeof a === "object" && typeof b === "object" &&
      !ArrayBuffer.isView(b) && isUnion(b) && b.type === 0) {
    return a.type === 0 ? null : `${path}.type (${a.type} vs 0)`;
  }
  if (typeof a !== typeof b) { return `${path} (${typeof a} vs ${typeof b})`; }
  if (a === null || typeof a !== "object") {
    return Object.is(a, b) ? null : `${path} (${a} vs ${b})`;
  }
  if (ArrayBuffer.isView(a)) {
    if (!ArrayBuffer.isView(b) || a.length !== b.length) { return `${path} (length)`; }
    for (let i = 0; i < a.length; i++) { if (!Object.is(a[i], b[i])) { return `${path}[${i}] (${a[i]} vs ${b[i]})`; } }
    return null;
  }
  const ka = Object.keys(a), kb = Object.keys(b);
  if (ka.length !== kb.length) { return `${path} (keys)`; }
  for (const k of ka) {
    const d = deepDiff(a[k], b[k], `${path}.${k}`);
    if (d !== null) { return d; }
  }
  return null;
}

// ---------------------------------------------------------------------------
// THE IDENTITY PATH REFUSES TOO: a forged count or length never reaches the
// consumer
// ---------------------------------------------------------------------------
//
// THE READ SIDE ALWAYS CHECKS, and "every read path" includes the IDENTITY
// one. A record whose block hashes to this build's own is read as ONE OP_COPY
// of the whole body, but the hash is a WIRE IDENTITY and not a security claim:
// a record carrying this build's own hash still arrives from a writer this
// reader cannot vouch for, and the count or length it carries is the number
// the consumer will index this reader's own storage by.
//
// So the decode checks, on BOTH paths, against THIS reader's bound, and the
// strict read (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.4) REFUSES a value past
// it by name — count_over_bound or length_over_bound — never clamps, hands
// every destination back at its declared default (R4), and moves no counter. Below, each forged
// value is planted in a record of the reference's own corpus, one at a time.
function forgedCounts(check, bench, fixed, corpusDir) {
  const corpus = new Uint8Array(readFileSync(resolve(corpusDir, "bench_fixed.bin")));
  const head = LAYOUT_AT + fixed.FixedTableFixedLayoutBytes;
  const R = bench.TableFixedRefusal;

  // ONE record of the reference's corpus, framed as a file of its own, so the
  // forged byte is the only thing that differs from bytes already proved good.
  const oneRecord = () => Uint8Array.from(corpus.subarray(0, head + fixed.FixedTableFixedRecordBytes));
  const BODY = head + 8; // the first record's body, past its hash

  // BenchMixed's body offsets, the same constants the emitter lays down
  const AT_ENTITIES = 52;   // entities [1..8]MixedEntity
  const AT_STATS = 464;     // stats    [..80]MixedStat
  const AT_NAME = 1126;     // player_name string(15)
  const AT_PAYLOAD = 1145;  // payload     bytes(16)
  const AT_HAS_EXTRA = 1227; // has_extra bool

  // The destination is poisoned, so a reset cannot masquerade as untouched.
  const read = (bytes) => {
    const values = [new bench.FixedTable()];
    poisonDeep(values[0]);
    const before = structuredClone(values[0]);
    bench.InitFixedTable(before); // R4: a refusal hands the row back at its declared default
    const report = new bench.TableFixedReport();
    const n = fixed.FixedTableFixedLoad(values, 1, bytes, bytes.length,
      fixed.FixedTableFixedNewPlan(), report);
    return { n, report, v: values[0].value, whole: values[0], before };
  };
  const refuses = (what, bytes, reason) => {
    const r = read(bytes);
    check(r.n === -1 && r.report.refused === reason,
      `IDENTITY REFUSE: ${what} refuses ${bench.TableFixedRefusalName(reason)} (n=${r.n} refused=${bench.TableFixedRefusalName(r.report.refused)})`);
    check(r.report.oor === 0 && r.report.widened === 0 && r.report.unknown === 0 && r.report.mismatch === 0,
      `IDENTITY REFUSE: ${what} moves no counter (oor=${r.report.oor} widened=${r.report.widened})`);
    const d = deepDiff(r.whole, r.before);
    check(d === null, `IDENTITY REFUSE: ${what} hands the destination back at its declared default (differs at ${d})`);
    return r;
  };

  // THE POSITIVE CONTROL FIRST: an untouched record reads clean. Without it a
  // refusal that fired on every record would read as a pass below.
  {
    const r = read(oneRecord());
    check(r.n === 1 && r.report.refused === 0 && r.report.oor === 0,
      `IDENTITY REFUSE: an untouched record of the reference's corpus reads clean (n=${r.n} refused=${r.report.refused})`);
    // and the record is NOT its defaults, so the reset checks below are not vacuous
    check(deepDiff(r.whole, new bench.FixedTable()) !== null,
      "IDENTITY REFUSE: the corpus record differs from a fresh one, so a reset is visible");
  }

  const forged = [
    ["a forged entities count of 9999", AT_ENTITIES, 9999, R.CountOverBound],
    ["a forged entities count of -1", AT_ENTITIES, -1, R.CountOverBound],
    ["a forged stats count of 2147483647", AT_STATS, 0x7fffffff, R.CountOverBound],
    ["a forged player_name length of 9999", AT_NAME, 9999, R.LengthOverBound],
    ["a forged payload length of 9999", AT_PAYLOAD, 9999, R.LengthOverBound],
  ];
  for (const [what, at, plant, reason] of forged) {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + at, plant, true);
    // the content rule checks a text's used units up to its bound and may be
    // met first (§5.4: the first refusal met is the read's), so the name's
    // bound is filled with lawful units: the length bound is what fires here
    if (at === AT_NAME) { bytes.fill(0x61, BODY + AT_NAME + 4, BODY + AT_NAME + 4 + 15); }
    refuses(what, bytes, reason);
  }

  // §4.5 fix 11: a player_name length IN bound over the writer's zero bytes
  // fails the content check over those used units; the refusal is
  // text_ill_formed, and the content rule — not the bound — is what fires.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + AT_NAME, 15, true);
    bytes.fill(0, BODY + AT_NAME + 4, BODY + AT_NAME + 4 + 15);
    refuses("an in-bound player_name length of 15 over zero bytes", bytes, R.TextIllFormed);
  }

  // A COUNT OR LENGTH AT THE BOUND IS LAWFUL, and reads with nothing moved: a
  // check that refused a legal value would be a refusal nobody could avoid.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + AT_ENTITIES, 8, true);
    const r = read(bytes);
    check(r.n === 1 && r.v.entitiesCount === 8 && r.report.refused === 0 && r.report.oor === 0,
      `IDENTITY REFUSE: a count AT the bound reads and moves no counter (n=${r.n} refused=${r.report.refused})`);
  }
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + AT_NAME, 15, true);
    bytes.fill(0x61, BODY + AT_NAME + 4, BODY + AT_NAME + 4 + 15);
    const r = read(bytes);
    check(r.n === 1 && r.v.playerNameLength === 15 && r.report.refused === 0 && r.report.oor === 0,
      `IDENTITY REFUSE: a player_name length AT the bound reads and moves no counter (n=${r.n} refused=${r.report.refused})`);
  }

  // A BOOL BYTE IS 0 OR 1 and nothing else: a 2 is wire damage and refuses
  // wire_damaged (the strict rule), while 1 reads true.
  {
    const bytes = oneRecord();
    bytes[BODY + AT_HAS_EXTRA] = 2;
    refuses("a bool byte of 2", bytes, R.WireDamaged);
  }
  {
    const bytes = oneRecord();
    bytes[BODY + AT_HAS_EXTRA] = 1;
    const r = read(bytes);
    check(r.n === 1 && r.v.hasExtra === true && r.report.refused === 0 && r.report.oor === 0,
      `IDENTITY REFUSE: a bool byte of 1 reads true and moves no counter (n=${r.n} refused=${r.report.refused})`);
  }
}

// ---------------------------------------------------------------------------
// E9: `repeated` NEVER RAISED
// ---------------------------------------------------------------------------
//
// The fixed wire never raises `repeated` (the counter the text form's
// repeated map key moves; SPEC-TABLES.md §16.2): "a pointer, map or unbounded
// array" in a fixed table's closure "is a compile refusal" (§5.5). THE REPORT
// HAS THE MEMBER, because a caller has one report type and not two, so E9 owes
// an assertion BY NAME. This case holds the counter at zero on this build's
// own record read back, and again on a record the read REFUSES, so it cannot
// pass by reading a report that is silent for every reason.
export function duplicateNeverRaised(check, bench, fixed) {
  const count = 1;
  const out = new Uint8Array(fixed.FixedTableFixedMeasure(count));
  const values = [new bench.FixedTable()];
  check(fixed.FixedTableFixedSave(values, count, out) === out.length,
    "E9 repeated: the fixture's own record saves to a full file");

  // the first record's body, past the file header, the layout and its hash, and
  // the entities offset forgedCounts already plants counts at
  const body = LAYOUT_AT + fixed.FixedTableFixedLayoutBytes + 8;
  const atEntities = 52; // entities [1..8]MixedEntity

  const read = (bytes) => {
    const v = [new bench.FixedTable()];
    const report = new bench.TableFixedReport();
    const n = fixed.FixedTableFixedLoad(v, 1, bytes, bytes.length,
      fixed.FixedTableFixedNewPlan(), report);
    return { n, report };
  };

  // THE CLEAN READ: the form's own writer, read back, raises no repeated.
  {
    const r = read(out);
    check(r.n === 1 && r.report.refused === 0 && r.report.repeated === 0,
      `E9 repeated: a clean fixed read raises no repeated (n ${r.n}, repeated ${r.report.repeated})`);
  }

  // NOT VACUOUS: the SAME member on a read that answers something else — a
  // forged count refuses count_over_bound — with `repeated` still at zero.
  {
    const forged = Uint8Array.from(out);
    new DataView(forged.buffer).setInt32(body + atEntities, 9999, true);
    const r = read(forged);
    check(r.n === -1 && r.report.refused === bench.TableFixedRefusal.CountOverBound && r.report.repeated === 0,
      `E9 repeated: a forged count refuses count_over_bound and NOT repeated (refused ${r.report.refused}, repeated ${r.report.repeated})`);
  }
}

// ---------------------------------------------------------------------------
// C8 — A FIXED-POINT FIELD'S BOUND IS IN VALUE UNITS AND ITS STORAGE IS RAW;
// A bits(N) IS BOUNDED BY 2^N - 1
// ---------------------------------------------------------------------------
//
// docs/FIXED-FORM-ALGORITHM.md §4.6: a fixed-point field's bounds are in VALUE
// UNITS and its storage is raw, so both ends are shifted by F first; a bits(N)
// is bounded by 2^N - 1. Under the strict read (§4.6, §5.4) a raw past either
// bound REFUSES out_of_range, hands the destination back at its declared default, and moves no
// counter; a raw AT the bound reads.
//
// The record is written by THIS BUILD's own writer, so the reader takes the
// identity plan, and the forged raw is the only thing that differs from bytes
// already known good. The bounds pass runs on BOTH paths
// (internal/codegen/jstable/fixedjs.go `emitDecodeBounds`), which is why the
// identity path is the honest place to pin it.
const C8_SERVER_TIME = 48;   // BenchMixed.server_time fixed(24,8) | max 65535
const C8_PING = 1221;        // BenchMixed.ping ufixed(8,8)        | max 250
const C8_CRC_HINT = 1223;    // BenchMixed.crc_hint bits(24)

export function fixedPointShiftBits(check, bench, fixed) {
  const head = LAYOUT_AT + fixed.FixedTableFixedLayoutBytes;
  const BODY = head + 8; // the first (only) record's body, past its hash
  const R = bench.TableFixedRefusal;

  const oneRecord = () => {
    const values = [new bench.FixedTable()];
    const bytes = new Uint8Array(fixed.FixedTableFixedMeasure(1));
    fixed.FixedTableFixedSave(values, 1, bytes);
    return bytes;
  };
  const read = (bytes) => {
    const values = [new bench.FixedTable()];
    poisonDeep(values[0]);
    const before = structuredClone(values[0]);
    bench.InitFixedTable(before); // R4: a refusal hands the row back at its declared default
    const report = new bench.TableFixedReport();
    const n = fixed.FixedTableFixedLoad(values, 1, bytes, bytes.length,
      fixed.FixedTableFixedNewPlan(), report);
    return { n, report, v: values[0].value, whole: values[0], before };
  };
  const reads = (what, bytes, get, want) => {
    const r = read(bytes);
    check(r.n === 1 && r.report.refused === 0 && r.report.oor === 0 && get(r.v) === want,
      `${what} reads ${want} and moves no counter (n ${r.n}, refused ${r.report.refused}, got ${get(r.v)})`);
  };
  const refuses = (what, bytes) => {
    const r = read(bytes);
    check(r.n === -1 && r.report.refused === R.OutOfRange,
      `${what} refuses out_of_range (n ${r.n}, refused ${bench.TableFixedRefusalName(r.report.refused)})`);
    check(r.report.oor === 0 && r.report.widened === 0,
      `${what} moves no counter (oor ${r.report.oor}, widened ${r.report.widened})`);
    const d = deepDiff(r.whole, r.before);
    check(d === null, `${what} hands the destination back at its declared default (differs at ${d})`);
  };

  // THE POSITIVE CONTROL FIRST: untouched storage is in range and reads.
  {
    const r = read(oneRecord());
    check(r.n === 1 && r.report.refused === 0 && r.report.oor === 0,
      `C8 F-SHIFT: an untouched record reads clean (n ${r.n}, refused ${r.report.refused})`);
  }

  // A FIXED FIELD'S DECLARED MAX IS IN VALUE UNITS: 65535<<8 = 16776960 is the
  // last lawful raw, and one past it refuses. F is 8, and 16776960 is
  // unreachable as a bound without the shift.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + C8_SERVER_TIME, 16777000, true);
    refuses("C8 F-SHIFT: a fixed(24,8) raw past max<<F", bytes);
  }
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + C8_SERVER_TIME, 16776960, true);
    reads("C8 F-SHIFT: a fixed(24,8) raw AT max<<F", bytes, (v) => v.serverTime, 16776960);
  }

  // THE UNSIGNED FIXED TWIN, so the shift is proved on both signednesses:
  // ping ufixed(8,8) | max = 250 -> 250<<8 = 64000.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setUint16(BODY + C8_PING, 65000, true);
    refuses("C8 F-SHIFT: a ufixed(8,8) raw past max<<F", bytes);
  }
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setUint16(BODY + C8_PING, 64000, true);
    reads("C8 F-SHIFT: a ufixed(8,8) raw AT max<<F", bytes, (v) => v.ping, 64000);
  }

  // A bits(N) IS BOUNDED BY 2^N - 1: crc_hint bits(24) rides in a 32-bit lane,
  // so the WIDTH is the bound and a set top byte refuses.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setUint32(BODY + C8_CRC_HINT, 0x01000000, true);
    refuses("C8 BITS: a bits(24) value past 2^24-1", bytes);
  }
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setUint32(BODY + C8_CRC_HINT, 16777215, true);
    reads("C8 BITS: a bits(24) value AT 2^24-1", bytes, (v) => v.crcHint, 16777215);
  }
}

// ---------------------------------------------------------------------------
// 1 and 2. THE PAIRED CORPUS: the reference's bytes, and the reference's values
// ---------------------------------------------------------------------------

async function pairedCorpus(check, bench, fixed, corpusDir) {
  const corpus = new Uint8Array(readFileSync(resolve(corpusDir, "bench_fixed.bin")));
  const oracle = JSON.parse(readFileSync(resolve(corpusDir, "bench_fixed.oracle.json"), "utf8"));
  const Count = oracle.length;

  // the framing, before anything is decoded
  check(corpus[0] === 3, "fixed form: the file opens with form byte 3");

  // §3'S HEADER IS ONE RULE FOR ALL FIVE FORMS, and the seven bytes behind the
  // form byte are RESERVED AND ZERO. A writer that put anything there would
  // read back perfectly and disagree with every peer the day the registry
  // spends one of them, so the zeros are checked and not assumed.
  let reservedZero = true;
  for (let i = 1; i < HASH_AT; i++) {
    if (corpus[i] !== 0) { reservedZero = false; break; }
  }
  check(reservedZero, "fixed form: the seven reserved header bytes are zero");

  const blockBytes = corpus[LAYOUT_LENGTH_AT] | (corpus[LAYOUT_LENGTH_AT + 1] << 8) |
    (corpus[LAYOUT_LENGTH_AT + 2] << 16) | (corpus[LAYOUT_LENGTH_AT + 3] << 24);
  check(blockBytes === fixed.FixedTableFixedLayoutBytes,
    `fixed form: the file's layout length ${blockBytes} is this build's ${fixed.FixedTableFixedLayoutBytes}`);

  // THE HEADER NAMES THE LAYOUT ONCE (§3): the hash at 8 is the hash of the
  // layout behind it, and it is the same number every record carries.
  const headHashLo = (corpus[HASH_AT] | (corpus[HASH_AT + 1] << 8) |
    (corpus[HASH_AT + 2] << 16) | (corpus[HASH_AT + 3] << 24)) >>> 0;
  const headHashHi = (corpus[HASH_AT + 4] | (corpus[HASH_AT + 5] << 8) |
    (corpus[HASH_AT + 6] << 16) | (corpus[HASH_AT + 7] << 24)) >>> 0;
  check(headHashLo === (fixed.FixedTableFixedHashLo >>> 0) &&
    headHashHi === (fixed.FixedTableFixedHashHi >>> 0),
    "fixed form: the header's layout hash at 8 is this build's own");

  // THE LAYOUT IS THE SAME RUN OF BYTES as the reference's, which is what makes
  // the hash the same number and the record positional against the same layout.
  let blockSame = true;
  for (let i = 0; i < blockBytes; i++) {
    if (corpus[LAYOUT_AT + i] !== fixed.FixedTableFixedLayoutCopy()[i]) { blockSame = false; break; }
  }
  check(blockSame, "fixed form: the layout is byte-identical to the C++ reference's");

  check(corpus.length === fixed.FixedTableFixedMeasure(Count),
    "fixed form: Measure is a constant and it is the file's own length");

  // THE READ: one prefill and one loop over one plan
  const values = Array.from({ length: Count }, () => new bench.FixedTable());
  const plan = fixed.FixedTableFixedNewPlan();
  const report = new bench.TableFixedReport();
  const n = fixed.FixedTableFixedLoad(values, Count, corpus, corpus.length, plan, report);
  check(n === Count, `fixed form: ${Count} records read (got ${n})`);
  check(report.refused === 0, "fixed form: the reference's own file is not refused");
  check(report.unknown === 0 && report.mismatch === 0 && report.oor === 0 && report.widened === 0,
    "fixed form: a same-schema read moves no counter");

  // 2. THE VALUES, against the reference's own statement of them
  let bad = 0;
  const eq = (a, b, what, k) => {
    if (a !== b && bad < 8) { check(false, `fixed form: record ${k} ${what}: ${a} !== ${b}`); bad++; }
    return a === b;
  };
  let allEqual = true;
  for (let k = 0; k < Count; k++) {
    const v = values[k].value, o = oracle[k];
    let ok = true;
    ok = eq(v.sequence, o.sequence, "sequence", k) && ok;
    ok = eq(v.ackSequence, o.ack_sequence, "ack_sequence", k) && ok;
    ok = eq(v.ackBits, o.ack_bits, "ack_bits", k) && ok;
    ok = eq(v.sessionId, BigInt(o.session_id), "session_id", k) && ok;
    ok = eq(v.clientId, o.client_id, "client_id", k) && ok;
    ok = eq(v.nonce, BigInt(o.nonce), "nonce", k) && ok;
    ok = eq(v.worldTime, BigInt(o.world_time), "world_time", k) && ok;
    ok = eq(v.frameTick, BigInt(o.frame_tick), "frame_tick", k) && ok;
    ok = eq(v.serverTime, o.server_time, "server_time", k) && ok;
    ok = eq(v.entitiesCount, o.entities_count, "entities_count", k) && ok;
    for (let i = 0; i < o.entities_count; i++) {
      const e = v.entities[i], oe = o.entities[i];
      ok = eq(e.entityId, oe.entity_id, `entities[${i}].entity_id`, k) && ok;
      ok = eq(e.posX, oe.pos_x, `entities[${i}].pos_x`, k) && ok;
      ok = eq(e.posY, oe.pos_y, `entities[${i}].pos_y`, k) && ok;
      ok = eq(e.posZ, oe.pos_z, `entities[${i}].pos_z`, k) && ok;
      ok = eq(e.yaw, oe.yaw, `entities[${i}].yaw`, k) && ok;
      ok = eq(e.pitch, oe.pitch, `entities[${i}].pitch`, k) && ok;
      ok = eq(e.velX, oe.vel_x, `entities[${i}].vel_x`, k) && ok;
      ok = eq(e.velY, oe.vel_y, `entities[${i}].vel_y`, k) && ok;
      ok = eq(e.velZ, oe.vel_z, `entities[${i}].vel_z`, k) && ok;
      ok = eq(e.health, oe.health, `entities[${i}].health`, k) && ok;
      ok = eq(e.weapon, oe.weapon, `entities[${i}].weapon`, k) && ok;
      ok = eq(e.damage, BigInt(oe.damage), `entities[${i}].damage`, k) && ok;
      ok = eq(e.moving, oe.moving, `entities[${i}].moving`, k) && ok;
      ok = eq(e.firing, oe.firing, `entities[${i}].firing`, k) && ok;
    }
    ok = eq(v.statsCount, o.stats_count, "stats_count", k) && ok;
    for (let i = 0; i < o.stats_count; i++) {
      ok = eq(v.stats[i].statId, o.stats[i].stat_id, `stats[${i}].stat_id`, k) && ok;
      ok = eq(v.stats[i].delta, o.stats[i].delta, `stats[${i}].delta`, k) && ok;
    }
    // the UNION: the tag, then the arm the tag names and only that arm
    ok = eq(v.gameEvent.type, o.game_event_type, "game_event.type", k) && ok;
    if (o.game_event_type === 1) {
      ok = eq(v.gameEvent.hit.targetId, o.hit.target_id, "hit.target_id", k) && ok;
      ok = eq(v.gameEvent.hit.damage, o.hit.damage, "hit.damage", k) && ok;
      ok = eq(v.gameEvent.hit.hitKind, o.hit.hit_kind, "hit.hit_kind", k) && ok;
      ok = eq(v.gameEvent.hit.crit, o.hit.crit, "hit.crit", k) && ok;
    } else if (o.game_event_type === 2) {
      ok = eq(v.gameEvent.chat.channel, o.chat.channel, "chat.channel", k) && ok;
      ok = eq(v.gameEvent.chat.speaker, o.chat.speaker, "chat.speaker", k) && ok;
    } else if (o.game_event_type === 3) {
      ok = eq(v.gameEvent.pickup.itemId, o.pickup.item_id, "pickup.item_id", k) && ok;
      ok = eq(v.gameEvent.pickup.amount, o.pickup.amount, "pickup.amount", k) && ok;
    }
    for (let i = 0; i < 4; i++) ok = eq(v.loadout[i], o.loadout[i], `loadout[${i}]`, k) && ok;
    ok = eq(v.playerNameLength, o.player_name_length, "player_name_length", k) && ok;
    for (let i = 0; i < 15; i++) ok = eq(v.playerName[i], o.player_name[i], `player_name[${i}]`, k) && ok;
    ok = eq(v.payloadLength, o.payload_length, "payload_length", k) && ok;
    for (let i = 0; i < 16; i++) ok = eq(v.payload[i], o.payload[i], `payload[${i}]`, k) && ok;
    // FLOATS ARE COMPARED AS BIT PATTERNS: this form carries the IEEE-754 bit
    // pattern with NO canonicalisation, so a NaN payload is part of the value.
    ok = eq(f32bits(v.aimX), o.aim_x_bits, "aim_x", k) && ok;
    ok = eq(f32bits(v.aimY), o.aim_y_bits, "aim_y", k) && ok;
    ok = eq(f32bits(v.aimZ), o.aim_z_bits, "aim_z", k) && ok;
    ok = eq(f32bits(v.recoil), o.recoil_bits, "recoil", k) && ok;
    ok = eq(f64bits(v.drift), BigInt(o.drift_bits), "drift", k) && ok;
    // 128-BIT: the low half then the high, which is this wire's order everywhere
    ok = eq(v.wideKey, BigInt(o.wide_key_lo) | (BigInt(o.wide_key_hi) << 64n), "wide_key", k) && ok;
    ok = eq(BigInt.asUintN(128, v.flux), BigInt(o.flux_lo) | (BigInt(o.flux_hi) << 64n), "flux", k) && ok;
    ok = eq(v.ping, o.ping, "ping", k) && ok;
    ok = eq(v.crcHint, o.crc_hint, "crc_hint", k) && ok;
    ok = eq(v.hasExtra, o.has_extra, "has_extra", k) && ok;
    ok = eq(v.extra, o.extra, "extra", k) && ok;
    ok = eq(v.idleTicks, o.idle_ticks, "idle_ticks", k) && ok;
    if (!ok) allEqual = false;
  }
  check(allEqual, `fixed form: every field of all ${Count} records is the value the C++ reference states`);

  // 1. THE WRITE: the same bytes back, and not one of them different
  const out = new Uint8Array(fixed.FixedTableFixedMeasure(Count));
  const wrote = fixed.FixedTableFixedSave(values, Count, out);
  check(wrote === corpus.length, `fixed form: Save wrote ${wrote}, the file is ${corpus.length}`);
  let at = -1;
  for (let i = 0; i < corpus.length; i++) if (out[i] !== corpus[i]) { at = i; break; }
  check(at < 0, at < 0
    ? "fixed form: the JavaScript writer's bytes are IDENTICAL to the C++ reference's"
    : `fixed form: first byte differing from the C++ reference at ${at} (js ${out[at]}, cpp ${corpus[at]})`);

  // and the identity plan really is ONE run, which is what the reader's own
  // storage being the wire's layout buys: the coalescer's best case, not a
  // step skipped
  check(fixed.FixedTableFixedBodyBytes === 1236,
    "fixed form: the body is the reference's 1236 bytes");
}

const CONV = new DataView(new ArrayBuffer(8));
function f32bits(x) { CONV.setFloat32(0, x, true); return CONV.getUint32(0, true); }
function f64bits(x) { CONV.setFloat64(0, x, true); return CONV.getBigUint64(0, true); }

// ---------------------------------------------------------------------------
// THE GUARD IS COMPARED AT ArgW BYTES, NEVER AS A PREFIX.
// compiler/fixedguardwidth_test.go holds the IR stamp; this is the run loop.
// A two-byte tag 0x0101 whose low byte is 1 is not arm 1. ONE PATH: this is
// TableFixedRun, the same loop identity and compiled both take.
// ---------------------------------------------------------------------------
function guardComparedAtArgW(check, home) {
  check(home.TableFixedLanes === 11,
    `ArgW: a plan entry is eleven int32 lanes, got ${home.TableFixedLanes}`);

  const run = (lanes, srcBytes) => {
    const src = new Uint8Array(srcBytes);
    const srcView = new DataView(src.buffer, src.byteOffset, src.length);
    const dst = new Uint8Array(1);
    const dstView = new DataView(dst.buffer);
    const r = new home.TableFixedReport();
    // op=COPY src=2 dst=0 size=1 aux=0 guard=0 arg=1 meta=0 argw=lanes[8]
    // guards=0 guardCount=0: one tag, so the guard lane is the whole chain (§4.1)
    home.TableFixedRun(new Int32Array(lanes), 1, src, srcView, 0, dst, dstView, null, r);
    return dst[0];
  };
  // COPY of the payload at byte 2, guarded at 0, answering to arm 1, width 2
  const twoByte = [0, 2, 0, 1, 0, 0, 1, 0, 2, 0, 0];
  check(run(twoByte, [0x01, 0x01, 0xAA]) === 0,
    "ArgW: a two-byte tag 0x0101 whose low byte is 1 does NOT run arm 1");
  check(run(twoByte, [0x01, 0x00, 0xAA]) === 0xAA,
    "ArgW: a two-byte tag 0x0001 DOES run arm 1");
  // ArgW 0 means 1, as C++: only the first byte is the tag
  const zeroMeansOne = [0, 2, 0, 1, 0, 0, 1, 0, 0, 0, 0];
  check(run(zeroMeansOne, [0x01, 0x01, 0xAA]) === 0xAA,
    "ArgW: zero is read as one, so a 0x0101 prefix matches arm 1 at width 0");
  const fourByte = [0, 4, 0, 1, 0, 0, 1, 0, 4, 0, 0];
  check(run(fourByte, [0x01, 0x01, 0x00, 0x00, 0xBB]) === 0,
    "ArgW: a four-byte tag 0x00000101 whose low byte is 1 does NOT run arm 1");
  check(run(fourByte, [0x01, 0x00, 0x00, 0x00, 0xBB]) === 0xBB,
    "ArgW: a four-byte tag 0x00000001 DOES run arm 1");
}

// ---------------------------------------------------------------------------
// W6: THE PLAN IS PARTITIONED (docs/FIXED-FORM-ALGORITHM.md §5.9 #42, §4.2, §4.4)
// ---------------------------------------------------------------------------
//
// THE SPLIT IS A PROPERTY OF THE PLAN'S ORDER, OWED BY EVERY LEG. Every
// UNGUARDED entry first, then every guarded one, and the plan states where the
// second half starts — `split` is the NUMBER OF UNGUARDED ENTRIES and not a
// count of guarded ones (§4.4). A leg whose run loop tests `guard != NONE` per
// entry still owes it, because the partition is what makes COALESCING ACROSS
// THE SPLIT ILLEGAL: two `copy` entries that merge while one is guarded and the
// other is not produce a run that lands a guarded arm's bytes unconditionally.
// The C++ reference holds the same property (test/tables/fixedform_main.cpp,
// plan_partition_case); this is its twin.
//
// THE PLAN THIS LEG ACTUALLY BUILDS IS THE COMPILED ONE. On this leg identity's
// plan is ONE COPY of the whole body (ut1/UT1Table.js, `UtRootFixedIdentity`), so
// it has no guarded half to test and the partition is what the compiler owes the
// lineage plans it lays down at module load. TableFixedCompile IS that compiler
// and it is exported, so the case compiles a reader's own layout against itself
// and reads the guard lane and `split` off the plan object — the same walk a
// lineage entry takes.
//
// THE DESTINATION ROWS ARE NOT THE ASSERTION. `dst` carries the reader's landing
// offsets, which place bytes and never choose a half; the partition is a fact of
// the GUARD lane and `split` alone, so the rows are zero here. A plan entry is
// eleven int32 lanes (TableFixedLanes: nine, and the guard chain's
// offset+count, §4.1) and the guard is lane 5; TableFixedNoGuard is -1 (Tblut1Table.js, `TableFixedLanes`/`TableFixedNoGuard`).
function partitionIsHeld(check, home, layout, bodyBytes, who) {
  const LANES = 11, GUARD = 5, OP = 0, SRC = 1, DST = 2, SIZE = 3, ARG = 6, ARGW = 8;
  const NO_GUARD = -1, COPY = 0;

  const theirs = new home.TableFixedLayoutView();
  check(home.TableFixedParseLayout(layout, 0, layout.length, theirs),
    `W6: ${who} — the layout parses`);
  const dst = new Int32Array(theirs.count * 5);
  const plan = new home.TableFixedPlan(256, bodyBytes, 4096);
  const report = new home.TableFixedReport();
  const made = home.TableFixedCompile(theirs, layout, dst, plan, report);
  check(made >= 0 && made === plan.count,
    `W6: ${who} — the plan compiled (${made})`);
  if (made < 0) { return null; }

  const e = plan.entries, count = plan.count, split = plan.split;
  check(split >= 0 && split <= count, `W6: ${who} — split is in range`);
  // EVERY ENTRY BELOW THE SPLIT CARRIES NO GUARD; EVERY ENTRY AT OR ABOVE IT
  // CARRIES ONE — which is the same as saying split is the number of unguarded
  // entries and not a count of guarded ones.
  let unguarded = 0;
  for (let i = 0; i < count; i++) {
    const guarded = e[i * LANES + GUARD] !== NO_GUARD;
    if (!guarded) { unguarded++; }
    if (i < split && guarded) {
      check(false, `W6: ${who} — entry ${i} is below the split and carries a GUARD`);
    }
    if (i >= split && !guarded) {
      check(false, `W6: ${who} — entry ${i} is above the split and carries NO guard`);
    }
  }
  check(unguarded === split,
    `W6: ${who} — split is the count of UNGUARDED entries (split ${split}, unguarded ${unguarded})`);

  // THE BOUNDARY PAIR: had the coalescer merged across the split, the entry
  // below it and the entry at it would be one entry. They are two, and this
  // says why they have to be.
  if (split > 0 && split < count) {
    const a = (split - 1) * LANES, b = split * LANES;
    const mergeable = e[a + OP] === COPY && e[b + OP] === COPY &&
      e[a + GUARD] === e[b + GUARD] && e[a + ARG] === e[b + ARG] && e[a + ARGW] === e[b + ARGW] &&
      e[a + SRC] + e[a + SIZE] === e[b + SRC] && e[a + DST] + e[a + SIZE] === e[b + DST];
    check(!mergeable, `W6: ${who} — the pair at the split was not coalesced across it`);
  }
  return { count, split };
}

export function planPartition(check, ut1, ut1home, fx1, fx1home) {
  // A PLAN WITH BOTH HALVES: UT1 carries a union, so its arms' entries are
  // guarded and the scalars around them are not.
  const u = partitionIsHeld(check, ut1home, ut1.UtRootFixedLayoutCopy(), ut1.UtRootFixedBodyBytes, "UT1 plan");
  if (u !== null) {
    check(u.split > 0 && u.split < u.count,
      `W6: UT1's plan really has BOTH halves, so the case is not vacuous (split ${u.split} of ${u.count})`);
  }

  // A PLAN WITH NO GUARDED ENTRY AT ALL: the split is then the whole plan, and
  // `split` being a count of UNGUARDED entries rather than of guarded ones is
  // what makes that come out right.
  const f = partitionIsHeld(check, fx1home, fx1.FxRootFixedLayoutCopy(), fx1.FxRootFixedBodyBytes, "FX1 plan");
  if (f !== null) {
    check(f.split === f.count,
      `W6: a plan with no guarded entry has its split at the END, not at zero (split ${f.split} of ${f.count})`);
  }
}

// ---------------------------------------------------------------------------
// A WIDTH-8 ORDINAL IS READ WHOLE — THROUGH SIXTY-FOUR BITS, NOT THIRTY-TWO.
//
// docs/FIXED-FORM-ALGORITHM.md §4.5's `ordinal` row reads `raw := LE(size,
// record+src)` "through a 64-bit temporary, an ordinal width of `8` being
// admissible", and the versioning bill's fix 7 records the defect this pins: a
// `uint32_t raw` truncated a width-8 ordinal to its low four bytes. The
// JavaScript twin of the C++ reference's `owed8_width8_ordinal_read_whole_case`
// (test/tables/fixedform_main.cpp:1089-1118).
//
// ONE PATH: this drives TableFixedRun directly — the same loop the identity and
// compiled plans both run — with an ordinal entry built by hand, an eight-byte
// tag lane remapping through a one-entry table, exactly as the reference builds
// its plan entry. The site under test is fixedruntime.go's ordinal op, whose
// width-8 read is two u32 lanes and whose `hi !== 0` arm is the whole fact.
// ---------------------------------------------------------------------------
export function ordinalWidth8ReadWhole(check, home) {
  // THE PLAN ENTRY IS ELEVEN int32 LANES, in the order this loop reads them:
  //   [op, src, dst, size, aux, guard, arg, meta, argw, guards, guardCount]
  // op 3 is TableFixedOpOrdinal (fixedruntime.go:98); guard -1 is
  // TableFixedNoGuard (fixedruntime.go:107), an unguarded top-level entry;
  // meta's low byte is the ordinal's DESTINATION width, 8 here; size is the
  // SOURCE width, 8, the width-8 lane this case is about. aux 0 is the remap
  // table's offset. Nothing here is read out of the code under test.
  const run = (srcBytes) => {
    const src = new Uint8Array(srcBytes);
    const srcView = new DataView(src.buffer);
    const dst = new Uint8Array(8);
    dst.fill(0xAB); // poison, so a lane the op never writes is visible
    const dstView = new DataView(dst.buffer);
    const r = new home.TableFixedReport();
    // the table's slot 0 is its length (one entry), slot 1 is variant 1's own
    // ordinal — the same shape as the reference's uint16 map { 1, 1 }
    const remap = new Int32Array([1, 1]);
    const lanes = [3, 0, 0, 8, 0, -1, 0, 8, 1, 0, 0];
    home.TableFixedRun(new Int32Array(lanes), 1, src, srcView, 0, dst, dstView, remap, r);
    return { landed: dstView.getBigUint64(0, true), refused: r.refused, oor: r.oor };
  };

  // 1. THE FORGED VALUE: 2^32 = 0x0000000100000000, whose LOW four bytes are
  //    zero and whose HIGH four carry the one. A 32-bit temporary reads 0 and
  //    lands None with nothing said — the truncation this case pins. Through
  //    all sixty-four bits it is an ordinal past the writer's one variant, so
  //    the strict read REFUSES out_of_range and writes nothing (§4.6, §5.4).
  {
    const { landed, refused, oor } = run([0, 0, 0, 0, 1, 0, 0, 0]);
    check(refused === home.TableFixedRefusal.OutOfRange,
      `ORDINAL WIDTH 8: the HIGH LANE is seen — a 2^32 ordinal refuses out_of_range, where a 32-bit temporary would have read 0 (got ${home.TableFixedRefusalName(refused)})`);
    check(landed === 0xABABABABABABABABn && oor === 0,
      `ORDINAL WIDTH 8: the refused ordinal writes nothing and moves no counter (got 0x${landed.toString(16)}, oor ${oor})`);
  }

  // 2. THE LEGITIMATE VALUE THE RULE ADMITS — the negative control. Ordinal 1
  //    is inside the one-entry table, so it remaps to 1 and moves no counter.
  {
    const { landed, refused, oor } = run([1, 0, 0, 0, 0, 0, 0, 0]);
    check(landed === 1n, `ORDINAL WIDTH 8: ordinal 1 remaps through the table (got ${landed})`);
    check(refused === 0 && oor === 0, `ORDINAL WIDTH 8: an admitted ordinal reads and moves no counter (refused ${refused}, oor ${oor})`);
  }
}

// ---------------------------------------------------------------------------
// THE FORM HEADER, READ BEFORE THE FILE'S LENGTH (§5.3 STEPS 1 AND 2)
// ---------------------------------------------------------------------------
//
// §5.3 step 1 reads the FORM BYTE BEFORE THE FILE'S LENGTH: a committed form-1
// file is TEN bytes and a form-2 batch THREE, so a reader that measured first
// would answer `truncated` for every real file of the two forms it is supposed
// to name, and SPEC-TABLES §3.4's promise ("a fixed reader given 1 answers
// `previous_form`, given 2 answers `message_form_as_file`") would hold for no
// file anyone has. Step 2 is the twenty-byte minimum and then the seven
// reserved bytes, REFUSED and not ignored.
//
// THESE ARE THE FIVE CASES THE ELIXIR MODEL CARRIES (test/elixir-fixedform/
// main.exs, the `form byte` block of its negative controls) and this leg did
// not. Every case here is answered at the header, before a hash is looked
// up, so none of it belongs to the lineage harness. THE PROBES RUN THE READER, and each asserts three things:
// the refusal by name (framing damage included: `truncated` and
// `reserved_not_zero`, §5.3's refusal table), that the report carries no
// second answer channel ("There is no `malformed` flag on this form's
// report", §5.3), and that not one destination field was written — REFUSE is
// total.
function formHeaderProbes(check, fx1, fx1home) {
  const R = fx1home.TableFixedRefusal;

  // A VALID FORM-3 FILE, one record of this leg's own FX1, to carry the
  // otherwise-good tail for the form-0 and reserved-byte cases.
  const one = new fx1home.FxRoot();
  one.keep = 4242; one.narrow = 40000; one.renamed = 321; one.gone = 654;
  one.nested.a = 111; one.nested.b = 222;
  const w1 = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([one], 1, w1) === w1.length,
    "form header: the valid form-3 fixture saves its own length");

  // THE DESTINATION IS POISONED, NOT RESET. A read that wrote a default would
  // look exactly like a read that wrote nothing if the destination started at
  // its construction values, so every field takes a sentinel a lawful read
  // never lands (0x5A) and the whole surface is compared after the refusal.
  const poison = (v) => {
    v.keep = 0x5A5A5A5A; v.narrow = 0x5A5A; v.renamed = 0x5A5A; v.gone = 0x5A5A;
    v.nested.a = 0x5A5A; v.nested.b = 0x5A5A;
    v.label.fill(0x5A); v.labelLength = 0x5A;
    v.marks.fill(0x5A); v.marksCount = 0x5A;
    v.blob.fill(0x5A); v.blobLength = 0x5A;
  };
  // R4: a refusal hands the row back at its declared default, which is what the
  // type's own reset gives the same poison.
  const untouched = (v) => {
    const ref = new fx1home.FxRoot();
    poison(ref);
    fx1home.InitFxRoot(ref);
    return deepDiff(v, ref) === null;
  };

  const refusedByName = (what, bytes, reason) => {
    const values = [new fx1home.FxRoot()];
    poison(values[0]);
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(values, 1, bytes, bytes.length, fx1.FxRootFixedNewPlan(), r);
    check(n === -1 && r.refused === reason && !("damaged" in r),
      `form header: ${what} is ${fx1home.TableFixedRefusalName(reason)}, and the report has no malformed flag`);
    check(r.unknown === 0 && r.mismatch === 0 && r.widened === 0 && r.oor === 0,
      `form header: ${what} moves no counter`);
    check(untouched(values[0]),
      `form header: ${what} hands the destination back at its declared default`);
  };

  // 1. ZERO BYTES: there is no first byte to read, `truncated` (§5.3).
  refusedByName("zero bytes", new Uint8Array(0), R.Truncated);

  // 2. A TEN-byte form-1 file: the form byte earns `previous_form` BEFORE the
  //    length is measured. Measured first it would be `truncated`.
  refusedByName("a ten-byte form-1 file", new Uint8Array([1, 0, 0, 0, 0, 0, 0, 0, 0, 0]), R.PreviousForm);

  // 3. A THREE-byte form-2 batch: `message_form_as_file`, not `truncated`.
  refusedByName("a three-byte form-2 batch", new Uint8Array([2, 1, 0]), R.MessageFormAsFile);

  // 4. FORM BYTE 0 with the otherwise-valid fixture tail: no form defines it,
  //    and the ordered registry names it by where it sits, `newer_form`.
  {
    const bad = Uint8Array.from(w1);
    bad[0] = 0;
    refusedByName("form byte 0 with a valid tail", bad, R.NewerForm);
  }

  // 5. A RESERVED BYTE NONZERO on an otherwise-valid form-3 file: the seven
  //    reserved bytes are REFUSED, not ignored — `reserved_not_zero`.
  {
    const bad = Uint8Array.from(w1);
    bad[3] = 1;
    refusedByName("reserved byte 3 set nonzero", bad, R.ReservedNotZero);
  }

  // 6. A FORM-3 FILE UNDER THE 20-BYTE HEADER: `truncated`, by name.
  refusedByName("a ten-byte form-3 file", Uint8Array.from(w1.subarray(0, 10)), R.Truncated);

  // 7. A RAGGED TAIL, rest mod record_bytes != 0: `truncated`, by name.
  {
    const bad = new Uint8Array(w1.length + 1);
    bad.set(w1);
    refusedByName("a ragged tail", bad, R.Truncated);
  }
}

// ---------------------------------------------------------------------------
// REFUSE IS TOTAL — the per-record hash refusal (W15, docs §5.8 row 9)
// ---------------------------------------------------------------------------
//
// `refuse_writes_nothing` ON THIS DRIVER'S OWN READER: the one refusal the
// prefill bug broke. §5.3 holds that a record whose hash names no layout this
// reader holds answers `no_layout`, and REFUSE is total — no counter moves,
// nothing is decoded, and not one destination byte is written, the prefill
// included. The reference USED to prefill before the per-record hash check, so
// a refused read had already written the caller's storage; the fix made the
// hash check first. A golden file that would change if the refusal broke is
// detection, not assertion: here the destination is PRE-POISONED with a
// sentinel (0x5A) and compared byte for byte AFTER the refusal, and every
// counter is asserted zero at the same time, in one case, by the rule's name.
function refuseTotalProbe(check, fx1, fx1home) {
  const R = fx1home.TableFixedRefusal;

  // a valid form-3 file of this leg's own FX1, to forge the record's hash in
  const one = new fx1home.FxRoot();
  one.keep = 4242; one.narrow = 40000; one.renamed = 321; one.gone = 654;
  one.nested.a = 111; one.nested.b = 222;
  const w1 = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([one], 1, w1) === w1.length,
    "refuse total: the valid form-3 fixture saves its own length");

  // the record's own hash, inverted: the header still names this build's own
  // layout, but record 0 now names none this reader holds — no_layout.
  const bad = Uint8Array.from(w1);
  bad[LAYOUT_AT + fx1.FxRootFixedLayoutBytes] ^= 0xff;

  // THE DESTINATION IS POISONED, NOT RESET. A read that wrote a default would
  // look exactly like a read that wrote nothing if the destination started at
  // its construction values, so every byte of the caller's image takes a
  // sentinel a lawful read never lands (0x5A), the decoded value takes one
  // too, and the whole surface is compared after the refusal.
  const plan = fx1.FxRootFixedNewPlan();
  plan.image.fill(0x5A);
  const values = [new fx1home.FxRoot()];
  const v = values[0];
  v.keep = 0x5A5A5A5A; v.narrow = 0x5A5A; v.renamed = 0x5A5A; v.gone = 0x5A5A;
  v.nested.a = 0x5A5A; v.nested.b = 0x5A5A;
  v.label.fill(0x5A); v.labelLength = 0x5A;
  v.marks.fill(0x5A); v.marksCount = 0x5A;
  v.blob.fill(0x5A); v.blobLength = 0x5A;

  const r = new fx1home.TableFixedReport();
  const n = fx1.FxRootFixedLoad(values, 1, bad, bad.length, plan, r);

  check(n === -1 && r.refused === R.NoLayout,
    "REFUSE is total: a record whose hash names no held layout refuses no_layout, and nothing else");
  check(r.unknown === 0 && r.mismatch === 0 && r.oor === 0 && r.widened === 0 && r.repeated === 0,
    "REFUSE is total: no counter moves");
  check(plan.image.every((b) => b === 0x5A),
    "REFUSE is total: not one destination byte is written, the prefill included");
  const reset = new fx1home.FxRoot();
  reset.keep = 0x5A5A5A5A; reset.narrow = 0x5A5A; reset.renamed = 0x5A5A; reset.gone = 0x5A5A;
  reset.nested.a = 0x5A5A; reset.nested.b = 0x5A5A;
  reset.label.fill(0x5A); reset.labelLength = 0x5A;
  reset.marks.fill(0x5A); reset.marksCount = 0x5A;
  reset.blob.fill(0x5A); reset.blobLength = 0x5A;
  fx1home.InitFxRoot(reset);
  check(deepDiff(v, reset) === null,
    "REFUSE is total: nothing is decoded, and the row is handed back at its declared default (R4)");
}

// ---------------------------------------------------------------------------
// THE C++ REFERENCE'S OWN BYTE ORACLE (make tables-fixedform-corpus)
// ---------------------------------------------------------------------------
//
// The reference writes one form-3 FILE per root with its values set BY HAND,
// and a port proves itself against them two ways: read a file and save it back
// and the bytes must be identical — which reaches every field, because a byte a
// port encodes differently is a byte that does not come back — and read a file
// written under ANOTHER schema's layout, which is the plan path.
//
// WHAT THIS CATCHES THAT NOTHING ELSE HERE DOES: §3.4's "the slack is zero".
// The paired corpus round trip cannot see it — a value READ from a file has the
// prefill's zeros past its used length already, so a writer that copies the
// whole span writes the same zeros back. The reference's own records are set
// from CONSTRUCTED storage instead, and `label` has a declared default, so
// record 1's `label_length = 0` means the whole span is slack over storage that
// holds "fx". A port that wrote the span would put those two bytes on the wire.
//
// THE SPLIT (§5.6, §5.9 #31): the fx1.bin block below is THIS BUILD'S OWN layout
// — its own hash, its own identity plan, its own writer — and §5 does not touch
// one byte of it, so it RUNS. Nothing else on this leg makes its two load-bearing
// assertions: the reference's own file saved back IDENTICAL, and "the slack is
// zero" over records built from CONSTRUCTED storage rather than read from a file.
// The half §5 retired — FX1 reading the reference's FX2 file with no lineage
// entry — is now the refusal TestJSFixedVersioningReferenceForwardRefusal holds.
function referenceOracleIdentity(check, fx1, fx1home, oracleDir) {
  const read = (name) => {
    try {
      return new Uint8Array(readFileSync(resolve(oracleDir, name)));
    } catch {
      return null;
    }
  };
  const textOf = (buf, n) => {
    let out = "";
    for (let i = 0; i < n; i++) { out += String.fromCharCode(buf[i]); }
    return out;
  };

  // ---- fx1.bin: THIS BUILD'S OWN LAYOUT, so the identity plan reads it
  {
    const file = read("fx1.bin");
    check(file !== null, "reference oracle: fx1.bin is present (make tables-fixedform-corpus)");
    if (file === null) { return; }
    const v = [new fx1home.FxRoot(), new fx1home.FxRoot()];
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(v, 2, file, file.length, fx1.FxRootFixedNewPlan(), r);
    check(n === 2 && r.refused === 0, `reference oracle: fx1.bin holds two records, got ${n}`);
    if (n !== 2) { return; }

    // the values the reference states, checked INDEPENDENTLY of the bytes: a
    // reader and a writer sharing one offset mistake round trip perfectly
    check(v[0].keep === 4242 && v[0].narrow === 40000 && v[0].renamed === 321 && v[0].gone === 654,
      `reference oracle: fx1 record 0 scalars — got ${v[0].keep}/${v[0].narrow}/${v[0].renamed}/${v[0].gone}`);
    check(v[0].nested.a === 111 && v[0].nested.b === 222,
      `reference oracle: fx1 record 0 nested — got ${v[0].nested.a}/${v[0].nested.b}`);
    check(v[0].labelLength === 3 && textOf(v[0].label, 3) === "fx1",
      `reference oracle: fx1 record 0 label — got ${v[0].labelLength} ${JSON.stringify(textOf(v[0].label, v[0].labelLength))}`);
    check(v[0].marksCount === 2 && v[0].marks[0] === 101 && v[0].marks[1] === 202,
      `reference oracle: fx1 record 0 marks — got count ${v[0].marksCount} [${v[0].marks[0]}, ${v[0].marks[1]}]`);
    check(v[0].blobLength === 4 && v[0].blob[0] === 0xDE && v[0].blob[1] === 0xAD &&
      v[0].blob[2] === 0xBE && v[0].blob[3] === 0xEF,
      `reference oracle: fx1 record 0 blob — got length ${v[0].blobLength}`);
    // record 1: NOTHING USED AT ALL, which is the slack case in full
    check(v[1].labelLength === 0 && v[1].marksCount === 0 && v[1].blobLength === 0,
      `reference oracle: fx1 record 1 uses nothing — got ${v[1].labelLength}/${v[1].marksCount}/${v[1].blobLength}`);

    const twin = new Uint8Array(fx1.FxRootFixedMeasure(2));
    check(fx1.FxRootFixedSave(v, 2, twin) === file.length,
      "reference oracle: fx1 saves back to the reference's own byte count");
    const at = firstDiff(twin, file);
    check(at < 0,
      `reference oracle: fx1.bin saved back is IDENTICAL to the C++ reference's — first byte differing from the C++ reference at ${at}` +
      (at < 0 ? "" : ` (ours 0x${twin[at].toString(16)}, reference 0x${file[at].toString(16)})`));

    // ---- THE SAME FILE BUILT FROM CONSTRUCTED STORAGE, which is the ONLY
    // instrument here that can see §3.4's "the slack is zero". A value READ
    // from a file already has the prefill's zeros past its used length, so a
    // writer that copies the whole declared span writes the same zeros back
    // and the round trip above cannot tell. These records are built the way
    // the reference builds them — from a FRESH value — and `label` has a
    // declared default of "fx", so record 1's `LabelLength = 0` leaves two
    // bytes of live storage behind the used length. A port that wrote the span
    // puts "fx" on the wire under a length that says nothing is there.
    const made = [new fx1home.FxRoot(), new fx1home.FxRoot()];
    check(made[0].labelLength === 2 && textOf(made[0].label, 2) === "fx",
      `reference oracle: a DECLARED DEFAULT is the value and not just its length — a fresh FxRoot's label is ${made[0].labelLength} ${JSON.stringify(textOf(made[0].label, made[0].labelLength))}, want 2 "fx"`);

    made[0].keep = 4242; made[0].narrow = 40000; made[0].renamed = 321; made[0].gone = 654;
    made[0].nested.a = 111; made[0].nested.b = 222;
    made[0].labelLength = setText(made[0].label, "fx1");
    made[0].marks[0] = 101; made[0].marks[1] = 202; made[0].marksCount = 2;
    made[0].blob[0] = 0xDE; made[0].blob[1] = 0xAD; made[0].blob[2] = 0xBE; made[0].blob[3] = 0xEF;
    made[0].blobLength = 4;
    made[1].keep = 1; made[1].narrow = 2; made[1].renamed = 3; made[1].gone = 4;
    made[1].nested.a = 5; made[1].nested.b = 6;
    made[1].labelLength = 0; // nothing used at all: the WHOLE span is slack,
    made[1].marksCount = 0;  // over storage that still holds the default "fx"
    made[1].blobLength = 0;

    const built = new Uint8Array(fx1.FxRootFixedMeasure(2));
    check(fx1.FxRootFixedSave(made, 2, built) === file.length,
      "reference oracle: the by-hand records measure the reference's own byte count");
    const bat = firstDiff(built, file);
    check(bat < 0,
      `reference oracle: fx1.bin built from CONSTRUCTED storage is identical to the C++ reference's — the slack is zero — first byte differing from the C++ reference at ${bat}` +
      (bat < 0 ? "" : ` (ours 0x${built[bat].toString(16)}, reference 0x${file[bat].toString(16)})`));
  }
}

// LIVE-COUNT: only used slots land, but every dead wire byte must be zero
// (§5.3 step 11). Stale constructed tails write zero; forged wire tails refuse
// before touching a destination. Keep a clean positive twin beside the poison.
function liveCountSlack(check, fx1, fx1home) {
  const one = new fx1home.FxRoot();
  one.marksCount = 1;
  one.marks[0] = 42;
  one.marks[1] = 999;
  one.marks[2] = 888;
  one.marks[3] = 777;
  const buf = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([one], 1, buf) === buf.length, "live-count: save of count=1");

  // keep(4) + narrow(2) + renamed(4) + gone(4) + nested(8) + label string(8) (4+8)
  const marksAt = 4 + 2 + 4 + 4 + 8 + 12;
  const recBody = HEADER_BYTES + 4 + fx1.FxRootFixedLayoutBytes + 8;
  const view = new DataView(buf.buffer, buf.byteOffset, buf.length);
  check(view.getInt32(recBody + marksAt, true) === 1, "live-count: the wire count is 1");
  check(view.getInt32(recBody + marksAt + 4, true) === 42, "live-count: live slot 0 is 42");
  check(view.getInt32(recBody + marksAt + 8, true) === 0 &&
    view.getInt32(recBody + marksAt + 12, true) === 0 &&
    view.getInt32(recBody + marksAt + 16, true) === 0,
    "live-count: write left slack as the template's zeros");
  const clean = [new fx1home.FxRoot()];
  const cr = new fx1home.TableFixedReport();
  check(fx1.FxRootFixedLoad(clean, 1, buf, buf.length, fx1.FxRootFixedNewPlan(), cr) === 1,
    "live-count: clean zero-slack twin reads");
  check(clean[0].marksCount === 1 && clean[0].marks[0] === 42 && clean[0].marks[1] === 0 &&
    clean[0].marks[2] === 0 && clean[0].marks[3] === 0,
    "live-count: clean live prefix and zero tails land");
  view.setInt32(recBody + marksAt + 8, 999, true);
  view.setInt32(recBody + marksAt + 12, 888, true);
  view.setInt32(recBody + marksAt + 16, 777, true);

  const back = [new fx1home.FxRoot()];
  poisonDeep(back[0]);
  const before = structuredClone(back[0]);
  fx1home.InitFxRoot(before); // R4: a refusal hands the row back at its declared default
  const r = new fx1home.TableFixedReport();
  const n = fx1.FxRootFixedLoad(back, 1, buf, buf.length, fx1.FxRootFixedNewPlan(), r);
  check(n === -1 && r.refused === fx1home.TableFixedRefusal.WireDamaged,
    `live-count: poisoned slack refuses wire_damaged, got ${n}`);
  check(deepDiff(back[0], before) === null, "live-count: poisoned slack hands the destination back at its declared default");
  check(r.oor === 0 && r.widened === 0, "live-count: a refused load commits no counters");
}

// ---------------------------------------------------------------------------
// P3 — HOSTILE BYTES, THE SWEEP (docs/FIXED-FORM-ALGORITHM.md §7 item 5, the
// coverage matrix row "byte-flip fuzz, sanitizer"; docs/SPEC-TABLES.md §3.4).
// THE REFERENCE IS test/tables/fixedform_main.cpp's `fuzz_case` and
// test/tables/fixedform_properties.cpp's P3; the C leg's port is
// test/c-tables/fixedform_main.c.
//
// A FIXED RECORD CARRIES NO LENGTHS AND NO TERMINATORS, so every offset this
// reader uses is arithmetic over sizes a stranger wrote down: a count, a length,
// a union tag, a nested body's position. Every byte of a form-3 file is flipped
// to a hostile value here and handed to THIS build's identity reader. UNDER THE
// STRICT READ (docs/plans/strict-only.md) there are exactly TWO answers: a CLEAN
// read — n records, nothing refused, no counter moved, every
// landed field inside the bound the schema declares — or a REFUSAL, n = -1 with
// a reason named — framing damage included, which refuses `truncated` or
// `reserved_not_zero` by name (§5.3: "There is no `malformed` flag on this
// form's report"). A throw, a negative read with no name, a landed read that moved `oor`, or an out-of-bound field is a third
// answer, which is what this refuses.
//
// REFUSE IS TOTAL, and the sweep holds it on every refusal: the destination is
// preloaded with the lawful record, so a refusal answered before any record was
// decoded (the header, the layout, the record's hash, or damage) must leave it
// at its declared default (R4). Content checks obey the same whole-batch boundary.
//
// THE SANITIZER HALF of §7 item 5 is what the C++ reference gets from ASan+UBSan.
// JavaScript cannot step outside a Uint8Array: an in-range DataView offset is
// always a value and an out-of-range one throws a RangeError, which this catches
// and reports as a third answer.
function fx1HostileIssue(v) {
  if (!(v.renamed >= 0 && v.renamed <= 1000)) { return `renamed ${v.renamed}`; }
  if (!(v.gone >= 0 && v.gone <= 1000)) { return `gone ${v.gone}`; }
  if (!(v.nested.a >= 0 && v.nested.a <= 1000)) { return `nested.a ${v.nested.a}`; }
  if (!(v.nested.b >= 0 && v.nested.b <= 1000)) { return `nested.b ${v.nested.b}`; }
  if (!(v.labelLength >= 0 && v.labelLength <= 8)) { return `label_length ${v.labelLength}`; }
  if (!(v.marksCount >= 0 && v.marksCount <= 4)) { return `marks_count ${v.marksCount}`; }
  if (!(v.blobLength >= 0 && v.blobLength <= 6)) { return `blob_length ${v.blobLength}`; }
  return null;
}

export function hostileByteSweep(check, fx1, fx1home) {
  const R = fx1home.TableFixedRefusal;
  const clean = new fx1home.FxRoot();
  clean.keep = 4242; clean.narrow = 40000; clean.renamed = 321; clean.gone = 654;
  clean.nested.a = 111; clean.nested.b = 222;
  clean.labelLength = setText(clean.label, "fx1");
  clean.marks[0] = 101; clean.marks[1] = 202; clean.marksCount = 2;
  clean.blob[0] = 0xDE; clean.blob[1] = 0xAD; clean.blob[2] = 0xBE; clean.blob[3] = 0xEF;
  clean.blobLength = 4;
  const file = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([clean], 1, file) === file.length,
    "P3 hostile bytes: the lawful record saves");

  // CHECK covers the complete batch before LAND, so content and framing
  // refusals alike preserve the caller's previously loaded value.
  const fresh = new fx1home.FxRoot();

  // THE POSITIVE CONTROL, and it is the rule's own legitimate value: the
  // UNFLIPPED record must read clean, in bound, with no counter moved. A sweep
  // whose refusals fired on this would be testing something else entirely.
  const lawful = [new fx1home.FxRoot()];
  {
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(lawful, 1, file, file.length, fx1.FxRootFixedNewPlan(), r);
    check(n === 1 && r.refused === 0 && r.oor === 0 && r.widened === 0,
      `P3 hostile bytes: the lawful record reads clean (n ${n}, refused ${r.refused}, oor ${r.oor})`);
    check(fx1HostileIssue(lawful[0]) === null,
      `P3 hostile bytes: the lawful record lands every field in bound (${fx1HostileIssue(lawful[0])})`);
    check(deepDiff(lawful[0], fresh) !== null,
      "P3 hostile bytes: the lawful record differs from a fresh one, so a reset is visible");
  }
  // R4: a refusal hands the preloaded row back at its declared default
  const reset = structuredClone(lawful[0]);
  fx1home.InitFxRoot(reset);
  // a destination preloaded with the lawful record, by the reader itself
  const preloaded = () => {
    const v = [new fx1home.FxRoot()];
    fx1.FxRootFixedLoad(v, 1, file, file.length, fx1.FxRootFixedNewPlan(), new fx1home.TableFixedReport());
    return v;
  };

  const POISON = [0x00, 0x01, 0x02, 0x7f, 0x80, 0xff];
  let named = 0, landed = 0, threw = 0, divergences = 0;
  const seen = new Map(); // refusal name -> count
  const fail = (msg) => { divergences++; if (divergences <= 20) { check(false, msg); } };
  for (let at = 0; at < file.length; at++) {
    for (const p of POISON) {
      if (file[at] === p) { continue; } // not a flip; the clean byte is already this
      const hit = Uint8Array.from(file);
      hit[at] = p;
      const back = preloaded();
      const r = new fx1home.TableFixedReport();
      const where = `byte ${at} -> 0x${p.toString(16)}`;
      let n;
      try {
        n = fx1.FxRootFixedLoad(back, 1, hit, hit.length, fx1.FxRootFixedNewPlan(), r);
      } catch (e) {
        threw++;
        fail(`P3 hostile bytes: ${where} THREW ${e.name}: ${e.message}`);
        continue;
      }
      if (n < 0) {
        if (r.refused === 0) {
          fail(`P3 hostile bytes: ${where} answered a THIRD way: n ${n} with no reason named`);
          continue;
        }
        if ("damaged" in r) {
          fail(`P3 hostile bytes: ${where} grew a malformed flag the fixed form's report does not have (§5.3)`);
        }
        named++;
        const name = fx1home.TableFixedRefusalName(r.refused);
        seen.set(name, (seen.get(name) || 0) + 1);
        if (r.oor !== 0 || r.widened !== 0 || r.unknown !== 0 || r.mismatch !== 0 || r.repeated !== 0) {
          fail(`P3 hostile bytes: ${where} refused and still moved a counter (oor ${r.oor}, widened ${r.widened})`);
        }
        const d = deepDiff(back[0], reset);
        if (d !== null) {
          fail(`P3 hostile bytes: ${where} refused ${fx1home.TableFixedRefusalName(r.refused)} and the destination is not at its declared default (differs at ${d})`);
        }
        continue;
      }
      landed++;
      if (n !== 1 || r.refused !== 0 || r.oor !== 0 || r.widened !== 0) {
        fail(`P3 hostile bytes: ${where} read n ${n} but is not clean (refused ${r.refused}, oor ${r.oor}, widened ${r.widened})`);
      }
      const issue = fx1HostileIssue(back[0]);
      if (issue !== null) {
        fail(`P3 hostile bytes: ${where} landed OUT OF BOUND: ${issue}`);
      }
    }
  }
  // NOT VACUOUS: each answer must actually have been SEEN — a clean read and a
  // refusal by name — and each refusal the strict read owes in place of a
  // clamp, and the framing refusal a flipped reserved byte owes, must have
  // fired on this file at least once.
  check(named > 0, `P3 hostile bytes: the sweep saw a named refusal (${named})`);
  check(landed > 0, `P3 hostile bytes: the sweep saw clean reads (${landed})`);
  for (const reason of [R.CountOverBound, R.LengthOverBound, R.OutOfRange, R.ReservedNotZero]) {
    const name = fx1home.TableFixedRefusalName(reason);
    check((seen.get(name) || 0) > 0, `P3 hostile bytes: the sweep saw the strict refusal ${name} (${seen.get(name) || 0})`);
  }
  check(divergences === 0, `P3 hostile bytes: ${divergences} divergences`);
  console.log(`P3 hostile bytes: ${file.length} bytes x ${POISON.length} poisons — ${named} refused by name ` +
    `(${[...seen].map(([k, c]) => `${k} ${c}`).join(", ")}), ${landed} clean reads, ` +
    `${threw} throws, ${divergences} divergences`);
}

// HOLE PREFILL, THE IDENTITY HALF: §5.3 step 8's own text, "identity's hole list
// is empty", because the identity plan is ONE COPY of the whole body and there is
// nothing left unwritten to prefill. There is no identity FLAG in the record loop
// — THE EMPTY LIST IS WHAT SKIPS THE WORK — so this assertion is the only thing
// standing between that and a prefill that runs on every identity read. It walks
// no stranger's layout: the plan, the cover and the holes are all this build's own.
function holePrefillIdentity(check, fx1, fx1home) {
  const identity = new Int32Array([0, 0, 0, fx1.FxRootFixedBodyBytes, 0, -1, 0, 0, 1]);
  const idPlan = fx1.FxRootFixedNewPlan();
  const idHoles = fx1home.TableFixedHoles(identity, 1, idPlan.cover, idPlan.holes);
  check(idHoles === 0, `hole prefill: identity's hole list is empty, got ${idHoles}`);
}

// BYTES(N) IS AN ARRAY ROW AND NOT A TEXT ROW (docs/SPEC-TABLES.md §3, §3.4;
// Johnny's fix on the Elixir leg, schema#835). On a TEXT row `dst` is the used
// LENGTH and `aux` is the buffer; on an ARRAY row it is the other way round —
// `dst` is the element base and `aux` is where the count lands — and the plan
// compiler's array case (`case 14`) reads the ARRAY pair. `bytes(N)` rides as
// a counted array of u8, so its destination row is the array's.
//
// The IDENTITY path cannot see the difference: it lands the whole body in one
// COPY and reads neither column. Only a COMPILED plan walks the row, which is
// why a text-convention row survived the Elixir leg's own gates until a
// compiled read landed the count inside the buffer and the first four content
// bytes inside the used length. THE INSTRUMENT IS THE TWO PATHS AGAINST EACH
// OTHER, over the same bytes: identity and compiled must land one blob.
//
// FX2's `blob` is the same field at the same wire id, so an FX2 reader over an
// FX1 record is the compiled path over a bytes(N) row this port wrote itself.
//
// THE SPLIT (§5.6, §5.9 #31): the INSTRUMENT here is the two paths against each
// other, and the compiled path is a stranger's layout, so on this driver the
// instrument is retired; the compiled half is held on the lineage harness
// (TestJSFixedVersioningBytesAndDefaults, lawful). What §5 does not touch, and
// what runs here, is the IDENTITY path's own landing: this leg writes a partly
// used `bytes(N)` with its own writer, reads it back through its own identity
// plan, and the used length, the content and the slack are all its own facts. A
// swapped destination pair is not what this half catches; a writer or an identity
// reader that lost the used length is.
function bytesArrayConventionIdentity(check, fx1, fx1home) {
  const one = new fx1home.FxRoot();
  one.keep = 4242; one.narrow = 40000; one.renamed = 321; one.gone = 654;
  one.nested.a = 111; one.nested.b = 222;
  one.labelLength = setText(one.label, "fx1");
  one.marks[0] = 101; one.marks[1] = 202; one.marksCount = 2;
  one.blob[0] = 0xDE; one.blob[1] = 0xAD; one.blob[2] = 0xBE; one.blob[3] = 0xEF;
  one.blobLength = 4;

  const w1 = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([one], 1, w1) === w1.length,
    "bytes(N) identity: FX1 save of a partly-used blob");

  const ident = [new fx1home.FxRoot()];
  const ri = new fx1home.TableFixedReport();
  check(fx1.FxRootFixedLoad(ident, 1, w1, w1.length, fx1.FxRootFixedNewPlan(), ri) === 1,
    "bytes(N) identity: the identity path reads the record");
  check(ri.refused === 0,
    "bytes(N) identity: the identity read moves no refusal counter");
  check(ident[0].blobLength === 4,
    `bytes(N) identity: the used length lands, got ${ident[0].blobLength}`);
  check(ident[0].blob[0] === 0xDE && ident[0].blob[1] === 0xAD &&
    ident[0].blob[2] === 0xBE && ident[0].blob[3] === 0xEF,
    `bytes(N) identity: the blob is the one that was written — got ` +
    `[${ident[0].blob[0]}, ${ident[0].blob[1]}, ${ident[0].blob[2]}, ${ident[0].blob[3]}], want [222, 173, 190, 239]`);
  check(ident[0].blob[4] === 0 && ident[0].blob[5] === 0,
    "bytes(N) identity: the slack behind the used length is zero");
  // W11 ASSERTION: bytes(N) is layout kind 14 (Array), not kind 12 (String),
  // and it takes the ARRAY's plan row (count at aux, not at dst).
  // Entry 11 is blob, Entry 12 is its element (u8).
  const LAYOUT_AT = 20; const ENTRY_BYTES = 17; const KIND_AT = 8;
  const blobEntryAt = LAYOUT_AT + 4 + 17 * 11;
  const elemEntryAt = LAYOUT_AT + 4 + 17 * 12;
  check(w1[blobEntryAt + KIND_AT] === 14,
    `bytes(N) is layout kind 14 (Array), not kind 12 (String): got ${w1[blobEntryAt + KIND_AT]}`);
  check(w1[elemEntryAt + KIND_AT] === 6,
    `bytes(N) element is kind 6 (u8): got ${w1[elemEntryAt + KIND_AT]}`);
}

// setText lays a string(N) member down the way every port must: the bytes,
// then the USED LENGTH beside them.
function setText(buf, s) {
  buf.fill(0);
  for (let i = 0; i < s.length; i++) { buf[i] = s.charCodeAt(i); }
  return s.length;
}

// fo1Values rebuilds, field for field, the four records
// test/js-tables/fixedoptional_corpus.cpp states in its own comments. Nothing
// is derived from the file: if these disagree with the reference the byte
// comparison below says so, which is the entire point of writing them twice.
function fo1Values(home) {
  const v = Array.from({ length: 4 }, () => new home.OptRoot());

  // 0 — EVERY OPTIONAL PRESENT, the union on its first arm
  v[0].nameLength = setText(v[0].name, "all");
  v[0].plain = 101;
  v[0].optPresent = true;
  v[0].opt.v = 11;
  v[0].opt.tagLength = setText(v[0].opt.tag, "op");
  v[0].numberPresent = true;
  v[0].number = 12;
  v[0].markPresent = true;
  v[0].mark = 2; // Gold — the ordinal is the variant's POSITION IN THE BLOCK, from 1
  v[0].wrap.n = 13;
  v[0].wrap.leafPresent = true;
  v[0].wrap.leaf.v = 14;
  v[0].wrap.leaf.tagLength = setText(v[0].wrap.leaf.tag, "wl");
  v[0].effect.type = 1; // Boost
  v[0].effect.boost.power = 15;
  v[0].tail = 16;

  // 1 — EVERY OPTIONAL ABSENT, the union on its second arm. Nothing behind a
  // zero flag is set, so every absent payload here is the value's CONSTRUCTION
  // state, which is the declared defaults — and that is what makes this record
  // byte-comparable at all (see optionalBytes below).
  v[1].nameLength = setText(v[1].name, "none");
  v[1].plain = 201;
  v[1].wrap.n = 202;
  v[1].effect.type = 2; // Ward
  v[1].effect.ward.charge = 0.5;
  v[1].tail = 203;

  // 2 — MIXED, the union at None: the ROOT's optional absent while the NESTED
  // table's is present, which is the pair that catches a present byte placed at
  // the root's offsets instead of the nested body's
  v[2].nameLength = setText(v[2].name, "mix");
  v[2].plain = 301;
  v[2].optPresent = false;
  v[2].numberPresent = true;
  v[2].number = -302;
  v[2].markPresent = true;
  v[2].mark = 1; // Bronze
  v[2].wrap.n = 303;
  v[2].wrap.leafPresent = true;
  v[2].wrap.leaf.v = -304;
  v[2].wrap.leaf.tagLength = setText(v[2].wrap.leaf.tag, "deep");
  v[2].tail = 305;

  // 3 — THE OTHER WAY ROUND: the root's present, the nested table's absent
  v[3].nameLength = setText(v[3].name, "root only");
  v[3].plain = 401;
  v[3].optPresent = true;
  v[3].opt.v = 402;
  v[3].opt.tagLength = setText(v[3].opt.tag, "r");
  v[3].numberPresent = false;
  v[3].markPresent = false;
  v[3].wrap.n = 403;
  v[3].wrap.leafPresent = false;
  v[3].effect.type = 2; // Ward
  v[3].effect.ward.charge = -1.25;
  v[3].tail = 404;
  return v;
}

function firstDiff(a, b) {
  const n = a.length < b.length ? a.length : b.length;
  for (let i = 0; i < n; i++) { if (a[i] !== b[i]) { return i; } }
  return a.length === b.length ? -1 : n;
}

function optionalBytes(check, o, dir) {
  const { fo1, fo1home, p1, p1home, p3, p3home } = o;

  // ---- THE WRITE: the reference's bytes, and not one of them different ----
  //
  // THE PAYLOAD BEHIND A ZERO FLAG IS WRITTEN TOO, from the value's own
  // storage, because that is the line the reference writes (its
  // emitFixedWriteField has no branch between the present byte and the
  // payload) and this form's rule is that the reference goes first and every
  // other port matches its BYTES. Both ports agree on record 1 above because
  // both spell a fresh value's storage the same way — the declared defaults —
  // so `opt`'s absent payload carries Leaf's own `v = 7` in BOTH files. If
  // that were not so, this comparison would be the thing that said it.
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "fo1.bin")));
    const out = new Uint8Array(fo1.OptRootFixedMeasure(4));
    check(fo1.OptRootFixedSave(fo1Values(fo1home), 4, out) === ref.length,
      `optionals: Save wrote the reference's ${ref.length} bytes`);
    const at = firstDiff(out, ref);
    check(at < 0, at < 0
      ? "optionals: the JavaScript writer's bytes are IDENTICAL to the C++ reference's"
      : `optionals: first byte differing from the C++ reference at ${at} (js ${out[at]}, cpp ${ref[at]})`);
    check(fo1.OptRootFixedBodyBytes === 72,
      `optionals: the body is the reference's 72 bytes (got ${fo1.OptRootFixedBodyBytes})`);
  }

  // ---- THE READ: the reference's own file, through the identity plan ----
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "fo1.bin")));
    const back = Array.from({ length: 4 }, () => new fo1home.OptRoot());
    const r = new fo1home.TableFixedReport();
    check(fo1.OptRootFixedLoad(back, 4, ref, ref.length, fo1.OptRootFixedNewPlan(), r) === 4,
      "optionals: the reference's four records read");
    check(r.oor === 0 && r.unknown === 0 && r.mismatch === 0 && r.refused === 0,
      "optionals: a clean read of the reference's own corpus moves no counter");

    check(back[0].optPresent && back[0].opt.v === 11 && back[0].opt.tagLength === 2 &&
      back[0].opt.tag[0] === 0x6f && back[0].opt.tag[1] === 0x70,
      "optionals: a PRESENT nested type lands its whole payload");
    check(back[0].numberPresent && back[0].number === 12, "optionals: a PRESENT scalar lands");
    check(back[0].markPresent && back[0].mark === 2, "optionals: a PRESENT enum lands its ordinal");
    check(back[0].wrap.n === 13 && back[0].wrap.leafPresent && back[0].wrap.leaf.v === 14,
      "optionals: a present flag INSIDE A NESTED TABLE lands at the nested body's own offset");
    check(back[0].effect.type === 1 && back[0].effect.boost.power === 15 && back[0].tail === 16,
      "optionals: the fields BEHIND the optionals land, so every present byte was counted in the layout");

    // AN ABSENT FIELD READS ITS DECLARED DEFAULT, never the bytes behind the
    // flag: `Leaf.v = 7` and `Wrap.leaf`'s the same
    check(!back[1].optPresent && back[1].opt.v === 7 && back[1].opt.tagLength === 0,
      "optionals: an ABSENT nested type reads absent, at its declared defaults");
    check(!back[1].numberPresent && back[1].number === 0, "optionals: an ABSENT scalar reads absent");
    check(!back[1].markPresent && back[1].mark === 0, "optionals: an ABSENT enum reads absent, at None");
    check(back[1].wrap.n === 202 && !back[1].wrap.leafPresent && back[1].wrap.leaf.v === 7,
      "optionals: an ABSENT flag inside a nested table reads absent");
    check(back[1].effect.type === 2 && back[1].effect.ward.charge === 0.5 && back[1].tail === 203,
      "optionals: the fields behind four absent optionals still land");

    check(!back[2].optPresent && back[2].numberPresent && back[2].number === -302 &&
      back[2].markPresent && back[2].mark === 1 &&
      back[2].wrap.leafPresent && back[2].wrap.leaf.v === -304 && back[2].wrap.leaf.tagLength === 4,
      "optionals: root ABSENT and nested PRESENT, in the same record");
    check(back[2].effect.type === 0 && back[2].tail === 305,
      "optionals: a union at None beside the mixed optionals");
    check(back[3].optPresent && back[3].opt.v === 402 && !back[3].numberPresent && !back[3].markPresent &&
      !back[3].wrap.leafPresent && back[3].wrap.n === 403 && back[3].tail === 404,
      "optionals: root PRESENT and nested ABSENT, the other way round");
  }

  // ---- P3's OWN CORPUS: present then absent, the reference's bytes ----
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "p3.bin")));
    const two = [new p3home.Chain(), new p3home.Chain()];
    two[0].nameLength = setText(two[0].name, "chain");
    two[0].linkPresent = true;
    two[0].link.value = 500;
    two[0].link.tagLength = setText(two[0].link.tag, "v");
    two[1].nameLength = setText(two[1].name, "bare");
    two[1].linkPresent = false;
    const out = new Uint8Array(p3.ChainFixedMeasure(2));
    check(p3.ChainFixedSave(two, 2, out) === ref.length, "P3: Save wrote the reference's byte count");
    const at = firstDiff(out, ref);
    check(at < 0, at < 0
      ? "P3: the JavaScript writer's bytes are IDENTICAL to the reference's p3.bin"
      : `P3: first byte differing from the reference at ${at} (js ${out[at]}, cpp ${ref[at]})`);

    const back = [new p3home.Chain(), new p3home.Chain()];
    const r = new p3home.TableFixedReport();
    check(p3.ChainFixedLoad(back, 2, ref, ref.length, p3.ChainFixedNewPlan(), r) === 2,
      "P3: the reference's two records read");
    check(back[0].linkPresent && back[0].link.value === 500 && back[0].nameLength === 5,
      "P3: the present record lands its payload");
    check(!back[1].linkPresent && back[1].link.value === 0 && back[1].nameLength === 4,
      "P3: the absent record lands absent, payload at its declared default");
  }

  // ---- P1's, which nests the SAME type BY VALUE ----
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "p1.bin")));
    const one = [new p1home.Chain()];
    one[0].nameLength = setText(one[0].name, "chain");
    one[0].link.value = 500;
    one[0].link.tagLength = setText(one[0].link.tag, "v");
    const out = new Uint8Array(p1.ChainFixedMeasure(1));
    check(p1.ChainFixedSave(one, 1, out) === ref.length, "P1: Save wrote the reference's byte count");
    const at = firstDiff(out, ref);
    check(at < 0, at < 0
      ? "P1: the JavaScript writer's bytes are IDENTICAL to the reference's p1.bin"
      : `P1: first byte differing from the reference at ${at} (js ${out[at]}, cpp ${ref[at]})`);
    // and a `?T` body is EXACTLY ONE BYTE longer than the `T` it wraps, which
    // is the whole of §3.4's departure from §2.3
    check(p3.ChainFixedBodyBytes === p1.ChainFixedBodyBytes + 1,
      `?Link's body is Link's plus the present byte (${p1.ChainFixedBodyBytes} -> ${p3.ChainFixedBodyBytes})`);
  }
}

// ---------------------------------------------------------------------------
// C7 — THE RANGED SCALAR, the hostile half (docs/FIXED-FORM-ALGORITHM.md §4.6,
// §5.4; the reference's bounds_case, item 1, test/tables/fixedform_main.cpp).
// ---------------------------------------------------------------------------
//
// A fixed record is a positional image and the one read loop moves bytes: it
// asks nothing about what they mean. The min and max a `plain int32 | min, max`
// declares are therefore held by the DECODE — the identity plan is ONE copy of
// the whole body. A record under this build's own hash still arrives from a
// writer this reader cannot vouch for, so under the strict read an
// out-of-range value REFUSES out_of_range: n = -1, the destination is handed
// back at its declared default, and no counter moves. `plain` is `int32 = 5 | min = 0, max = 1000`.
//
// The JS writer refuses invalid values before output (§5.1). Save a lawful
// record and forge the scalar's wire bytes, so the reader sees a real form-3
// header and the intended range violation.
//
// The COMPILED half — a plan carrying an OLD writer's bound — is a stranger's
// layout and is owed on the lineage harness; only the identity half runs here.
export function rangedScalarClamp(check, o) {
  const { fo1, fo1home } = o;

  // Save one lawful record, forge plain at its independent schema offset,
  // then read into poisoned storage. Tail also checks the neighboring field.
  const read = (planted) => {
    const one = new fo1home.OptRoot();
    one.plain = 5;
    one.tail = 203;
    const w = new Uint8Array(fo1.OptRootFixedMeasure(1));
    check(fo1.OptRootFixedSave([one], 1, w) === w.length,
      "C7 RANGED: the lawful record saves");
    // name string(12) occupies its i32 count plus twelve bytes; plain follows.
    const body = HEADER_BYTES + 4 + fo1.OptRootFixedLayoutBytes + 8;
    new DataView(w.buffer, w.byteOffset, w.length).setInt32(body + 4 + 12, planted, true);
    const back = [new fo1home.OptRoot()];
    poisonDeep(back[0]);
    const before = structuredClone(back[0]);
    fo1home.InitOptRoot(before); // R4: a refusal hands the row back at its declared default
    const r = new fo1home.TableFixedReport();
    const n = fo1.OptRootFixedLoad(back, 1, w, w.length, fo1.OptRootFixedNewPlan(), r);
    return { n, v: back[0], r, before };
  };

  // OFF EITHER END: the declaration's own answer is a refusal by name.
  const refuses = (planted) => {
    const { n, v, r, before } = read(planted);
    const invalid = new fo1home.OptRoot(); invalid.plain = planted;
    const untouched = new Uint8Array(fo1.OptRootFixedMeasure(1)).fill(0xa5);
    check(fo1.OptRootFixedSave([invalid], 1, untouched) === -1 && untouched.every((b) => b === 0xa5),
      `C7 RANGED: writer refuses ${planted} before output`);
    check(n === -1 && r.refused === fo1home.TableFixedRefusal.OutOfRange,
      `C7 RANGED: a value of ${planted} refuses out_of_range (n ${n}, refused ${fo1home.TableFixedRefusalName(r.refused)})`);
    check(r.oor === 0 && r.widened === 0,
      `C7 RANGED: a value of ${planted} moves no counter (oor ${r.oor}, widened ${r.widened})`);
    const d = deepDiff(v, before);
    check(d === null, `C7 RANGED: a value of ${planted} hands the destination back at its declared default (differs at ${d})`);
  };
  // IN RANGE: the value stands, the neighbour lands, nothing moves.
  const reads = (planted) => {
    const { n, v, r } = read(planted);
    check(n === 1 && r.refused === 0 && r.oor === 0 && v.plain === planted,
      `C7 RANGED: a value of ${planted} reads as itself and moves no counter (n ${n}, refused ${r.refused}, got ${v.plain})`);
    check(v.tail === 203,
      `C7 RANGED: a value of ${planted} leaves the neighbour behind it untouched (got ${v.tail})`);
  };

  refuses(5000);  // past max
  refuses(1001);  // one past max
  refuses(-7);    // under min
  refuses(-1);    // one under min
  // THE POSITIVE CONTROL: both legitimate ends read and move no counter, so a
  // refusal that fired on a legal value would be visible.
  reads(1000);    // the legitimate max
  reads(0);       // the legitimate min
}

// ---------------------------------------------------------------------------

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  // every check is counted: the last line is "ran N, failed M", which
  // tools/mustrun holds against the count the recipe registers — so a section
  // that stops running is red, and so is this file doing nothing because the
  // guard above did not fire
  let ran = 0;
  let failed = 0;
  const check = (ok, what) => {
    ran++;
    if (!ok) {
      console.log("FAILED: " + what);
      failed++;
    }
  };
  const generated = process.argv[2] ?? "build/js-fixed";
  const corpus = process.argv[3] ?? "build/js-fixed-corpus";
  const optional = process.argv[4] ?? "build/js-fixed-optional";
  const oracle = process.argv[5] ?? "build/fixedform-corpus";
  await checkFixedForm(check, generated, corpus, optional, oracle);
  if (failed !== 0 || ran === 0) {
    console.log("FAILED");
    console.log(`ran ${ran}, failed ${failed}`);
    process.exit(1);
  }
  console.log("tables JS fixed form: the vocabulary block and its hash are the C++ " +
    "reference's byte for byte, all 64 paired records read to the values the reference " +
    "states and write back IDENTICAL to its corpus, and the OPTIONALS match the " +
    "reference at every place a present byte can ride. THE VERSIONING CONFORMANCE " +
    "MOVED: a fixed table reads BACKWARD and never forward (§5), and the rows are " +
    "held by internal/codegen/jstable/fixedversioning_test.go over the reference\'s " +
    "own corpus — and the identity halves run here, so the reference\'s own " +
    "fx1.bin still saves back identical, \"the slack is zero\" is still checked over " +
    "CONSTRUCTED storage, and §5.3 step 8\'s empty identity hole list is still " +
    "asserted here and not only owed");
  console.log("OK");
  console.log(`ran ${ran}, failed ${failed}`);
}
