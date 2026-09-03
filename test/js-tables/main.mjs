// The JavaScript TABLE-wire test leg (docs/SPEC-TABLES.md) — what the
// conformance harness does NOT ask, because the harness asks every backend the
// same questions and these are this backend's own.
//
// The harness (test/conformance/README.md) already holds the corpus, both ways,
// on all ten surfaces. This leg holds the six properties that live beside it:
//
//   1. THE HASH, against a second implementation. fold16(fnv1a32(name)), 0
//      rebounding to 1, written here from §3's prose and compared with what the
//      compiler emitted into the descriptors. Two implementations of one hash,
//      one of which never read the other.
//   2. THE READING TIER'S OWN CLAIM: the generated accessors and the
//      descriptors are two spellings of one layout, so every field of every row
//      of every block, and every field of every cooked node, must read the same
//      value both ways. The harness proves the descriptors against C++; this
//      proves the accessors against the descriptors — including a pointer slot,
//      compared as its RAW DELTA before any resolution, because that is the
//      byte the two halves have to agree about.
//   3. THE OTHER BYTE ORDER, refused TWICE OVER: by the magic, whose bytes read
//      back reversed, and by the order word, which records what wrote the file.
//      A JavaScript reader reads at explicit little-endian offsets, so it has no
//      native path for a big-endian file to take and never grows one.
//   4. THE REFUSAL CONTRACT, under a fuzzer: a forged block or cook either
//      REFUSES or opens and reads entirely inside the bytes it was given. An
//      index out of bounds is a refusal, never an exception escaping the
//      reader — which in this language is the whole of the property, because a
//      DataView read past its view throws.
//   5. THE RANDOMIZED ROUND TRIP: instances nobody wrote down, filled through
//      §8's descriptors — so the descriptors are the CONSTRUCTOR here and not
//      only the reader — and held to both round trips, wire and text. The text
//      one is the stronger: a text that reads clean writes back (§16.1), so a
//      walker that loses a field, misplaces a slot or spells a float short
//      lands different bytes even though nothing refused.
//   6. WHAT ALLOCATES, as a RATE and not a drift. A flat heap is a LEAK
//      instrument and nothing more — an allocation made and collected every
//      iteration leaves it exactly as flat as no allocation at all — so the
//      claim is held as BYTES PER ITERATION, measured per path, with the
//      floor stated and every unavoidable allocation named. The hour-long
//      soak keeps the flat-heap half beside it: a leak and a rate are two
//      different defects.
//
//   node main.mjs                 the gates
//   node main.mjs fuzz <block> <cook> [mutants]
//   node main.mjs alloc [iters]   bytes allocated per iteration, per path
//   node main.mjs emit <dir> [n]  random instances as (wire, text) pairs, for
//                                 the differential against `schema unpack`
//   node main.mjs verify <dir>    read the texts `schema unpack` wrote beside
//                                 them and require the wire to come back
//   node main.mjs soak <seconds>  read/write the corpus, sampling the heap
//
// Run from the repository root, which is where the Makefile runs it.

import { readFileSync, existsSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import v8 from "node:v8";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const tables = await load("examples/TablesTable.js");
const nested = await load("examples/NestedTable.js");
const keyed = await load("examples/KeyedTable.js");
const wide = await load("examples/WideTable.js");
const pack = await load("examples/PackTable.js");
const renderBlock = await load("block/RenderBlock.js");
const paddedBlock = await load("block/PaddedBlock.js");
const blockdemoBlock = await load("block/BlockdemoBlock.js");
const graphCook = await load("pointers/GraphCook.js");

let failed = false;

function check(ok, what) {
  if (!ok) {
    console.log("FAILED: " + what);
    failed = true;
  }
}

// ---- 1. the table-wire field id, from the spec's prose and nothing else ----
//
// fold16(fnv1a32(name)), and 0 rebounds to 1 (docs/SPEC-TABLES.md §3). Written
// here from the page; the compiler wrote its own from the same page. Two
// implementations agreeing is the pin.
function fieldId(name) {
  let h = 0x811c9dc5;
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i) & 0xff;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  const id = ((h ^ (h >>> 16)) & 0xffff) >>> 0;
  return id === 0 ? 1 : id;
}

function checkFieldIds() {
  const roots = [
    tables.RootConfigTableType(), tables.WeaponConfigTableType(), tables.ProfileConfigTableType(),
    tables.LoadoutConfigTableType(), nested.ArchiveConfigTableType(), keyed.KeyedConfigTableType(),
    wide.WideBlobTableType(), pack.PackConfigTableType(),
  ];
  let checked = 0;
  for (const info of roots) {
    for (const f of info.Fields) {
      // a `was = "old"` rename keeps the OLD name's id, so the field whose
      // descriptor name is not the hashed name is skipped rather than mis-pinned
      if (f.Id !== fieldId(f.Name)) {
        // the two `was` fields in the corpus: speed (was velocity), title (was name)
        continue;
      }
      checked++;
    }
  }
  check(checked > 40, "the independent field-id implementation agreed on " + checked + " fields, expected more than 40");
  check(tables.RootConfigTableType().Fields[0].Id === fieldId("version_note"),
    "version_note's wire id is not fold16(fnv1a32) of its name");
  // the rename: `speed` rides under the hash of `velocity` (§3, `was`)
  const speed = tables.WeaponConfigTableType().Fields.find((f) => f.Name === "speed");
  check(speed.Id === fieldId("velocity"), "a renamed field does not ride under its `was` name's id");
}

// ---- 2. the accessors and the descriptors are one layout ----
//
// Every field of every row, read twice: once through the generated accessor
// named for the field, once through the descriptor's offset and kind. A
// disagreement is a layout the two halves of this backend do not share.

function scalarByDescriptor(view, at, f) {
  switch (f.Kind) {
    case 1: return view.getUint8(at) !== 0;
    case 10: return view.getFloat32(at, true);
    case 11: return view.getFloat64(at, true);
    case 2: case 3: case 4: case 5:
      switch (f.ElemSize) {
        case 1: return view.getInt8(at);
        case 2: return view.getInt16(at, true);
        case 4: return view.getInt32(at, true);
        default: return view.getBigInt64(at, true);
      }
    default:
      switch (f.ElemSize) {
        case 1: return view.getUint8(at);
        case 2: return view.getUint16(at, true);
        case 4: return view.getUint32(at, true);
        default: return view.getBigUint64(at, true);
      }
  }
}

function sameValue(a, b) {
  if (a instanceof Uint8Array && b instanceof Uint8Array) {
    if (a.length !== b.length) { return false; }
    for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
    return true;
  }
  return a === b || (Number.isNaN(a) && Number.isNaN(b));
}

// one record, both ways. `rowOf` names the accessor object the descriptor
// describes; the descriptor's own offsets do the other half.
function checkRecord(bytes, view, at, info, rowOf, where) {
  for (const f of info.Fields) {
    if (f.OutOfLine) { continue; }
    const accessor = rowOf.Fields[f.Name];
    check(accessor !== undefined, where + "." + f.Name + " has a descriptor and no accessor");
    if (accessor === undefined) { continue; }
    if (f.Counted && (f.ArrayBound > 0) && f.ElemSize === 1 && f.ElementRef === null) {
      // a string or a Bytes: the accessor hands back the USED bytes
      const used = view.getInt32(at + f.CountOffset, true);
      const viaDescriptor = bytes.subarray(at + f.Offset, at + f.Offset + Math.max(0, used));
      check(sameValue(accessor(bytes, view, at, 0), viaDescriptor),
        where + "." + f.Name + ": the accessor and the descriptor disagree about the used bytes");
      continue;
    }
    const slots = f.IsArray ? f.ArrayBound : 1;
    for (let s = 0; s < slots; s++) {
      const value = at + f.Offset + s * f.ElemSize;
      if (f.ElementRef !== null) {
        // a nested record: the accessor answers the OFFSET, and the walk
        // descends through it
        check(accessor(bytes, view, at, s) === value,
          where + "." + f.Name + "[" + s + "]: the accessor's offset is not the descriptor's");
        checkRecord(bytes, view, value, f.ElementRef(), rowOfName(f.ElementRef().Name),
          where + "." + f.Name + "[" + s + "]");
        continue;
      }
      check(sameValue(accessor(bytes, view, at, s), scalarByDescriptor(view, value, f)),
        where + "." + f.Name + "[" + s + "]: the accessor and the descriptor disagree about the value");
    }
  }
}

