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
//   3. THE VERSIONING CONFORMANCE, which is the invariant §3.4 states before
//      any wire: the form is positional BY PLAN, and the positions are the
//      WRITER's block, never the reader's own layout. The FX1/FX2 pair is the
//      C++ leg's own (test/tables/fixedform_main.cpp) and this is its twin —
//      a widened field, a renamed one, an unknown field, an unknown nested
//      TYPE, and a missing field taking its declared default, every one of
//      them through THE SAME LOOP over a plan compiled from the other side's
//      block. Beside them the NEGATIVE CONTROLS: a wrong plan must come out
//      wrong, and every refusal must be by name and never damage.

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

export async function checkFixedForm(check, generated, corpusDir, optionalDir, oracleDir) {
  const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

  const bench = await load("bench/BenchTable.js");
  const fixed = await load("bench/FixedTableTable.js");
  const fx1 = await load("fx1/FX1Table.js");
  const fx2 = await load("fx2/FX2Table.js");
  const fx1home = await load("fx1/Tblfx1Table.js");
  const fx2home = await load("fx2/Tblfx2Table.js");
  const ut1 = await load("ut1/UT1Table.js");
  const ut2 = await load("ut2/UT2Table.js");
  const ut1home = await load("ut1/Tblut1Table.js");
  const ut2home = await load("ut2/Tblut2Table.js");
  const ut1types = await load("ut1/UT1.js");
  const ut2types = await load("ut2/UT2.js");

  await pairedCorpus(check, bench, fixed, corpusDir);
  forgedCounts(check, bench, fixed, corpusDir);
  versioning(check, fx1, fx2, fx1home, fx2home);
  liveCountSlack(check, fx1, fx1home);
  holePrefill(check, fx1, fx2, fx1home, fx2home);
  negativeControls(check, fx1, fx2, fx1home, fx2home);
  unionArmText(check, ut1, ut2, ut1home, ut2home, ut1types, ut2types);
  if (oracleDir) {
    referenceOracle(check, fx1, fx2, fx1home, oracleDir);
  }

  if (optionalDir) {
    const o = {
      p1: await load("p1/P1Table.js"), p1home: await load("p1/Tblp1Table.js"),
      p3: await load("p3/P3Table.js"), p3home: await load("p3/Tblp3Table.js"),
      fo1: await load("fo1/FO1Table.js"), fo1home: await load("fo1/Tblfo1Table.js"),
      fo2: await load("fo2/FO2Table.js"), fo2home: await load("fo2/Tblfo2Table.js"),
    };
    optionalBytes(check, o, optionalDir);
    optionalVersioning(check, o, optionalDir);
    optionalControls(check, o, optionalDir);
  }
}

