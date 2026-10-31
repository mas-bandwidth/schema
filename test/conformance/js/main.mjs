// THE JAVASCRIPT CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and test/conformance/cs's Program,
// and it is deliberately the same shape: one process per surface, every
// expectation in the data, nothing literal here. It answers every surface this
// backend has — the block form's open and its row dump, the cook's node dump
// and its writer (§7.6), both forgery batteries and the reason beside each
// refusal (§6.5), the message form, and the
// variable form's wire, report and text surfaces for the roots its variable
// wire carries.
//
//   node main.mjs <manifest> list
//   node main.mjs <manifest> <surface> <outdir>
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.
//
// THE POINTER COLUMN, IN A LANGUAGE WITH NO POINTERS. A forgery line carries
// the buffer the caller holds — an aligned base, or that many bytes past one —
// and JavaScript has no addresses to align. What it has is a view's byteOffset
// inside its buffer, which is the one placement fact a consumer can state and
// the one the generated Open measures; so a `lead` of N is a view starting N
// bytes into a fresh ArrayBuffer, and its residue modulo the form's alignment
// is exactly the residue the C++ and C# legs place by hand.

import { existsSync, readFileSync, writeFileSync, readSync, writeSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

// The generated tree is a PATH, not a static import, for one reason: the
// harness's negative control points this same driver at a sabotaged copy of
// the generated modules and requires the matrix to go red. A static import
// would name one tree forever, and the control would have nothing to aim at.
// The default is what the Makefile writes, and the working directory is the
// repository root by contract.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

let renderBlock, paddedBlock, graphCook;
if (process.argv[2] !== "wire-fuzz") {
  renderBlock = await load("block/RenderBlock.js");
  paddedBlock = await load("block/PaddedBlock.js");
  graphCook = await load("pointers/GraphCook.js");
}

// ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

function readManifest(path) {
  const lines = [];
  for (const raw of readFileSync(path, "utf8").split("\n")) {
    const text = raw.trim();
    if (text.length === 0 || text[0] === "#") { continue; }
    lines.push(text.split(/\s+/));
  }
  return lines;
}

function kind(lines, what) {
  return lines.filter((f) => f[0] === what);
}

function fail(message) {
  process.stderr.write("driver: " + message + "\n");
  process.exit(1);
}

// ---- placing a fixture the way its forgery line asks for
//
// EXACTLY THE CLAIM, at a base `lead` bytes into a fresh buffer, with what fits
// copied and the rest zero. The claim may run PAST the file, which is what two
// rows of the block battery are about, or short of it, which is what a
// truncation is.
function place(source, claim, lead) {
  const buffer = new ArrayBuffer(lead + claim);
  const bytes = new Uint8Array(buffer, lead, claim);
  const copy = Math.min(claim, source.length);
  bytes.set(source.subarray(0, copy), 0);
  return bytes;
}

const blocks = process.argv[2] === "wire-fuzz" ? {} : {
  block_render: renderBlock.RenderFrameBlock,
  block_padded: paddedBlock.PaddedFrameBlock,
};

function blockNamed(name) {
  for (const prefix of Object.keys(blocks)) {
    if (name.startsWith(prefix)) { return blocks[prefix]; }
  }
  fail("no block named " + name);
  return null;
}

// A block's base is 64-byte aligned by construction (§19.1) and `extent` is the
// length the CALLER claims, which a forgery may set past the bytes the image
// carries. The allocation is the claim, so a reader that walks past what it was
// given walks into memory this process owns and nothing else's — and in this
// language it does not walk at all: a read past a view throws, so Open bounds
// every term before it adds one.
// A REFUSAL HAS TWO SPELLINGS in this language and the verdict has one. Bytes
// that are not this build's are a null handle; a CALLER's error — no
// Uint8Array, or a view at an unaligned byteOffset — is a throw with the fix in
// it. The manifest asks whether the reader refused, and both are refusals.
function opens(open, bytes) {
  try {
    return open(bytes) !== null;
  } catch (e) {
    if (e instanceof RangeError || e instanceof TypeError) { return false; }
    throw e;
  }
}

function openBlock(name, source, extent, lead) {
  // A CLAIM SHORTER THAN THE IMAGE IS A TRUNCATION: `place` copies what fits
  // and zeroes the rest, so the buffer is the claim in that direction too. And
  // `lead` is the POINTER column: 0 an aligned base, 1..63 that many bytes past
  // one, because an unaligned base is a pointer fact and not a file fact.
  const claim = extent < 0 ? source.length : extent;
  const block = blockNamed(name);
  return opens((b) => block.Open(b), place(source, claim, lead)) ? "open\n" : "refuse\n";
}

function surfaceBlock(lines, outDir) {
  for (const f of kind(lines, "block")) {
    writeFileSync(join(outDir, f[1]), openBlock(f[1], new Uint8Array(readFileSync(f[3])), -1, 0));
  }
}

// ---- THE BASE-ALIGNMENT GATE (docs/SPEC-TABLES.md §19.1, docs/PORTING.md I5)
//
// Every committed block image at every lead 0..64, through the generated Open:
// 0 and 64 open, 1..63 refuse. NOT a listed surface (the harness would run a
// listed name as one); `<driver> <manifest> block-lead`, the way Elixir's is.
// A REFUSAL HERE IS ONE THING: the RangeError whose message names the
// multiple-of-64 rule. A null (bytes that are not this build's), an open
// handle at a nonzero residue, or any other throw is a failure of the gate,
// because a base-alignment check that is never required to fire is dead code
// to every gate and an unaligned read succeeds on the hosts this builds on.
function blockLead(lines) {
  const align = 64;
  let missed = 0;
  let images = 0;
  let totalOpened = 0;
  let totalRefused = 0;
  for (const f of kind(lines, "block")) {
    const source = new Uint8Array(readFileSync(f[3]));
    const block = blockNamed(f[1]);
    images++;
    let opened = 0;
    let refused = 0;
    for (let lead = 0; lead <= align; lead++) {
      const bytes = place(source, source.length, lead);
      let verdict;
      try {
        const handle = block.Open(bytes);
        verdict = handle === null ? "null" : "open";
      } catch (e) {
        verdict = (e instanceof RangeError && /multiple of 64/.test(e.message)) ? "refuse" : "other throw: " + e;
      }
      const want = lead % align === 0 ? "open" : "refuse";
      if (verdict === want) {
        if (want === "open") { opened++; } else { refused++; }
      } else {
        missed++;
        process.stderr.write(f[1] + " lead " + lead + " " + (verdict === "open" ? "opened" : verdict) +
          ", wanted " + want + "\n");
      }
    }
    process.stdout.write(f[1] + ": " + (align + 1) + " leads, " + opened + " opened (0 and 64), " +
      refused + " refused (1..63)\n");
    totalOpened += opened;
    totalRefused += refused;
  }
  if (images === 0) { fail("block-lead: the manifest names no block image"); }
  if (missed > 0) { fail("block-lead: " + missed + " lead(s) missed the rule"); }
  process.stdout.write(images + " images x " + (align + 1) + " leads: " + totalOpened + " opened (0 and 64), " +
    totalRefused + " refused (1..63)\n");
}

function surfaceForgery(lines, outDir) {
  for (const f of kind(lines, "forgery")) {
    if (f[2] !== "block") { continue; } // the cook's battery is its own
    writeFileSync(join(outDir, f[1]), openBlock(f[3], new Uint8Array(readFileSync(f[4])), Number(f[5]), f[6] === "null" ? 0 : Number(f[6])));
  }
}

// THE OTHER BYTE ORDER, over both accelerators (docs/SPEC-TABLES.md §19.1,
// §7.1). The image is the same file with the MAGIC WORD's eight bytes
// reversed, which is what that word looks like to a reader of the other order
// — so the expectation does not depend on the host this runs on: whatever this
// build's order is, the magic it now reads is not its own, and every Open puts
// that check first. Every leg on every host must refuse.
function foreign(source) {
  const out = new Uint8Array(source.length);
  out.set(source);
  if (out.length >= 8) {
    for (let i = 0; i < 4; i++) {
      const swap = out[i]; out[i] = out[7 - i]; out[7 - i] = swap;
    }
  }
  return out;
}

function surfaceBlockForeign(lines, outDir) {
  for (const f of kind(lines, "block")) {
    writeFileSync(join(outDir, f[1]), openBlock(f[1], foreign(new Uint8Array(readFileSync(f[3]))), -1, 0));
  }
}

function surfaceCookForeign(lines, outDir) {
  for (const f of kind(lines, "cook")) {
    const source = foreign(new Uint8Array(readFileSync(f[4])));
    const cook = cookNamed(f[3]);
    writeFileSync(join(outDir, f[1]), opens((b) => cook.Open(b), place(source, source.length, 0)) ? "open\n" : "refuse\n");
  }
}

// ---- the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
//
// The twin of the C++ leg's walk, and like it, written against §8's descriptors
// and NOTHING ELSE: no generated accessor, no field named in this file. That is
// the claim §19.2 makes for the descriptors, and a walk that reached for an
// accessor would be proving something else. A FLOAT is its IEEE-754 bit
// pattern, because a block row is a byte-identical projection and its bits are
// the fact.

function hex(value, digits) {
  return value.toString(16).padStart(digits, "0");
}

function dumpScalar(view, at, k, width) {
  switch (k) {
    case 1: return view.getUint8(at) !== 0 ? "true" : "false";
    case 10: return "0x" + hex(view.getUint32(at, true), 8);
    case 11: return "0x" + hex(view.getBigUint64(at, true), 16);
    case 2: case 3: case 4: case 5:
      switch (width) {
        case 1: return String(view.getInt8(at));
        case 2: return String(view.getInt16(at, true));
        case 4: return String(view.getInt32(at, true));
        default: return String(view.getBigInt64(at, true));
      }
    default:
      switch (width) {
        case 1: return String(view.getUint8(at));
        case 2: return String(view.getUint16(at, true));
        case 4: return String(view.getUint32(at, true));
        default: return String(view.getBigUint64(at, true));
      }
  }
}

function dumpText(bytes, at, used) {
  if (!(used >= 0)) { used = 0; }
  let out = "\"";
  for (let i = 0; i < used; i++) {
    const c = bytes[at + i];
    if (c >= 0x20 && c < 0x7f && c !== 0x22 && c !== 0x5c) { out += String.fromCharCode(c); }
    else { out += "\\x" + hex(c, 2); }
  }
  return out + "\" len=" + used;
}

function dumpJoin(prefix, name) {
  return prefix.length === 0 ? name : prefix + "." + name;
}

function elementOf(f) {
  return f.ElementRef === null ? null : f.ElementRef();
}

function dumpRecord(out, bytes, view, at, info, path) {
  if (info === null) { fail("a descriptor names no record"); }
  for (const f of info.Fields) {
    if (f.OutOfLine) { continue; }
    const name = dumpJoin(path, f.Name);
    if (f.Counted) {
      const used = view.getInt32(at + f.CountOffset, true);
      if (used < 0 || used > f.ArrayBound) {
        fail(info.Name + "." + f.Name + " carries a used length of " + used +
          ", outside [ 0, " + f.ArrayBound + " ]");
      }
      out.push("  " + name + " = " + dumpText(bytes, at + f.Offset, used));
    } else {
      const slots = f.IsArray ? f.ArrayBound : 1;
      for (let s = 0; s < slots; s++) {
        const where = f.IsArray ? name + "[" + s + "]" : name;
        const value = at + f.Offset + s * f.ElemSize;
        const element = elementOf(f);
        if (element !== null) { dumpRecord(out, bytes, view, value, element, where); }
        else { out.push("  " + where + " = " + dumpScalar(view, value, f.Kind, f.ElemSize)); }
      }
    }
    if (f.Optional) {
      out.push("  " + name + "#present = " + (view.getUint8(at + f.PresentOffset) !== 0 ? "true" : "false"));
    }
  }
}

function dumpBlock(block, info) {
  const out = [];
  const { Bytes: bytes, View: view } = block;
  out.push("projection " + info.Name + " @0");
  dumpRecord(out, bytes, view, 0, info, "");
  for (const f of info.Fields) {
    if (!f.OutOfLine) { continue; }
    const offsetOf = view.getBigUint64(f.OffsetOfOffset, true);
    const count = view.getUint32(f.CountOffset, true);
    const stride = view.getUint32(f.StrideOffset, true);
    const rowInfo = elementOf(f);
    if (rowInfo === null) { fail(f.Name + " names no element"); }
    out.push("array " + f.Name + " " + rowInfo.Name + " @" + offsetOf +
      " count=" + count + " stride=" + stride);
    for (let r = 0; r < count; r++) {
      const at = Number(offsetOf) + r * stride;
      out.push("row " + r + " @" + at);
      dumpRecord(out, bytes, view, at, rowInfo, "");
    }
  }
  return out.join("\n") + "\n";
}

function surfaceBlockDump(lines, outDir) {
  for (const f of kind(lines, "block")) {
    const source = new Uint8Array(readFileSync(f[3]));
    const handle = blockNamed(f[1]).Open(place(source, source.length, 0));
    if (handle === null) { fail(f[1] + " does not open"); }
    writeFileSync(join(outDir, f[1]), dumpBlock(handle, blockNamed(f[1]).Type));
  }
}

// ---- the COOK's node dump (docs/SPEC-TABLES.md §7.5)
//
// Every node this side reaches, through its OWN derefs. Generic over the cook
// descriptors, which is the whole point of them: a pointer slot is eight bytes
// holding the SIGNED SELF-RELATIVE delta of §6.3, so a deref is one add, and a
// delta of zero is null. A by-value nesting — a record inside a record, an
// element of a bounded or enum-keyed array — is not a node; it is storage
// inside one, and the walk descends through it to reach the pointer slots
// inside.

const cooks = process.argv[2] === "wire-fuzz" ? {} : {
  Scene: graphCook.SceneCook,
  Depot: graphCook.DepotCook,
  Album: graphCook.AlbumCook,
  TreeNode: graphCook.TreeNodeCook,
  ListNode: graphCook.ListNodeCook,
  Layer: graphCook.LayerCook,
  Meta: graphCook.MetaCook,
  Settings: graphCook.SettingsCook,
};

function cookNamed(root) {
  const c = cooks[root];
  if (c === undefined) { fail("no cook root named " + root); }
  return c;
}

function cookScalar(view, at, storage, width) {
  switch (storage) {
    case "Bool": return view.getUint8(at) !== 0 ? "true" : "false";
    case "Float":
      // Nothing in the pointered corpus is a float, and a canonical
      // cross-language spelling of one is a decision this gate should not make
      // in passing. The day a float arrives, the gate says so rather than
      // drifting.
      fail("the dump met a float, whose canonical cross-language spelling this gate does not fix");
      return "";
    case "Signed":
      switch (width) {
        case 1: return String(view.getInt8(at));
        case 2: return String(view.getInt16(at, true));
        case 4: return String(view.getInt32(at, true));
        default: return String(view.getBigInt64(at, true));
      }
    default:
      switch (width) {
        case 1: return String(view.getUint8(at));
        case 2: return String(view.getUint16(at, true));
        case 4: return String(view.getUint32(at, true));
        default: return String(view.getBigUint64(at, true));
      }
  }
}

function cookDump(cook, rootType) {
  const out = [];
  const reached = [];
  const { Bytes: bytes, View: view, Region: region, Length: length } = cook;

  function findNode(offset) {
    for (let i = 0; i < reached.length; i++) {
      if (reached[i].Offset === offset) { return i; }
    }
    return -1;
  }

  function emit(path, value) { out.push("  " + path + " = " + value); }

  function storageWalk(at, type, path, depth) {
    for (const f of type.Fields) {
      const name = dumpJoin(path, f.Name);
      // every COUNT COMPANION, against its declared bound, and a NEGATIVE one
      // refuses too — an extent is never negative, and a walker handed one
      // indexes backwards out of the region (§7.4's pass two)
      let used = -1;
      if (f.CountOffset >= 0) {
        used = view.getInt32(at + f.CountOffset, true);
        if (used < 0 || used > f.ArrayBound) {
          fail(type.Name + "." + f.Name + " carries a count companion of " + used +
            ", outside [ 0, " + f.ArrayBound + " ]");
        }
      }
      if (f.IsPointer) {
        const delta = view.getBigInt64(at + f.Offset, true);
        if (delta === 0n) {
          emit(name, "null"); // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
          continue;
        }
        const target = Number(BigInt(at + f.Offset) + delta);
        if (target < region || target >= region + length) {
          fail(type.Name + "." + f.Name + " resolves outside the region — a delta of " + delta +
            " from a slot at " + (at + f.Offset - region));
        }
        if (f.RecordRef === null) {
          fail(type.Name + "." + f.Name + " is a pointer whose descriptor names no record");
        }
        emit(name, "-> @" + (target - region));
        node(target - region, f.RecordRef(), depth + 1);
        continue;
      }
      if (f.Storage === "String" || f.Storage === "Bytes") {
        emit(name, dumpText(bytes, at + f.Offset, used));
      } else if (f.Storage === "Record") {
        // a nested record — by value, or every slot of an array of them. A
        // COUNTED array writes all N slots (§7.2), and a slot past the live
        // count holds the value-initialized element, whose pointer slots are
        // zero: walking all of them is what the check does too.
        const slots = f.IsArray ? f.ArrayBound : 1;
        for (let s = 0; s < slots; s++) {
          const element = f.IsArray ? name + "[" + s + "]" : name;
          storageWalk(at + f.Offset + s * f.ElemSize, f.RecordRef(), element, depth);
        }
      } else {
        const slots = f.IsArray ? f.ArrayBound : 1;
        for (let s = 0; s < slots; s++) {
          const element = f.IsArray ? name + "[" + s + "]" : name;
          emit(element, cookScalar(view, at + f.Offset + s * f.ElemSize, f.Storage, f.ElemSize));
        }
      }
      if (f.CountOffset >= 0 && f.Storage !== "String" && f.Storage !== "Bytes") {
        emit(name + "#count", String(used));
      }
      if (f.PresentOffset >= 0) {
        emit(name + "#present", view.getUint8(at + f.PresentOffset) !== 0 ? "true" : "false");
      }
    }
  }

  function node(offset, type, depth) {
    if (depth > 4096) {
      fail("the walk nested past any depth a region can hold — a cycle the deref did not close");
    }
    const at = findNode(offset);
    if (at >= 0) {
      if (reached[at].type !== type) {
        fail("two references name the node at offset " + offset + " as two different tables: " +
          reached[at].type.Name + " and " + type.Name);
      }
      // one node, one visit: sharing and a back-reference are the same fact (§6.3)
      return;
    }
    if (offset > length || type.Size > length - offset) {
      fail("the node at offset " + offset + " (" + type.Name + ", sizeof " + type.Size +
        ") does not fit inside the region's " + length + " bytes");
    }
    const index = reached.length;
    reached.push({ offset, type });
    out.push("node " + index + " " + type.Name + " @" + offset);
    storageWalk(region + offset, type, "", depth);
  }

  node(0, rootType, 0);
  return out.join("\n") + "\n";
}

function surfaceCook(lines, outDir) {
  for (const f of kind(lines, "cook")) {
    const source = new Uint8Array(readFileSync(f[4]));
    const cookType = cookNamed(f[3]);
    const handle = cookType.Open(place(source, source.length, 0));
    if (handle === null) { fail(f[1] + " does not open"); }
    writeFileSync(join(outDir, f[1]), cookDump(handle, cookType.Type));
  }
}

function surfaceCookForgery(lines, outDir) {
  for (const f of kind(lines, "forgery")) {
    if (f[2] !== "cook") { continue; }
    const source = new Uint8Array(readFileSync(f[4]));
    const extent = Number(f[5]);
    const claim = extent < 0 ? source.length : extent;
    const nullBuffer = f[6] === "null";
    const lead = nullBuffer ? 0 : Number(f[6]);
    const bytes = nullBuffer ? null : place(source, claim, lead);
    const cook = cookNamed(f[3]);
    writeFileSync(join(outDir, f[1]), opens((b) => cook.Open(b), bytes) ? "open\n" : "refuse\n");
  }
}

// ---- THE REASON BESIDE THE NULL (docs/SPEC-TABLES.md §6.5, §7, §19.2) ----
//
// cook-reason and block-reason (test/conformance/README.md): each forgery of
// the kind, placed exactly as its line asks — the claim, the lead, or no buffer
// at all — and the generated OpenReason's answer, the TableRefuseReason value
// name of the FIRST failing clause or `ok`. The harness compares the rows the
// manifest's `refusal` lines name. The reason is the generated reader's own:
// this driver places bytes and prints what it is told, and judges nothing.
function forgedPlacement(f) {
  const source = new Uint8Array(readFileSync(f[4]));
  const extent = Number(f[5]);
  const claim = extent < 0 ? source.length : extent;
  if (f[6] === "null") { return null; }
  return place(source, claim, Number(f[6]));
}

function surfaceReason(kindName, lines, outDir) {
  for (const f of kind(lines, "forgery")) {
    if (f[2] !== kindName) { continue; }
    const reader = kindName === "cook" ? cookNamed(f[3]) : blockNamed(f[3]);
    writeFileSync(join(outDir, f[1]), reader.OpenReason(forgedPlacement(f)) + "\n");
  }
}

// ---- the MESSAGE surface (docs/SPEC-TABLES.md §3.3) ----------------------
//
// `message` is the one surface that reads TWO files (test/conformance/
// README.md): the CONNECTION's announcement carries a message's id table, the
// driver reads that first into one direction's VOCABULARY, and the batch
// resolves against it. The manifest's `connection` line names the
// announcement and the `message` line names the pair of wires, so the driver
// joins the two by key and derives no vocabulary of its own. The verbs are
// PLURAL, because the form's primitive is a BATCH of bodies of one root and a
// single message is the batch of one.
//
// THE ROW LIST IS THE MANIFEST'S. Each row names a connection's unit and the
// root its batch carries; the manifest's `message` lines are the corpus's
// claim, this table is the leg's, and TestMessageFormConformanceJavaScript
// (internal/codegen/jstable) holds the two to one set. The home module
// carries the unit's `TableVocabulary` and `AnnounceRead`; the table module
// carries the root's `LoadMessages`, `MeasureMessages` and `SaveMessages`
// verbs (docs/SPEC-TABLES.md §3.3). A row whose unit this backend cannot
// generate, or whose root is a plain `table` the form refuses by name, is
// answered `<case>.absent` beside where the answer would go.
const messageRows = [
  ["backenddemo", "LoginRequest", "backend/BackenddemoTable.js", "backend/BackendTable.js"],
  ["backenddemo", "MatchResult", "backend/BackenddemoTable.js", "backend/BackendTable.js"],
  ["backenddemo", "StorePurchase", "backend/BackenddemoTable.js", "backend/BackendTable.js"],
  ["vocabdemo", "Wide09", "vocab/VocabdemoTable.js", "vocab/VocabTable.js"],
  ["vocabdemo", "Wide00", "vocab/VocabdemoTable.js", "vocab/VocabTable.js"],
  ["vocab9demo", "Wide19", "vocab9/Vocab9demoTable.js", "vocab9/Vocab9Table.js"],
  ["vocab9demo", "Wide00", "vocab9/Vocab9demoTable.js", "vocab9/Vocab9Table.js"],
  ["graphdemo", "Scene", "pointers/GraphdemoTable.js", "pointers/GraphTable.js"],
];

function messageRow(unit, root) {
  for (const r of messageRows) {
    if (r[0] === unit && r[1] === root) { return r; }
  }
  return null;
}

// A LEG THAT CANNOT ANSWER ONE CASE SAYS SO BESIDE WHERE THE ANSWER WOULD GO,
// `<case>.absent`, and the matrix counts it (test/conformance/README.md, "Exit
// codes"). The JavaScript table backend emits the form-2 verbs for its FIXED
// roots only, so a message row whose unit cannot be generated for this backend
// (docs/SPEC-TABLES.md §11) or whose root is a plain `table` the form refuses
// by name (§3.3) is such a case; the surface itself is answered, so the matrix
// counts the rows rather than the feature.
function messageAbsent(outDir, name, reason) {
  writeFileSync(join(outDir, name + ".absent"), reason + "\n");
}

async function surfaceMessage(lines, outDir) {
  const connections = kind(lines, "connection");
  for (const f of kind(lines, "message")) {
    // message <name> <connection> <root> <file-form wire> <message-form wire>
    const conn = connections.find((c) => c[1] === f[2]);
    if (conn === undefined) { fail("message " + f[1] + " names no connection " + f[2]); }
    const row = messageRow(conn[2], f[3]);
    if (row === null) { messageAbsent(outDir, f[1], "plain table: docs/SPEC-TABLES.md §3.3"); continue; }
    let runtime, unit;
    try {
      runtime = await load(row[2]);
      unit = await load(row[3]);
    } catch (e) {
      messageAbsent(outDir, f[1], "no generated unit: docs/SPEC-TABLES.md §11");
      continue;
    }
    const loadMessages = unit[f[3] + "LoadMessages"];
    const measureMessages = unit[f[3] + "MeasureMessages"];
    const saveMessages = unit[f[3] + "SaveMessages"];
    if (loadMessages === undefined || measureMessages === undefined || saveMessages === undefined) {
      messageAbsent(outDir, f[1], "plain table: docs/SPEC-TABLES.md §3.3");
      continue;
    }
    // AnnounceRead the connection's announcement into one direction's
    // vocabulary, LoadMessages the batch against it, MeasureMessages the
    // bodies it filled, SaveMessages them back into one buffer, the bytes.
    const vocabulary = new runtime.TableVocabulary();
    vocabulary.Init(new Array(runtime.TableMessageEntriesHere));
    const report = {};
    if (!runtime.AnnounceRead(vocabulary, new Uint8Array(readFileSync(conn[4])), report)) {
      fail(f[1] + ": the announcement was refused: " + conn[4]);
    }
    const values = Array.from({ length: runtime.TableMessageBatchMax }, () => new runtime[f[3]]());
    const count = loadMessages(values, vocabulary, new Uint8Array(readFileSync(f[5])), report);
    if (count < 1) { fail(f[1] + ": message load " + JSON.stringify(report)); }
    const body = values.slice(0, count);
    const size = measureMessages(body, report);
    if (size < 0) { fail(f[1] + ": message measure " + JSON.stringify(report)); }
    const buffer = new Uint8Array(size);
    if (saveMessages(body, buffer, report) !== size) { fail(f[1] + ": message save " + JSON.stringify(report)); }
    writeFileSync(join(outDir, f[1]), buffer);
  }
}

// ---- the WIRE-CARRYING surfaces (docs/SPEC-TABLES.md §3, §3.1, §4, §16, §16.7)
//
// The variable form, form byte 1, over the roots this leg's variable wire
// carries: the pointer corpus's Scene (graphdemo), the byte-buffer corpus's
// Catalog (blobdemo), every root of the map corpus (mapdemo: Cells and Pairs
// hold fixed-length arrays [N]T as map values, Text a wstring(N)), the scalar
// corpus's SimState (scalars, tblscalars2: fixed/ufixed and 128-bit kinds),
// the wide-text corpus's Caption and Stamp (widedemo) and the message corpus's
// ToolMessage (messagedemo), each through its unit's
// <Package>Variable.js, and the stream, list, backend, vocabulary and
// evolution-pair units below. A case whose pair is not here is answered
// `<case>.absent` beside where the answer would go, which the matrix counts as
// the row of work that is left, never as a pass. The table is the manifest's
// instance and report pairs less a named uncarried set (empty today), and
// TestWireRowsConformanceJavaScript (internal/codegen/jstable) holds it there.
const wireRows = [
  ["tabledemo", "RootConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "ProfileConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "LoadoutConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "WideBlob", "examples/TabledemoVariable.js"],
  ["tabledemo", "ArchiveConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "KeyedConfig", "examples/TabledemoVariable.js"],
  ["tblv1", "Cfg", "v1/Tblv1Variable.js"],
  ["tblv2", "Cfg", "v2/Tblv2Variable.js"],
  ["tblw1", "Fleet", "w1/Tblw1Variable.js"],
  ["tblw2", "Fleet", "w2/Tblw2Variable.js"],
  ["tblr1", "Cfg", "r1/Tblr1Variable.js"],
  ["tblr2", "Cfg", "r2/Tblr2Variable.js"],
  ["tblp1", "Chain", "p1/Tblp1Variable.js"],
  ["tblp3", "Chain", "p3/Tblp3Variable.js"],
  ["graphdemo", "Scene", "pointers/GraphdemoVariable.js"],
  ["blobdemo", "Catalog", "blobs/BlobdemoVariable.js"],
  // a map whose value is a fixed-length array [N]T (§2.8, §3)
  ["mapdemo", "Cells", "maps/MapdemoVariable.js"],
  ["mapdemo", "Pairs", "maps/MapdemoVariable.js"],
  // a map whose value is a wstring(N), kind 33 (§2.8, §3)
  ["mapdemo", "Text", "maps/MapdemoVariable.js"],
  // the rest of the map corpus (§2.8): string(N) and integer keys, values of
  // every carried field line, maps of maps and of pointers
  ["mapdemo", "Chunks", "maps/MapdemoVariable.js"],
  ["mapdemo", "Crews", "maps/MapdemoVariable.js"],
  ["mapdemo", "Depth", "maps/MapdemoVariable.js"],
  ["mapdemo", "Docs", "maps/MapdemoVariable.js"],
  ["mapdemo", "Fleet", "maps/MapdemoVariable.js"],
  ["mapdemo", "Runs", "maps/MapdemoVariable.js"],
  ["mapdemo", "Slots", "maps/MapdemoVariable.js"],
  ["mapdemo", "Spans", "maps/MapdemoVariable.js"],
  ["mapdemo", "Trails", "maps/MapdemoVariable.js"],
  // fixed(W, F) / ufixed(W, F) and int128 / uint128 as the raw integer at the
  // declared width (§3, §15), and the evolved type reading them (§4)
  ["scalars", "SimState", "scalars/ScalardemoVariable.js"],
  ["tblscalars2", "SimState", "scalars2/Scalardemo2Variable.js"],
  // wstring(N) as a field, in a type, in an array of that type and as an arm
  // (§3; SPEC.md §4.12)
  ["widedemo", "Caption", "wide/WideVariable.js"],
  ["widedemo", "Stamp", "wide/WideVariable.js"],
  // the message corpus's root on the variable wire: union arms of every
  // carried field type, wide text among them (§2.6, §3)
  ["messagedemo", "ToolMessage", "messages/MessagedemoVariable.js"],
  // the stream corpus (§2.6, §3): a union arm that is a table, the parts
  // array and the chain, and the report rows its forged counts owe
  ["streamdemo", "Feed", "stream/StreamdemoVariable.js"],
  // the list corpus (§2.3, §3): arrays of tables, of unions, of maps and of
  // scalars, nested and shared, and the evolved Save reading a widened list
  ["listdemo", "Album", "lists/ListdemoVariable.js"],
  ["listdemo", "Army", "lists/ListdemoVariable.js"],
  ["listdemo", "Mixed", "lists/ListdemoVariable.js"],
  ["listdemo", "Save", "lists/ListdemoVariable.js"],
  ["listdemo", "Sheet", "lists/ListdemoVariable.js"],
  ["listdemo", "Unbounded", "lists/ListdemoVariable.js"],
  // the message roots on the file form (§3.3): the backend unit's three and
  // the wide-vocabulary units' roots on both sides of the one-byte boundary
  ["backenddemo", "LoginRequest", "backend/BackenddemoVariable.js"],
  ["backenddemo", "MatchResult", "backend/BackenddemoVariable.js"],
  ["backenddemo", "StorePurchase", "backend/BackenddemoVariable.js"],
  ["vocabdemo", "Wide00", "vocab/VocabdemoVariable.js"],
  ["vocabdemo", "Wide09", "vocab/VocabdemoVariable.js"],
  ["vocab9demo", "Wide00", "vocab9/Vocab9demoVariable.js"],
  ["vocab9demo", "Wide19", "vocab9/Vocab9demoVariable.js"],
  // the evolution pairs (§4): a union arm moved and reshaped (A1 read as A2),
  // a key's kind changed both ways (K1/K2), a member list grown and shrunk
  // (M1/M2), and a chain read by a type that did not write it (P1/P3 as P2)
  ["tblm1", "Msg", "m1/Tblm1Variable.js"],
  ["tblm2", "Msg", "m2/Tblm2Variable.js"],
  ["tbla2", "Root", "a2/Tbla2Variable.js"],
  ["tblk1", "Root", "k1/Tblk1Variable.js"],
  ["tblk2", "Root", "k2/Tblk2Variable.js"],
  ["tblp2", "Chain", "p2/Tblp2Variable.js"],
  // a pointer under a false guard and inside an absent optional over a
  // variable closure is no edge (§2.3, §3.1)
  ["tblg1", "Guarded", "g1/Tblg1Variable.js"],
];

// fuzzUnits binds the roster units no text surface names (tbla2, tblk1,
// tblk2, tblp2) for the wire-fuzz arm. They are wire rows too, which
// wireFuzzCodec reads first, so this table is the arm's own statement of them;
// tblg1 is bound through wireRows alone.
const fuzzUnits = {
  tbla2: "a2/Tbla2Variable.js",
  tblk1: "k1/Tblk1Variable.js", tblk2: "k2/Tblk2Variable.js",
  tblp2: "p2/Tblp2Variable.js",
};

// retainUnits binds the units the manifest's retain rows name
// (docs/SPEC-TABLES.md §6.6): the RT reader and the two newer builds it is retained
// across. The table retains the root's LoadRetain, MeasureRetain and
// SaveRetain, which are absent on a unit this leg names no module for.
const retainUnits = {
  tblrt1: "rt1/Tblrt1Variable.js",
  tblrt2: "rt2/Tblrt2Variable.js",
  tblrt3: "rt3/Tblrt3Variable.js",
};

// retainCodec binds one retain row's unit and root: the retaining pair beside
// the ordinary Measure, which the row measures before it loads. A unit this leg
// names no module for answers null, and the surface writes its `<case>.absent`.
async function retainCodec(unit, root) {
  const path = retainUnits[unit];
  if (path === undefined || !existsSync(resolve(generated, path))) { return null; }
  const m = await load(path);
  for (const f of ["LoadMeasure", "LoadRetain", "MeasureRetain", "SaveRetain"]) {
    if (typeof m[root + f] !== "function") { return null; }
  }
  return {
    m,
    loadMeasure: m[root + "LoadMeasure"],
    loadRetain: m[root + "LoadRetain"],
    measureRetain: m[root + "MeasureRetain"],
    saveRetain: m[root + "SaveRetain"],
  };
}

function retainAbsent(outDir, name, reason) {
  writeFileSync(join(outDir, name + ".absent"), reason + "\n");
}

// wireFuzzCodec binds one root the harness's default roster names
// (testdata/conformance/tables/MANIFEST.txt, docs/SPEC-TABLES.md §4.2) to this
// leg's variable wire, the twin of the Go leg's findCodec
// (test/conformance/go/wire_fuzz.go): the unit's module is the wire row's, or
// else the unit's <Package>Variable.js (textUnits below), and the root is bound
// when that module exports its Load, LoadMeasure and Save. A unit this leg names
// no module for answers known=0, which the harness counts as absent, and
// tables-js-wire-fuzz-all pins that absent count (make/js.mk). A unit it DOES
// name is a root it claims to open, so a missing module, or one without the
// root's Load, LoadMeasure or Save, fails by name as codecOf does, rather than
// dropping the root to absent where nothing would say so (§4.2: a roster root
// the leg cannot open is a named failure, never a quiet skip).
async function wireFuzzCodec(unit, root) {
  const row = wireRows.find((r) => r[0] === unit && r[1] === root);
  // cards is bound here alone: it is no conformance-manifest unit, so it
  // stays out of wireRows and textUnits, which the run surfaces read.
  const path = row !== undefined ? row[2] : unit === "cards" ? "cards/CardsVariable.js" : textUnits[unit] ?? fuzzUnits[unit];
  if (path === undefined) { return null; }
  if (!existsSync(resolve(generated, path))) {
    fail(unit + "." + root + ": the wire-fuzz module is missing: " + resolve(generated, path));
  }
  const m = await load(path);
  for (const f of ["Load", "LoadMeasure", "Save"]) {
    if (typeof m[root + f] !== "function") {
      fail(unit + "." + root + ": the variable wire surface " + root + f + " is missing from " + path);
    }
  }
  return {
    load: m[root + "Load"], loadMeasure: m[root + "LoadMeasure"], save: m[root + "Save"],
    loadRetain: m[root + "LoadRetain"], saveRetain: m[root + "SaveRetain"],
    report: () => new m.TableVariableReport(),
    retainStore: () => new m.TableVariableRetain(new Uint8Array(1 << 20), new Array((1 << 20) / 8)),
  };
}

async function wireCodec(unit, root) {
  const row = wireRows.find((r) => r[0] === unit && r[1] === root);
  if (row === undefined) { return null; }
  return codecOf(unit, root, row[2]);
}

async function codecOf(unit, root, path) {
  const m = await load(path);
  const codec = {
    load: m[root + "Load"], save: m[root + "Save"], toJson: m[root + "ToJson"], fromJson: m[root + "FromJson"],
    measureReason: m[root + "MeasureReason"],
    report: () => new m.TableVariableReport(),
  };
  if (Object.values(codec).some((f) => typeof f !== "function")) { fail(unit + "." + root + ": the variable surface is missing from " + path); }
  return codec;
}

// ---- THE TEXT FORM FOR EVERY TABLE (docs/SPEC-TABLES.md §16, §16.7)
//
// json-read, json-write and json-hostile answer EVERY instance whose unit this
// leg generates, not only the wire rows above: the unit's <Package>Variable.js
// is found by package, and a root it carries is one it exports a
// ToJson/FromJson pair for. A root the unit's module does not carry (refused
// by name at generation, §11) and a unit this leg does not generate at all
// answer `<case>.absent`, which the matrix counts as the row of work that is
// left, never as a pass.
const textUnits = {
  tabledemo: "examples/TabledemoVariable.js",
  graphdemo: "pointers/GraphdemoVariable.js",
  blobdemo: "blobs/BlobdemoVariable.js",
  mapdemo: "maps/MapdemoVariable.js",
  blockdemo: "block/BlockdemoVariable.js",
  blockhome: "blockhome/BlockhomeVariable.js",
  messagedemo: "messages/MessagedemoVariable.js",
  streamdemo: "stream/StreamdemoVariable.js",
  scalars: "scalars/ScalardemoVariable.js",
  backenddemo: "backend/BackenddemoVariable.js",
  vocabdemo: "vocab/VocabdemoVariable.js",
  vocab9demo: "vocab9/Vocab9demoVariable.js",
  tblv1: "v1/Tblv1Variable.js", tblv2: "v2/Tblv2Variable.js",
  tblw1: "w1/Tblw1Variable.js", tblw2: "w2/Tblw2Variable.js",
  tblr1: "r1/Tblr1Variable.js", tblr2: "r2/Tblr2Variable.js",
  tblp1: "p1/Tblp1Variable.js", tblp3: "p3/Tblp3Variable.js",
  tblm1: "m1/Tblm1Variable.js", tblm2: "m2/Tblm2Variable.js",
  widedemo: "wide/WideVariable.js",
  listdemo: "lists/ListdemoVariable.js",
};

async function textCodec(unit, root) {
  const path = textUnits[unit];
  if (path === undefined || !existsSync(resolve(generated, path))) { return null; }
  const m = await load(path);
  if (typeof m[root + "ToJson"] !== "function") { return null; }
  return codecOf(unit, root, path);
}

// The WIRE-FUZZ arm (docs/SPEC-TABLES.md §4.2) answers here, after wireRows
// and textUnits, so wireFuzzCodec can bind every root of the harness's default
// roster; it needs none of the block or cook modules, which it skips above.
if (process.argv[2] === "wire-fuzz") {
  await wireFuzz();
  process.exit(0);
}

function wireAbsent(outDir, name) {
  writeFileSync(join(outDir, name + ".absent"), "not carried on this leg's variable wire: docs/SPEC-TABLES.md §11\n");
}

const noText = (f) => f.length > 5 && f[5] === "no-text";

async function surfaceWire(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    const codec = await wireCodec(f[2], f[3]);
    if (codec === null) { wireAbsent(outDir, f[1]); continue; }
    const v = codec.load(new Uint8Array(readFileSync(f[4])), codec.report());
    if (v === null) { fail(f[1] + ": load refused " + f[4]); }
    const saved = codec.save(v);
    if (saved === null) {
      // THE NAMED WRITER REFUSAL (docs/SPEC-TABLES.md §2.5): a blob two slots
      // name is refused by every writer, and the leg answers it by its own
      // reason, as the Go driver answers from its encode's error. The harness
      // holds the reason to the manifest's; any other refusal is a red here.
      const reason = codec.measureReason(v)[1];
      if (reason !== "blob_shared") { fail(f[1] + ": save refused the value (" + reason + ")"); }
      writeFileSync(join(outDir, f[1] + ".absent"), reason);
      continue;
    }
    writeFileSync(join(outDir, f[1]), saved);
  }
}

async function surfaceReport(lines, outDir) {
  for (const f of kind(lines, "report")) {
    const codec = await wireCodec(f[2], f[3]);
    if (codec === null) { wireAbsent(outDir, f[1]); continue; }
    const r = codec.report();
    const ok = codec.load(new Uint8Array(readFileSync(f[4])), r) !== null;
    const malformed = r.malformed || (!ok && !r.refused);
    const verdict = r.refused ? "refused" : "read";
    writeFileSync(join(outDir, f[1]), [r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, malformed, verdict].join(",") + "\n");
  }
}

async function surfaceJsonRead(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    if (noText(f)) { continue; }
    const codec = await textCodec(f[2], f[3]);
    if (codec === null) { wireAbsent(outDir, f[1]); continue; }
    const r = codec.report();
    const v = codec.fromJson(readFileSync(join("testdata/conformance/tables/json", f[1] + ".json"), "utf8"), r);
    if (r.malformed) { fail(f[1] + ": FromJson refused the text"); }
    const saved = codec.save(v);
    if (saved === null) { fail(f[1] + ": save refused the value"); }
    writeFileSync(join(outDir, f[1]), saved);
  }
}

async function surfaceJsonWrite(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    if (noText(f)) { continue; }
    const codec = await textCodec(f[2], f[3]);
    if (codec === null) { wireAbsent(outDir, f[1] + ".json"); continue; }
    const v = codec.load(new Uint8Array(readFileSync(f[4])), codec.report());
    if (v === null) { fail(f[1] + ": load refused " + f[4]); }
    const text = codec.toJson(v);
    if (text === null) { fail(f[1] + ": ToJson refused the value"); }
    writeFileSync(join(outDir, f[1] + ".json"), text);
  }
}