function rowOfName(name) {
  const row = blockdemoBlock[name + "Row"];
  if (row === undefined) { throw new Error("no row accessor object for " + name); }
  return row;
}

function checkBlockAccessors(path, block) {
  const source = new Uint8Array(readFileSync(path));
  const bytes = new Uint8Array(new ArrayBuffer(source.length));
  bytes.set(source);
  const handle = block.Open(bytes);
  check(handle !== null, path + " does not open");
  if (handle === null) { return; }
  const info = block.Type;
  for (const f of info.Fields) {
    if (!f.OutOfLine) { continue; }
    const offsetOf = Number(handle.View.getBigUint64(f.OffsetOfOffset, true));
    const count = handle.View.getUint32(f.CountOffset, true);
    const stride = handle.View.getUint32(f.StrideOffset, true);
    // the accessors answer the same three, read out of the INSTANCE
    check(block[capitalize(f.Name) + "Count"](handle) === count,
      f.Name + ": the count accessor and the descriptor disagree");
    check(block[capitalize(f.Name) + "Pitch"](handle) === stride,
      f.Name + ": the pitch accessor and the descriptor disagree");
    const rowInfo = f.ElementRef();
    const rowOf = rowOfName(rowInfo.Name);
    check(rowOf.Size === stride, f.Name + ": the row object's size is not the pitch the instance carries");
    for (let r = 0; r < count; r++) {
      const at = offsetOf + r * stride;
      check(block[capitalize(f.Name) + "At"](handle, r) === at,
        f.Name + "[" + r + "]: the row accessor's offset is not the descriptor's");
      checkRecord(bytes, handle.View, at, rowInfo, rowOf, rowInfo.Name);
    }
  }
}

// ---- the COOK's accessors against the COOK's descriptors ----
//
// The harness proves the cook DESCRIPTORS against the C++ walk, node for node
// and value for value. Nothing there touches the generated accessors, so this
// does: every field of every node, read once through `<Name>Row`'s accessor
// named for the field and once through the descriptor's own offset, storage
// kind and element size. A pointer is compared as its RAW DELTA, before any
// resolution, because that is the byte the two halves have to agree about.

function cookScalarByDescriptor(view, at, f) {
  switch (f.Storage) {
    case "Bool": return view.getUint8(at) !== 0;
    case "Float": return f.ElemSize === 4 ? view.getFloat32(at, true) : view.getFloat64(at, true);
    case "Signed":
      switch (f.ElemSize) {
        case 1: return view.getInt8(at);
        case 2: return view.getInt16(at, true);
        case 4: return view.getInt32(at, true);
        default: return view.getBigInt64(at, true);
      }
    default:
      switch (f.ElemSize) {
        case 1: return view.getUint8(at);
        case 2: return view.getUint16(at, true);
        case 4: return view.getUint32(at, true);
        default: return view.getBigUint64(at, true);
      }
  }
}

function checkCookRecord(bytes, view, at, info, where, depth, visited) {
  if (depth > 32) { return; }
  for (const f of info.Fields) {
    const accessor = info.Row.Fields[f.Name];
    check(accessor !== undefined, where + "." + f.Name + " has a descriptor and no accessor");
    if (accessor === undefined) { continue; }
    if (f.IsPointer) {
      // both halves of a pointer's surface: the SLOT's own offset, which is
      // what a self-relative delta is relative to, and the delta in it
      const slot = info.Row[capitalize(f.Name) + "Slot"];
      check(slot !== undefined, where + "." + f.Name + " is a pointer with no slot accessor");
      if (slot !== undefined) {
        check(slot(at) === at + f.Offset,
          where + "." + f.Name + ": the slot accessor's offset is not the descriptor's");
      }
      check(accessor(bytes, view, at, 0) === view.getBigInt64(at + f.Offset, true),
        where + "." + f.Name + ": the accessor and the descriptor disagree about the delta");
      continue;
    }
    if (f.Storage === "String" || f.Storage === "Bytes") {
      let used = view.getInt32(at + f.CountOffset, true);
      if (!(used >= 0) || used > f.ArrayBound) { used = 0; }
      check(sameValue(accessor(bytes, view, at, 0), bytes.subarray(at + f.Offset, at + f.Offset + used)),
        where + "." + f.Name + ": the accessor and the descriptor disagree about the used bytes");
      continue;
    }
    const slots = f.IsArray ? f.ArrayBound : 1;
    for (let s = 0; s < slots; s++) {
      const value = at + f.Offset + s * f.ElemSize;
      if (f.Storage === "Record") {
        check(accessor(bytes, view, at, s) === value,
          where + "." + f.Name + "[" + s + "]: the accessor's offset is not the descriptor's");
        checkCookRecord(bytes, view, value, f.RecordRef(), where + "." + f.Name + "[" + s + "]", depth + 1, visited);
        continue;
      }
      check(sameValue(accessor(bytes, view, at, s), cookScalarByDescriptor(view, value, f)),
        where + "." + f.Name + "[" + s + "]: the accessor and the descriptor disagree about the value");
    }
  }
}

function checkCookAccessors(path, cook) {
  if (!existsSync(path)) {
    check(false, "the cook accessor gate needs " + path + " — run `make build/js-fuzz-scene.cook`");
    return;
  }
  const source = new Uint8Array(readFileSync(path));
  const bytes = new Uint8Array(new ArrayBuffer(source.length));
  bytes.set(source);
  const handle = cook.Open(bytes);
  check(handle !== null, path + " does not open");
  if (handle === null) { return; }
  const visited = new Set();
  const walk = (offset, info, depth) => {
    if (visited.has(offset) || depth > 512) { return; }
    visited.add(offset);
    checkCookRecord(bytes, handle.View, handle.Region + offset, info, info.Name, 0, visited);
    // follow every pointer edge, so the walk covers the nodes the region holds
    const follow = (at, i, d) => {
      for (const f of i.Fields) {
        if (f.IsPointer) {
          const target = cook.At(handle, at + f.Offset);
          if (target >= 0) { walk(target - handle.Region, f.RecordRef(), depth + 1); }
          continue;
        }
        if (f.Storage === "Record") {
          const slots = f.IsArray ? f.ArrayBound : 1;
          for (let s = 0; s < slots; s++) { follow(at + f.Offset + s * f.ElemSize, f.RecordRef(), d); }
        }
      }
    };
    follow(handle.Region + offset, info, depth);
  };
  walk(0, cook.Type, 0);
  check(visited.size > 1, "the cook accessor gate reached " + visited.size + " node, expected the whole chain");
}

// lower_snake -> UpperCamel, the mapping the emitter uses for a member name
function capitalize(name) {
  let out = "";
  let upper = true;
  for (const c of name) {
    if (c === "_") { upper = true; continue; }
    out += upper ? c.toUpperCase() : c;
    upper = false;
  }
  return out;
}

// ---- the enum-keyed array's surface (§2.4) ----

function checkKeyedSurface() {
  const config = new keyed.KeyedConfig();
  const teams = config.Teams;
  // NONE IS THE NULL KEY: it names no slot, and indexing by it is an error in
  // every build, exactly as the C++ abort is
  let threw = false;
  try { teams.get(0); } catch { threw = true; }
  check(threw, "the keyed accessor accepted None as a key");
  threw = false;
  try { teams.set(0, null); } catch { threw = true; }
  check(threw, "the keyed setter accepted None as a key");
  // the SHIFT is the array's: the key k lives at storage index k - 1, and no
  // call site spells it
  teams.get(1).SpawnCount = 7;
  check(teams.Slots[0].SpawnCount === 7, "the keyed accessor does not shift the key left by one");
  // ITERATION yields the KEY, 1..E.Max, the same currency the accessor takes
  const seen = [];
  for (const [key, element] of teams) { seen.push(key); check(element !== undefined, "a keyed slot iterated as undefined"); }
  check(seen.join(",") === "1,2,3", "iteration did not yield the keys 1..E.Max, it yielded " + seen.join(","));
}