// ---------------------------------------------------------------------------
// THE IDENTITY PATH CLAMPS TOO: a forged count or length never reaches the
// consumer
// ---------------------------------------------------------------------------
//
// THE READ SIDE ALWAYS CHECKS, and "every read path" includes the IDENTITY
// one. A record whose block hashes to this build's own is read as ONE OP_COPY
// of the whole body — no per-field op runs at all — so the compiled plan's own
// `count` op, which clamps, never fires. But the hash is a WIRE IDENTITY and
// not a security claim (the runtime says so itself): a record carrying this
// build's own hash still arrives from a writer this reader cannot vouch for,
// and the count or length it carries is the number the consumer will index
// this reader's own storage by.
//
// So the projection clamps, on BOTH paths, to THIS reader's bound — and counts
// one `clamped` when it fires, which is §4's event and not a refusal: the
// record still reads. Below, each forged value is planted in a record of the
// reference's own corpus, one at a time, and the bound is what comes out.
function forgedCounts(check, bench, fixed, corpusDir) {
  const corpus = new Uint8Array(readFileSync(resolve(corpusDir, "bench_fixed.bin")));
  const head = LAYOUT_AT + fixed.FixedTableFixedLayoutBytes;

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

  const read = (bytes) => {
    const values = [new bench.FixedTable()];
    const report = new bench.TableFixedReport();
    const n = fixed.FixedTableFixedLoad(values, 1, bytes, bytes.length,
      fixed.FixedTableFixedNewPlan(), report);
    return { n, report, v: values[0].Value };
  };

  // THE POSITIVE CONTROL FIRST: an untouched record moves no counter. Without
  // it a clamp that fired on every record would read as a pass below.
  {
    const r = read(oneRecord());
    check(r.n === 1 && r.report.clamped === 0,
      "IDENTITY CLAMP: an untouched record of the reference's corpus clamps nothing");
  }

  const forged = [
    ["entities count", AT_ENTITIES, 9999, 8, (v) => v.EntitiesCount],
    ["entities count, negative", AT_ENTITIES, -1, 0, (v) => v.EntitiesCount],
    ["stats count", AT_STATS, 0x7fffffff, 80, (v) => v.StatsCount],
    ["player_name length", AT_NAME, 9999, 15, (v) => v.PlayerNameLength],
    ["payload length", AT_PAYLOAD, 9999, 16, (v) => v.PayloadLength],
  ];
  for (const [what, at, plant, bound, get] of forged) {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + at, plant, true);
    const r = read(bytes);
    check(r.n === 1 && !r.report.malformed && r.report.refused === 0,
      `IDENTITY CLAMP: a forged ${what} is a clamp and not a refusal — the record still reads`);
    check(get(r.v) === bound,
      `IDENTITY CLAMP: a forged ${what} of ${plant} reaches the consumer as this reader's bound ${bound} (got ${get(r.v)})`);
    check(r.report.clamped === 1,
      `IDENTITY CLAMP: a forged ${what} moves clamped exactly once (got ${r.report.clamped})`);
  }

  // A COUNT ALREADY IN BOUNDS IS NOT TOUCHED, and the counter does not move
  // for it: a clamp that fired on a legal value would be a counter nobody
  // could read.
  {
    const bytes = oneRecord();
    new DataView(bytes.buffer).setInt32(BODY + AT_ENTITIES, 8, true);
    const r = read(bytes);
    check(r.n === 1 && r.v.EntitiesCount === 8 && r.report.clamped === 0,
      "IDENTITY CLAMP: a count AT the bound is not a clamp and moves no counter");
  }

  // A BOOL LANDS AS `byte != 0`, whatever the byte holds — §3.4's own rule,
  // and never damage, so a peer's 2 is true and no counter moves for it.
  {
    const bytes = oneRecord();
    bytes[BODY + AT_HAS_EXTRA] = 2;
    const r = read(bytes);
    check(r.n === 1 && r.v.HasExtra === true && r.report.clamped === 0,
      "IDENTITY CLAMP: a bool byte of 2 lands as true and moves no counter");
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
    if (corpus[LAYOUT_AT + i] !== fixed.FixedTableFixedLayout[i]) { blockSame = false; break; }
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
  check(!report.malformed && report.refused === 0, "fixed form: the reference's own file is not refused");
  check(report.unknown === 0 && report.kindMismatch === 0 && report.clamped === 0 && report.widened === 0,
    "fixed form: a same-schema read moves no counter");

  // 2. THE VALUES, against the reference's own statement of them
  let bad = 0;
  const eq = (a, b, what, k) => {
    if (a !== b && bad < 8) { check(false, `fixed form: record ${k} ${what}: ${a} !== ${b}`); bad++; }
    return a === b;
  };
  let allEqual = true;
  for (let k = 0; k < Count; k++) {
    const v = values[k].Value, o = oracle[k];
    let ok = true;
    ok = eq(v.Sequence, o.sequence, "sequence", k) && ok;
    ok = eq(v.AckSequence, o.ack_sequence, "ack_sequence", k) && ok;
    ok = eq(v.AckBits, o.ack_bits, "ack_bits", k) && ok;
    ok = eq(v.SessionId, BigInt(o.session_id), "session_id", k) && ok;
    ok = eq(v.ClientId, o.client_id, "client_id", k) && ok;
    ok = eq(v.Nonce, BigInt(o.nonce), "nonce", k) && ok;
    ok = eq(v.WorldTime, BigInt(o.world_time), "world_time", k) && ok;
    ok = eq(v.FrameTick, BigInt(o.frame_tick), "frame_tick", k) && ok;
    ok = eq(v.ServerTime, o.server_time, "server_time", k) && ok;
    ok = eq(v.EntitiesCount, o.entities_count, "entities_count", k) && ok;
    for (let i = 0; i < o.entities_count; i++) {
      const e = v.Entities[i], oe = o.entities[i];
      ok = eq(e.EntityId, oe.entity_id, `entities[${i}].entity_id`, k) && ok;
      ok = eq(e.PosX, oe.pos_x, `entities[${i}].pos_x`, k) && ok;
      ok = eq(e.PosY, oe.pos_y, `entities[${i}].pos_y`, k) && ok;
      ok = eq(e.PosZ, oe.pos_z, `entities[${i}].pos_z`, k) && ok;
      ok = eq(e.Yaw, oe.yaw, `entities[${i}].yaw`, k) && ok;
      ok = eq(e.Pitch, oe.pitch, `entities[${i}].pitch`, k) && ok;
      ok = eq(e.VelX, oe.vel_x, `entities[${i}].vel_x`, k) && ok;
      ok = eq(e.VelY, oe.vel_y, `entities[${i}].vel_y`, k) && ok;
      ok = eq(e.VelZ, oe.vel_z, `entities[${i}].vel_z`, k) && ok;
      ok = eq(e.Health, oe.health, `entities[${i}].health`, k) && ok;
      ok = eq(e.Weapon, oe.weapon, `entities[${i}].weapon`, k) && ok;
      ok = eq(e.Damage, BigInt(oe.damage), `entities[${i}].damage`, k) && ok;
      ok = eq(e.Moving, oe.moving, `entities[${i}].moving`, k) && ok;
      ok = eq(e.Firing, oe.firing, `entities[${i}].firing`, k) && ok;
    }
    ok = eq(v.StatsCount, o.stats_count, "stats_count", k) && ok;
    for (let i = 0; i < o.stats_count; i++) {
      ok = eq(v.Stats[i].StatId, o.stats[i].stat_id, `stats[${i}].stat_id`, k) && ok;
      ok = eq(v.Stats[i].Delta, o.stats[i].delta, `stats[${i}].delta`, k) && ok;
    }
    // the UNION: the tag, then the arm the tag names and only that arm
    ok = eq(v.GameEvent.Type, o.game_event_type, "game_event.type", k) && ok;
    if (o.game_event_type === 1) {
      ok = eq(v.GameEvent.Hit.TargetId, o.hit.target_id, "hit.target_id", k) && ok;
      ok = eq(v.GameEvent.Hit.Damage, o.hit.damage, "hit.damage", k) && ok;
      ok = eq(v.GameEvent.Hit.HitKind, o.hit.hit_kind, "hit.hit_kind", k) && ok;
      ok = eq(v.GameEvent.Hit.Crit, o.hit.crit, "hit.crit", k) && ok;
    } else if (o.game_event_type === 2) {
      ok = eq(v.GameEvent.Chat.Channel, o.chat.channel, "chat.channel", k) && ok;
      ok = eq(v.GameEvent.Chat.Speaker, o.chat.speaker, "chat.speaker", k) && ok;
    } else if (o.game_event_type === 3) {
      ok = eq(v.GameEvent.Pickup.ItemId, o.pickup.item_id, "pickup.item_id", k) && ok;
      ok = eq(v.GameEvent.Pickup.Amount, o.pickup.amount, "pickup.amount", k) && ok;
    }
    for (let i = 0; i < 4; i++) ok = eq(v.Loadout[i], o.loadout[i], `loadout[${i}]`, k) && ok;
    ok = eq(v.PlayerNameLength, o.player_name_length, "player_name_length", k) && ok;
    for (let i = 0; i < 15; i++) ok = eq(v.PlayerName[i], o.player_name[i], `player_name[${i}]`, k) && ok;
    ok = eq(v.PayloadLength, o.payload_length, "payload_length", k) && ok;
    for (let i = 0; i < 16; i++) ok = eq(v.Payload[i], o.payload[i], `payload[${i}]`, k) && ok;
    // FLOATS ARE COMPARED AS BIT PATTERNS: this form carries the IEEE-754 bit
    // pattern with NO canonicalisation, so a NaN payload is part of the value.
    ok = eq(f32bits(v.AimX), o.aim_x_bits, "aim_x", k) && ok;
    ok = eq(f32bits(v.AimY), o.aim_y_bits, "aim_y", k) && ok;
    ok = eq(f32bits(v.AimZ), o.aim_z_bits, "aim_z", k) && ok;
    ok = eq(f32bits(v.Recoil), o.recoil_bits, "recoil", k) && ok;
    ok = eq(f64bits(v.Drift), BigInt(o.drift_bits), "drift", k) && ok;
    // 128-BIT: the low half then the high, which is this wire's order everywhere
    ok = eq(v.WideKey, BigInt(o.wide_key_lo) | (BigInt(o.wide_key_hi) << 64n), "wide_key", k) && ok;
    ok = eq(BigInt.asUintN(128, v.Flux), BigInt(o.flux_lo) | (BigInt(o.flux_hi) << 64n), "flux", k) && ok;
    ok = eq(v.Ping, o.ping, "ping", k) && ok;
    ok = eq(v.CrcHint, o.crc_hint, "crc_hint", k) && ok;
    ok = eq(v.HasExtra, o.has_extra, "has_extra", k) && ok;
    ok = eq(v.Extra, o.extra, "extra", k) && ok;
    ok = eq(v.IdleTicks, o.idle_ticks, "idle_ticks", k) && ok;
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
// 3. THE VERSIONING CONFORMANCE — the twin of test/tables/fixedform_main.cpp
// ---------------------------------------------------------------------------

function versioning(check, fx1, fx2, fx1home, fx2home) {
  // an FX1 record, every field off its default so nothing passes by accident
  const one = new fx1home.FxRoot();
  one.Keep = 4242;
  one.Narrow = 40000;      // a uint16 value the widened read must reproduce
  one.Renamed = 321;
  one.Gone = 654;
  one.Nested.A = 111;
  one.Nested.B = 222;

  const w1 = new Uint8Array(fx1.FxRootFixedMeasure(1));
  check(fx1.FxRootFixedSave([one], 1, w1) === w1.length, "FX1 save");

  // 1. SAME SCHEMA — the identity plan
  {
    const back = [new fx1home.FxRoot()];
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(back, 1, w1, w1.length, fx1.FxRootFixedNewPlan(), r);
    check(n === 1, "same schema: one record");
    check(back[0].Keep === 4242 && back[0].Narrow === 40000 && back[0].Renamed === 321 && back[0].Gone === 654,
      "same schema: the scalars");
    check(back[0].Nested.A === 111 && back[0].Nested.B === 222, "same schema: the nesting");
    check(r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && !r.malformed && r.refused === 0,
      "same schema: a clean read moves no counter");
  }

  // 2, 4, 5. FX2 READS FX1 — a plan compiled from FX1's block
  {
    const back = [new fx2home.FxRoot()];
    const r = new fx2home.TableFixedReport();
    const n = fx2.FxRootFixedLoad(back, 1, w1, w1.length, fx2.FxRootFixedNewPlan(), r);
    check(n === 1, "older writer: one record");
    check(back[0].Keep === 4242, "older writer: an unmoved field");
    check(back[0].Narrow === 40000, "WIDENED: uint16 into uint32, exactly");
    check(r.widened === 1, `WIDENED: one widened counts (got ${r.widened})`);
    check(back[0].RenamedTo === 321, "RENAMED: `was =` keeps the wire id");
    check(back[0].Added === 11, "MISSING: a field the writer does not carry takes its declared default");
    check(back[0].Extra.X === 0 && back[0].Extra.Y === 0, "MISSING: a whole nested type takes its defaults");
    check(back[0].Nested.A === 111 && back[0].Nested.B === 222, "older writer: the nesting");
    check(r.unknown === 1, `older writer: \`gone\` is the one field this reader cannot name (got ${r.unknown})`);
    check(r.kindMismatch === 0 && !r.malformed && r.refused === 0, "older writer: nothing else fired");
  }

  // 3. FX1 READS FX2 — an unknown field and an unknown NESTED TYPE
  {
    const two = new fx2home.FxRoot();
    two.Keep = 5150;
    two.Narrow = 70000;
    two.RenamedTo = 808;
    two.Added = 909;
    two.Nested.A = 33;
    two.Nested.B = 44;
    two.Extra.X = 55;
    two.Extra.Y = 66;
    const w2 = new Uint8Array(fx2.FxRootFixedMeasure(1));
    check(fx2.FxRootFixedSave([two], 1, w2) === w2.length, "FX2 save");

    const back = [new fx1home.FxRoot()];
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(back, 1, w2, w2.length, fx1.FxRootFixedNewPlan(), r);
    check(n === 1, "newer writer: one record");
    check(back[0].Keep === 5150, "newer writer: an unmoved field lands past the unknowns");
    check(back[0].Renamed === 808, "newer writer: `was =` reads the other way too");
    check(back[0].Gone === 9, "newer writer: a field the writer dropped takes its declared default");
    check(back[0].Nested.A === 33 && back[0].Nested.B === 44,
      "newer writer: the nesting lands past the unknown type");
    // `added` is an unknown FIELD; `extra` is an unknown nested TYPE, and
    // stepping over it by its block size is what puts `nested` in the right
    // place above
    check(r.unknown === 2, `newer writer: two names this reader does not have (got ${r.unknown})`);
    // narrow is uint32 THERE and uint16 HERE: coming back DOWN the ladder is a
    // kind that MOVED, reported and never reinterpreted
    check(r.kindMismatch === 1, `newer writer: the narrowed field is a kind mismatch (got ${r.kindMismatch})`);
    check(back[0].Narrow === 3, "newer writer: a narrowed field keeps its declared default, never a truncation");
  }

  // THE PLAN IS COMPILED ONCE PER PEER, NOT ONCE PER RECORD, and the same plan
  // handed back is reused: the cache is by hash and the hash is the block's.
  {
    const many = Array.from({ length: 4 }, () => {
      const v = new fx1home.FxRoot();
      v.Keep = 1; v.Narrow = 2; v.Renamed = 3; v.Gone = 4; v.Nested.A = 5; v.Nested.B = 6;
      return v;
    });
    const buf = new Uint8Array(fx1.FxRootFixedMeasure(4));
    check(fx1.FxRootFixedSave(many, 4, buf) === buf.length, "FX1 save: four records");
    const plan = fx2.FxRootFixedNewPlan();
    const back = Array.from({ length: 4 }, () => new fx2home.FxRoot());
    const r = new fx2home.TableFixedReport();
    check(fx2.FxRootFixedLoad(back, 4, buf, buf.length, plan, r) === 4, "cached plan: four records");
    check(plan.ready === true, "cached plan: the plan is marked ready for its hash");
    const compiled = plan.count;
    const r2 = new fx2home.TableFixedReport();
    check(fx2.FxRootFixedLoad(back, 4, buf, buf.length, plan, r2) === 4, "cached plan: read again on the same plan");
    check(plan.count === compiled, "cached plan: the second read compiled nothing new");
    check(r2.widened === 4, `cached plan: one widened per record (got ${r2.widened})`);
  }
}

// ---------------------------------------------------------------------------
// THE NEGATIVE CONTROLS
// ---------------------------------------------------------------------------

function negativeControls(check, fx1, fx2, fx1home, fx2home) {
  const one = new fx1home.FxRoot();
  one.Keep = 4242; one.Narrow = 40000; one.Renamed = 321; one.Gone = 654;
  one.Nested.A = 111; one.Nested.B = 222;
  const w1 = new Uint8Array(fx1.FxRootFixedMeasure(1));
  fx1.FxRootFixedSave([one], 1, w1);

  // THE NEGATIVE CONTROL §3.4 NAMES: A READER GIVEN THE WRONG PLAN FOR A
  // RECORD GOES RED. The plan is the whole of this form's safety, so a test
  // that never watched a wrong plan fail is a test that never checked the
  // right one worked. Here the plan compiled for FX1's own block is run
  // against an FX2 record, which is the same loop over the wrong plan.
  {
    const two = new fx2home.FxRoot();
    two.Keep = 5150; two.Narrow = 70000; two.RenamedTo = 808; two.Added = 909;
    two.Nested.A = 33; two.Nested.B = 44; two.Extra.X = 55; two.Extra.Y = 66;
    const w2 = new Uint8Array(fx2.FxRootFixedMeasure(1));
    fx2.FxRootFixedSave([two], 1, w2);

    const right = [new fx1home.FxRoot()];
    const rr = new fx1home.TableFixedReport();
    fx1.FxRootFixedLoad(right, 1, w2, w2.length, fx1.FxRootFixedNewPlan(), rr);

    // now run FX1's IDENTITY plan over the FX2 body — the wrong plan for these
    // bytes — straight through the shared loop, and watch it disagree
    const plan = fx1.FxRootFixedNewPlan();
    const wrong = [new fx1home.FxRoot()];
    const wr = new fx1home.TableFixedReport();
    const body = w2.subarray(LAYOUT_AT + fx2.FxRootFixedLayoutBytes + 8);
    const bodyView = new DataView(body.buffer, body.byteOffset, body.length);
    plan.image.set(new Uint8Array(plan.image.length)); // the prefill
    fx1home.TableFixedRun(new Int32Array([0, 0, 0, fx1.FxRootFixedBodyBytes, 0, -1, 0, 0]),
      1, body, bodyView, 0, plan.image, plan.view, null, wr);
    fx1home.FxRootFixedDecode(wrong[0], plan.view, 0, wr);
    check(wrong[0].Keep !== right[0].Keep || wrong[0].Renamed !== right[0].Renamed ||
      wrong[0].Nested.A !== right[0].Nested.A || wrong[0].Nested.B !== right[0].Nested.B,
      "NEGATIVE CONTROL: the identity plan run over another writer's record comes out WRONG");
  }

  // EVERY REFUSAL IS BY NAME, and none of them is one of §4's six events:
  // nothing is decoded and there is nothing to count.
  const fresh = () => [new fx1home.FxRoot()];
  {
    // THE FORM BYTE SAYS WHICH DIRECTION (§3). The registry is ORDERED, so a
    // byte this reader cannot read is named by WHERE IT SITS relative to this
    // form — and never by one word for all three.
    const bad = Uint8Array.from(w1);
    bad[0] = 4; // §4.2's unknown-form control byte, which is 4 now that 3 is taken
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.NewerForm && !r.malformed,
      "REFUSAL: a form byte NEWER than this build is newer_form, and malformed does not fire");
  }
  {
    const bad = Uint8Array.from(w1);
    bad[0] = 1; // form 1, the variable form: a form that came BEFORE this one
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.PreviousForm && !r.malformed,
      "REFUSAL: form 1, the variable form, is previous_form and NOT newer_form");
  }
  {
    const bad = Uint8Array.from(w1);
    bad[0] = 2; // form 2, the message form: a WIRE where a FILE was expected
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.MessageFormAsFile && !r.malformed,
      "REFUSAL: form 2 handed to a file reader is message_form_as_file, named apart from both");
  }
  {
    const bad = Uint8Array.from(w1);
    // a layout length that overruns the file
    bad[LAYOUT_LENGTH_AT] = 0xff; bad[LAYOUT_LENGTH_AT + 1] = 0xff;
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.LayoutMalformed,
      "REFUSAL: a layout length that overruns the file is layout_malformed");
  }
  {
    // a layout whose ENTRY COUNT does not close its own tree is refused WHOLE
    const bad = Uint8Array.from(w1);
    bad[LAYOUT_AT + 4 + 13] = 0x7f; // the root's child count, which now closes nothing
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r);
    check(n === -1 && r.refused === fx1home.TableFixedRefusal.LayoutMalformed,
      "REFUSAL: a layout whose tree does not close is layout_malformed, and it sets nothing");
  }
  {
    // THE HEADER NAMES THE LAYOUT ONCE, AND IT IS CHECKED LAST (§3): a header
    // whose hash is not the hash of the layout behind it is a lying header,
    // and the layout's OWN rules refuse under their own names first so that a
    // broken layout is never reported as this.
    const bad = Uint8Array.from(w1);
    bad[HASH_AT] ^= 0xff;
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.LayoutMalformed,
      "REFUSAL: a header whose hash is not the layout's is layout_malformed");
  }
  {
    // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS is a refusal by
    // name — never a guess and never damage
    const bad = Uint8Array.from(w1);
    bad[LAYOUT_AT + fx1.FxRootFixedLayoutBytes] ^= 0xff; // the record's own hash
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.NoLayout,
      "REFUSAL: a record whose hash is not the layout's is no_layout");
  }
  {
    // BYTES LEFT OVER ARE malformed — §3's rule, for the same reason
    const bad = new Uint8Array(w1.length + 3);
    bad.set(w1);
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 && r.malformed,
      "REFUSAL: bytes left over past the last whole record are malformed");
  }
  {
    // the caller's capacity is the caller's, and more records than it holds is
    // a refusal by name rather than a write past the end
    const many = Array.from({ length: 3 }, () => one);
    const buf = new Uint8Array(fx1.FxRootFixedMeasure(3));
    fx1.FxRootFixedSave(many, 3, buf);
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, buf, buf.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.BatchTooLarge,
      "REFUSAL: more records than the caller's capacity is batch_too_large");
  }
  {
    // THE PLAN'S STORAGE IS THE CALLER'S, and a plan that does not fit is a
    // refusal by name — this codec never allocates its way out
    const two = new fx2home.FxRoot();
    const w2 = new Uint8Array(fx2.FxRootFixedMeasure(1));
    fx2.FxRootFixedSave([two], 1, w2);
    const tiny = fx1.FxRootFixedNewPlan(1, 4);
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, w2, w2.length, tiny, r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.PlanTooLarge,
      "REFUSAL: a layout whose compiled plan does not fit the caller's storage is plan_too_large");
  }
  {
    // a write that does not fit answers -1 and touches nothing, exactly as the
    // flat packet writer does
    const small = new Uint8Array(fx1.FxRootFixedMeasure(1) - 1);
    check(fx1.FxRootFixedSave([one], 1, small) === -1,
      "REFUSAL: a buffer too small for the file answers -1");
  }
}


