import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { WriteStream, ReadStream } from "../../../../serialize.js/src/index.js";
import * as p from "../../../build/packet-defaults/js/plain/Plain.js";
const dir = process.argv[2];
const base = process.argv[3] ? pathToFileURL(process.argv[3] + "/") : new URL("../../../build/packet-defaults/js/defaults/", import.meta.url);
const d = await import(new URL("Defaults.js", base));
const flat = await import(new URL("DefaultsFlat.js", base));
// The constant-default pins: both schemas declare package arithmetic, so each is
// generated into its own directory (build/packet-defaults/js/constant-<name>/).
const constantDir = new URL("../../../build/packet-defaults/js/", import.meta.url);
const constants = {};
for (const name of ["hexadecimal-addition", "unsigned-subtraction"]) {
  constants[name] = {
    runtime: await import(new URL(`constant-${name}/Defaults.js`, constantDir)),
    flat: await import(new URL(`constant-${name}/DefaultsFlat.js`, constantDir)),
  };
}
const pinsRead = new Set();
const zero = () => Object.assign(new d.Sample(), {
  name: new Uint8Array(6), nameLength: 0, token: new Uint8Array(4), tokenLength: 0, caps: 0n,
  emptyName: new Uint8Array(3), emptyNameLength: 0, emptyToken: new Uint8Array(2), emptyTokenLength: 0, emptyCaps: 0n,
});
const sample = () => Object.assign(zero(), { name: Uint8Array.from([195,169,240,144,128,128]), nameLength: 6,
  token: Uint8Array.from([92,110,92,116]), tokenLength: 4, caps: 5n });
const short = () => Object.assign(zero(), { name: Uint8Array.from([65,0,0,0,0,0]), nameLength: 1,
  token: Uint8Array.from([0,255,0,0]), tokenLength: 2, caps: 2n });
const dirty = () => Object.assign(zero(), { name: new Uint8Array(6).fill(100), nameLength: 6,
  token: new Uint8Array(4).fill(161), tokenLength: 4, caps: 7n, emptyName: new Uint8Array(3).fill(111),
  emptyNameLength: 3, emptyToken: new Uint8Array(2).fill(177), emptyTokenLength: 2, emptyCaps: 7n });
function overlay(initial, sent) {
  initial.name.set(sent.name.subarray(0, sent.nameLength)); initial.nameLength = sent.nameLength;
  initial.token.set(sent.token.subarray(0, sent.tokenLength)); initial.tokenLength = sent.tokenLength;
  initial.caps = sent.caps; initial.emptyNameLength = 0; initial.emptyTokenLength = 0; initial.emptyCaps = 0n;
  return initial;
}
function write(mod, type, value) {
  const stream = new WriteStream(new Uint8Array(4096));
  assert(mod["Write" + type](stream, value), "runtime write");
  const bits = stream.bitsProcessed(); stream.flush();
  return { bytes: stream.data().slice(), bits };
}
function flatValue(type, value) {
  return type === "Choice" ? Object.assign(new d.ChoiceEnvelope(), { choice: value }) : value;
}
function flatType(type) { return type === "Choice" ? "ChoiceEnvelope" : type; }
function checkRead(tier, type, bytes, bits, initial, want) {
  if (tier === "runtime") {
    const stream = new ReadStream(bytes);
    assert(d["Read" + type](stream, initial), "runtime read");
    // the accepted end consumes the final byte's zero padding
    assert.equal(stream.bitsProcessed(), (bits + 7) & ~7, "read consumed bits");
  } else {
    const buffer = new Uint8Array(bytes.length + 8); buffer.set(bytes);
    assert(flat["Read" + flatType(type) + "Flat"](new DataView(buffer.buffer), bits, flatValue(type, initial)), "flat exact-bit read");
    assert.equal(flat["Read" + flatType(type) + "Flat"](new DataView(buffer.buffer), bits - 1, flatValue(type, new d[type]())), false, "flat one-bit-short refusal");
  }
  assert.deepEqual(initial, want, `${tier} ${type}: values and backing storage`);
}
function pin(type, name, value, initial, want) {
  pinsRead.add(name);
  const bytes = new Uint8Array(readFileSync(`${dir}/${name}.bin`));
  const bits = Number(readFileSync(`${dir}/${name}.bits`, "utf8").trim());
  assert.deepEqual(write(d, type, value), { bytes, bits }, `C++ pin ${name}`);
  const buffer = new Uint8Array(4096);
  const size = flat["Write" + flatType(type) + "Flat"](new DataView(buffer.buffer), flatValue(type, value));
  assert.deepEqual(buffer.slice(0, size), bytes, `flat C++ pin ${name}`);
  for (const tier of ["runtime", "flat"]) checkRead(tier, type, bytes, bits, initial(), want());
}