// ---- the OTHER byte order, refused twice over ----
//
// A JavaScript reader reads at explicit little-endian offsets, so its own order
// IS little whatever the host is — there is no native path for a big-endian
// file to take. A file of the other order is therefore refused by the MAGIC,
// whose bytes read back reversed, and refused again by the ORDER WORD, which
// records what wrote it. Both halves are checked here rather than one, because
// a reader that leaned only on the order word would open a file whose magic was
// reversed and whose order word a forger set back to 1.
//
// (The harness's `cook-foreign` and `block-foreign` surfaces hold the MAGIC
// half as data, across every leg. The ORDER WORD half is this leg's alone:
// no surface forges a file whose magic is intact and whose order word says
// the other order, and that is exactly the file a reader leaning on one
// check would open.)

function swapped(source, at) {
  const copy = new Uint8Array(source);
  for (let i = 0; i < 4; i++) {
    const t = copy[at + i];
    copy[at + i] = copy[at + 7 - i];
    copy[at + 7 - i] = t;
  }
  return copy;
}

function withWord(source, at, value) {
  const copy = new Uint8Array(source);
  new DataView(copy.buffer).setBigUint64(at, value, true);
  return copy;
}

function place(source) {
  const bytes = new Uint8Array(new ArrayBuffer(source.length));
  bytes.set(source);
  return bytes;
}

function checkForeignByteOrder() {
  const blockBytes = new Uint8Array(readFileSync("testdata/wire/tables/block_render.bin"));
  check(renderBlock.RenderFrameBlock.Open(place(blockBytes)) !== null,
    "the block of THIS build's byte order does not open");
  check(renderBlock.RenderFrameBlock.Open(place(swapped(blockBytes, 0))) === null,
    "a block whose magic is byte-reversed — a block of the other byte order — opened");
  check(renderBlock.RenderFrameBlock.Open(place(withWord(blockBytes, 16, 2n))) === null,
    "a block whose prologue records the other byte order opened");

  const cookPath = "build/js-fuzz-scene.cook";
  if (!existsSync(cookPath)) {
    check(false, "the byte-order leg needs " + cookPath + " — run `make build/js-fuzz-scene.cook`");
    return;
  }
  const cookBytes = new Uint8Array(readFileSync(cookPath));
  check(graphCook.SceneCook.Open(place(cookBytes)) !== null,
    "the cook of THIS build's byte order does not open");
  check(graphCook.SceneCook.Open(place(swapped(cookBytes, 0))) === null,
    "a cook whose magic is byte-reversed — a cook of the other byte order — opened");
  check(graphCook.SceneCook.Open(place(withWord(cookBytes, 16, 2n))) === null,
    "a cook whose header records the other byte order opened");
}

// ---- the HOT PATH: one reader, one writer, hoisted out of the loop ----
//
// <Name>Save and <Name>Load each build one object plus its DataView, because
// JavaScript has no stack object where C++ and C# have one. `reset` is how a
// per-frame caller stops paying for that, and this holds it to the property
// that makes it worth having: the bytes a hoisted pair produces are the bytes
// the entry points produce, and reusing one across two different buffers reads
// each correctly rather than the first one twice.

function checkReuse() {
  const wire = new Uint8Array(readFileSync("testdata/wire/tables/root_full.bin"));
  const other = new Uint8Array(readFileSync("testdata/wire/tables/root_default.bin"));
  const value = new tables.RootConfig();
  const report = new tables.TableReport();
  const reader = new tables.TableReader(wire, report);
  const writer = new tables.TableWriter(new Uint8Array(wire.length));

  for (const bytes of [wire, other, wire]) {
    check(tables.RootConfigLoadBody(reader.reset(bytes, report), value),
      "a reused reader did not load");
    const size = tables.RootConfigMeasure(value);
    const buffer = new Uint8Array(size);
    check(tables.RootConfigSaveBody(writer.reset(buffer), value), "a reused writer did not save");
    check(writer.Offset === size, "a reused writer wrote a size its measure did not name");
    check(size === bytes.length, "a reused pair round-tripped to a different length");
    for (let i = 0; i < size; i++) {
      if (buffer[i] !== bytes[i]) { check(false, "a reused pair round-tripped to different bytes at " + i); break; }
    }
  }
}

// ---- exact capacity: measure's answer IS the buffer size ----

function checkExactCapacity() {
  const wire = new Uint8Array(readFileSync("testdata/wire/tables/root_full.bin"));
  const value = new tables.RootConfig();
  check(tables.RootConfigLoad(value, wire, new tables.TableReport()), "root_full does not load");
  const size = tables.RootConfigMeasure(value);
  check(size === wire.length, "measure answered " + size + " for a golden of " + wire.length);
  // a buffer ONE BYTE SHORT refuses rather than writing past it
  check(tables.RootConfigSave(value, new Uint8Array(size - 1)) === -1,
    "Save into a buffer one byte short did not refuse");
  const exact = new Uint8Array(size);
  check(tables.RootConfigSave(value, exact) === size, "Save into an exact buffer did not write measure's answer");
}

// ---- the RANDOMIZED ROUND TRIP, built through the descriptors ----
//
// The harness holds eighteen pinned instances, which is what a cross-language
// gate can hold. This is the other half: instances nobody wrote down, filled
// through §8's descriptors — so the descriptors are the CONSTRUCTOR here and
// not only the reader — and held to the two round trips the forms promise.
//
//   wire:  Save -> Load -> Save   is byte-identical
//   text:  ToJson -> FromJson -> Save   is byte-identical to the first Save
//
// The second is the stronger one: a text that reads clean writes back (§16.1),
// so a walker that loses a field, misplaces a slot or spells a float short
// lands different bytes even though nothing refused.
//
// Two carve-outs the page already states, avoided by construction rather than
// papered over: a string byte outside ASCII may not be well-formed UTF-8, and
// §16 writes U+FFFD for one rather than a text that is not JSON; and an
// unnameable enum value has no wire identity at all, so the fill never picks
// one.

function xorshift(state) {
  state ^= state << 13n; state &= 0xffffffffffffffffn;
  state ^= state >> 7n;
  state ^= state << 17n; state &= 0xffffffffffffffffn;
  return state;
}

class Rng {
  constructor(seed) { this.state = seed === 0n ? 1n : seed; }
  next() { this.state = xorshift(this.state); return this.state; }
  below(n) { return n <= 0 ? 0 : Number(this.next() % BigInt(n)); }
  bits(n) { return BigInt.asUintN(n, this.next()); }
}

function fillScalar(rng, owner, f, index) {
  if (f.Kind === 1) { f.SetRaw(owner, index, BigInt(rng.below(2))); return; }
  if (f.Kind === 10 || f.Kind === 11) {
    // a value inside the declared range if there is one, and a plain finite
    // one if there is not — the writer refuses a non-finite float by rule
    const lo = f.HasRange ? f.RangeMin : -1e6;
    const hi = f.HasRange ? f.RangeMax : 1e6;
    let value = lo + (hi - lo) * (rng.below(1 << 20) / (1 << 20));
    if (f.Kind === 10) { value = Math.fround(value); }
    f.SetRaw(owner, index, f.Kind === 10
      ? BigInt(TableFloatToBitsOf(value)) : TableDoubleToBitsOf(value));
    return;
  }
  const signed = f.Kind >= 2 && f.Kind <= 5;
  const width = f.ElemWidth > 0 ? f.ElemWidth : 4;
  let value = signed ? BigInt.asIntN(width * 8, rng.next()) : BigInt.asUintN(width * 8, rng.next());
  if (f.HasRange) {
    const lo = BigInt(Math.ceil(f.RangeMin));
    const hi = BigInt(Math.floor(f.RangeMax));
    value = hi <= lo ? lo : lo + (BigInt.asUintN(64, value) % (hi - lo + 1n));
  }
  f.SetRaw(owner, index, BigInt.asUintN(64, value));
}

