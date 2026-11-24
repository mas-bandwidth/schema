// The JavaScript leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. Both generated tiers answer each case: the
// runtime tier (Void.js over serialize.js streams) and the flat tier
// (VoidFlat.js over a DataView). The flat tier codes every union at the root
// (WriteMixedFlat, ReadMixedFlat, ...), so no envelope type is needed. The
// pins are the Go oracle's bytes, never this writer's.
//
//   node main.mjs <testdata/wire/packet-void> [<generated dir>]
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";

const dir = process.argv[2];
if (!dir) {
  console.error("usage: node main.mjs <testdata/wire/packet-void> [<generated dir>]");
  process.exit(2);
}
const generated = process.argv[3]
  ? pathToFileURL(process.argv[3] + "/")
  : new URL("../../../build/packet-void/js/", import.meta.url);
const v = await import(new URL("Void.js", generated));
const flat = await import(new URL("VoidFlat.js", generated));

class CaseFailed extends Error {}
function fail(why) {
  throw new CaseFailed(why);
}

const TYPES = new Set(["Mixed", "Tags", "Single", "Empty", "Frame", "Batch"]);

// The payload arm is the type's construction form: entries empty, marker 5.
function mixed(type) {
  const m = new v.Mixed();
  m.type = type;
  if (type === v.MixedType.Payload) {
    m.payload = new v.DefaultArm();
  }
  return m;
}

// The value of each accept case, by name; undefined for a name this driver
// does not know, which is FAILED and never written with another value.
function build(name) {
  const tagged = (Type, type) => Object.assign(new v[Type](), { type });
  switch (name) {
    case "mixed-none": return { type: "Mixed", value: mixed(v.MixedType.None) };
    case "mixed-ping": return { type: "Mixed", value: mixed(v.MixedType.Ping) };
    case "mixed-payload": return { type: "Mixed", value: mixed(v.MixedType.Payload) };
    case "tags-none": return { type: "Tags", value: tagged("Tags", v.TagsType.None) };
    case "tags-ping": return { type: "Tags", value: tagged("Tags", v.TagsType.Ping) };
    case "tags-pong": return { type: "Tags", value: tagged("Tags", v.TagsType.Pong) };
    case "single-none": return { type: "Single", value: tagged("Single", v.SingleType.None) };
    case "single-ack": return { type: "Single", value: tagged("Single", v.SingleType.Ack) };
    case "empty-none": return { type: "Empty", value: tagged("Empty", v.EmptyType.None) };
    case "frame-ping": {
      const f = new v.Frame();
      f.lead = 5; f.choice = mixed(v.MixedType.Ping); f.tail = 6;
      return { type: "Frame", value: f };
    }
    case "batch-empty": return { type: "Batch", value: new v.Batch() };
    case "batch-ping": {
      const b = new v.Batch();
      b.valuesCount = 1; b.values[0] = mixed(v.MixedType.Ping);
      return { type: "Batch", value: b };
    }
    case "batch-ping-payload": {
      const b = new v.Batch();
      b.valuesCount = 2; b.values[0] = mixed(v.MixedType.Ping); b.values[1] = mixed(v.MixedType.Payload);
      return { type: "Batch", value: b };
    }
  }
  return undefined;
}

// A payload whose every field differs from its construction form.
function dirtyMixed() {
  const m = new v.Mixed();
  m.type = v.MixedType.Payload;
  m.payload.entriesCount = 2;
  m.payload.marker = 2;
  for (const e of m.payload.entries) e.retries = 99;
  return m;
}

// Every read lands on storage dirtied first: a selected payload must be
// constructed with its declared defaults, whatever the storage held.
function dirty(type) {
  switch (type) {
    case "Mixed": return dirtyMixed();
    case "Tags": return Object.assign(new v.Tags(), { type: v.TagsType.Pong });
    case "Single": return Object.assign(new v.Single(), { type: v.SingleType.Ack });
    case "Empty": return new v.Empty();
    case "Frame": return Object.assign(new v.Frame(), { lead: 7, choice: dirtyMixed(), tail: 7 });
    case "Batch": return Object.assign(new v.Batch(), { valuesCount: 2, values: [dirtyMixed(), dirtyMixed()] });
  }
  return undefined;
}

function mixedEqual(got, want) {
  if (got.type !== want.type) return false;
  if (want.type !== v.MixedType.Payload) return true;
  if (got.payload.entriesCount !== want.payload.entriesCount || got.payload.marker !== want.payload.marker) return false;
  // every backing entry, used or not, holds what the payload's construction
  // gave it: a read constructs the arm before decoding
  for (let i = 0; i < 2; i++) {
    if (got.payload.entries[i].retries !== want.payload.entries[i].retries) return false;
  }
  return true;
}

function equal(type, got, want) {
  switch (type) {
    case "Mixed": return mixedEqual(got, want);
    case "Tags": case "Single": case "Empty": return got.type === want.type;
    case "Frame": return got.lead === want.lead && got.tail === want.tail && mixedEqual(got.choice, want.choice);
    case "Batch":
      if (got.valuesCount !== want.valuesCount) return false;
      for (let i = 0; i < want.valuesCount; i++) {
        if (!mixedEqual(got.values[i], want.values[i])) return false;
      }
      return true;
  }
  return false;
}

