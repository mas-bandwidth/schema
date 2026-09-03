// THE JAVASCRIPT CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and test/conformance/cs's Program,
// and it is deliberately the same shape: one process per surface, every
// expectation in the data, nothing literal here. It answers every surface this
// backend has — the tolerant wire, the read report, the text form both ways,
// the hostile text corpus, the block form's open and its row dump, the cook's
// node dump, and both forgery batteries.
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

import { readFileSync, writeFileSync } from "node:fs";
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

const tabledemoTables = await load("examples/TablesTable.js");
const tabledemoNested = await load("examples/NestedTable.js");
const tabledemoKeyed = await load("examples/KeyedTable.js");
const tabledemoWide = await load("examples/WideTable.js");
const tabledemoPack = await load("examples/PackTable.js");
const tblv1 = await load("v1/V1Table.js");
const tblv2 = await load("v2/V2Table.js");
const tblp1 = await load("p1/P1Table.js");
const tblp3 = await load("p3/P3Table.js");
const renderBlock = await load("block/RenderBlock.js");
const paddedBlock = await load("block/PaddedBlock.js");
const graphCook = await load("pointers/GraphCook.js");

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

// absent says this backend cannot answer THIS CASE — a feature it lacks, not a
// test it failed. The harness counts it and the matrix prints it beside what
// the leg did answer (test/conformance/README.md).
function absent(outDir, name) {
  writeFileSync(join(outDir, name + ".absent"), new Uint8Array(0));
}

// noText marks an instance the corpus carries on the WIRE only: the variable
// class has no text form yet (docs/SPEC-TABLES.md §16.2), so the TEXT surfaces
// skip it rather than reporting a form nobody has.
function noText(f) {
  return f.length > 5 && f[5] === "no-text";
}

// ---- the codec table: one row per (unit, root) the corpus names

function row(unit, root, m, name) {
  return {
    unit, root,
    load: (bytes, report) => {
      const value = new m[name]();
      const ok = m[name + "Load"](value, bytes, report);
      return ok ? value : null;
    },
    measure: (v) => m[name + "Measure"](v),
    save: (v, buffer) => m[name + "Save"](v, buffer),
    fromJson: (text, report) => {
      const value = new m[name]();
      const ok = m[name + "FromJson"](value, text, report);
      return ok ? value : null;
    },
    toJsonMeasure: (v) => m[name + "ToJsonMeasure"](v),
    toJson: (v, buffer) => m[name + "ToJson"](v, buffer),
    Report: () => new m.TableReport(),
  };
}

const codecs = [
  row("tabledemo", "RootConfig", tabledemoTables, "RootConfig"),
  row("tabledemo", "ProfileConfig", tabledemoTables, "ProfileConfig"),
  row("tabledemo", "LoadoutConfig", tabledemoTables, "LoadoutConfig"),
  row("tabledemo", "WideBlob", tabledemoWide, "WideBlob"),
  row("tabledemo", "ArchiveConfig", tabledemoNested, "ArchiveConfig"),
  row("tabledemo", "KeyedConfig", tabledemoKeyed, "KeyedConfig"),
  row("tabledemo", "PackConfig", tabledemoPack, "PackConfig"),
  row("tblv1", "Cfg", tblv1, "Cfg"),
  row("tblv2", "Cfg", tblv2, "Cfg"),
  row("tblp1", "Chain", tblp1, "Chain"),
  row("tblp3", "Chain", tblp3, "Chain"),
];

function find(unit, root) {
  for (const c of codecs) {
    if (c.unit === unit && c.root === root) { return c; }
  }
  return null;
}

function counters(report) {
  return report.Unknown + "," + report.KindMismatch + "," + report.Clamped + "," +
    report.Duplicate + "," + (report.Malformed ? "true" : "false") + "\n";
}

// ---- the surfaces

function surfaceWire(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    const codec = find(f[2], f[3]);
    if (codec === null) {
      // no codec for this unit's root: the JS backend refuses a pointered
      // unit's wire by name (§11), which is a missing FEATURE
      absent(outDir, f[1]);
      continue;
    }
    const wire = new Uint8Array(readFileSync(f[4]));
    const report = codec.Report();
    const value = codec.load(wire, report);
    if (value === null) { fail(f[1] + " does not load"); }
    const size = codec.measure(value);
    const buffer = new Uint8Array(size);
    if (codec.save(value, buffer) !== size) { fail(f[1] + " saves a size its measure did not name"); }
    writeFileSync(join(outDir, f[1]), buffer);
  }
}

// json-read: the text is the input and the WIRE is the answer, so the pass
// proves the reader against bytes this driver did not write.
function surfaceJsonRead(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    if (noText(f)) { continue; }
    const codec = find(f[2], f[3]);
    if (codec === null) {
      absent(outDir, f[1]);
      continue;
    }
    const text = new Uint8Array(readFileSync(join("testdata", "conformance", "tables", "json", f[1] + ".json")));
    const report = codec.Report();
    const value = codec.fromJson(text, report);
    if (value === null) { fail(f[1] + " does not read as JSON"); }
    const size = codec.measure(value);
    if (size < 0) { fail(f[1] + " measures as unsaveable after a clean read"); }
    const buffer = new Uint8Array(size);
    if (codec.save(value, buffer) !== size) { fail(f[1] + " saves a size its measure did not name"); }
    writeFileSync(join(outDir, f[1]), buffer);
  }
}