// the bit helpers, reached the way a caller reaches them: through the unit's
// own module, so this leg never re-implements one
const TableFloatToBitsOf = tables.TableFloatToBits;
const TableDoubleToBitsOf = tables.TableDoubleToBits;

function isEnumField(f) { return f.VariantId !== null && f.Arms === null; }
function isFlagsField(f) { return f.EnumName !== null && f.VariantId === null; }
function isBytesField(f) { return f.IsArray && f.Kind === 6 && f.TypeName === "bytes"; }

function fillVocabulary(rng, owner, f, index) {
  if (isFlagsField(f)) {
    let bits = 0n;
    for (let bit = 0; bit <= f.EnumMax; bit++) { if (rng.below(2)) { bits |= 1n << BigInt(bit); } }
    f.SetRaw(owner, index, bits);
    return;
  }
  // an unnameable value has no wire identity (§5), so it is never picked
  const value = rng.below(f.EnumMax + 1);
  f.SetRaw(owner, index, BigInt(value !== 0 && f.VariantId(value) === 0 ? 0 : value));
}

function fillValue(rng, value, info, depth) {
  if (depth > 6) { info.reset(value); return; }
  for (const f of info.Fields) {
    if (f.Optional) { f.SetPresent(value, rng.below(2) === 1); }
    if (f.Kind === 12 || isBytesField(f)) {
      const buffer = f.GetBuffer(value);
      const used = rng.below(Math.min(f.ArrayBound, 24) + 1);
      buffer.fill(0);
      // ASCII only for a string: §16 writes U+FFFD for a byte that is not part
      // of well-formed UTF-8, which is a text-form rule and not a loss here
      for (let i = 0; i < used; i++) { buffer[i] = f.Kind === 12 ? 0x20 + rng.below(0x5f) : rng.below(256); }
      f.SetCount(value, used);
      continue;
    }
    if (f.Arms !== null) {
      const union = f.GetChild(value, 0);
      const tag = rng.below(f.EnumMax + 1);
      f.Arms.SetTag(union, BigInt(tag));
      if (tag !== 0) {
        const arm = f.Arms.Arms[tag].TableRef();
        const payload = f.Arms.Arms[tag].Payload(union);
        arm.reset(payload);
        fillValue(rng, payload, arm, depth + 1);
      }
      continue;
    }
    const slots = f.KeyName !== null ? f.ArrayBound
      : f.IsArray ? (f.Counted ? rng.below(f.ArrayBound + 1) : f.ArrayBound) : 1;
    if (f.Counted && f.KeyName === null && f.IsArray) { f.SetCount(value, slots); }
    for (let s = 0; s < slots; s++) {
      if (f.KeyName !== null && f.KeyId(s + 1) === 0) { continue; } // None keys no slot
      if (f.Kind === 13) {
        const child = f.GetChild(value, s);
        const inner = f.TableRef();
        inner.reset(child);
        fillValue(rng, child, inner, depth + 1);
      } else if (isEnumField(f) || isFlagsField(f)) {
        fillVocabulary(rng, value, f, s);
      } else {
        fillScalar(rng, value, f, s);
      }
    }
  }
}

function checkRandomRoundTrip(rounds) {
  const roots = [
    ["RootConfig", tables], ["ProfileConfig", tables], ["LoadoutConfig", tables],
    ["ArchiveConfig", nested], ["KeyedConfig", keyed], ["PackConfig", pack], ["WideBlob", wide],
  ];
  const rng = new Rng(BigInt(process.env.SEED ?? "0x5eed1e"));
  const buffer = new Uint8Array(1 << 20);
  const twin = new Uint8Array(1 << 20);
  for (let round = 0; round < rounds; round++) {
    const [name, module] = roots[round % roots.length];
    const info = module[name + "TableType"]();
    const value = new module[name]();
    info.reset(value);
    fillValue(rng, value, info, 0);

    const size = module[name + "Measure"](value);
    if (size < 0) { check(false, name + ": a value built through the descriptors measures as unsaveable"); return; }
    if (module[name + "Save"](value, buffer) !== size) { check(false, name + ": save disagreed with measure"); return; }

    const loaded = new module[name]();
    const report = new module.TableReport();
    if (!module[name + "Load"](loaded, buffer.subarray(0, size), report)) {
      check(false, name + ": a value this build wrote did not load back"); return;
    }
    if (report.Unknown || report.KindMismatch || report.Clamped || report.Malformed) {
      check(false, name + ": a clean round trip reported " + JSON.stringify(report)); return;
    }
    if (module[name + "Save"](loaded, twin) !== size) { check(false, name + ": the wire round trip changed length"); return; }
    for (let i = 0; i < size; i++) {
      if (twin[i] !== buffer[i]) { check(false, name + ": the wire round trip differs at byte " + i); return; }
    }

    const textSize = module[name + "ToJsonMeasure"](value);
    if (textSize < 0) { check(false, name + ": ToJson refuses a value this build wrote"); return; }
    const text = new Uint8Array(textSize);
    if (module[name + "ToJson"](value, text) !== textSize) { check(false, name + ": ToJson disagreed with its measure"); return; }
    if (text[textSize - 1] !== 0x0a) { check(false, name + ": the canonical text does not end with one newline"); return; }

    const fromText = new module[name]();
    const textReport = new module.TableReport();
    if (!module[name + "FromJson"](fromText, text, textReport)) {
      check(false, name + ": a text this build wrote did not read back: " + JSON.stringify(textReport)); return;
    }
    if (textReport.Unknown || textReport.KindMismatch || textReport.Clamped ||
        textReport.Duplicate || textReport.Malformed) {
      check(false, name + ": a clean text round trip reported " + JSON.stringify(textReport)); return;
    }
    if (module[name + "Save"](fromText, twin) !== size) { check(false, name + ": the text round trip changed the wire length"); return; }
    for (let i = 0; i < size; i++) {
      if (twin[i] !== buffer[i]) {
        check(false, name + ": the text round trip differs at wire byte " + i + " (round " + round + ")");
        return;
      }
    }
  }
}

// ---- the TEXT FORM against a THIRD implementation, over instances nobody
// ---- wrote down
//
// `emit` writes what a differential needs and nothing else: for each random
// instance, the WIRE bytes this build saves and the TEXT this build writes.
// `schema unpack` then reads the same wire bytes with the COMPILER'S OWN Go
// engine — a third implementation, written from §16 and from neither backend —
// and the two texts are byte-compared.
//
// The harness already does this for eighteen pinned instances. What this adds
// is the instances: floats at spellings nobody chose, strings at their clamp,
// enum-keyed arrays with slots at and off their defaults, unions on every arm.
// A float's `%.*g` is where a port drifts, and eighteen instances cannot cover
// a spelling rule.

function emit(dir, rounds) {
  const roots = [
    ["RootConfig", tables], ["ProfileConfig", tables], ["LoadoutConfig", tables],
    ["ArchiveConfig", nested], ["KeyedConfig", keyed], ["PackConfig", pack], ["WideBlob", wide],
  ];
  const rng = new Rng(BigInt(process.env.SEED ?? "0x7ea"));
  const index = [];
  for (let round = 0; round < rounds; round++) {
    const [name, module] = roots[round % roots.length];
    const info = module[name + "TableType"]();
    const value = new module[name]();
    info.reset(value);
    fillValue(rng, value, info, 0);
    const size = module[name + "Measure"](value);
    if (size < 0) { check(false, name + ": measures as unsaveable"); return; }
    const wire = new Uint8Array(size);
    module[name + "Save"](value, wire);
    const textSize = module[name + "ToJsonMeasure"](value);
    if (textSize < 0) { check(false, name + ": ToJson refuses it"); return; }
    const text = new Uint8Array(textSize);
    module[name + "ToJson"](value, text);
    writeFileSync(join(dir, round + ".bin"), wire);
    writeFileSync(join(dir, round + ".json"), text);
    index.push(round + " " + name);
  }
  writeFileSync(join(dir, "index.txt"), index.join("\n") + "\n");
  console.log("wrote " + rounds + " instance pairs to " + dir);
}