// A fresh Value (defaults from constants) writes the pinned bytes and bit count
// in both tiers, and the pin reads back over zeroed storage to a fresh Value.
function constantPin(name) {
  const { runtime, flat: flatMod } = constants[name];
  pinsRead.add(name);
  const bytes = new Uint8Array(readFileSync(`${dir}/${name}.bin`));
  const bits = Number(readFileSync(`${dir}/${name}.bits`, "utf8").trim());
  const fresh = () => new runtime.Value();
  assert.deepEqual(write(runtime, "Value", fresh()), { bytes, bits }, `C++ pin ${name}`);
  const buffer = new Uint8Array(4096);
  const size = flatMod.WriteValueFlat(new DataView(buffer.buffer), fresh());
  assert.deepEqual(buffer.slice(0, size), bytes, `flat C++ pin ${name}`);
  const initial = fresh(); runtime.ZeroValue(initial);
  const stream = new ReadStream(bytes);
  assert(runtime.ReadValue(stream, initial), `runtime read ${name}`);
  assert.equal(stream.bitsProcessed(), (bits + 7) & ~7, `${name} read consumed bits`);
  assert.deepEqual(initial, fresh(), `runtime ${name}: reads back to a fresh Value`);
  const padded = new Uint8Array(bytes.length + 8); padded.set(bytes);
  const flatInitial = fresh(); runtime.ZeroValue(flatInitial);
  assert(flatMod.ReadValueFlat(new DataView(padded.buffer), bits, flatInitial), `flat exact-bit read ${name}`);
  assert.deepEqual(flatInitial, fresh(), `flat ${name}: reads back to a fresh Value`);
  const short = fresh(); runtime.ZeroValue(short);
  assert.equal(flatMod.ReadValueFlat(new DataView(padded.buffer), bits - 1, short), false, `flat one-bit-short refusal ${name}`);
}

assert.deepEqual(new d.Sample(), sample(), "packet-default constructor bytes");
const reused = dirty(), nameBuffer = reused.name, tokenBuffer = reused.token;
d.InitSample(reused); assert.deepEqual(reused, sample());
assert.equal(reused.name, nameBuffer); assert.equal(reused.token, tokenBuffer);
d.ZeroSample(reused); assert.deepEqual(reused, zero());
const empty = new d.EmptyOnly();
assert.deepEqual([empty.nameLength, empty.tokenLength, empty.caps, ...empty.name, ...empty.token], [0,0,0n,0,0,0]);
const prefix = new d.Prefix();
assert.deepEqual([...prefix.name, prefix.nameLength, ...prefix.token, prefix.tokenLength], [195,169,0,0,0,2,92,110,0,0,0,2]);
prefix.name.fill(255); prefix.token.fill(255); d.InitPrefix(prefix); assert.deepEqual(prefix, new d.Prefix());
const wide = new d.WideMask(); assert.equal(wide.high, 1n << 63n); assert.equal(wide.all, (1n << 64n) - 1n);
const wideBytes = new Uint8Array(16).fill(255, 8); wideBytes[7] = 128;
assert.deepEqual(write(d, "WideMask", wide), { bytes: wideBytes, bits: 128 });
const split = Object.assign(new d.SplitMask(), { lead: 5, mask: 1n << 32n, tail: 2 });
assert.deepEqual(write(d, "SplitMask", split), { bytes: Uint8Array.from([5,0,0,0,40]), bits: 38 });
for (const tier of ["runtime", "flat"]) {
  checkRead(tier, "WideMask", wideBytes, 128, Object.assign(new d.WideMask(), { high: 0n, all: 0n }), wide);
  checkRead(tier, "SplitMask", Uint8Array.from([5,0,0,0,40]), 38, new d.SplitMask(), split);
}
const plain = Object.assign(new p.Sample(), sample());
assert.deepEqual(write(p, "Sample", plain), write(d, "Sample", new d.Sample()), "defaultless twin");
pin("Sample", "sample-defaults", new d.Sample(), zero, sample);
const batch = new d.Batch(); assert.equal(batch.countedCount, 1); assert.deepEqual(batch.head, sample());
for (const s of [...batch.items, ...batch.counted]) assert.deepEqual(s, sample());
function reusedBatch() { const b = new d.Batch(); b.counted[1] = dirty(); b.counted[2] = short(); return b; }
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch);
const z = new d.ZeroCount(); assert.equal(z.itemsCount, 0); assert.deepEqual(z.items, [sample(), sample()]);
pin("ZeroCount", "zero-count", z, () => Object.assign(new d.ZeroCount(), { items: [dirty(),short()], itemsCount: 2 }),
  () => Object.assign(new d.ZeroCount(), { items: [dirty(),short()], itemsCount: 0 }));