// ---------------------------------------------------------------------------
// 4. THE OPTIONALS — §3.4's kind 35, against the C++ reference's own bytes
// ---------------------------------------------------------------------------
//
// ON THE PACKET WIRE `?T` AND A PLAIN `T` ARE THE SAME BITS (SPEC §2.3). ON
// THIS FORM THEY ARE NOT: an optional rides as kind 35, a PRESENT BYTE at the
// field's own offset with the payload WHOLE behind it, so the two spellings are
// one byte apart and every offset behind them moves. That makes the flag's
// position a wire fact, and a wire fact has exactly one honest oracle here —
// the reference's bytes.
//
// OptRoot's body, which is §3.4's arithmetic and nothing else, is what the
// forgeries below index. It is spelled out because a forgery that indexed the
// wrong byte would be testing nothing:
//
//   0..3    name's used length      16..19  plain
//   4..15   name's 12 bytes         20      opt PRESENT
//                                   21..36  opt's Leaf: v, tag length, tag(8)
//   37      num PRESENT             38..41  num
//   42      mark PRESENT            43      mark's ordinal
//   44..47  wrap.n                  48      wrap.leaf PRESENT
//   49..64  wrap.leaf's Leaf        65      effect's tag
//   66..69  effect's widest arm     70..71  tail
const OPT_PRESENT = 20, OPT_V = 21, NUM_PRESENT = 37, MARK_PRESENT = 42;
const WRAP_LEAF_PRESENT = 48, WRAP_LEAF_V = 49, EFFECT_TAG = 65;