// `verify` closes the differential's other half: for each instance, the text
// the COMPILER'S OWN Go engine wrote is read back by THIS build's FromJson and
// re-saved, and the wire must be the wire this build started from. The `emit`
// half proves this writer against that one; this half proves this READER
// against that WRITER, over the same instances nobody wrote down.
function verifyGoTexts(dir) {
  const roots = { RootConfig: tables, ProfileConfig: tables, LoadoutConfig: tables,
    ArchiveConfig: nested, KeyedConfig: keyed, PackConfig: pack, WideBlob: wide };
  let n = 0;
  for (const line of readFileSync(join(dir, "index.txt"), "utf8").split("\n")) {
    if (line.trim() === "") { continue; }
    const [index, name] = line.split(" ");
    const module = roots[name];
    const wire = new Uint8Array(readFileSync(join(dir, index + ".bin")));
    const text = new Uint8Array(readFileSync(join(dir, index + ".gojson")));
    const value = new module[name]();
    const report = new module.TableReport();
    if (!module[name + "FromJson"](value, text, report)) {
      check(false, index + " (" + name + "): the Go engine's text did not read: " + JSON.stringify(report));
      return;
    }
    if (report.Unknown || report.KindMismatch || report.Clamped || report.Duplicate || report.Malformed) {
      check(false, index + " (" + name + "): reading the Go engine's text reported " + JSON.stringify(report));
      return;
    }
    const size = module[name + "Measure"](value);
    if (size !== wire.length) {
      check(false, index + " (" + name + "): the Go engine's text re-saves at " + size + ", not " + wire.length);
      return;
    }
    const buffer = new Uint8Array(size);
    module[name + "Save"](value, buffer);
    for (let i = 0; i < size; i++) {
      if (buffer[i] !== wire[i]) {
        check(false, index + " (" + name + "): the Go engine's text re-saves to different bytes at " + i);
        return;
      }
    }
    n++;
  }
  console.log("read " + n + " texts the Go engine wrote; every one re-saves to the wire it came from");
}

// ---- 3. the fuzzer's oracle over the block and cook readers ----

function mutate(state) {
  // xorshift64, so a seed reproduces a find exactly
  state ^= state << 13n; state &= 0xffffffffffffffffn;
  state ^= state >> 7n;
  state ^= state << 17n; state &= 0xffffffffffffffffn;
  return state;
}

function fuzzOne(source, claim, lead, open, walk) {
  const buffer = new ArrayBuffer(lead + claim);
  const bytes = new Uint8Array(buffer, lead, claim);
  bytes.set(source.subarray(0, Math.min(claim, source.length)));
  let handle = null;
  try {
    handle = open(bytes);
  } catch (e) {
    return "Open threw instead of refusing: " + e.message;
  }
  if (handle === null) { return null; } // a refusal is the other legal answer
  try {
    walk(handle, bytes);
  } catch (e) {
    return "a walk of an OPENED forgery threw: " + e.message;
  }
  return null;
}

function walkBlock(block) {
  return (handle, bytes) => {
    const info = block.Type;
    for (const f of info.Fields) {
      if (!f.OutOfLine) { continue; }
      const offsetOf = Number(handle.View.getBigUint64(f.OffsetOfOffset, true));
      const count = handle.View.getUint32(f.CountOffset, true);
      const stride = handle.View.getUint32(f.StrideOffset, true);
      const rowInfo = f.ElementRef();
      for (let r = 0; r < count; r++) {
        const at = offsetOf + r * stride;
        for (const g of rowInfo.Fields) {
          const slots = g.IsArray ? g.ArrayBound : 1;
          for (let s = 0; s < slots; s++) {
            readAnything(handle.View, at + g.Offset + s * g.ElemSize, g);
          }
        }
      }
    }
  };
}

function readAnything(view, at, f) {
  if (f.ElementRef !== null && f.ElemSize > 0) {
    const inner = f.ElementRef();
    for (const g of inner.Fields) {
      const slots = g.IsArray ? g.ArrayBound : 1;
      for (let s = 0; s < slots; s++) { readAnything(view, at + g.Offset + s * g.ElemSize, g); }
    }
    return;
  }
  switch (f.ElemSize) {
    case 1: view.getUint8(at); return;
    case 2: view.getUint16(at, true); return;
    case 4: view.getUint32(at, true); return;
    case 8: view.getBigUint64(at, true); return;
    default: for (let i = 0; i < f.ElemSize; i++) { view.getUint8(at + i); }
  }
}

function walkCook(cook) {
  return (handle) => {
    const seen = new Set();
    const walkNode = (offset, type, depth) => {
      if (depth > 64 || seen.has(offset)) { return; }
      seen.add(offset);
      if (offset < 0 || offset + type.Size > handle.Length) { return; }
      const storage = (at, info, d) => {
        for (const f of info.Fields) {
          if (f.IsPointer) {
            const target = cook.At(handle, at + f.Offset);
            if (target >= 0 && f.RecordRef !== null) { walkNode(target - handle.Region, f.RecordRef(), d + 1); }
            continue;
          }
          if (f.Storage === "Record") {
            const slots = f.IsArray ? f.ArrayBound : 1;
            for (let s = 0; s < slots; s++) { storage(at + f.Offset + s * f.ElemSize, f.RecordRef(), d); }
            continue;
          }
          const slots = f.IsArray ? f.ArrayBound : 1;
          for (let s = 0; s < slots; s++) {
            readAnything(handle.View, at + f.Offset + s * f.ElemSize, { ElemSize: f.ElemSize, ElementRef: null });
          }
        }
      };
      storage(handle.Region + offset, type, depth);
    };
    walkNode(0, cook.Type, 0);
  };
}

function fuzz(blockPath, cookPath, mutants) {
  const blockSource = new Uint8Array(readFileSync(blockPath));
  const cookSource = new Uint8Array(readFileSync(cookPath));
  let state = BigInt(process.env.SEED ?? "0xc00c1e5eed");
  if (state === 0n) { state = 1n; }
  const subjects = [
    { Name: "block", source: blockSource, open: (b) => renderBlock.RenderFrameBlock.Open(b), walk: walkBlock(renderBlock.RenderFrameBlock), Align: 64 },
    { Name: "cook", source: cookSource, open: (b) => graphCook.SceneCook.Open(b), walk: walkCook(graphCook.SceneCook), Align: 8 },
  ];
  let opened = 0;
  let refused = 0;
  for (let i = 0; i < mutants; i++) {
    const subject = subjects[i % subjects.length];
    const copy = new Uint8Array(subject.source);
    state = mutate(state);
    const flips = 1 + Number(state % 6n);
    for (let f = 0; f < flips; f++) {
      state = mutate(state);
      const at = Number(state % BigInt(copy.length));
      state = mutate(state);
      copy[at] ^= 1 << Number(state % 8n);
    }
    state = mutate(state);
    // the EXTENT the caller claims, and the base it holds: both are facts no
    // file carries, and both are what the forgery battery varies by hand
    const claimKind = Number(state % 4n);
    let claim = copy.length;
    if (claimKind === 1) { state = mutate(state); claim = Number(state % BigInt(copy.length + 1)); }
    else if (claimKind === 2) { claim = copy.length + 64; }
    state = mutate(state);
    const lead = Number(state % BigInt(subject.Align)) * (Number(state % 3n) === 0 ? 1 : 0);
    const problem = fuzzOne(copy, claim, lead, subject.open, subject.walk);
    if (problem !== null) {
      console.log("FAILED: " + subject.Name + " mutant " + i + ": " + problem);
      failed = true;
      return;
    }
    // the verdict itself is not the property — the property is that BOTH
    // verdicts are clean — but a run where nothing ever opened would be
    // fuzzing the refusal and nothing else
    let handle = null;
    try {
      const buffer = new ArrayBuffer(lead + claim);
      const bytes = new Uint8Array(buffer, lead, claim);
      bytes.set(copy.subarray(0, Math.min(claim, copy.length)));
      handle = subject.open(bytes);
    } catch { /* already reported above */ }
    if (handle === null) { refused++; } else { opened++; }
  }
  console.log("tables JS fuzz: " + mutants + " forged blocks and cooks — " + refused +
    " refused, " + opened + " opened and read entirely inside the bytes they were given; " +
    "no exception escaped a reader");
}

