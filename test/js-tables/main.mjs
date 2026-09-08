// The JavaScript TABLE accelerator test leg (docs/SPEC-TABLES.md) — what the
// conformance harness does NOT ask, because the harness asks every backend the
// same questions and these are this backend's own.
//
// This leg holds the properties that live beside the conformance matrix:
//
//   1. THE READING TIER'S OWN CLAIM: the generated accessors and the
//      descriptors are two spellings of one layout, so every field of every row
//      of every block, and every field of every cooked node, must read the same
//      value both ways. The harness proves the descriptors against C++; this
//      proves the accessors against the descriptors — including a pointer slot,
//      compared as its RAW DELTA before any resolution, because that is the
//      byte the two halves have to agree about.
//   2. THE OTHER BYTE ORDER, refused TWICE OVER: by the magic, whose bytes read
//      back reversed, and by the order word, which records what wrote the file.
//      A JavaScript reader reads at explicit little-endian offsets, so it has no
//      native path for a big-endian file to take and never grows one.
//   3. THE REFUSAL CONTRACT, under a fuzzer: a forged block or cook either
//      REFUSES or opens and reads entirely inside the bytes it was given. An
//      index out of bounds is a refusal, never an exception escaping the
//      reader — which in this language is the whole of the property, because a
//      DataView read past its view throws.
//   4. WHAT ALLOCATES, as a RATE and not a drift. A flat heap is a LEAK
//      instrument and nothing more — an allocation made and collected every
//      iteration leaves it exactly as flat as no allocation at all — so the
//      claim is held as BYTES PER ITERATION, measured per path, with the
//      floor stated and every unavoidable allocation named.
//
//   node main.mjs                 the gates
//   node main.mjs fuzz <block> <cook> [mutants]
//   node main.mjs alloc [iters]   bytes allocated per iteration, per path
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

// ---- 1. the accessors and the descriptors are one layout ----
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
          if (target !== null) { walk(target - handle.Region, f.RecordRef(), depth + 1); }
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


// ---- one module, one WHOLE surface: the nested type rows ride along ----
//
// A block's own module re-exports every record object of the unit, the `type`
// rows a table row nests included: RenderShipRow.PositionAt answers an offset
// only RenderVector3Row can read, and a consumer that imported RenderBlock.js
// must find it there.

function checkWholeSurface() {
  const source = new Uint8Array(readFileSync("testdata/wire/tables/block_render.bin"));
  const bytes = new Uint8Array(source);
  const block = renderBlock.RenderFrameBlock.Open(bytes);
  check(block !== null, "block_render does not open for the surface check");
  if (block === null) { return; }
  check(renderBlock.RenderVector3Row !== undefined && renderBlock.RenderQuaternionRow !== undefined,
    "RenderBlock.js does not re-export the type rows its own row accessors hand offsets into");
  if (renderBlock.RenderVector3Row === undefined) { return; }
  const at = renderBlock.RenderFrameBlock.ShipsAt(block, 0);
  const position = renderBlock.RenderShipRow.PositionAt(at);
  check(renderBlock.RenderVector3Row.X(block.View, position) ===
    blockdemoBlock.RenderVector3Row.X(block.View, position),
    "the re-exported RenderVector3Row is not the home's");
  // the same for a cook module: every record of the closure, wherever it is
  // defined, is reachable from the module the consumer imported
  for (const name of ["SceneRow", "ListNodeRow", "TreeNodeRow"]) {
    check(graphCook[name] !== undefined, "GraphCook.js does not re-export " + name);
  }
  // and a FLAGS bit is testable without a BigInt: Has agrees with the mask
  const count = renderBlock.RenderFrameBlock.ShipsCount(block);
  for (let r = 0; r < count; r++) {
    const row = renderBlock.RenderFrameBlock.ShipsAt(block, r);
    const mask = renderBlock.RenderShipRow.Flags(block.View, row);
    for (let bit = 0; bit < 64; bit++) {
      const expected = ((mask >> BigInt(bit)) & 1n) === 1n;
      if (renderBlock.RenderShipRow.FlagsHas(block.View, row, bit) !== expected) {
        check(false, "ship " + r + " FlagsHas(" + bit + ") disagrees with the mask " + mask);
        break;
      }
    }
  }
}

// ---- what Open answers, and what it throws ----
//
// Null means one thing: not this build's bytes. The CALLER's errors throw with
// the fix in the message: a view at an unaligned byteOffset — which is what a
// pooled Node Buffer under 4 KiB is — and no Uint8Array at all.