// setText lays a string(N) member down the way every port must: the bytes,
// then the USED LENGTH beside them.
// ---------------------------------------------------------------------------
// A TEXT FIELD UNDER A UNION ARM: two facts, two lanes
// ---------------------------------------------------------------------------
//
// A PLAN ENTRY CARRIES TWO FACTS THAT ARE NOT THE SAME FACT: the ordinal the
// GUARD byte must hold for the entry to run, and the ARGUMENT the entry's own
// op takes — for a text entry, its flavour. They had one lane between them, so
// a `string(N)` under a union's arm could be guarded correctly or read with the
// right flavour and could not be both.
//
// Neither failure is quiet. The flavour of a `string(N)` is 1 and UT1's second
// arm's ordinal is 2, so a shared lane does not merely lose one fact — it turns
// a byte string into a WIDE one, halving the bound. And under a compiled plan
// the entry runs only when the tag happens to equal the flavour, which means it
// runs under the WRONG ARM and writes across a sibling's storage, which a union
// shares.
//
// UT2 inserts `mid` IN THE MIDDLE of the union, so the arm carrying the string
// moves from ordinal 2 to ordinal 3 and the read is a COMPILED plan: the guard
// must hold THEIR ordinal while the tag lands MINE, which is the other half of
// the same confusion. The pair is the C++ leg's own (test/tables/UT1.schema,
// UT2.schema) and this is its JavaScript twin.
function unionArmText(check, ut1, ut2, ut1home, ut2home, ut1types, ut2types) {
  const LABEL = "arm-text";
  const textOf = (buf, n) => {
    let out = "";
    for (let i = 0; i < n; i++) { out += String.fromCharCode(buf[i]); }
    return out;
  };

  // 1. UT2 WRITES ARM `b` — ordinal 3 THERE — AND UT1 READS IT AS ORDINAL 2.
  //    The string is guarded by the WRITER's ordinal and lands under the
  //    READER's, and its flavour is neither number.
  {
    const v = new ut2home.UtRoot();
    v.Head = 41;
    v.Pick.Type = ut2types.PickType.B;
    v.Pick.B.LabelLength = setText(v.Pick.B.Label, LABEL);
    v.Pick.B.M = 55;
    v.Tail = 66;
    const buf = new Uint8Array(ut2.UtRootFixedMeasure(1));
    check(ut2.UtRootFixedSave([v], 1, buf) === buf.length, "union-arm text: UT2 wrote its arm-b record");

    const out = [new ut1home.UtRoot()];
    const r = new ut1home.TableFixedReport();
    const n = ut1.UtRootFixedLoad(out, 1, buf, buf.length, ut1.UtRootFixedNewPlan(), r);
    check(n === 1 && !r.malformed, "union-arm text: UT1 read UT2's record through a compiled plan");
    const got = out[0];
    check(got.Pick.Type === ut1types.PickType.B,
      `union-arm text: the tag lands on UT1's OWN ordinal for arm b — got ${got.Pick.Type}, want ${ut1types.PickType.B}`);
    const label = textOf(got.Pick.B.Label, got.Pick.B.LabelLength);
    check(got.Pick.B.LabelLength === LABEL.length && label === LABEL,
      `union-arm text: the arm's string survives BOTH the guard and the flavour — got ${got.Pick.B.LabelLength} ${JSON.stringify(label)}, want ${LABEL.length} ${JSON.stringify(LABEL)}`);
    check(got.Pick.B.M === 55, `union-arm text: the scalar behind the string is 55, got ${got.Pick.B.M}`);
    check(got.Head === 41 && got.Tail === 66,
      `union-arm text: the fields either side of the union are 41/66, got ${got.Head}/${got.Tail}`);
  }

  // 2. UT2 WRITES ARM `a` — ordinal 1, WHICH IS ALSO A BYTE STRING'S FLAVOUR.
  //    This is the sharper half: with one lane between the two facts, arm b's
  //    text entry runs under arm a's tag and smears its payload across a
  //    sibling arm's storage.
  {
    const v = new ut2home.UtRoot();
    v.Head = 71;
    v.Pick.Type = ut2types.PickType.A;
    v.Pick.A.N = -404;
    v.Tail = 77;
    const buf = new Uint8Array(ut2.UtRootFixedMeasure(1));
    check(ut2.UtRootFixedSave([v], 1, buf) === buf.length, "union-arm text: UT2 wrote its arm-a record");

    const out = [new ut1home.UtRoot()];
    const r = new ut1home.TableFixedReport();
    const n = ut1.UtRootFixedLoad(out, 1, buf, buf.length, ut1.UtRootFixedNewPlan(), r);
    check(n === 1 && !r.malformed, "union-arm text: UT1 read UT2's arm-a record");
    const got = out[0];
    check(got.Pick.Type === ut1types.PickType.A,
      `union-arm text: the tag lands on arm a — got ${got.Pick.Type}, want ${ut1types.PickType.A}`);
    check(got.Pick.A.N === -404,
      `union-arm text: arm a's scalar is untouched by arm b's text entry — got ${got.Pick.A.N}, want -404`);
    check(got.Head === 71 && got.Tail === 77,
      `union-arm text: the fields either side of the union are 71/77, got ${got.Head}/${got.Tail}`);
  }

  // 3. AND THE OTHER DIRECTION, so neither side is the only one proved: UT1
  //    writes arm b at ordinal 2, UT2 reads it as ordinal 3.
  {
    const v = new ut1home.UtRoot();
    v.Head = 81;
    v.Pick.Type = ut1types.PickType.B;
    v.Pick.B.LabelLength = setText(v.Pick.B.Label, "back");
    v.Pick.B.M = -12;
    v.Tail = 88;
    const buf = new Uint8Array(ut1.UtRootFixedMeasure(1));
    check(ut1.UtRootFixedSave([v], 1, buf) === buf.length, "union-arm text: UT1 wrote its arm-b record");

    const out = [new ut2home.UtRoot()];
    const r = new ut2home.TableFixedReport();
    const n = ut2.UtRootFixedLoad(out, 1, buf, buf.length, ut2.UtRootFixedNewPlan(), r);
    check(n === 1 && !r.malformed, "union-arm text: UT2 read UT1's record through a compiled plan");
    const got = out[0];
    const label = textOf(got.Pick.B.Label, got.Pick.B.LabelLength);
    check(got.Pick.Type === ut2types.PickType.B && label === "back" && got.Pick.B.M === -12,
      `union-arm text: the arm's string and scalar land the other way round too — tag ${got.Pick.Type} ${JSON.stringify(label)} ${got.Pick.B.M}`);
    check(got.Head === 81 && got.Tail === 88,
      `union-arm text: the fields either side of the union are 81/88, got ${got.Head}/${got.Tail}`);
  }
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
function referenceOracle(check, fx1, fx2, fx1home, oracleDir) {
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
    check(n === 2 && !r.malformed, `reference oracle: fx1.bin holds two records, got ${n}`);
    if (n !== 2) { return; }

    // the values the reference states, checked INDEPENDENTLY of the bytes: a
    // reader and a writer sharing one offset mistake round trip perfectly
    check(v[0].Keep === 4242 && v[0].Narrow === 40000 && v[0].Renamed === 321 && v[0].Gone === 654,
      `reference oracle: fx1 record 0 scalars — got ${v[0].Keep}/${v[0].Narrow}/${v[0].Renamed}/${v[0].Gone}`);
    check(v[0].Nested.A === 111 && v[0].Nested.B === 222,
      `reference oracle: fx1 record 0 nested — got ${v[0].Nested.A}/${v[0].Nested.B}`);
    check(v[0].LabelLength === 3 && textOf(v[0].Label, 3) === "fx1",
      `reference oracle: fx1 record 0 label — got ${v[0].LabelLength} ${JSON.stringify(textOf(v[0].Label, v[0].LabelLength))}`);
    check(v[0].MarksCount === 2 && v[0].Marks[0] === 101 && v[0].Marks[1] === 202,
      `reference oracle: fx1 record 0 marks — got count ${v[0].MarksCount} [${v[0].Marks[0]}, ${v[0].Marks[1]}]`);
    check(v[0].BlobLength === 4 && v[0].Blob[0] === 0xDE && v[0].Blob[1] === 0xAD &&
      v[0].Blob[2] === 0xBE && v[0].Blob[3] === 0xEF,
      `reference oracle: fx1 record 0 blob — got length ${v[0].BlobLength}`);
    // record 1: NOTHING USED AT ALL, which is the slack case in full
    check(v[1].LabelLength === 0 && v[1].MarksCount === 0 && v[1].BlobLength === 0,
      `reference oracle: fx1 record 1 uses nothing — got ${v[1].LabelLength}/${v[1].MarksCount}/${v[1].BlobLength}`);

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
    check(made[0].LabelLength === 2 && textOf(made[0].Label, 2) === "fx",
      `reference oracle: a DECLARED DEFAULT is the value and not just its length — a fresh FxRoot's label is ${made[0].LabelLength} ${JSON.stringify(textOf(made[0].Label, made[0].LabelLength))}, want 2 "fx"`);

    made[0].Keep = 4242; made[0].Narrow = 40000; made[0].Renamed = 321; made[0].Gone = 654;
    made[0].Nested.A = 111; made[0].Nested.B = 222;
    made[0].LabelLength = setText(made[0].Label, "fx1");
    made[0].Marks[0] = 101; made[0].Marks[1] = 202; made[0].MarksCount = 2;
    made[0].Blob[0] = 0xDE; made[0].Blob[1] = 0xAD; made[0].Blob[2] = 0xBE; made[0].Blob[3] = 0xEF;
    made[0].BlobLength = 4;
    made[1].Keep = 1; made[1].Narrow = 2; made[1].Renamed = 3; made[1].Gone = 4;
    made[1].Nested.A = 5; made[1].Nested.B = 6;
    made[1].LabelLength = 0; // nothing used at all: the WHOLE span is slack,
    made[1].MarksCount = 0;  // over storage that still holds the default "fx"
    made[1].BlobLength = 0;

    const built = new Uint8Array(fx1.FxRootFixedMeasure(2));
    check(fx1.FxRootFixedSave(made, 2, built) === file.length,
      "reference oracle: the by-hand records measure the reference's own byte count");
    const bat = firstDiff(built, file);
    check(bat < 0,
      `reference oracle: fx1.bin built from CONSTRUCTED storage is identical to the C++ reference's — the slack is zero — first byte differing from the C++ reference at ${bat}` +
      (bat < 0 ? "" : ` (ours 0x${built[bat].toString(16)}, reference 0x${file[bat].toString(16)})`));
  }

  // ---- fx2.bin: ANOTHER WRITER'S LAYOUT, so the plan path reads it
  {
    const file = read("fx2.bin");
    check(file !== null, "reference oracle: fx2.bin is present");
    if (file === null) { return; }
    const v = [new fx1home.FxRoot()];
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(v, 1, file, file.length, fx1.FxRootFixedNewPlan(), r);
    check(n === 1 && !r.malformed,
      `reference oracle: FX1 reads the reference's FX2 file through a COMPILED plan, got ${n}`);
    if (n !== 1) { return; }
    check(v[0].Keep === 5150, `reference oracle: fx2's keep lands, got ${v[0].Keep}`);
    check(v[0].Renamed === 808, `reference oracle: fx2's renamed_to lands under was, got ${v[0].Renamed}`);
    // narrow is 70000 in FX2 and a uint16 here: a kind that MOVED, not a widening
    check(v[0].Nested.A === 33 && v[0].Nested.B === 44,
      `reference oracle: fx2's nested lands, got ${v[0].Nested.A}/${v[0].Nested.B}`);
    check(v[0].LabelLength === 3 && textOf(v[0].Label, 3) === "fx2",
      `reference oracle: fx2's short string lands through the plan — got ${v[0].LabelLength} ${JSON.stringify(textOf(v[0].Label, v[0].LabelLength))}`);
    check(v[0].MarksCount === 1 && v[0].Marks[0] === 303,
      `reference oracle: fx2's partly-used array lands through the plan — got count ${v[0].MarksCount} [${v[0].Marks[0]}]`);
    check(v[0].BlobLength === 3 && v[0].Blob[0] === 1 && v[0].Blob[1] === 2 && v[0].Blob[2] === 3,
      `reference oracle: fx2's partly-used bytes(N) lands through the plan — got length ${v[0].BlobLength}`);
    check(v[0].Gone === 9,
      `reference oracle: a field FX2 does not carry keeps its declared default, got ${v[0].Gone}`);
  }
}