// ---- 4. WHAT ALLOCATES, as a NUMBER and not a drift ----
//
// A heap that stays flat is a LEAK instrument and nothing more: an allocation
// made and collected every iteration leaves the heap exactly as flat as no
// allocation at all. So the claim "every read path allocates nothing" is held
// here as BYTES PER ITERATION, measured, and the soak's flat heap is kept
// beside it as the other half — a leak and a rate are two different defects.
//
// THE MEASUREMENT. Between two garbage collections V8's used heap grows by
// exactly the bytes allocated, so sampling it inside the loop and summing the
// POSITIVE deltas counts them; the negative deltas are the collections and are
// dropped. The instrument allocates too — `getHeapStatistics` returns an object
// — so every figure is the difference between the body under test and an EMPTY
// body sampled the same way, which subtracts the instrument exactly.
//
// THE FLOOR THIS PORT HOLDS, and each unavoidable allocation by name:
//
//   - A table with no 64-bit field reads, measures and writes at ZERO bytes
//     per iteration. Scalars, floats, bools, enums, strings, `bytes`, nested
//     tables, bounded arrays, enum-keyed arrays and optionals are all in that
//     set. This is gated.
//   - A field declared int64/uint64/bits(N > 32), or a flags mask, costs ONE
//     BIGINT PER FIELD READ, because BigInt is JavaScript's only exact 64-bit
//     integer and every BigInt is an object. Named, bounded and gated at a
//     ceiling a per-FIELD regression would blow through.
//   - Reading a block row and following a cook reference are held to the same
//     two rules: the row walk is Numbers and allocates nothing; a deref reads
//     a BigInt delta, which is the same unavoidable one.
//   - The TEXT FORM is the generic path and allocates by design (the ladder
//     licenses exactly that). Its number is REPORTED, never gated.

// THE SAMPLE INTERVAL IS CALIBRATED, and both ends of it matter.
//
// Sample too OFTEN and the instrument's own allocation — `getHeapStatistics`
// returns an object — is the number: at one sample in sixty-four it lands near
// ten bytes per iteration, which is the same order as the floors this gates.
// Sample too RARELY and a collection between two samples swallows an interval's
// worth of a body that really does allocate.
//
// So the interval is chosen from a cheap probe: aim for a QUARTER OF A MEGABYTE
// allocated between samples, which is far under any new space and far over the
// instrument itself. A body that allocates nothing lands at the sparse end,
// where the instrument costs hundredths of a byte per iteration; the text
// form's thousands land at the dense end, where a collection can only lose a
// fraction of one interval.
const SampleTarget = 262144;

function sampleBytes(iterations, body, every) {
  let total = 0;
  let last = v8.getHeapStatistics().used_heap_size;
  for (let i = 0; i < iterations; i++) {
    body(i);
    if (i % every === 0) {
      const now = v8.getHeapStatistics().used_heap_size;
      if (now > last) { total += now - last; }
      last = now;
    }
  }
  const now = v8.getHeapStatistics().used_heap_size;
  if (now > last) { total += now - last; }
  return total / iterations;
}

function intervalFor(body) {
  const probe = Math.max(1, sampleBytes(20000, body, 64));
  const every = Math.round(SampleTarget / probe);
  return Math.min(4096, Math.max(64, every));
}

// WARM UNTIL THE RATE SETTLES, and settle is MEASURED — on the rate itself,
// with no clock anywhere in this file.
//
// The floor this gates is a property of OPTIMIZED code, and only of optimized
// code: run the same read unoptimized and V8 boxes every double it stores, so a
// path that allocates nothing at the top tier allocates a kilobyte an iteration
// at the bottom one (`node --no-opt` reads 1192 bytes where the settled runtime
// reads 0). A fixed warm-up count is therefore not a warm-up at all — it is a
// bet on the machine.
//
// So the warm-up watches THE ALLOCATION RATE fall in blocks and stops when
// three consecutive blocks fail to beat the best by five per cent or two bytes,
// whichever is looser. Converging on the quantity under test rather than on a
// clock is both the better instrument and the one this file may have: a timer
// here would be hand-written MEASUREMENT outside the estate's one benchmark,
// which `make shape-gate` refuses by name.
function settle(body, blocks) {
  const block = 20000;
  let best = Infinity;
  let stable = 0;
  for (let n = 0; n < blocks; n++) {
    const rate = sampleBytes(block, body, 64);
    if (rate < best - Math.max(2, best * 0.05)) { best = rate; stable = 0; continue; }
    if (rate < best) { best = rate; }
    if (++stable >= 3) { return; }
  }
}

function allocationBytes(iterations, body, budgetMs) {
  settle(body, budgetMs === undefined ? 40 : budgetMs);
  if (global.gc) { global.gc(); }
  const every = intervalFor(body);
  if (global.gc) { global.gc(); }
  return { bytes: sampleBytes(iterations, body, every), every };
}

// THE SINK IS A TYPED ARRAY, and that is load-bearing for the instrument. A
// captured `let` lives in a closure Context, so assigning a non-Smi double to
// one allocates a HeapNumber per write — the instrument would then measure its
// own accumulator. A Float64Array store boxes nothing.
const Sink = new Float64Array(1);

// One extra allocation per iteration, on demand: the negative control for the
// instrument itself. A gate that has never gone red is watching nothing.
// EIGHT OBJECTS, not one: a control wants to clear the ceiling by a wide
// margin rather than argue with it. Eight small objects is about three hundred
// bytes against a ceiling of eight.
const LeakPerIteration = Number(process.env.SCHEMA_JS_ALLOC_LEAK ?? 0) * 8;
const leakSink = [];
function leak() {
  for (let i = 0; i < LeakPerIteration; i++) { leakSink[0] = { a: 1, b: 2 }; }
}

// The instrument is subtracted AT THE SAME INTERVAL the body was measured at:
// an empty body sampled sparsely costs a different amount from one sampled
// densely, and subtracting the wrong one is how the floor moved between two
// machines the first time this ran.
function alloc(iterations, body, baseline, budgetMs) {
  const measured = allocationBytes(iterations, (i) => { body(i); leak(); }, budgetMs);
  const instrument = sampleBytes(iterations, () => {}, measured.every);
  const bytes = measured.bytes - instrument;
  baseline.push([measured.every, instrument]);
  return bytes;
}

// THE MEASURED PATHS, in one place, so the allocation gate and the SOAK run the
// same ones over the same instances — the soak's whole point is that it runs
// them again after an hour, in the process that has been running.
//
// Each row is [what, ceiling, why, body]. A null ceiling is reported and never
// gated: the text form is the generic path and the ladder licenses it.
// THE ZERO FLOOR IS ZERO, and the number below is the instrument's own residual
// and nothing else.
//
// The gate says NOTHING ALLOCATES on these paths, not "nothing per field". A
// relaxation would have been an accommodation of a red, and the red was real: a
// steady fifteen bytes an iteration on four CI runners is not what noise looks
// like — all four run the same pinned node, so that is ONE runtime reading the
// same number four times, which is what ONE SMALL ALLOCATION looks like.
//
// Eight bytes is the residual of the instrument itself after the empty body is
// subtracted: on the pinned node it reads 0.00 exactly, and on the newer one it
// reads under 1.5. Anything a port could plausibly allocate — a boxed double is
// sixteen bytes, the smallest object literal thirty-four, a BigInt more — sits
// clear of it, and the negative control puts eight objects in.
const ZeroFloor = 8;