function checkOpenAnswers() {
  const source = new Uint8Array(readFileSync("testdata/wire/tables/block_render.bin"));
  const pooled = new Uint8Array(new ArrayBuffer(source.length + 8), 8, source.length);
  pooled.set(source);
  let message = "";
  try { renderBlock.RenderFrameBlock.Open(pooled); } catch (e) { message = e instanceof RangeError ? e.message : ""; }
  check(message.includes("new Uint8Array(bytes)"),
    "Open of a view at byteOffset 8 did not throw a RangeError naming the fix: " + message);
  check(renderBlock.RenderFrameBlock.Open(new Uint8Array(pooled)) !== null,
    "the same bytes copied into a fresh Uint8Array do not open");
  for (const notBytes of [null, undefined, "bytes", 7, new ArrayBuffer(64)]) {
    let threw = false;
    try { renderBlock.RenderFrameBlock.Open(notBytes); } catch (e) { threw = e instanceof TypeError; }
    check(threw, "block Open of " + String(notBytes) + " did not throw a TypeError");
    threw = false;
    try { graphCook.SceneCook.Open(notBytes); } catch (e) { threw = e instanceof TypeError; }
    check(threw, "cook Open of " + String(notBytes) + " did not throw a TypeError");
  }
  const cookPath = "build/js-fuzz-scene.cook";
  if (!existsSync(cookPath)) { return; }
  const cookSource = new Uint8Array(readFileSync(cookPath));
  const cookPooled = new Uint8Array(new ArrayBuffer(cookSource.length + 4), 4, cookSource.length);
  cookPooled.set(cookSource);
  message = "";
  try { graphCook.SceneCook.Open(cookPooled); } catch (e) { message = e instanceof RangeError ? e.message : ""; }
  check(message.includes("new Uint8Array(bytes)"), "cook Open of an unaligned view did not throw a RangeError naming the fix: " + message);
  // a null reference derefs to null; a reference that leaves the region throws
  const cook = graphCook.SceneCook.Open(new Uint8Array(cookSource));
  check(cook !== null, "the cook does not open for the At check");
  if (cook === null) { return; }
  const forged = new Uint8Array(cookSource);
  const forgedCook = graphCook.SceneCook.Open(forged);
  const slot = graphCook.SceneRow.HeadSlot(forgedCook.Region);
  new DataView(forged.buffer).setBigInt64(slot, 0n, true);
  check(graphCook.SceneCook.At(forgedCook, slot) === null, "a zero delta did not deref to null");
  new DataView(forged.buffer).setBigInt64(slot, 1n << 40n, true);
  message = "";
  try { graphCook.SceneCook.At(forgedCook, slot); } catch (e) { message = e instanceof RangeError ? e.message : ""; }
  check(message.includes("leaves the region"), "a delta that leaves the region did not throw a RangeError naming it: " + message);
}

// ---- 2. the fuzzer's oracle over the block and cook readers ----

function mutate(state) {
  // xorshift64, so a seed reproduces a find exactly
  state ^= state << 13n; state &= 0xffffffffffffffffn;
  state ^= state >> 7n;
  state ^= state << 17n; state &= 0xffffffffffffffffn;
  return state;
}

// A REFUSAL HAS TWO SPELLINGS: null for bytes that are not this build's, and
// a RangeError with the fix in it for the one thing the CALLER placed — a base
// at an unaligned byteOffset, which this fuzzer places on purpose. Any other
// exception out of Open is a defect. A walk of an opened forgery may meet a
// reference that leaves the region, which At refuses by name; a DataView's own
// exception from inside a row is a read that escaped, and the defect.
function fuzzOne(source, claim, lead, open, walk, align) {
  const buffer = new ArrayBuffer(lead + claim);
  const bytes = new Uint8Array(buffer, lead, claim);
  bytes.set(source.subarray(0, Math.min(claim, source.length)));
  let handle = null;
  try {
    handle = open(bytes);
  } catch (e) {
    if (e instanceof RangeError && lead % align !== 0 && e.message.includes("new Uint8Array(bytes)")) {
      return null; // the caller's placement, refused with the fix named
    }
    return "Open threw instead of refusing: " + e.message;
  }
  if (handle === null) { return null; } // a refusal is the other legal answer
  try {
    walk(handle, bytes);
  } catch (e) {
    if (e instanceof RangeError && e.message.includes("leaves the region")) {
      return null; // a reference the reader refused before following
    }
    return "a walk of an OPENED forgery threw: " + e.message;
  }
  return null;
}