assert.equal(new d.Conditional().enabled, true); assert.deepEqual(new d.Conditional().value, sample());
pin("Conditional", "conditional-on", new d.Conditional(), () => Object.assign(new d.Conditional(), { enabled: false, value: zero() }), () => new d.Conditional());
pin("Conditional", "conditional-off", Object.assign(new d.Conditional(), { enabled: false }),
  () => Object.assign(new d.Conditional(), { value: dirty() }), () => Object.assign(new d.Conditional(), { enabled: false, value: zero() }));
const choice = () => Object.assign(new d.Choice(), { type: d.ChoiceType.Sample });
pin("Choice", "choice-sample", choice(), () => Object.assign(new d.Choice(), { type: d.ChoiceType.Conditional }), choice);
pin("Sample", "sample-short", short(), dirty, () => overlay(dirty(), short()));
pin("Sample", "sample-empty", zero(), dirty, () => overlay(dirty(), zero()));
for (const sent of [short(), zero()]) {
  const value = choice(); value.sample = sent; const wire = write(d, "Choice", value);
  for (const tier of ["runtime", "flat"]) {
    const output = Object.assign(new d.Choice(), { type: d.ChoiceType.Conditional });
    const arm = output.sample, name = arm.name, token = arm.token;
    for (let attempt = 0; attempt < 2; attempt++) {
      arm.name.fill(100); arm.token.fill(161); arm.emptyName.fill(111); arm.emptyToken.fill(177);
      const want = choice(); want.sample = overlay(sample(), sent);
      checkRead(tier, "Choice", wire.bytes, wire.bits, output, want);
      assert.equal(output.sample, arm); assert.equal(arm.name, name); assert.equal(arm.token, token);
    }
  }
}
const offChoice = Object.assign(new d.Choice(), { type: d.ChoiceType.Conditional }); offChoice.conditional.enabled = false;
const offWire = write(d, "Choice", offChoice);
for (const tier of ["runtime", "flat"]) {
  const output = choice();
  for (let attempt = 0; attempt < 2; attempt++) {
    output.conditional.enabled = true; output.conditional.value = dirty();
    const want = Object.assign(new d.Choice(), { type: d.ChoiceType.Conditional });
    want.conditional.enabled = false; want.conditional.value = zero();
    checkRead(tier, "Choice", offWire.bytes, offWire.bits, output, want);
  }
}
constantPin("hexadecimal-addition");
constantPin("unsigned-subtraction");
// Every .bin of the directory must have been read: a pin this driver does not
// know fails loudly, never skipped.
const bins = readdirSync(dir).filter((f) => f.endsWith(".bin")).map((f) => f.slice(0, -4));
assert(bins.length > 0, "the pins directory is not empty");
for (const name of bins) assert(pinsRead.has(name), `pin ${name} was never read by the driver`);
console.log("packet defaults JavaScript: constructors, ten C++ goldens, both tiers and reused storage OK");