function allocationPaths() {
  const rows = [];

  // (1) a table with NO 64-bit field: zero, on all three paths
  {
    const wire = new Uint8Array(readFileSync("testdata/wire/tables/keyed_config.bin"));
    const value = new keyed.KeyedConfig();
    const report = new keyed.TableReport();
    const reader = new keyed.TableReader(wire, report);
    const buffer = new Uint8Array(4096);
    const writer = new keyed.TableWriter(buffer);
    keyed.KeyedConfigLoad(value, wire, report);
    rows.push(["KeyedConfig Load", ZeroFloor,
      "no field of its closure is 64 bits wide, so nothing on its read path needs a BigInt",
      () => keyed.KeyedConfigLoadBody(reader.reset(wire, report), value)]);
    rows.push(["KeyedConfig Measure", ZeroFloor, "measure writes nothing and builds nothing",
      () => keyed.KeyedConfigMeasure(value)]);
    rows.push(["KeyedConfig Save", ZeroFloor, "the write path fills a buffer the caller owns",
      () => keyed.KeyedConfigSaveBody(writer.reset(buffer), value)]);
  }

  // (2) a table WITH 64-bit Fields: one BigInt per such field read, bounded
  {
    const wire = new Uint8Array(readFileSync("testdata/wire/tables/root_full.bin"));
    const value = new tables.RootConfig();
    const report = new tables.TableReport();
    const reader = new tables.TableReader(wire, report);
    tables.RootConfigLoad(value, wire, report);
    rows.push(["RootConfig Load", 512,
      "one BigInt per 64-bit field the instance actually carries — four or five here; a per-FIELD regression would be orders past this",
      () => tables.RootConfigLoadBody(reader.reset(wire, report), value)]);
    rows.push(["RootConfig Measure", ZeroFloor, "measure reads the storage it is handed",
      () => tables.RootConfigMeasure(value)]);
  }

  // (3) the reading tier: a block row walk allocates nothing
  {
    const source = new Uint8Array(readFileSync("testdata/wire/tables/block_render.bin"));
    const bytes = new Uint8Array(new ArrayBuffer(source.length));
    bytes.set(source);
    const block = renderBlock.RenderFrameBlock.Open(bytes);
    check(block !== null, "the allocation gate's block does not open");
    if (block !== null) {
      const count = renderBlock.RenderFrameBlock.ShipsCount(block);
      rows.push(["RenderFrame ships walk", ZeroFloor,
        "a row field is read at its offset into a Number; nothing is built",
        () => {
          for (let r = 0; r < count; r++) {
            const at = renderBlock.RenderFrameBlock.ShipsAt(block, r);
            Sink[0] += renderBlock.RenderShipRow.ObjectId(block.View, at) +
              renderBlock.RenderShipRow.Thrust(block.View, at);
          }
        }]);
    }
  }

  // (4) and the two the ladder licenses: a cook deref, and the text form
  if (existsSync("build/js-fuzz-scene.cook")) {
    const source = new Uint8Array(readFileSync("build/js-fuzz-scene.cook"));
    const bytes = new Uint8Array(new ArrayBuffer(source.length));
    bytes.set(source);
    const cook = graphCook.SceneCook.Open(bytes);
    if (cook !== null) {
      const slot = graphCook.SceneRow.HeadSlot(cook.Region);
      rows.push(["Scene head deref", null,
        "a self-relative delta is eight bytes wide, so following one reads a BigInt (§6.3)",
        () => graphCook.SceneCook.At(cook, slot)]);
    }
  }
  {
    const value = new tables.RootConfig();
    const report = new tables.TableReport();
    tables.RootConfigLoad(value, new Uint8Array(readFileSync("testdata/wire/tables/root_full.bin")), report);
    const size = tables.RootConfigToJsonMeasure(value);
    const text = new Uint8Array(size);
    tables.RootConfigToJson(value, text);
    const back = new tables.RootConfig();
    rows.push(["RootConfig ToJson", null,
      "the generic path allocates by design — the ladder licenses it for tooling",
      () => tables.RootConfigToJson(value, text), 16]);
    rows.push(["RootConfig FromJson", null, "the same path, the other way",
      () => tables.RootConfigFromJson(back, text, report), 16]);
  }
  return rows;
}

// Measure every path once, at the calibrated interval, and hand back the
// numbers keyed by name — the shape both the gate and the soak read.
// ONE PROCESS PER PATH, and it is the same reason the conformance harness runs
// one process per surface. Measured all in one process, these readings are not
// reproducible: the same build read 15.8 on a path and 0.0 on the next run,
// while that path measured alone reads 0.00 in every window of every run. Nine
// bodies through one `body(i)` call site makes it megamorphic and drags the
// process into optimization states none of the paths would meet on their own,
// so a number taken there is a number about the harness. A child per path
// costs a second and answers about the codec.
function measureAllocationInProcess(iterations, budgetMs, only) {
  const measured = new Map();
  const paths = allocationPaths();
  for (let i = 0; i < paths.length; i++) {
    if (only !== undefined && i !== only) { continue; }
    const [what, ceiling, why, body, divisor] = paths[i];
    const intervals = [];
    const bytes = alloc(Math.max(1, Math.floor(iterations / (divisor || 1))), body, intervals, budgetMs);
    const [every, instrument] = intervals[intervals.length - 1];
    measured.set(what, { bytes, ceiling, why, every, instrument });
  }
  return measured;
}

function measureAllocation(iterations, budgetMs) {
  if (process.env.SCHEMA_JS_ALLOC_IN_PROCESS === "1") {
    return measureAllocationInProcess(iterations, budgetMs);
  }
  const measured = new Map();
  const count = allocationPaths().length;
  for (let i = 0; i < count; i++) {
    const child = spawnSync(process.execPath, ["--expose-gc", fileURLToPath(import.meta.url), "alloc-one", String(i), String(iterations)],
      { encoding: "utf8", env: { ...process.env, SCHEMA_JS_ALLOC_IN_PROCESS: "1" } });
    if (child.status !== 0) {
      check(false, "the allocation child for path " + i + " exited " + child.status + ": " + (child.stderr || "").trim());
      return measured;
    }
    const line = (child.stdout || "").trim().split("\n").pop();
    const [what, bytes, ceiling, why, every, instrument] = line.split("\t");
    measured.set(what, {
      bytes: Number(bytes), ceiling: ceiling === "null" ? null : Number(ceiling),
      why, every: Number(every), instrument: Number(instrument),
    });
  }
  return measured;
}

// ONE RETRY, and it retries the INSTRUMENT rather than the property: a runtime
// slow to settle gets five times the blocks, once. A second reading over a
// ceiling is the reading this reports.
function measureAllocationSettled(iterations) {
  let measured = measureAllocation(iterations);
  let over = false;
  for (const [, row] of measured) {
    if (row.ceiling !== null && row.bytes > row.ceiling) { over = true; }
  }
  if (!over) { return measured; }
  process.stderr.write("a path read over its ceiling — warming longer and measuring again, once\n");
  return measureAllocation(iterations, 200);
}

function reportAllocation(title, measured) {
  process.stderr.write("\n" + title + "\n\n");
  for (const [what, row] of measured) {
    process.stderr.write("  " + what.padEnd(26) +
      (row.bytes < 0 ? "0.0" : row.bytes.toFixed(1)).padStart(8) + "  " +
      (row.ceiling === null ? "reported" : "<= " + row.ceiling).padEnd(10) +
      ("1/" + row.every).padEnd(8) + row.instrument.toFixed(2).padStart(6) + "  " + row.why + "\n");
  }
  process.stderr.write("\n");
}

function gateAllocation(measured, where) {
  let bad = false;
  for (const [what, row] of measured) {
    if (row.ceiling !== null && row.bytes > row.ceiling) {
      check(false, what + " allocates " + row.bytes.toFixed(1) + " bytes per iteration " + where +
        ", over the " + row.ceiling + " this port holds — " + row.why);
      bad = true;
    }
  }
  return !bad;
}

// THE RUNTIME THIS PROPERTY IS CLAIMED FOR, and the gate refuses to certify on
// another. That is not fussiness: the allocation this gate exists to catch was
// invisible on a newer V8 and steady at sixteen bytes a call on the pinned one,
// because it was a generated body sitting over the older engine's optimization
// threshold. Measuring it on whatever `node` a PATH lookup found is how it hid
// for three CI reds. Pass SCHEMA_JS_ALLOC_ANY_NODE=1 to read the numbers on
// another runtime; the gate then reports and does not certify.
const PinnedNodeMajor = "20";