const hex = (b) => (b.length === 0 ? "(none)" : Buffer.from(b).toString("hex"));
const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

function runtimeWrite(type, value) {
  const stream = new WriteStream(new Uint8Array(64));
  if (!v["Write" + type](stream, value)) fail("runtime write refused");
  const bits = stream.bitsProcessed();
  stream.flush();
  return { bytes: stream.data().slice(), bits };
}

// The flat writer reports bytes, not bits; its bits are the flat
// exact-bit read's count (ReadXFlatBits), checked on the read side.
function flatWrite(type, value) {
  const buffer = new Uint8Array(64);
  const n = flat["Write" + type + "Flat"](new DataView(buffer.buffer), value);
  if (n < 0) fail("flat write refused");
  return buffer.slice(0, n);
}

// The flat reads take a buffer with FlatReadSlack bytes past the packet; the
// logical length stays exact.
function flatView(pin) {
  const buffer = new Uint8Array(pin.length + flat.FlatReadSlack);
  buffer.set(pin);
  return new DataView(buffer.buffer);
}

function accept(name, type, bits) {
  const built = build(name);
  if (built === undefined) fail("unknown accept case");
  if (built.type !== type) fail(`case type ${type}, the driver builds ${built.type}`);
  const want = built.value;
  const pin = new Uint8Array(readFileSync(join(dir, name + ".bin")));

  // runtime tier
  const wrote = runtimeWrite(type, want);
  if (wrote.bits !== bits) fail(`runtime write has ${wrote.bits} bits, want ${bits}`);
  if (!same(wrote.bytes, pin)) fail(`runtime write bytes ${hex(wrote.bytes)} differ from the pin ${hex(pin)}`);
  let got = dirty(type);
  const stream = new ReadStream(pin);
  if (!v["Read" + type](stream, got)) fail("runtime read refused");
  // the accepted end consumes the final byte's zero padding
  if (stream.bitsProcessed() !== pin.length * 8) fail(`runtime read has ${stream.bitsProcessed()} bits, want ${pin.length * 8}`);
  if (!equal(type, got, want)) fail("runtime read value differs");
  const again = runtimeWrite(type, got);
  if (again.bits !== bits || !same(again.bytes, pin)) fail("the runtime read value writes other bytes than the pin");

  // flat tier
  const flatBytes = flatWrite(type, want);
  if (!same(flatBytes, pin)) fail(`flat write bytes ${hex(flatBytes)} differ from the pin ${hex(pin)}`);
  got = dirty(type);
  const read = flat["Read" + type + "FlatBits"](flatView(pin), pin.length * 8, got);
  if (read !== bits) fail(`flat read has ${read} bits, want ${bits}`);
  if (!equal(type, got, want)) fail("flat read value differs");
  got = dirty(type);
  if (!flat["Read" + type + "Flat"](flatView(pin), pin.length * 8, got)) fail("flat one-packet read refused");
  if (!equal(type, got, want)) fail("flat one-packet read value differs");
  if (!same(flatWrite(type, got), pin)) fail("the flat read value writes other bytes than the pin");
}

function refuse(name, type) {
  if (!TYPES.has(type)) fail(`unknown type ${type}`);
  const pin = new Uint8Array(readFileSync(join(dir, name + ".bin")));
  if (v["Read" + type](new ReadStream(pin), dirty(type))) fail("runtime read accepted");
  if (flat["Read" + type + "Flat"](flatView(pin), pin.length * 8, dirty(type))) fail("flat read accepted");
}

// The generated surface beside the wire: tag extents, debug names and maximum
// sizes. A miss exits non-zero, which tools/casegate holds red.
if (v.MixedTypeMax !== 2 || v.TagsTypeMax !== 2 || v.SingleTypeMax !== 1 || v.EmptyTypeMax !== 0 ||
    v.EnumNameMixedType(v.MixedType.Ping) !== "Ping" || v.EnumNameTagsType(v.TagsType.Pong) !== "Pong" ||
    v.MixedMaxBits !== 23 || v.TagsMaxBits !== 2 || v.SingleMaxBits !== 1 || v.EmptyMaxBits !== 0) {
  console.log("FAILED: packet-void tag extents, debug names or maximum bits");
  process.exit(1);
}

let lines;
try {
  lines = readFileSync(join(dir, "cases.txt"), "utf8").split("\n");
} catch (e) {
  console.error(`cannot read cases.txt: ${e.message}`);
  process.exit(2);
}
for (const line of lines) {
  const f = line.trim().split(/\s+/);
  if (f[0] === "" || f[0].startsWith("#")) continue;
  const name = f[0];
  try {
    if (f.length < 4) fail("a case line needs <name> <type> <verdict> <bits>");
    if (f[2] === "accept") accept(name, f[1], Number(f[3]));
    else if (f[2] === "refuse") refuse(name, f[1]);
    else fail(`verdict ${f[2]}`);
    console.log(`case ${name} ok`);
  } catch (e) {
    const why = e instanceof CaseFailed ? e.message : `${e.name}: ${e.message}`;
    console.log(`case ${name} FAILED: ${why.replace(/\n/g, " ")}`);
  }
}