// json-write: the wire is the input and the TEXT is the answer, compared
// against a text a third implementation wrote.
function surfaceJsonWrite(lines, outDir) {
  for (const f of kind(lines, "instance")) {
    if (noText(f)) { continue; }
    const codec = find(f[2], f[3]);
    if (codec === null) {
      absent(outDir, f[1]);
      continue;
    }
    const wire = new Uint8Array(readFileSync(f[4]));
    const report = codec.Report();
    const value = codec.load(wire, report);
    if (value === null) { fail(f[1] + " does not load"); }
    const size = codec.toJsonMeasure(value);
    if (size < 0) { fail(f[1] + " holds a value ToJson refuses"); }
    const text = new Uint8Array(size);
    if (codec.toJson(value, text) !== size) { fail(f[1] + " writes a text its measure did not name"); }
    writeFileSync(join(outDir, f[1] + ".json"), text);
  }
}

// json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
function surfaceJsonHostile(lines, outDir) {
  for (const f of kind(lines, "json-hostile")) {
    const codec = find(f[2], f[3]);
    if (codec === null) { fail("no codec for " + f[2] + "." + f[3]); }
    // the tree is what `schema pack` reads, so the text is <tree>/<root>.Json
    const text = new Uint8Array(readFileSync(join(f[4], f[3] + ".json")));
    const report = codec.Report();
    const value = codec.fromJson(text, report);
    const verdict = value === null || report.Malformed
      ? "refused\n"
      : report.Unknown + "," + report.KindMismatch + "," + report.Clamped + "," + report.Duplicate + ",false\n";
    writeFileSync(join(outDir, f[1]), verdict);
  }
}

function surfaceReport(lines, outDir) {
  for (const f of kind(lines, "report")) {
    const codec = find(f[2], f[3]);
    if (codec === null) {
      absent(outDir, f[1]);
      continue;
    }
    const wire = new Uint8Array(readFileSync(f[4]));
    const report = codec.Report();
    const value = codec.load(wire, report);
    if (value === null) { report.Malformed = true; }
    writeFileSync(join(outDir, f[1]), counters(report));
  }
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

const blocks = {
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
function openBlock(name, source, extent) {
  const claim = extent < 0 || extent < source.length ? source.length : extent;
  return blockNamed(name).Open(place(source, claim, 0)) !== null ? "open\n" : "refuse\n";
}

function surfaceBlock(lines, outDir) {
  for (const f of kind(lines, "block")) {
    writeFileSync(join(outDir, f[1]), openBlock(f[1], new Uint8Array(readFileSync(f[3])), -1));
  }
}

function surfaceForgery(lines, outDir) {
  for (const f of kind(lines, "forgery")) {
    if (f[2] !== "block") { continue; } // the cook's battery is its own
    writeFileSync(join(outDir, f[1]), openBlock(f[3], new Uint8Array(readFileSync(f[4])), Number(f[5])));
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
    writeFileSync(join(outDir, f[1]), openBlock(f[1], foreign(new Uint8Array(readFileSync(f[3]))), -1));
  }
}

function surfaceCookForeign(lines, outDir) {
  for (const f of kind(lines, "cook")) {
    const source = foreign(new Uint8Array(readFileSync(f[4])));
    const opened = cookNamed(f[3]).Open(place(source, source.length, 0));
    writeFileSync(join(outDir, f[1]), opened !== null ? "open\n" : "refuse\n");
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

const cooks = {
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
    const opened = cookNamed(f[3]).Open(bytes);
    writeFileSync(join(outDir, f[1]), opened !== null ? "open\n" : "refuse\n");
  }
}

// ---- main

const args = process.argv.slice(2);
if (args.length < 2) {
  process.stderr.write("usage: main.mjs <manifest> list\n       main.mjs <manifest> <surface> <outdir>\n");
  process.exit(2);
}
const lines = readManifest(args[0]);
const surface = args[1];
if (surface === "list") {
  process.stdout.write("wire\nreport\njson-read\njson-write\njson-hostile\ncook\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\n");
  process.exit(0);
}
if (args.length < 3) {
  process.stderr.write("usage: main.mjs <manifest> <surface> <outdir>\n");
  process.exit(2);
}
const outDir = args[2];
switch (surface) {
  case "wire": surfaceWire(lines, outDir); break;
  case "report": surfaceReport(lines, outDir); break;
  case "json-read": surfaceJsonRead(lines, outDir); break;
  case "json-write": surfaceJsonWrite(lines, outDir); break;
  case "json-hostile": surfaceJsonHostile(lines, outDir); break;
  case "cook": surfaceCook(lines, outDir); break;
  case "cook-foreign": surfaceCookForeign(lines, outDir); break;
  case "block": surfaceBlock(lines, outDir); break;
  case "block-foreign": surfaceBlockForeign(lines, outDir); break;
  case "block-dump": surfaceBlockDump(lines, outDir); break;
  case "forgery": surfaceForgery(lines, outDir); break;
  case "cook-forgery": surfaceCookForgery(lines, outDir); break;
  default: process.exit(2);
}