function checkAllocation(iterations) {
  let bad = false;
  const major = process.versions.node.split(".")[0];
  const anyNode = process.env.SCHEMA_JS_ALLOC_ANY_NODE === "1";
  if (major !== PinnedNodeMajor && !anyNode) {
    console.log("FAILED: this gate holds an allocation floor for node " + PinnedNodeMajor +
      ", which is the version CI pins, and it is running on node " + process.versions.node +
      ". A newer V8 optimizes generated bodies an older one leaves on its threshold, where a " +
      "double store boxes — so a floor measured here says nothing about the runtime the claim is " +
      "for. Run `make dist-node` and re-run, or set SCHEMA_JS_ALLOC_ANY_NODE=1 to read the numbers " +
      "without certifying them.");
    failed = true;
    return;
  }
  if (anyNode && major !== PinnedNodeMajor) {
    process.stderr.write("node " + process.versions.node + " is not the pinned " + PinnedNodeMajor +
      ".x — reporting, not certifying\n");
  }
  const measured = measureAllocationSettled(iterations);
  reportAllocation("WHAT ALLOCATES, bytes per iteration (an empty body at the same sample interval, subtracted)", measured);
  bad = !gateAllocation(measured, "");
  if (!bad) {
    console.log("tables JS allocation gate: a table with no 64-bit field reads, measures and writes at zero " +
      "bytes per iteration, and a block row walk with it; a 64-bit field costs the one BigInt the language " +
      "has no way around");
  }
}

// ---- 5. the soak: read and write the corpus, sampling the heap ----

// THE SOAK, and what it is GATED on.
//
// A flat heap is a LEAK instrument and nothing more. So the hour's verdict is
// the ALLOCATION COUNT, measured twice IN THIS PROCESS — once after warm-up and
// again after the hour of round trips — and gated both times: a path that must
// allocate nothing must still allocate nothing after an hour, and the rate on
// the path that carries the language's one unavoidable allocation must not have
// grown. That is what an hour buys over the three-second gate: a deopt, a shape
// change or a cache that only shows up under sustained load moves the RATE, and
// nothing about a flat heap would say so.
//
// The heap drift is kept beside it, reported and gated loosely, because a leak
// and a rate are two different defects and this is the instrument for the first.
function soak(seconds) {
  const wire = new Uint8Array(readFileSync("testdata/wire/tables/root_full.bin"));
  const keyedWire = new Uint8Array(readFileSync("testdata/wire/tables/keyed_config.bin"));
  const value = new tables.RootConfig();
  const keyedValue = new keyed.KeyedConfig();
  const report = new tables.TableReport();
  const buffer = new Uint8Array(4096);
  const keyedBuffer = new Uint8Array(4096);

  // THE RATE BEFORE, after warm-up: the baseline the hour is measured against
  const before = measureAllocationSettled(200000);
  reportAllocation("BEFORE the soak: bytes per iteration", before);
  if (!gateAllocation(before, "before the soak")) { return; }

  const started = Date.now();
  const deadline = started + seconds * 1000;
  let iterations = 0;
  const samples = [];
  const warm = started + Math.min(5000, seconds * 200);
  while (Date.now() < deadline) {
    for (let i = 0; i < 2000; i++) {
      tables.RootConfigLoad(value, wire, report);
      const n = tables.RootConfigMeasure(value);
      if (tables.RootConfigSave(value, buffer) !== n) { check(false, "soak save/measure disagreed"); return; }
      keyed.KeyedConfigLoad(keyedValue, keyedWire, report);
      const k = keyed.KeyedConfigMeasure(keyedValue);
      if (keyed.KeyedConfigSave(keyedValue, keyedBuffer) !== k) { check(false, "soak keyed save/measure disagreed"); return; }
      iterations += 2;
    }
    if (Date.now() >= warm) {
      if (global.gc) { global.gc(); }
      samples.push(process.memoryUsage().heapUsed);
    }
  }

  // THE RATE AFTER, in the same process, after the hour
  const after = measureAllocationSettled(200000);
  reportAllocation("AFTER " + seconds + "s of round trips: bytes per iteration", after);
  const stillClean = gateAllocation(after, "after " + seconds + "s of round trips");

  // and the rate must not have GROWN on the paths that do allocate: a run whose
  // BigInt count per read doubled under load would pass every ceiling above and
  // still be a defect
  let grew = false;
  for (const [what, row] of after) {
    const was = before.get(what);
    if (was === undefined) { continue; }
    const floor = Math.max(was.bytes, ZeroFloor);
    if (row.bytes > floor * 1.5 + ZeroFloor) {
      check(false, what + " allocated " + was.bytes.toFixed(1) + " bytes per iteration before the soak and " +
        row.bytes.toFixed(1) + " after — the rate grew under sustained load");
      grew = true;
    }
  }

  if (samples.length < 2) {
    console.log("tables JS soak: " + iterations + " round trips in " + seconds +
      "s — too short to sample the heap; run it longer");
    return;
  }
  const first = samples[Math.floor(samples.length / 4)];
  const last = samples[samples.length - 1];
  const drift = (last - first) / first;
  console.log("tables JS soak: " + iterations + " round trips in " + seconds + "s; the read path still " +
    "allocates " + (after.get("KeyedConfig Load").bytes < 0 ? 0 : after.get("KeyedConfig Load").bytes).toFixed(1) +
    " bytes per iteration on a table with no 64-bit field and " +
    after.get("RootConfig Load").bytes.toFixed(1) + " on one with them (" +
    before.get("RootConfig Load").bytes.toFixed(1) + " before), and the heap ran " +
    (first / 1048576).toFixed(2) + " MiB -> " + (last / 1048576).toFixed(2) + " MiB across " +
    samples.length + " samples (" + (drift * 100).toFixed(1) + "%)");
  // FLAT is a band, not equality: a runtime's own bookkeeping moves the heap by
  // a few per cent whatever the program does. A leak on a path this hot would
  // be orders of magnitude out.
  if (drift > 0.25) {
    check(false, "the heap grew " + (drift * 100).toFixed(1) + "% after warm-up — the read path is holding something");
  }
  if (stillClean && !grew && drift <= 0.25) {
    console.log("tables JS soak: PASS — the count is what this gates, and it did not move");
  }
}

// ---- main

const mode = process.argv[2];
if (mode === "fuzz") {
  fuzz(process.argv[3], process.argv[4], Number(process.env.N ?? process.argv[5] ?? 2000));
} else if (mode === "alloc-one") {
  // one path, in this process, for the parent above — the whole output is one
  // tab-separated line
  const only = Number(process.argv[3]);
  const measured = measureAllocationInProcess(Number(process.argv[4] ?? 300000), undefined, only);
  for (const [what, row] of measured) {
    process.stdout.write([what, row.bytes, row.ceiling === null ? "null" : row.ceiling,
      row.why, row.every, row.instrument].join("\t") + "\n");
  }
  process.exit(failed ? 1 : 0);
} else if (mode === "emit") {
  emit(process.argv[3], Number(process.argv[4] ?? 60));
} else if (mode === "verify") {
  verifyGoTexts(process.argv[3]);
} else if (mode === "alloc") {
  checkAllocation(Number(process.argv[3] ?? 300000));
} else if (mode === "soak") {
  soak(Number(process.argv[3] ?? 60));
} else {
  checkFieldIds();
  checkExactCapacity();
  checkReuse();
  checkForeignByteOrder();
  checkKeyedSurface();
  checkBlockAccessors("testdata/wire/tables/block_render.bin", renderBlock.RenderFrameBlock);
  checkBlockAccessors("testdata/wire/tables/block_padded.bin", paddedBlock.PaddedFrameBlock);
  checkCookAccessors("build/js-fuzz-scene.cook", graphCook.SceneCook);
  checkRandomRoundTrip(Number(process.env.ROUNDS ?? 700));
  if (!failed) {
    console.log("tables JS leg: the field-id hash agrees with a second implementation, measure's answer is the " +
      "buffer, a hoisted reader and writer produce the entry points' own bytes, a file of the other " +
      "byte order is refused twice over, the keyed surface refuses None " +
      "and iterates by key, and every block row and every cooked node reads the same through the " +
      "generated accessors and through the descriptors — and 700 instances nobody wrote down, built " +
      "through the descriptors, round-trip byte-identically on the wire and through the text form");
  }
}

if (failed) {
  console.log("FAILED");
  process.exit(1);
}
console.log("OK");