// LIVE-COUNT: decode walks MarksCount, never the bound. Constructed count=1,
// wire slack poisoned after write (write already walks MarksCount, so the
// template's zeros are what the writer put there). A fresh object after load
// has template zeros in the slack and clamped 0, not 3. Walking the bound
// would have copied the poison into the object.
function liveCountSlack(check, fx1, fx1home) {
  const one = new fx1home.FxRoot();
  one.MarksCount = 1;
  one.Marks[0] = 42;
  one.Marks[1] = 999;
  one.Marks[2] = 888;
  one.Marks[3] = 777;
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
  view.setInt32(recBody + marksAt + 8, 999, true);
  view.setInt32(recBody + marksAt + 12, 888, true);
  view.setInt32(recBody + marksAt + 16, 777, true);

  const back = [new fx1home.FxRoot()];
  const r = new fx1home.TableFixedReport();
  const n = fx1.FxRootFixedLoad(back, 1, buf, buf.length, fx1.FxRootFixedNewPlan(), r);
  check(n === 1 && !r.malformed && r.refused === 0, `live-count: load of the poisoned-slack record, got ${n}`);
  check(back[0].MarksCount === 1 && back[0].Marks[0] === 42,
    `live-count: live prefix lands — got count ${back[0].MarksCount} [${back[0].Marks[0]}]`);
  check(back[0].Marks[1] === 0 && back[0].Marks[2] === 0 && back[0].Marks[3] === 0,
    `live-count: object slack is template zeros, not the poisoned wire — got [${back[0].Marks[1]}, ${back[0].Marks[2]}, ${back[0].Marks[3]}]`);
  check(r.clamped === 0,
    `live-count: clamped counts 0 not 3 (got ${r.clamped})`);
}

