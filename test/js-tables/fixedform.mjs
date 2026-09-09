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

//   node test/js-tables/fixedform.mjs <generated> <corpus>
//
// It is its own driver rather than a mode of main.mjs because it shares nothing
// with that leg: main.mjs opens the two ACCELERATORS, which this form has no
// symbol of, and a unit carrying the fixed form need not have a block at all.
//
// Run from the repository root, which is where the Makefile runs it.

export async function checkFixedForm(check, generated, corpusDir) {
  const load = (p) => import(pathToFileURL(resolve(generated, p)).href);

  const bench = await load("bench/BenchTable.js");
  const fixed = await load("bench/FixedTableTable.js");
  const fx1 = await load("fx1/FX1Table.js");
  const fx2 = await load("fx2/FX2Table.js");
  const fx1home = await load("fx1/Tblfx1Table.js");
  const fx2home = await load("fx2/Tblfx2Table.js");

  await pairedCorpus(check, bench, fixed, corpusDir);
  forgedCounts(check, bench, fixed, corpusDir);
  versioning(check, fx1, fx2, fx1home, fx2home);
  negativeControls(check, fx1, fx2, fx1home, fx2home);
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
  const head = 5 + fixed.FixedTableFixedBlockBytes;

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
  const blockBytes = corpus[1] | (corpus[2] << 8) | (corpus[3] << 16) | (corpus[4] << 24);
  check(blockBytes === fixed.FixedTableFixedBlockBytes,
    `fixed form: the file's block length ${blockBytes} is this build's ${fixed.FixedTableFixedBlockBytes}`);

  // THE BLOCK IS THE SAME RUN OF BYTES as the reference's, which is what makes
  // the hash the same number and the record positional against the same layout.
  let blockSame = true;
  for (let i = 0; i < blockBytes; i++) {
    if (corpus[5 + i] !== fixed.FixedTableFixedBlock[i]) { blockSame = false; break; }
  }
  check(blockSame, "fixed form: the vocabulary block is byte-identical to the C++ reference's");

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
    const body = w2.subarray(5 + fx2.FxRootFixedBlockBytes + 8);
    plan.image.set(new Uint8Array(plan.image.length)); // the prefill
    fx1home.TableFixedRun(new Int32Array([0, 0, 0, fx1.FxRootFixedBodyBytes, 0, -1, 0, 0]),
      1, body, 0, plan.image, null, wr);
    fx1home.FxRootFixedDecode(wrong[0], plan.view, 0, wr);
    check(wrong[0].Keep !== right[0].Keep || wrong[0].Renamed !== right[0].Renamed ||
      wrong[0].Nested.A !== right[0].Nested.A || wrong[0].Nested.B !== right[0].Nested.B,
      "NEGATIVE CONTROL: the identity plan run over another writer's record comes out WRONG");
  }

  // EVERY REFUSAL IS BY NAME, and none of them is one of §4's six events:
  // nothing is decoded and there is nothing to count.
  const fresh = () => [new fx1home.FxRoot()];
  {
    const bad = Uint8Array.from(w1);
    bad[0] = 4; // §4.2's unknown-form control byte, which is 4 now that 3 is taken
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.NewerForm && !r.malformed,
      "REFUSAL: a form byte this build does not carry is newer_form, and malformed does not fire");
  }
  {
    const bad = Uint8Array.from(w1);
    bad[1] = 0xff; bad[2] = 0xff; // a block length that overruns the file
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.BlockMalformed,
      "REFUSAL: a block length that overruns the file is block_malformed");
  }
  {
    // a block whose ENTRY COUNT does not close its own tree is refused WHOLE
    const bad = Uint8Array.from(w1);
    bad[5 + 4 + 13] = 0x7f; // the root's child count, which now closes nothing
    const r = new fx1home.TableFixedReport();
    const n = fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r);
    check(n === -1 && r.refused === fx1home.TableFixedRefusal.BlockMalformed,
      "REFUSAL: a block whose tree does not close is block_malformed, and it sets nothing");
  }
  {
    // A RECORD WHOSE HASH NAMES NO BLOCK THIS READER HOLDS is a refusal by
    // name — never a guess and never damage
    const bad = Uint8Array.from(w1);
    bad[5 + fx1.FxRootFixedBlockBytes] ^= 0xff; // the record's own hash
    const r = new fx1home.TableFixedReport();
    check(fx1.FxRootFixedLoad(fresh(), 1, bad, bad.length, fx1.FxRootFixedNewPlan(), r) === -1 &&
      r.refused === fx1home.TableFixedRefusal.NoBlock,
      "REFUSAL: a record whose hash is not the block's is no_block");
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
      "REFUSAL: a block whose compiled plan does not fit the caller's storage is plan_too_large");
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
  await checkFixedForm(check, generated, corpus);
  if (failed) {
    console.log("FAILED");
    process.exit(1);
  }
  console.log("tables JS fixed form: the vocabulary block and its hash are the C++ " +
    "reference's byte for byte, all 64 paired records read to the values the reference " +
    "states and write back IDENTICAL to its corpus, the versioning conformance lands " +
    "through a plan compiled from the other side's block, and every refusal is by name");
  console.log("OK");
}