function opensOrRefuses(open, bytes) {
  try {
    return open(bytes) !== null;
  } catch {
    return false;
  }
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
            if (target !== null && f.RecordRef !== null) { walkNode(target - handle.Region, f.RecordRef(), d + 1); }
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
    const problem = fuzzOne(copy, claim, lead, subject.open, subject.walk, subject.Align);
    if (problem !== null) {
      console.log("FAILED: " + subject.Name + " mutant " + i + ": " + problem);
      failed = true;
      return;
    }
    // the verdict itself is not the property — the property is that BOTH
    // verdicts are clean — but a run where nothing ever opened would be
    // fuzzing the refusal and nothing else
    const buffer = new ArrayBuffer(lead + claim);
    const bytes = new Uint8Array(buffer, lead, claim);
    bytes.set(copy.subarray(0, Math.min(claim, copy.length)));
    if (opensOrRefuses(subject.open, bytes)) { opened++; } else { refused++; }
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
// densely, and subtracting the wrong one moves the floor between two machines.
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
// subtracted: on one V8 major it reads 0.00 exactly and on another under 1.5
// (0.0 to 0.4 on the pinned 26.7.0, measured 2026-09-07). Anything a port could plausibly allocate — a boxed double is
// sixteen bytes, the smallest object literal thirty-four, a BigInt more — sits
// clear of it, and the negative control puts eight objects in.
const ZeroFloor = 8;

function allocationPaths() {
  const rows = [];

  // (1) the reading tier: a block row walk allocates nothing, a flags bit is
  // tested without a BigInt, and a 64-bit row field reads as one BigInt
  {
    const source = new Uint8Array(readFileSync("testdata/wire/tables/block_render.bin"));
    const bytes = new Uint8Array(new ArrayBuffer(source.length));
    bytes.set(source);
    const block = renderBlock.RenderFrameBlock.Open(bytes);
    check(block !== null, "the allocation gate's block does not open");
    if (block !== null) {
      const count = renderBlock.RenderFrameBlock.ShipsCount(block);
      rows.push(["RenderFrame ships walk", ZeroFloor,
        "a row field is read at its offset into a Number, a flags bit through one 32-bit word; nothing is built",
        () => {
          for (let r = 0; r < count; r++) {
            const at = renderBlock.RenderFrameBlock.ShipsAt(block, r);
            Sink[0] += renderBlock.RenderShipRow.ObjectId(block.View, at) +
              renderBlock.RenderShipRow.Thrust(block.View, at) +
              (renderBlock.RenderShipRow.FlagsHas(block.View, at, 1) ? 1 : 0);
          }
        }]);
      rows.push(["RenderFrame ships flags", 64 * count,
        "the whole mask through Flags is a BigInt per row — the licensed allocation, stated; FlagsHas beside it is the zero path",
        () => {
          for (let r = 0; r < count; r++) {
            const at = renderBlock.RenderFrameBlock.ShipsAt(block, r);
            Sink[0] += Number(renderBlock.RenderShipRow.Flags(block.View, at) & 1n);
          }
        }]);
    }
  }

  // (2) a cook deref composes two 32-bit reads and allocates nothing
  if (existsSync("build/js-fuzz-scene.cook")) {
    const source = new Uint8Array(readFileSync("build/js-fuzz-scene.cook"));
    const bytes = new Uint8Array(new ArrayBuffer(source.length));
    bytes.set(source);
    const cook = graphCook.SceneCook.Open(bytes);
    if (cook !== null) {
      const slot = graphCook.SceneRow.HeadSlot(cook.Region);
      rows.push(["Scene head deref", ZeroFloor,
        "a self-relative delta is composed from two 32-bit reads, never read as a BigInt (§6.3)",
        () => { Sink[0] += graphCook.SceneCook.At(cook, slot); }]);
    }
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
// another. That is not fussiness: an allocation one major optimizes away can be
// steady at sixteen bytes a call on another — a generated body sitting over
// that engine's inlining threshold — and a number read on whatever `node` a
// PATH lookup found says nothing about the runtime the claim is for. Pass
// SCHEMA_JS_ALLOC_ANY_NODE=1 to read the numbers on another runtime; the gate
// then reports and does not certify.
const PinnedNodeMajor = "26";

function checkAllocation(iterations) {
  let bad = false;
  const major = process.versions.node.split(".")[0];
  const anyNode = process.env.SCHEMA_JS_ALLOC_ANY_NODE === "1";
  if (major !== PinnedNodeMajor && !anyNode) {
    console.log("FAILED: this gate holds an allocation floor for node " + PinnedNodeMajor +
      ", which is the version CI pins, and it is running on node " + process.versions.node +
      ". V8 inlines generated bodies differently between majors, and a body left over the " +
      "inlining threshold boxes its double store — so a floor measured here says nothing about " +
      "the runtime the claim is for. Unpack the pinned runtime into dist/ (make/js.mk says how) and re-run, or set " +
      "SCHEMA_JS_ALLOC_ANY_NODE=1 to read the numbers without certifying them.");
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
    console.log("tables JS allocation gate: a block row walk allocates at zero " +
      "bytes per iteration; a 64-bit field costs the one BigInt the language " +
      "has no way around");
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
} else if (mode === "alloc") {
  checkAllocation(Number(process.argv[3] ?? 300000));
} else {
  checkForeignByteOrder();
  checkWholeSurface();
  checkOpenAnswers();
  checkBlockAccessors("testdata/wire/tables/block_render.bin", renderBlock.RenderFrameBlock);
  checkBlockAccessors("testdata/wire/tables/block_padded.bin", paddedBlock.PaddedFrameBlock);
  checkCookAccessors("build/js-fuzz-scene.cook", graphCook.SceneCook);
  if (!failed) {
    console.log("tables JS leg: a file of the other byte order is refused twice over, " +
      "every block and cook module re-exports the unit's whole surface, Open " +
      "throws on a caller's error and answers null to another build's bytes, " +
      "and every block row and every cooked node reads the same through the generated " +
      "accessors and through the descriptors");
  }
}

if (failed) {
  console.log("FAILED");
  process.exit(1);
}
console.log("OK");