// HOLE PREFILL: identity's hole list is empty (one COPY of the whole body).
// Compiled FX2→FX1 has holes for Gone; a dirty image still reads the default.
function holePrefill(check, fx1, fx2, fx1home, fx2home) {
  const identity = new Int32Array([0, 0, 0, fx1.FxRootFixedBodyBytes, 0, -1, 0, 0]);
  const idPlan = fx1.FxRootFixedNewPlan();
  const idHoles = fx1home.TableFixedHoles(identity, 1, idPlan.cover, idPlan.holes);
  check(idHoles === 0, `hole prefill: identity's hole list is empty, got ${idHoles}`);

  const two = new fx2home.FxRoot();
  two.Keep = 5150;
  two.Narrow = 70000;
  two.RenamedTo = 808;
  two.Added = 909;
  two.Nested.A = 33;
  two.Nested.B = 44;
  const w2 = new Uint8Array(fx2.FxRootFixedMeasure(1));
  check(fx2.FxRootFixedSave([two], 1, w2) === w2.length, "hole prefill: FX2 save");

  const plan = fx1.FxRootFixedNewPlan();
  const back = [new fx1home.FxRoot()];
  const r = new fx1home.TableFixedReport();
  const n = fx1.FxRootFixedLoad(back, 1, w2, w2.length, plan, r);
  check(n === 1, "hole prefill: FX1 reads FX2 through a compiled plan");
  check(back[0].Gone === 9, `hole prefill: Gone keeps its declared default, got ${back[0].Gone}`);
  const compiledHoles = fx1home.TableFixedHoles(plan.entries, plan.count, plan.cover, plan.holes);
  check(compiledHoles > 0, `hole prefill: compiled FX2→FX1 has holes (Gone), got ${compiledHoles}`);

  plan.image.fill(0xff);
  const dirty = [new fx1home.FxRoot()];
  const r2 = new fx1home.TableFixedReport();
  check(fx1.FxRootFixedLoad(dirty, 1, w2, w2.length, plan, r2) === 1,
    "hole prefill: a second load on a dirty image");
  check(dirty[0].Gone === 9,
    `hole prefill: holes restore Gone's default after a dirty image, got ${dirty[0].Gone}`);
}

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
  v[0].NameLength = setText(v[0].Name, "all");
  v[0].Plain = 101;
  v[0].OptPresent = true;
  v[0].Opt.V = 11;
  v[0].Opt.TagLength = setText(v[0].Opt.Tag, "op");
  v[0].NumPresent = true;
  v[0].Num = 12;
  v[0].MarkPresent = true;
  v[0].Mark = 2; // Gold — the ordinal is the variant's POSITION IN THE BLOCK, from 1
  v[0].Wrap.N = 13;
  v[0].Wrap.LeafPresent = true;
  v[0].Wrap.Leaf.V = 14;
  v[0].Wrap.Leaf.TagLength = setText(v[0].Wrap.Leaf.Tag, "wl");
  v[0].Effect.Type = 1; // Boost
  v[0].Effect.Boost.Power = 15;
  v[0].Tail = 16;

  // 1 — EVERY OPTIONAL ABSENT, the union on its second arm. Nothing behind a
  // zero flag is set, so every absent payload here is the value's CONSTRUCTION
  // state, which is the declared defaults — and that is what makes this record
  // byte-comparable at all (see optionalBytes below).
  v[1].NameLength = setText(v[1].Name, "none");
  v[1].Plain = 201;
  v[1].Wrap.N = 202;
  v[1].Effect.Type = 2; // Ward
  v[1].Effect.Ward.Charge = 0.5;
  v[1].Tail = 203;

  // 2 — MIXED, the union at None: the ROOT's optional absent while the NESTED
  // table's is present, which is the pair that catches a present byte placed at
  // the root's offsets instead of the nested body's
  v[2].NameLength = setText(v[2].Name, "mix");
  v[2].Plain = 301;
  v[2].OptPresent = false;
  v[2].NumPresent = true;
  v[2].Num = -302;
  v[2].MarkPresent = true;
  v[2].Mark = 1; // Bronze
  v[2].Wrap.N = 303;
  v[2].Wrap.LeafPresent = true;
  v[2].Wrap.Leaf.V = -304;
  v[2].Wrap.Leaf.TagLength = setText(v[2].Wrap.Leaf.Tag, "deep");
  v[2].Tail = 305;

  // 3 — THE OTHER WAY ROUND: the root's present, the nested table's absent
  v[3].NameLength = setText(v[3].Name, "root only");
  v[3].Plain = 401;
  v[3].OptPresent = true;
  v[3].Opt.V = 402;
  v[3].Opt.TagLength = setText(v[3].Opt.Tag, "r");
  v[3].NumPresent = false;
  v[3].MarkPresent = false;
  v[3].Wrap.N = 403;
  v[3].Wrap.LeafPresent = false;
  v[3].Effect.Type = 2; // Ward
  v[3].Effect.Ward.Charge = -1.25;
  v[3].Tail = 404;
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
    check(r.clamped === 0 && r.unknown === 0 && r.kindMismatch === 0 && !r.malformed && r.refused === 0,
      "optionals: a clean read of the reference's own corpus moves no counter");

    check(back[0].OptPresent && back[0].Opt.V === 11 && back[0].Opt.TagLength === 2 &&
      back[0].Opt.Tag[0] === 0x6f && back[0].Opt.Tag[1] === 0x70,
      "optionals: a PRESENT nested type lands its whole payload");
    check(back[0].NumPresent && back[0].Num === 12, "optionals: a PRESENT scalar lands");
    check(back[0].MarkPresent && back[0].Mark === 2, "optionals: a PRESENT enum lands its ordinal");
    check(back[0].Wrap.N === 13 && back[0].Wrap.LeafPresent && back[0].Wrap.Leaf.V === 14,
      "optionals: a present flag INSIDE A NESTED TABLE lands at the nested body's own offset");
    check(back[0].Effect.Type === 1 && back[0].Effect.Boost.Power === 15 && back[0].Tail === 16,
      "optionals: the fields BEHIND the optionals land, so every present byte was counted in the layout");

    // AN ABSENT FIELD READS ITS DECLARED DEFAULT, never the bytes behind the
    // flag: `Leaf.v = 7` and `Wrap.leaf`'s the same
    check(!back[1].OptPresent && back[1].Opt.V === 7 && back[1].Opt.TagLength === 0,
      "optionals: an ABSENT nested type reads absent, at its declared defaults");
    check(!back[1].NumPresent && back[1].Num === 0, "optionals: an ABSENT scalar reads absent");
    check(!back[1].MarkPresent && back[1].Mark === 0, "optionals: an ABSENT enum reads absent, at None");
    check(back[1].Wrap.N === 202 && !back[1].Wrap.LeafPresent && back[1].Wrap.Leaf.V === 7,
      "optionals: an ABSENT flag inside a nested table reads absent");
    check(back[1].Effect.Type === 2 && back[1].Effect.Ward.Charge === 0.5 && back[1].Tail === 203,
      "optionals: the fields behind four absent optionals still land");

    check(!back[2].OptPresent && back[2].NumPresent && back[2].Num === -302 &&
      back[2].MarkPresent && back[2].Mark === 1 &&
      back[2].Wrap.LeafPresent && back[2].Wrap.Leaf.V === -304 && back[2].Wrap.Leaf.TagLength === 4,
      "optionals: root ABSENT and nested PRESENT, in the same record");
    check(back[2].Effect.Type === 0 && back[2].Tail === 305,
      "optionals: a union at None beside the mixed optionals");
    check(back[3].OptPresent && back[3].Opt.V === 402 && !back[3].NumPresent && !back[3].MarkPresent &&
      !back[3].Wrap.LeafPresent && back[3].Wrap.N === 403 && back[3].Tail === 404,
      "optionals: root PRESENT and nested ABSENT, the other way round");
  }

  // ---- P3's OWN CORPUS: present then absent, the reference's bytes ----
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "p3.bin")));
    const two = [new p3home.Chain(), new p3home.Chain()];
    two[0].NameLength = setText(two[0].Name, "chain");
    two[0].LinkPresent = true;
    two[0].Link.Value = 500;
    two[0].Link.TagLength = setText(two[0].Link.Tag, "v");
    two[1].NameLength = setText(two[1].Name, "bare");
    two[1].LinkPresent = false;
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
    check(back[0].LinkPresent && back[0].Link.Value === 500 && back[0].NameLength === 5,
      "P3: the present record lands its payload");
    check(!back[1].LinkPresent && back[1].Link.Value === 0 && back[1].NameLength === 4,
      "P3: the absent record lands absent, payload at its declared default");
  }

  // ---- P1's, which nests the SAME type BY VALUE ----
  {
    const ref = new Uint8Array(readFileSync(resolve(dir, "p1.bin")));
    const one = [new p1home.Chain()];
    one[0].NameLength = setText(one[0].Name, "chain");
    one[0].Link.Value = 500;
    one[0].Link.TagLength = setText(one[0].Link.Tag, "v");
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
// THE OPTIONALS THROUGH A COMPILED PLAN — the positions are the WRITER's
// ---------------------------------------------------------------------------

function optionalVersioning(check, o, dir) {
  const { fo1, fo1home, fo2, fo2home, p1, p1home, p3, p3home } = o;
  const fo1bin = new Uint8Array(readFileSync(resolve(dir, "fo1.bin")));

  // FO2 READS FO1. FO2 inserts `added` BEFORE `wrap`, so `wrap.leaf`'s present
  // byte sits four bytes later in an FO2 body than in an FO1 record: a reader
  // that landed a present byte at its OWN offset instead of following the plan
  // would read it absent on every record and never say so.
  {
    const back = Array.from({ length: 4 }, () => new fo2home.OptRoot());
    const r = new fo2home.TableFixedReport();
    const n = fo2.OptRootFixedLoad(back, 4, fo1bin, fo1bin.length, fo2.OptRootFixedNewPlan(), r);
    check(n === 4, `compiled plan: FO2 reads FO1's four records (got ${n})`);
    check(back[0].Plain2 === 101, "compiled plan: `was =` keeps the wire id across the rename");
    check(back[0].Added === 77, "compiled plan: the inserted field takes its declared default");
    check(back[0].OptPresent && back[0].Opt.V === 11 && back[0].Opt.TagLength === 2,
      "compiled plan: a PRESENT optional's flag AND payload land");
    check(back[0].NumPresent && back[0].Num === 12 && back[0].MarkPresent && back[0].Mark === 2,
      "compiled plan: an optional scalar and an optional enum land");
    check(back[0].Wrap.N === 13 && back[0].Wrap.LeafPresent && back[0].Wrap.Leaf.V === 14,
      "compiled plan: a present flag inside a nested table lands past the inserted field");
    check(back[0].Effect.Type === 1 && back[0].Effect.Boost.Power === 15 && back[0].Tail === 16,
      "compiled plan: the fields behind the optionals land");
    check(!back[1].OptPresent && back[1].Opt.V === 7 && !back[1].NumPresent && !back[1].MarkPresent &&
      !back[1].Wrap.LeafPresent && back[1].Wrap.Leaf.V === 7,
      "compiled plan: an ABSENT optional lands absent, at its declared default");
    check(back[1].Tail === 203 && back[1].Effect.Type === 2 && back[1].Effect.Ward.Charge === 0.5,
      "compiled plan: the record behind four absent optionals is intact");
    check(!back[2].OptPresent && back[2].Wrap.LeafPresent && back[2].Wrap.Leaf.V === -304,
      "compiled plan: root absent and nested present, through the plan");
    check(back[3].OptPresent && back[3].Opt.V === 402 && !back[3].Wrap.LeafPresent,
      "compiled plan: root present and nested absent, through the plan");
    check(r.unknown === 0 && r.kindMismatch === 0 && r.clamped === 0 && !r.malformed && r.refused === 0,
      `compiled plan: nothing fired reading an older generation (unknown ${r.unknown}, kind ${r.kindMismatch})`);
  }

  // AND THE OTHER DIRECTION: FO1 reads FO2, where `added` is a name it has no
  // field for — one unknown, and every optional still lands
  {
    const two = Array.from({ length: 1 }, () => new fo2home.OptRoot());
    two[0].NameLength = setText(two[0].Name, "newer");
    two[0].Plain2 = 909;
    two[0].Added = 910;
    two[0].OptPresent = true;
    two[0].Opt.V = 911;
    two[0].NumPresent = true;
    two[0].Num = 912;
    two[0].Wrap.N = 913;
    two[0].Wrap.LeafPresent = true;
    two[0].Wrap.Leaf.V = 914;
    two[0].Tail = 915;
    const w = new Uint8Array(fo2.OptRootFixedMeasure(1));
    check(fo2.OptRootFixedSave(two, 1, w) === w.length, "FO2 save");

    const back = [new fo1home.OptRoot()];
    const r = new fo1home.TableFixedReport();
    check(fo1.OptRootFixedLoad(back, 1, w, w.length, fo1.OptRootFixedNewPlan(), r) === 1,
      "newer writer: one record");
    check(back[0].Plain === 909, "newer writer: the renamed field reads the other way too");
    check(back[0].OptPresent && back[0].Opt.V === 911 && back[0].NumPresent && back[0].Num === 912,
      "newer writer: the optionals land past the unknown field");
    check(back[0].Wrap.N === 913 && back[0].Wrap.LeafPresent && back[0].Wrap.Leaf.V === 914 &&
      back[0].Tail === 915,
      "newer writer: the nested present byte lands past the unknown field");
    check(r.unknown === 1, `newer writer: added is the one name this reader lacks (got ${r.unknown})`);
    check(r.kindMismatch === 0 && !r.malformed && r.refused === 0, "newer writer: nothing else fired");
  }

  // P3 READS P1 — `?Link` against a `Link` nested by value. §3.4's ONE
  // departure from §2.3 lives here, and the C++ leg pins the same fact
  // (test/tables/fixedform_main.cpp's p_case): the block says kind 35 on one
  // side and kind 13 on the other, so the edit is REPORTED and never a silent
  // reread of a payload one byte out of place.
  {
    const p1bin = new Uint8Array(readFileSync(resolve(dir, "p1.bin")));
    const back = [new p3home.Chain()];
    const r = new p3home.TableFixedReport();
    check(p3.ChainFixedLoad(back, 1, p1bin, p1bin.length, p3.ChainFixedNewPlan(), r) === 1,
      "P3 reads P1: one record");
    check(back[0].NameLength === 5 && back[0].Name[0] === 0x63,
      "P3 reads P1: the plain field lands");
    check(r.kindMismatch === 1,
      `OPTIONAL vs VALUE is a reported kind on this form, never a silent reread (got ${r.kindMismatch})`);
    check(!back[0].LinkPresent && back[0].Link.Value === 0,
      "P3 reads P1: the optional reads ABSENT at its declared default, not the value one byte over");
  }

  // AND BACK: P1 reads P3, the same mismatch from the other side
  {
    const p3bin = new Uint8Array(readFileSync(resolve(dir, "p3.bin")));
    const back = [new p1home.Chain(), new p1home.Chain()];
    const r = new p1home.TableFixedReport();
    check(p1.ChainFixedLoad(back, 2, p3bin, p3bin.length, p1.ChainFixedNewPlan(), r) === 2,
      "P1 reads P3: two records");
    check(back[0].NameLength === 5 && back[1].NameLength === 4,
      "P1 reads P3: the plain field lands both records");
    check(r.kindMismatch === 1,
      `P1 reads P3: VALUE vs OPTIONAL is the same reported kind, once per plan (got ${r.kindMismatch})`);
    check(back[0].Link.Value === 0,
      "P1 reads P3: the nested value keeps its declared default rather than reading the flag as a payload byte");
  }
}

// ---------------------------------------------------------------------------
// THE OPTIONALS' NEGATIVE CONTROLS
// ---------------------------------------------------------------------------

function optionalControls(check, o, dir) {
  const { fo1, fo1home, fo2, fo2home } = o;
  const corpus = new Uint8Array(readFileSync(resolve(dir, "fo1.bin")));
  const head = LAYOUT_AT + fo1.OptRootFixedLayoutBytes;
  const rec = 80; // 8 + 72

  // ONE record of the reference's corpus, framed as a file of its own, so a
  // forged byte is the only thing that differs from bytes already proved good.
  const oneRecord = (k) => {
    const out = new Uint8Array(head + rec);
    out.set(corpus.subarray(0, head));
    out.set(corpus.subarray(head + k * rec, head + (k + 1) * rec), head);
    return out;
  };
  const body = head + 8; // one record per file, so the body is always here
  const load = (bytes, mods = fo1, home = fo1home) => {
    const back = [new home.OptRoot()];
    const r = new home.TableFixedReport();
    const n = mods.OptRootFixedLoad(back, 1, bytes, bytes.length, mods.OptRootFixedNewPlan(), r);
    return { n, v: back[0], r };
  };

  // the corpus reads clean one record at a time, before anything is forged
  {
    const { n, v, r } = load(oneRecord(1));
    check(n === 1 && !v.OptPresent && v.Opt.V === 7 && r.clamped === 0,
      "control base: record 1 alone reads absent and clean");
  }

  // A PRESENT BYTE IS A FLAG, NOT A NUMBER. A peer's `true` is allowed to be
  // any non-zero byte — that is the rule `bool` already reads under — so 7
  // means PRESENT and the payload behind it is projected.
  {
    const bad = oneRecord(1);
    const b = body;
    bad[b + OPT_PRESENT] = 7;
    bad[b + OPT_V] = 0x39; bad[b + OPT_V + 1] = 0x30; // 12345, so a projection is visible
    bad[b + WRAP_LEAF_PRESENT] = 0xff;
    bad[b + WRAP_LEAF_V] = 0x2a;
    bad[b + NUM_PRESENT] = 2;
    bad[b + MARK_PRESENT] = 0x80;
    const { n, v, r } = load(bad);
    check(n === 1 && v.OptPresent && v.Opt.V === 12345,
      `NEGATIVE CONTROL: a present byte of 7 reads as PRESENT and projects the payload (got ${v.OptPresent}, ${v.Opt.V})`);
    check(v.Wrap.LeafPresent && v.Wrap.Leaf.V === 42,
      "NEGATIVE CONTROL: 0xff inside a nested table reads as present too");
    check(v.NumPresent && v.MarkPresent,
      "NEGATIVE CONTROL: every present byte is a flag, at every kind an optional wraps");
    check(r.clamped === 0 && !r.malformed && r.refused === 0,
      "NEGATIVE CONTROL: a non-canonical present byte is not damage and moves no counter");
  }

  // AND THE OTHER HALF: THE PAYLOAD BEHIND A ZERO FLAG IS NEVER PROJECTED. The
  // bytes are whatever the writer's storage held, they are not the value, and a
  // reader that landed them would hand the consumer a value the writer said was
  // not there. The field reads its DECLARED DEFAULT instead — `Leaf.v = 7`.
  {
    const bad = oneRecord(1);
    const b = body;
    bad[b + OPT_V] = 0xff; bad[b + OPT_V + 1] = 0xff; bad[b + OPT_V + 2] = 0xff;
    bad[b + OPT_V + 3] = 0x7f;                        // 2147483647 behind a zero flag
    bad[b + WRAP_LEAF_V] = 0x99;
    bad[b + NUM_PRESENT + 1] = 0x7b;                  // num's payload, flag still zero
    bad[b + MARK_PRESENT + 1] = 0x09;                 // an ordinal naming no variant
    const { n, v, r } = load(bad);
    check(n === 1 && !v.OptPresent && v.Opt.V === 7,
      `NEGATIVE CONTROL: an ABSENT payload is ignored on read; the field is its declared default (got ${v.Opt.V})`);
    check(!v.Wrap.LeafPresent && v.Wrap.Leaf.V === 7,
      "NEGATIVE CONTROL: an absent payload inside a nested table is ignored too");
    check(!v.NumPresent && v.Num === 0 && !v.MarkPresent && v.Mark === 0,
      "NEGATIVE CONTROL: an absent scalar and an absent enum ignore their payloads");
    check(!r.malformed && r.refused === 0 && r.kindMismatch === 0,
      "NEGATIVE CONTROL: ignoring an absent payload is not an event; nothing is counted");
  }

  // THE SAME TWO FACTS ON THE COMPILED PATH, where the flag arrives through a
  // one-byte OP_COPY rather than the whole-body one
  {
    const bad = oneRecord(1);
    const b = body;
    bad[b + OPT_PRESENT] = 7;
    bad[b + OPT_V] = 0x39; bad[b + OPT_V + 1] = 0x30;
    const { n, v } = load(bad, fo2, fo2home);
    check(n === 1 && v.OptPresent && v.Opt.V === 12345,
      "NEGATIVE CONTROL: a present byte of 7 reads as present through a COMPILED plan too");
  }
  {
    const bad = oneRecord(1);
    const b = body;
    bad[b + OPT_V] = 0xff; bad[b + OPT_V + 1] = 0xff;
    const { n, v } = load(bad, fo2, fo2home);
    check(n === 1 && !v.OptPresent && v.Opt.V === 7,
      "NEGATIVE CONTROL: an absent payload is ignored through a COMPILED plan too");
  }

  // A UNION TAG BEYOND THE ARM COUNT LANDS AS None (0) AND COUNTS ONE clamped.
  // It is the twin of the forged count above and it is here for the same
  // reason: on the IDENTITY path the whole body is one OP_COPY, so the tag
  // arrives raw and a consumer would otherwise be handed an ordinal naming an
  // arm that does not exist.
  {
    const bad = oneRecord(0);
    bad[body + EFFECT_TAG] = 9; // Effect has two arms
    const { n, v, r } = load(bad);
    check(n === 1 && v.Effect.Type === 0,
      `NEGATIVE CONTROL: a union tag beyond the arm count lands as None (got ${v.Effect.Type})`);
    check(r.clamped === 1, `NEGATIVE CONTROL: a tag beyond the arms counts one clamped (got ${r.clamped})`);
    check(!r.malformed && r.refused === 0,
      "NEGATIVE CONTROL: a tag beyond the arms is an event and not a refusal — the record still reads");
    check(v.Tail === 16, "NEGATIVE CONTROL: the rest of the record still lands");
  }
  {
    // and on the COMPILED path it is already None: an arm's entries run only
    // under their own tag's guard, so a tag matching no guard leaves the
    // prefill's zero standing and nothing is reinterpreted
    const bad = oneRecord(0);
    bad[body + EFFECT_TAG] = 9;
    const { n, v } = load(bad, fo2, fo2home);
    check(n === 1 && v.Effect.Type === 0,
      "a union tag beyond the arm count is None on the COMPILED path too, by the guard matching nothing");
  }
}

// ---------------------------------------------------------------------------

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  let failed = false;
  const check = (ok, what) => {
    if (!ok) {
      console.log("FAILED: " + what);
      failed = true;
    }
  };
  const generated = process.argv[2] ?? "build/js-fixed";
  const corpus = process.argv[3] ?? "build/js-fixed-corpus";
  const optional = process.argv[4] ?? "build/js-fixed-optional";
  const oracle = process.argv[5] ?? "build/fixedform-corpus";
  await checkFixedForm(check, generated, corpus, optional, oracle);
  if (failed) {
    console.log("FAILED");
    process.exit(1);
  }
  console.log("tables JS fixed form: the vocabulary block and its hash are the C++ " +
    "reference's byte for byte, all 64 paired records read to the values the reference " +
    "states and write back IDENTICAL to its corpus, the versioning conformance lands " +
    "through a plan compiled from the other side's block, the OPTIONALS match the " +
    "reference at every place a present byte can ride, and every refusal is by name");
  console.log("OK");
}