// json-hostile: one tree per rule the text form states (docs/SPEC-TABLES.md
// §16.2, §16.3, §17.5), the text at <tree>/<root>.json as `schema pack` reads
// it. The answer is the REPORT the read produces, or `refused`.
async function surfaceJsonHostile(lines, outDir) {
  for (const f of kind(lines, "json-hostile")) {
    const codec = await textCodec(f[2], f[3]);
    if (codec === null) { wireAbsent(outDir, f[1]); continue; }
    const r = codec.report();
    const v = codec.fromJson(readFileSync(join(f[4], f[3] + ".json"), "utf8"), r);
    const verdict = v === null || v === undefined || r.malformed || r.refused
      ? "refused"
      : [r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, false, "read"].join(",");
    writeFileSync(join(outDir, f[1]), verdict + "\n");
  }
}

// ---- the COOK WRITE surface (docs/SPEC-TABLES.md §7.6, §19.1)
//
// Measure, refuse a short buffer without writing, then cook both byte orders.
// Every cook-write row in the manifest loads through its unit's
// <Package>Variable.js and writes through its CookMeasure / CookFrom verbs.
const cookWriteRows = [
  ["tabledemo", "RootConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "ProfileConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "LoadoutConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "WideBlob", "examples/TabledemoVariable.js"],
  ["tabledemo", "ArchiveConfig", "examples/TabledemoVariable.js"],
  ["tabledemo", "KeyedConfig", "examples/TabledemoVariable.js"],
  ["tblv1", "Cfg", "v1/Tblv1Variable.js"],
  ["tblv2", "Cfg", "v2/Tblv2Variable.js"],
  ["tblp1", "Chain", "p1/Tblp1Variable.js"],
  ["tblp3", "Chain", "p3/Tblp3Variable.js"],
  ["graphdemo", "Scene", "pointers/GraphdemoVariable.js"],
  ["messagedemo", "ToolMessage", "messages/MessagedemoVariable.js"],
  ["tblm1", "Msg", "m1/Tblm1Variable.js"],
  ["tblm2", "Msg", "m2/Tblm2Variable.js"],
  ["streamdemo", "Feed", "stream/StreamdemoVariable.js"],
  ["blobdemo", "Catalog", "blobs/BlobdemoVariable.js"],
  ["scalars", "SimState", "scalars/ScalardemoVariable.js"],
];

function cookWriteAbsent(outDir, name) {
  writeFileSync(join(outDir, name + ".absent"),
    "not answered on this leg (docs/SPEC-TABLES.md §7.6)\n");
}

async function surfaceCookWrite(lines, outDir) {
  const instances = kind(lines, "instance");
  for (const f of kind(lines, "cook-write")) {
    const inst = instances.find((c) => c[1] === f[1]);
    if (inst === undefined) { fail("cook-write " + f[1] + " names no instance"); }
    const row = cookWriteRows.find((r) => r[0] === inst[2] && r[1] === inst[3]);
    if (row === undefined) {
      cookWriteAbsent(outDir, f[1]);
      cookWriteAbsent(outDir, f[1] + "-be");
      continue;
    }
    let m;
    try {
      m = await load(row[2]);
    } catch (e) {
      fail(f[1] + ": " + row[2] + " did not load: " + e);
    }
    const loadValue = m[inst[3] + "Load"];
    const measure = m[inst[3] + "CookMeasure"];
    const from = m[inst[3] + "CookFrom"];
    if (typeof loadValue !== "function" || typeof measure !== "function" || typeof from !== "function") {
      fail(f[1] + ": cook write surface missing from " + row[2]);
    }
    const value = loadValue(new Uint8Array(readFileSync(inst[4])), new m.TableVariableReport());
    if (value === null) { fail(f[1] + ": cook input did not load"); }
    const size = measure(value);
    if (typeof size !== "number" || size < 1) { fail(f[1] + ": cook measure refused"); }
    for (const order of [1, 2]) {
      const name = order === 2 ? f[1] + "-be" : f[1];
      const backing = new Uint8Array(size);
      backing.fill(0xa5);
      const short = backing.subarray(0, size - 1);
      if (from(value, short, order) !== false || backing.some((b) => b !== 0xa5)) {
        fail(name + ": short capacity wrote bytes");
      }
      const buffer = new Uint8Array(size);
      if (from(value, buffer, order) !== true) { fail(name + ": cook write refused"); }
      writeFileSync(join(outDir, name), buffer);
    }
  }
}

// ---- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6) -----------------------------
//
// THE TWO RETAIN SURFACES ARE ONE ROUND TRIP AND TWO SHAPES
// (test/conformance/README.md): `retain` prints the two load counters beside
// the `unknown` retention never moves and the save's own `retain_lost`;
// `retain-save` writes the bytes the pair saved. A row's capacity is a RULE:
// `short` is ONE BYTE SHORT OF THE LAST RECORD, so the driver loads once at a
// roomy capacity, reads what its own buffer used, and loads again one byte
// under that. The id list's capacity is a COUNT and travels as one.
async function surfaceRetain(lines, outDir, saves) {
  const capacityBytes = 1 << 20;
  const fullIds = 1 << 17;
  for (const f of lines) {
    if (f[0] !== "retain" && f[0] !== "retain-message") { continue; }
    if (f[0] === "retain-message") {
      // A retain-message row's root is a plain `table` (RT1's Node), which
      // carries no message verbs: a form-2 SaveRetain refuses by name (§3.3),
      // so the row is absent here and the reason names the clause.
      retainAbsent(outDir, f[1], "plain table: docs/SPEC-TABLES.md §3.3");
      continue;
    }
    // retain <case> <unit> <root> <wire> <capacity> <ids>
    const codec = await retainCodec(f[2], f[3]);
    if (codec === null) {
      retainAbsent(outDir, f[1], "retention is not carried for " + f[2] + ": docs/SPEC-TABLES.md §6.6");
      continue;
    }
    const wire = new Uint8Array(readFileSync(f[4]));
    if (codec.loadMeasure(wire) < 0) { fail(f[1] + ": load measure refused " + f[4]); }
    let ids = fullIds;
    if (f[6] !== "full") { ids = Number(f[6]); }
    let capacity = capacityBytes;
    const newStore = (bytes, idCount) => new codec.m.TableVariableRetain(new Uint8Array(bytes), new Array(idCount));
    let store = newStore(capacity, ids);
    let report = new codec.m.TableVariableReport();
    if (f[5] === "short") {
      // ONE BYTE SHORT OF THE LAST RECORD this port wrote: the record's own
      // byte cost is the port's, so the probe measures it and the row travels
      // as the rule rather than as a number.
      if (codec.loadRetain(wire, store, report) === null) { fail(f[1] + ": capacity probe refused"); }
      capacity = Math.max(0, store.used - 1);
      store = newStore(capacity, ids);
      report = new codec.m.TableVariableReport();
    }
    const root = codec.loadRetain(wire, store, report);
    if (root === null || report.malformed) { fail(f[1] + ": retained load refused " + JSON.stringify(report)); }
    const retained = report.retained, lost = report.retainLost, unknown = report.unknown;
    const size = codec.measureRetain(root, store);
    if (size < 0) { fail(f[1] + ": retaining measure refused"); }
    const out = codec.saveRetain(root, store, report);
    if (out === null) { fail(f[1] + ": retaining save refused " + JSON.stringify(report)); }
    const saveLost = report.retainLost - lost;
    // THE SAVE IS REPEATABLE: a second walk with a fresh report writes the same
    // bytes and answers the same save-side loss, exactly as the reference's pair
    // is held to (test/conformance/go/retain.go).
    const second = new codec.m.TableVariableReport();
    const again = codec.saveRetain(root, store, second);
    if (again === null || again.length !== out.length || second.retainLost !== saveLost ||
        again.some((b, i) => b !== out[i])) {
      fail(f[1] + ": save is not repeatable");
    }
    if (!saves) {
      writeFileSync(join(outDir, f[1]), retained + "," + lost + "," + unknown + " " + saveLost + "\n");
    } else {
      writeFileSync(join(outDir, f[1]), out);
    }
  }
}

// ---- main

const args = process.argv.slice(2);

// ---- the WIRE-FUZZ arm (docs/SPEC-TABLES.md §4.2, test/conformance/harness/
// wirefuzz.go). The harness owns the mutants and the oracle; this process only
// binds a roster entry to the generated Card codec and answers the report, the
// region measure and the saved bytes. The protocol is the twin of the Go and C
// legs (test/conformance/go/wire_fuzz.go, test/c-tables/wire_fuzz_main.c).
async function wireFuzz() {
  const readExact = (n) => {
    const buf = Buffer.alloc(n);
    let off = 0;
    while (off < n) {
      const r = readSync(0, buf, off, n - off, null);
      if (r === 0) { return null; }
      off += r;
    }
    return buf;
  };
  const readU32 = () => { const b = readExact(4); return b === null ? null : b.readUInt32LE(0); };
  const readU16 = () => { const b = readExact(2); return b === null ? null : b.readUInt16LE(0); };
  const readU8 = () => { const b = readExact(1); return b === null ? null : b[0]; };

  const count = readU32();
  if (count === null) { process.exit(0); }
  const roster = new Array(count).fill(null);
  const known = Buffer.alloc(count);
  for (let i = 0; i < count; i++) {
    const uLen = readU16();
    const unit = uLen === null ? "" : readExact(uLen).toString("utf8");
    const rLen = readU16();
    const root = rLen === null ? "" : readExact(rLen).toString("utf8");
    const form = readU8();
    const retain = readU8();
    known[i] = 0;
    if (form === 1) {
      // A --retain roster binds only a root whose retaining pair exists, as
      // the Go leg's findRetentionCodec does; it never falls back to Load.
      const codec = await wireFuzzCodec(unit, root);
      const retaining = codec !== null && typeof codec.loadRetain === "function" && typeof codec.saveRetain === "function";
      if (codec !== null && (retain !== 1 || retaining)) {
        roster[i] = { ...codec, retain: retain === 1 };
        known[i] = 1;
      }
    }
  }
  writeSync(1, known);

  for (;;) {
    const head = readExact(8);
    if (head === null) { break; }
    const index = head.readUInt32LE(0);
    const size = head.readUInt32LE(4);
    const wireBuf = readExact(size);
    if (wireBuf === null) { break; }
    const wire = new Uint8Array(wireBuf.buffer, wireBuf.byteOffset, wireBuf.byteLength);
    const codec = roster[index];

    let loaded = false, measure = -1, saved = null;
    const report = { reason: "", unknown: 0, kindMismatch: 0, widened: 0, clamped: 0, duplicate: 0, malformed: false, refused: 0, retained: 0, retainLost: 0 };
    if (codec !== null) {
      measure = codec.loadMeasure(wire);
      if (measure >= 0) {
        const rep = codec.report();
        let card = null;
        if (codec.retain) {
          const retainStore = codec.retainStore();
          card = codec.loadRetain(wire, retainStore, rep);
          loaded = card !== null;
          if (loaded) {
            saved = codec.saveRetain(card, retainStore, rep);
          }
        } else {
          card = codec.load(wire, rep);
          loaded = card !== null;
          if (loaded) { saved = codec.save(card); }
        }
        report.unknown = rep.unknown;
        report.kindMismatch = rep.kindMismatch;
        report.widened = rep.widened;
        report.clamped = rep.clamped;
        report.duplicate = rep.duplicate;
        report.malformed = rep.malformed;
        report.refused = rep.refused;
        report.retained = rep.retained;
        report.retainLost = rep.retainLost;
        report.reason = rep.reason;
      }
    }

    // EVERY REPLY IS FLUSHED BEFORE THE NEXT MUTANT IS READ
    // (test/conformance/README.md). The next read below blocks in readSync,
    // which never yields to the event loop, so an asynchronous
    // process.stdout.write could sit unflushed while this leg waits for the
    // next mutant — and the harness bounds in-flight mutants, so the two would
    // deadlock. Write synchronously so each reply is on the pipe before the
    // next blocking read.
    const header = Buffer.alloc(47);
    header[0] = loaded ? 1 : 0;
    header.writeInt32LE(report.unknown | 0, 1);
    header.writeInt32LE(report.kindMismatch | 0, 5);
    header.writeInt32LE(report.widened | 0, 9);
    header.writeInt32LE(report.clamped | 0, 13);
    header.writeInt32LE(report.duplicate | 0, 17);
    header[21] = report.malformed ? 1 : 0;
    header[22] = report.refused ? 1 : 0;
    header.writeBigInt64LE(BigInt(measure), 23);
    header.writeInt32LE(report.retained | 0, 31);
    header.writeInt32LE(report.retainLost | 0, 35);
    if (saved === null) {
      header.writeBigInt64LE(-1n, 39);
      writeSync(1, header);
    } else {
      header.writeBigInt64LE(BigInt(saved.length), 39);
      writeSync(1, header);
      writeSync(1, Buffer.from(saved));
    }
  }
  process.exit(0);
}

if (args.length < 2) {
  process.stderr.write("usage: main.mjs <manifest> list\n       main.mjs <manifest> <surface> <outdir>\n");
  process.exit(2);
}
const lines = readManifest(args[0]);
const surface = args[1];
if (surface === "list") {
  // the WIRE-CARRYING surfaces ride the id-table form (docs/SPEC-TABLES.md §3,
  // §3.1, §16.7) for the roots this leg's variable wire carries — graphdemo's
  // Scene and blobdemo's Catalog — and answer every other case ABSENT, one by
  // one. `message` answers a row at a time too — form 2 is this backend's
  // (§3.3). A row a surface cannot reach is `<case>.absent`, which the matrix
  // counts rather than reading as a feature the leg lacks. The retain surfaces
  // answer every row whose unit this leg generates, and name the reason beside
  // a row it cannot reach.
  process.stdout.write("wire\nreport\nretain\nretain-save\njson-read\njson-write\njson-hostile\ncook\ncook-write\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\ncook-reason\nblock-reason\nmessage\n");
  process.exit(0);
}
if (surface === "block-lead") {
  blockLead(lines);
  process.exit(0);
}
if (args.length < 3) {
  process.stderr.write("usage: main.mjs <manifest> <surface> <outdir>\n");
  process.exit(2);
}
const outDir = args[2];
switch (surface) {
  case "wire": await surfaceWire(lines, outDir); break;
  case "report": await surfaceReport(lines, outDir); break;
  case "retain": await surfaceRetain(lines, outDir, false); break;
  case "retain-save": await surfaceRetain(lines, outDir, true); break;
  case "json-read": await surfaceJsonRead(lines, outDir); break;
  case "json-write": await surfaceJsonWrite(lines, outDir); break;
  case "json-hostile": await surfaceJsonHostile(lines, outDir); break;
  case "message": await surfaceMessage(lines, outDir); break;
  case "cook": surfaceCook(lines, outDir); break;
  case "cook-write": await surfaceCookWrite(lines, outDir); break;
  case "cook-foreign": surfaceCookForeign(lines, outDir); break;
  case "block": surfaceBlock(lines, outDir); break;
  case "block-foreign": surfaceBlockForeign(lines, outDir); break;
  case "block-dump": surfaceBlockDump(lines, outDir); break;
  case "forgery": surfaceForgery(lines, outDir); break;
  case "cook-forgery": surfaceCookForgery(lines, outDir); break;
  case "cook-reason": surfaceReason("cook", lines, outDir); break;
  case "block-reason": surfaceReason("block", lines, outDir); break;
  default: process.exit(2);
}
